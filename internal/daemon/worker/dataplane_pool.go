// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/opendumpling/opendumpling/internal/daemon/cluster"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/observability/event"
)

const (
	// dataPlanePoolWaitTimeout bounds how long a session waits for a free
	// data plane connection towards its egress worker before giving up.  The
	// connections are established ahead of time by the downstream worker, so
	// this only ever elapses when every one of them carries a session.
	dataPlanePoolWaitTimeout = 30 * time.Second

	// dataPlaneAcceptorErrorBackoff is how long the data plane acceptor waits
	// before retrying after an unexpected accept error.
	dataPlaneAcceptorErrorBackoff = 500 * time.Millisecond
)

// dataPlanePool holds the idle data plane connections a set of downstream
// workers established towards this worker, keyed by the downstream worker's
// public id.
//
// A connection is taken from the pool for exactly one session and is never
// returned to it: the downstream worker closes it and dials a replacement,
// which keeps the accounting on both sides of the link trivially consistent.
type dataPlanePool struct {
	mu sync.Mutex
	// idle maps a downstream worker id to its connections which do not carry
	// a session yet.
	idle map[string][]net.Conn
	// notify is signalled whenever a connection is added or the pool closes.
	notify chan struct{}
	closed bool
}

func newDataPlanePool() *dataPlanePool {
	return &dataPlanePool{
		idle:   make(map[string][]net.Conn),
		notify: make(chan struct{}, 1),
	}
}

func (p *dataPlanePool) signal() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

// add makes a connection established by the given downstream worker available
// to future sessions.
func (p *dataPlanePool) add(ctx context.Context, workerId string, conn net.Conn) error {
	const op = "worker.(dataPlanePool).add"
	switch {
	case conn == nil:
		return errors.New(ctx, errors.Internal, op, "data plane connection is nil")
	case workerId == "":
		return errors.New(ctx, errors.Internal, op, "downstream worker id is empty")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New(ctx, errors.Internal, op, "data plane pool is closed")
	}
	p.idle[workerId] = append(p.idle[workerId], conn)
	p.signal()
	return nil
}

// take hands out an idle connection towards the given downstream worker,
// waiting up to dataPlanePoolWaitTimeout for one to become available.
func (p *dataPlanePool) take(ctx context.Context, workerId string) (net.Conn, error) {
	return p.takeWithin(ctx, workerId, dataPlanePoolWaitTimeout)
}

// takeWithin is take with an explicit upper bound on how long it waits for a
// connection to become available.
func (p *dataPlanePool) takeWithin(ctx context.Context, workerId string, wait time.Duration) (net.Conn, error) {
	const op = "worker.(dataPlanePool).take"

	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	for {
		p.mu.Lock()
		switch {
		case p.closed:
			p.mu.Unlock()
			return nil, errors.New(ctx, errors.Internal, op, "data plane pool is closed")
		}
		if conns := p.idle[workerId]; len(conns) > 0 {
			conn := conns[0]
			if len(conns) == 1 {
				delete(p.idle, workerId)
			} else {
				p.idle[workerId] = conns[1:]
			}
			p.mu.Unlock()
			return conn, nil
		}
		notify := p.notify
		p.mu.Unlock()

		select {
		case <-notify:
		case <-waitCtx.Done():
			return nil, errors.New(ctx, errors.WorkerConnNotFound, op,
				fmt.Sprintf("no data plane connection to worker %q became available within %s", workerId, wait))
		}
	}
}

// close closes every idle connection and makes the pool refuse new ones.
func (p *dataPlanePool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for workerId, conns := range p.idle {
		for _, conn := range conns {
			conn.Close()
		}
		delete(p.idle, workerId)
	}
	p.signal()
}

