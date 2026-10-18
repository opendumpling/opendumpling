// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/opendumpling/opendumpling/internal/daemon/cluster"
	pbs "github.com/opendumpling/opendumpling/internal/gen/controller/servers/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDownstreamDialer hands out a single connection for a single worker id
// and records what was asked of it.
type testDownstreamDialer struct {
	workerId string
	conn     net.Conn
	err      error
	asked    []string
}

func (d *testDownstreamDialer) DialDownstreamWorker(_ context.Context, workerId string) (net.Conn, error) {
	d.asked = append(d.asked, workerId)
	if d.err != nil {
		return nil, d.err
	}
	return d.conn, nil
}

// testEchoEndpoint starts a TCP server which echoes back everything it
// receives.
func testEchoEndpoint(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = io.Copy(c, c)
				c.Close()
			}()
		}
	}()
	return l.Addr().String()
}

func TestRouteDialerSingleHop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoint := testEchoEndpoint(t)

	for _, route := range [][]string{nil, {"w_ingress"}} {
		d, err := routeDialer(ctx, endpoint, "w_ingress", &pbs.AuthorizeConnectionResponse{Route: route}, nil)
		require.NoError(t, err)

		conn, err := d.Dial(ctx)
		require.NoError(t, err)
		assert.IsType(t, &net.TCPConn{}, conn)
		assert.NotEmpty(t, d.LastConnectionAddr().Ip())
		require.NoError(t, conn.Close())
	}
}

func TestRouteDialerMultiHop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoint := testEchoEndpoint(t)

	ingress, egress := net.Pipe()
	defer egress.Close()
	downstream := &testDownstreamDialer{workerId: "w_egress", conn: ingress}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = cluster.ServeDataPlaneConn(ctx, egress, nil)
	}()

	d, err := routeDialer(ctx, endpoint, "w_ingress", &pbs.AuthorizeConnectionResponse{
		Route: []string{"w_ingress", "w_egress"},
	}, downstream)
	require.NoError(t, err)

	conn, err := d.Dial(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"w_egress"}, downstream.asked)
	// The address the egress worker connected to is reported back, which is
	// the only address the controller should ever see.
	assert.NotEmpty(t, d.LastConnectionAddr().Ip())
	assert.NotZero(t, d.LastConnectionAddr().Port())

	_, err = conn.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(buf))

	require.NoError(t, conn.Close())
	<-served
}

func TestRouteDialerForwardsTheRemainingRoute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoint := testEchoEndpoint(t)

	// ingress <-> mid
	midIngress, ingress := net.Pipe()
	defer midIngress.Close()
	// mid <-> egress
	egress, midEgress := net.Pipe()
	defer midEgress.Close()

	midDownstream := &testDownstreamDialer{workerId: "w_egress", conn: midEgress}
	next := func(context.Context, string) (net.Conn, error) {
		return midDownstream.DialDownstreamWorker(ctx, midDownstream.workerId)
	}
	go func() {
		_ = cluster.ServeDataPlaneConn(ctx, midIngress, next)
	}()
	go func() {
		_ = cluster.ServeDataPlaneConn(ctx, egress, nil)
	}()

	d, err := routeDialer(ctx, endpoint, "w_ingress", &pbs.AuthorizeConnectionResponse{
		Route: []string{"w_ingress", "w_mid", "w_egress"},
	}, &testDownstreamDialer{workerId: "w_mid", conn: ingress})
	require.NoError(t, err)

	conn, err := d.Dial(ctx)
	require.NoError(t, err)

	_, err = conn.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(buf))
	require.NoError(t, conn.Close())
}

func TestRouteDialerErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("empty-endpoint", func(t *testing.T) {
		t.Parallel()
		_, err := routeDialer(ctx, "", "w_ingress", &pbs.AuthorizeConnectionResponse{}, nil)
		require.ErrorContains(t, err, "endpoint is empty")
	})

	t.Run("no-downstream-connections", func(t *testing.T) {
		t.Parallel()
		_, err := routeDialer(ctx, "127.0.0.1:22", "w_ingress", &pbs.AuthorizeConnectionResponse{
			Route: []string{"w_ingress", "w_egress"},
		}, struct{}{})
		require.ErrorContains(t, err, "no data plane connection")
	})

	t.Run("next-hop-unavailable", func(t *testing.T) {
		t.Parallel()
		d, err := routeDialer(ctx, "127.0.0.1:22", "w_ingress", &pbs.AuthorizeConnectionResponse{
			Route: []string{"w_ingress", "w_egress"},
		}, &testDownstreamDialer{err: assert.AnError})
		require.NoError(t, err)
		_, err = d.Dial(ctx)
		require.ErrorContains(t, err, "unable to reach the next hop")
	})

	t.Run("endpoint-unreachable", func(t *testing.T) {
		t.Parallel()
		// Port 1 on localhost is closed, so the egress worker reports the
		// failure back to the ingress worker.
		ingress, egress := net.Pipe()
		defer egress.Close()
		go func() {
			_ = cluster.ServeDataPlaneConn(ctx, egress, nil)
		}()
		d, err := routeDialer(ctx, "127.0.0.1:1", "w_ingress", &pbs.AuthorizeConnectionResponse{
			Route: []string{"w_ingress", "w_egress"},
		}, &testDownstreamDialer{workerId: "w_egress", conn: ingress})
		require.NoError(t, err)
		_, err = d.Dial(ctx)
		require.ErrorContains(t, err, "unable to reach the endpoint")
	})
}
