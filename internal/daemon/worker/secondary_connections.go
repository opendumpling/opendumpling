// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"net"

	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/observability/event"
	"google.golang.org/grpc"
)

// secondaryConnections bundles everything needed to serve the workers which
// connect to this worker rather than to a controller.
type secondaryConnections struct {
	// grpcServer serves the reverse grpc connections of downstream workers.
	// It is the very same server that serves the controller facing listener:
	// every service registered on it forwards to the controller, so a
	// downstream worker talking to this worker is talking to the controller.
	grpcServer *grpc.Server
	// downstreamReceiver serves the session data plane of the downstream
	// workers, when this worker runs one.
	downstreamReceiver any
}

// dataPlaneListenerServer is implemented by a reverse connection receiver which
// knows how to serve the connections of the downstream workers over the data
// plane listener.
type dataPlaneListenerServer interface {
	ServeDataPlaneListener(context.Context, net.Listener) error
}

// serveSecondaryConnections starts serving the workers which connect to this
// worker: l is the reverse grpc listener and l2 the data plane listener.
//
// This is what turns a worker into an ingress worker for a multi hop route; a
// private worker dials a public worker on both listeners, which lets it reach
// the controller through the public worker and gives the public worker a way
// to reach the private worker's data plane.
func serveSecondaryConnections(ctx context.Context, l, l2 net.Listener, sc any) error {
	const op = "worker.serveSecondaryConnections"

	connections, ok := sc.(*secondaryConnections)
	switch {
	case !ok || connections == nil:
		return errors.New(ctx, errors.InvalidParameter, op, "secondary connections were not provided")
	case l == nil:
		return errors.New(ctx, errors.InvalidParameter, op, "reverse grpc listener is nil")
	case connections.grpcServer == nil:
		return errors.New(ctx, errors.InvalidParameter, op, "downstream grpc server is nil")
	}

	go func() {
		if err := connections.grpcServer.Serve(l); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			event.WriteError(ctx, op, err, event.WithInfoMsg("reverse grpc server stopped"))
		}
	}()

	if l2 == nil {
		return nil
	}
	if receiver, ok := connections.downstreamReceiver.(dataPlaneListenerServer); ok {
		go func() {
			if err := receiver.ServeDataPlaneListener(ctx, l2); err != nil && !errors.Is(err, net.ErrClosed) {
				event.WriteError(ctx, op, err, event.WithInfoMsg("downstream data plane listener stopped"))
			}
		}()
		return nil
	}
	// Without a receiver there is nothing which could make sense of a data
	// plane connection, so it is closed rather than left hanging.
	if err := l2.Close(); err != nil {
		return errors.Wrap(ctx, err, op, errors.WithMsg("unable to close data plane listener"))
	}
	return nil
}
