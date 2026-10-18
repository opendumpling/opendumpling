// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"math/rand"
	"net"
	"time"

	"github.com/opendumpling/opendumpling/internal/daemon/cluster"
	"github.com/opendumpling/opendumpling/internal/daemon/common"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/observability/event"
)

const (
	// dataPlaneDialMinBackoff is the first delay between two attempts to
	// establish a data plane connection towards an upstream.
	dataPlaneDialMinBackoff = time.Second

	// dataPlaneDialMaxBackoff bounds the delay between two attempts to
	// establish a data plane connection towards an upstream.
	dataPlaneDialMaxBackoff = 30 * time.Second

	// dataPlaneRefillDelay is how long a private worker waits before
	// replacing a data plane connection which ended, so that an upstream
	// going away does not turn into a dial storm.
	dataPlaneRefillDelay = 250 * time.Millisecond
)

// startDataPlaneUpstream maintains the data plane connections a private worker
// holds towards its upstreams.
//
// A private worker is never dialed by a client: it dials its upstreams ahead
// of time and each of those connections, once the upstream asks for it, turns
// into a session towards an endpoint this worker can reach.  This is what
// lets a private worker sit behind NAT or a firewall with no reachable
// inbound port.
//
// It is a no-op for a worker which is not configured as private.
func (w *Worker) startDataPlaneUpstream(ctx context.Context) {
	const op = "worker.(Worker).startDataPlaneUpstream"

	if !w.conf.RawConfig.Worker.IsPrivate() {
		return
	}
	if len(w.conf.RawConfig.Worker.InitialUpstreams) == 0 {
		event.WriteError(ctx, op, errors.New(ctx, errors.InvalidParameter, op,
			"a private worker needs at least one upstream to dial for its data plane connections"))
		return
	}
	poolSize := w.conf.RawConfig.Worker.DataPlanePoolSize()
	event.WriteSysEvent(ctx, op, "worker is private, maintaining data plane connections to its upstreams",
		"pool_size", poolSize, "upstreams", w.conf.RawConfig.Worker.InitialUpstreams)
	for i := 0; i < poolSize; i++ {
		w.tickerWg.Add(1)
		go func() {
			defer w.tickerWg.Done()
			w.maintainDataPlaneConn(ctx)
		}()
	}
}

// maintainDataPlaneConn keeps a single data plane connection towards one of
// the worker's upstreams alive, serving whatever the upstream asks of it, and
// replaces it once it is no longer usable.
func (w *Worker) maintainDataPlaneConn(ctx context.Context) {
	const op = "worker.(Worker).maintainDataPlaneConn"

	dial := w.upstreamDialerFunc(common.DataPlaneProxyAlpnValue)
	backoff := dataPlaneDialMinBackoff
	for ctx.Err() == nil {
		addr, err := w.randomUpstreamAddress()
		if err != nil {
			event.WriteError(ctx, op, err)
			if !w.waitForDataPlaneRetry(ctx, backoff) {
				return
			}
			continue
		}
		conn, err := dial(ctx, addr)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			event.WriteError(ctx, op, err, event.WithInfoMsg("error dialing data plane connection to upstream", "address", addr))
			if !w.waitForDataPlaneRetry(ctx, backoff) {
				return
			}
			backoff = nextDataPlaneDialBackoff(backoff)
			continue
		}
		backoff = dataPlaneDialMinBackoff
		// Serving returns once the session which used the connection ended
		// or the upstream closed it; either way a replacement is dialed.
		if err := cluster.ServeDataPlaneConn(ctx, conn, w.dataPlaneNextHop); err != nil && ctx.Err() == nil {
			event.WriteSysEvent(ctx, op, "data plane connection to upstream ended", "address", addr, "error", err.Error())
		}
		if !w.waitForDataPlaneRetry(ctx, dataPlaneRefillDelay) {
			return
		}
	}
}

// dataPlaneNextHop returns a data plane connection towards one of this
// worker's own downstream workers.  It is only consulted when this worker is
// an intermediate hop of a route rather than its egress worker.
func (w *Worker) dataPlaneNextHop(ctx context.Context, workerId string) (net.Conn, error) {
	const op = "worker.(Worker).dataPlaneNextHop"

	dialer, ok := w.downstreamReceiver.(downstreamDataPlaneDialer)
	if !ok || dialer == nil {
		return nil, errors.New(ctx, errors.Internal, op,
			"worker holds no data plane connection towards its downstream workers")
	}
	return dialer.DialDownstreamWorker(ctx, workerId)
}

// downstreamDataPlaneDialer is the subset of the reverse connection receiver
// which hands out data plane connections towards a downstream worker.
type downstreamDataPlaneDialer interface {
	DialDownstreamWorker(ctx context.Context, workerId string) (net.Conn, error)
}

// randomUpstreamAddress returns one of the worker's configured upstream
// addresses, chosen at random.
func (w *Worker) randomUpstreamAddress() (string, error) {
	const op = "worker.(Worker).randomUpstreamAddress"

	upstreams := w.conf.RawConfig.Worker.InitialUpstreams
	if len(upstreams) == 0 {
		return "", errors.New(context.Background(), errors.InvalidParameter, op, "worker has no upstream addresses")
	}
	return upstreams[rand.Intn(len(upstreams))], nil
}

// nextDataPlaneDialBackoff doubles the provided delay, capped at
// dataPlaneDialMaxBackoff.
func nextDataPlaneDialBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > dataPlaneDialMaxBackoff {
		return dataPlaneDialMaxBackoff
	}
	if next < dataPlaneDialMinBackoff {
		return dataPlaneDialMinBackoff
	}
	return next
}

// waitForDataPlaneRetry waits for the given delay and reports whether the
// worker is still running.
func (w *Worker) waitForDataPlaneRetry(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
