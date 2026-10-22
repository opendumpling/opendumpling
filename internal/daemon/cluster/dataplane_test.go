// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDataPlaneEndpoint starts a TCP server which echoes back everything it
// receives and returns its address.
func testDataPlaneEndpoint(t *testing.T) string {
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

func TestDataPlaneFrame(t *testing.T) {
	t.Parallel()

	t.Run("round-trip", func(t *testing.T) {
		t.Parallel()
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		want := &DataPlaneDialRequest{
			Endpoint:       "127.0.0.1:22",
			RemainingRoute: []string{"w_1234", "w_5678"},
		}
		go func() {
			_ = writeDataPlaneFrame(client, want)
		}()
		got := new(DataPlaneDialRequest)
		require.NoError(t, readDataPlaneFrameInto(server, got))
		assert.Equal(t, want, got)
	})

	t.Run("unknown-version", func(t *testing.T) {
		t.Parallel()
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		go func() {
			_, _ = client.Write([]byte{99, 0, 0, 0, 2, '{', '}'})
		}()
		_, err := readDataPlaneFrame(server)
		require.ErrorContains(t, err, "unsupported data plane protocol version")
	})

	t.Run("oversized-message", func(t *testing.T) {
		t.Parallel()
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		require.ErrorContains(t, writeDataPlaneFrame(server, map[string]string{"a": padString('a', maxDataPlaneMessageSize)}),
			"out of bounds")
	})

	t.Run("empty-message", func(t *testing.T) {
		t.Parallel()
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		go func() {
			_, _ = client.Write([]byte{dataPlaneProtoVersion, 0, 0, 0, 0})
		}()
		_, err := readDataPlaneFrame(server)
		require.ErrorContains(t, err, "out of bounds")
	})
}

func padString(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

func TestDataPlaneDialToEgress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoint := testDataPlaneEndpoint(t)

	egress, ingress := net.Pipe()
	defer ingress.Close()
	served := make(chan error, 1)
	go func() {
		served <- ServeDataPlaneConn(ctx, egress, nil)
	}()

	resp, err := DataPlaneDial(ctx, ingress, &DataPlaneDialRequest{Endpoint: endpoint})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "127.0.0.1", resp.Ip)
	assert.NotZero(t, resp.Port)

	// The connection now carries the session bytes.
	_, err = ingress.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(ingress, buf)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(buf))

	require.NoError(t, ingress.Close())
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeDataPlaneConn did not return after the session ended")
	}
}

func TestDataPlaneDialToEgressResolvesHostname(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoint := testDataPlaneEndpoint(t)
	_, port, err := net.SplitHostPort(endpoint)
	require.NoError(t, err)
	endpoint = net.JoinHostPort("localhost", port)

	egress, ingress := net.Pipe()
	defer ingress.Close()
	served := make(chan error, 1)
	go func() {
		served <- ServeDataPlaneConn(ctx, egress, nil)
	}()

	resp, err := DataPlaneDial(ctx, ingress, &DataPlaneDialRequest{Endpoint: endpoint})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.NotNil(t, net.ParseIP(resp.Ip))
	assert.NotEqual(t, "localhost", resp.Ip)
	assert.Equal(t, uint32(mustParsePort(t, endpoint)), resp.Port)

	_, err = ingress.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(ingress, buf)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(buf))

	require.NoError(t, ingress.Close())
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeDataPlaneConn did not return after the session ended")
	}
}

func mustParsePort(t *testing.T, endpoint string) uint64 {
	t.Helper()
	_, port, err := net.SplitHostPort(endpoint)
	require.NoError(t, err)
	parsed, err := strconv.ParseUint(port, 10, 32)
	require.NoError(t, err)
	return parsed
}

func TestDataPlaneDialToUnreachableEndpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	egress, ingress := net.Pipe()
	defer ingress.Close()
	done := make(chan error, 1)
	go func() {
		done <- ServeDataPlaneConn(ctx, egress, nil)
	}()

	// Port 1 on localhost is closed, which makes the egress worker report the
	// failure back rather than ending the connection.
	resp, err := DataPlaneDial(ctx, ingress, &DataPlaneDialRequest{Endpoint: "127.0.0.1:1"})
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.Error)

	// The connection survived the failed dial, so a working one follows.
	require.NoError(t, ingress.Close())
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeDataPlaneConn did not return after the connection was closed")
	}
}

func TestDataPlaneDialThroughIntermediateHop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoint := testDataPlaneEndpoint(t)

	// ingress <-> mid
	midIngress, ingress := net.Pipe()
	defer ingress.Close()
	// mid <-> egress
	egress, midEgress := net.Pipe()
	defer midEgress.Close()

	next := func(context.Context, string) (net.Conn, error) {
		return midEgress, nil
	}
	midDone := make(chan error, 1)
	go func() {
		midDone <- ServeDataPlaneConn(ctx, midIngress, next)
	}()
	egressDone := make(chan error, 1)
	go func() {
		egressDone <- ServeDataPlaneConn(ctx, egress, nil)
	}()

	resp, err := DataPlaneDial(ctx, ingress, &DataPlaneDialRequest{
		Endpoint:       endpoint,
		RemainingRoute: []string{"w_egress"},
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "127.0.0.1", resp.Ip)

	_, err = ingress.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(ingress, buf)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(buf))

	require.NoError(t, ingress.Close())
	select {
	case <-midDone:
	case <-time.After(5 * time.Second):
		t.Fatal("intermediate hop did not return after the session ended")
	}
	select {
	case <-egressDone:
	case <-time.After(5 * time.Second):
		t.Fatal("egress hop did not return after the session ended")
	}
}

func TestDataPlaneDialWithoutNextHop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mid, ingress := net.Pipe()
	defer ingress.Close()
	done := make(chan error, 1)
	go func() {
		done <- ServeDataPlaneConn(ctx, mid, nil)
	}()

	_, err := DataPlaneDial(ctx, ingress, &DataPlaneDialRequest{
		Endpoint:       "127.0.0.1:22",
		RemainingRoute: []string{"w_egress"},
	})
	require.Error(t, err)

	require.NoError(t, ingress.Close())
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeDataPlaneConn did not return")
	}
}

func TestDataPlaneDialRequestValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	_, err := DataPlaneDial(ctx, nil, &DataPlaneDialRequest{Endpoint: "127.0.0.1:22"})
	require.ErrorContains(t, err, "connection is nil")

	_, err = DataPlaneDial(ctx, client, nil)
	require.ErrorContains(t, err, "request is nil")

	_, err = DataPlaneDial(ctx, client, &DataPlaneDialRequest{})
	require.ErrorContains(t, err, "no endpoint")

	require.ErrorContains(t, ServeDataPlaneConn(ctx, nil, nil), "connection is nil")
}