// dataPlaneReceiver is the reverseConnReceiver of a worker which other workers
// connect to.  It accepts the data plane connections of its downstream
// workers and makes them available to the sessions this worker proxies.
//
// It is created for every worker because only the controller knows which
// workers end up in front of which; a worker without downstream workers simply
// never accepts a connection and its pool stays empty.
type dataPlaneReceiver struct {
	pool *dataPlanePool
}

var _ reverseConnReceiver = (*dataPlaneReceiver)(nil)

func newDataPlaneReceiver() reverseConnReceiver {
	return &dataPlaneReceiver{pool: newDataPlanePool()}
}

// StartProcessingPendingConnections satisfies the reverseConnReceiver
// interface.  The actual work happens in ServeDataPlaneListener, which is
// handed the listener by serveSecondaryConnections; without downstream
// workers there is nothing to process, so this simply blocks until the worker
// shuts down.
func (r *dataPlaneReceiver) StartProcessingPendingConnections(ctx context.Context, _ func() string) error {
	<-ctx.Done()
	r.pool.close()
	return nil
}

// StartConnectionMgmtTicking satisfies the reverseConnReceiver interface.  The
// pool needs no periodic maintenance: connections are added by the acceptor
// and removed by the sessions using them.
func (r *dataPlaneReceiver) StartConnectionMgmtTicking(ctx context.Context, _ func() string, _ int) error {
	<-ctx.Done()
	return nil
}

// ServeDataPlaneListener accepts the data plane connections of the downstream
// workers and puts them in the pool.  It only returns when the listener or the
// context is closed.
func (r *dataPlaneReceiver) ServeDataPlaneListener(ctx context.Context, l net.Listener) error {
	const op = "worker.(dataPlaneReceiver).ServeDataPlaneListener"

	if l == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "data plane listener is nil")
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			event.WriteError(ctx, op, err, event.WithInfoMsg("error accepting downstream data plane connection"))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(dataPlaneAcceptorErrorBackoff):
			}
			continue
		}
		workerId, err := dataPlaneConnWorkerId(ctx, conn)
		if err != nil {
			event.WriteError(ctx, op, err, event.WithInfoMsg("unable to determine the worker of an incoming data plane connection"))
			conn.Close()
			continue
		}
		if err := r.pool.add(ctx, workerId, conn); err != nil {
			event.WriteError(ctx, op, err, event.WithInfoMsg("unable to track an incoming data plane connection"))
			conn.Close()
			continue
		}
		event.WriteSysEvent(ctx, op, "accepted downstream data plane connection", "worker_id", workerId)
	}
}

// DialDownstreamWorker returns a data plane connection towards the given
// downstream worker, waiting for one to become available if none is idle.  It
// is what the session dialer of the ingress worker uses to reach the next hop
// of a multi hop route.
func (r *dataPlaneReceiver) DialDownstreamWorker(ctx context.Context, workerId string) (net.Conn, error) {
	const op = "worker.(dataPlaneReceiver).DialDownstreamWorker"

	if workerId == "" {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "downstream worker id is empty")
	}
	conn, err := r.pool.take(ctx, workerId)
	if err != nil {
		return nil, errors.Wrap(ctx, err, op, errors.WithMsg("no data plane connection available"))
	}
	return conn, nil
}

// dataPlaneConnWorkerId resolves the public id of the downstream worker which
// established the provided connection.
func dataPlaneConnWorkerId(ctx context.Context, conn net.Conn) (string, error) {
	const op = "worker.dataPlaneConnWorkerId"

	info, err := cluster.GetWorkerInfoFromStateMap(ctx, conn)
	switch {
	case err != nil:
		return "", errors.Wrap(ctx, err, op, errors.WithMsg("unable to read worker info from the connection state"))
	case info == nil:
		return "", errors.New(ctx, errors.Internal, op, "no worker info in the connection state")
	case info.WorkerId == "":
		return "", errors.New(ctx, errors.Internal, op, "connection state carries no worker id")
	}
	return info.WorkerId, nil
}

func init() {
	reverseConnReceiverFactory = newDataPlaneReceiver
}
