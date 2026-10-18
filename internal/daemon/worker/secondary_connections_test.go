// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type fakeHealthServer struct {
	healthpb.UnimplementedHealthServer
}

func (fakeHealthServer) Check(context.Context, *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func TestServeSecondaryConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	healthpb.RegisterHealthServer(grpcServer, fakeHealthServer{})

	require.NoError(t, serveSecondaryConnections(ctx, grpcListener, nil, &secondaryConnections{grpcServer: grpcServer}))
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.Dial(grpcListener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	// The reverse grpc listener serves the worker's own services; a downstream
	// worker calling them gets forwarded to the controller.
	rpcCtx, rpcCancel := context.WithTimeout(ctx, 10*time.Second)
	defer rpcCancel()
	resp, err := healthpb.NewHealthClient(conn).Check(rpcCtx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, resp.GetStatus())
}

func TestServeSecondaryConnections_Validation(t *testing.T) {
	ctx := context.Background()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	require.Error(t, serveSecondaryConnections(ctx, listener, nil, nil))
	require.Error(t, serveSecondaryConnections(ctx, listener, nil, &secondaryConnections{}))
	require.Error(t, serveSecondaryConnections(ctx, nil, nil, &secondaryConnections{grpcServer: grpc.NewServer()}))
}

// Without a reverse connection receiver there is nothing which could make
// sense of a data plane connection, so it is closed rather than left hanging.
func TestServeSecondaryConnections_ClosesDataPlaneListenerWithoutReceiver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	dataPlaneListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	t.Cleanup(grpcServer.Stop)

	require.NoError(t, serveSecondaryConnections(ctx, grpcListener, dataPlaneListener,
		&secondaryConnections{grpcServer: grpcServer}))

	require.Eventually(t, func() bool {
		_, err := dataPlaneListener.Accept()
		return err != nil
	}, 5*time.Second, 10*time.Millisecond)
}

type fakeDataPlaneReceiver struct {
	served chan net.Listener
}

func (f *fakeDataPlaneReceiver) ServeDataPlaneListener(_ context.Context, l net.Listener) error {
	f.served <- l
	_, _ = l.Accept()
	return nil
}

func TestServeSecondaryConnections_DataPlaneReceiverServes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	dataPlaneListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	receiver := &fakeDataPlaneReceiver{served: make(chan net.Listener, 1)}
	grpcServer := grpc.NewServer()
	t.Cleanup(grpcServer.Stop)

	require.NoError(t, serveSecondaryConnections(ctx, grpcListener, dataPlaneListener,
		&secondaryConnections{grpcServer: grpcServer, downstreamReceiver: receiver}))

	select {
	case served := <-receiver.served:
		assert.Equal(t, dataPlaneListener, served)
	case <-time.After(5 * time.Second):
		t.Fatal("data plane listener was never served")
	}

	// Closing the listener releases the blocked Accept call.
	require.NoError(t, dataPlaneListener.Close())
}
