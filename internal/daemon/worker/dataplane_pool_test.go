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
)

func TestDataPlanePool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("add-and-take", func(t *testing.T) {
		t.Parallel()
		pool := newDataPlanePool()
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()

		require.NoError(t, pool.add(ctx, "w_a", a))
		_, c := net.Pipe()
		require.Error(t, pool.add(ctx, "", c))
		require.Error(t, pool.add(ctx, "w_b", nil))
		c.Close()

		got, err := pool.take(ctx, "w_a")
		require.NoError(t, err)
		assert.Same(t, a, got)

		// Connections are handed out once, so the pool is empty again.
		_, err = pool.takeWithin(ctx, "w_a", 100*time.Millisecond)
		require.ErrorContains(t, err, "became available")
	})

	t.Run("take-waits-for-a-connection", func(t *testing.T) {
		t.Parallel()
		pool := newDataPlanePool()
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()

		taken := make(chan net.Conn, 1)
		errs := make(chan error, 1)
		go func() {
			conn, err := pool.takeWithin(ctx, "w_a", 5*time.Second)
			errs <- err
			if err == nil {
				taken <- conn
			}
		}()
		// Give the goroutine a moment to block in take before offering a
		// connection.
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, pool.add(ctx, "w_a", a))

		select {
		case err := <-errs:
			require.NoError(t, err)
			assert.Same(t, a, <-taken)
		case <-time.After(10 * time.Second):
			t.Fatal("take did not return after a connection was added")
		}
	})

	t.Run("take-is-bounded", func(t *testing.T) {
		t.Parallel()
		pool := newDataPlanePool()

		_, err := pool.takeWithin(ctx, "w_a", 100*time.Millisecond)
		require.ErrorContains(t, err, "became available")
	})

	t.Run("close-closes-idle-connections", func(t *testing.T) {
		t.Parallel()
		pool := newDataPlanePool()
		a, b := net.Pipe()
		defer b.Close()

		require.NoError(t, pool.add(ctx, "w_a", a))
		pool.close()

		// A closed pipe returns an error on read.
		_ = b.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err := b.Read(make([]byte, 1))
		require.Error(t, err)

		_, err = pool.take(ctx, "w_a")
		require.ErrorContains(t, err, "closed")
		require.Error(t, pool.add(ctx, "w_a", b))
	})
}

func TestDataPlaneConnWorkerId(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	_, err := dataPlaneConnWorkerId(ctx, a)
	require.ErrorContains(t, err, "unable to read worker info")

	_, err = dataPlaneConnWorkerId(ctx, nil)
	require.Error(t, err)
}

func TestDataPlaneReceiver(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("dial-downstream-requires-an-id", func(t *testing.T) {
		t.Parallel()
		r := newDataPlaneReceiver().(*dataPlaneReceiver)
		_, err := r.DialDownstreamWorker(ctx, "")
		require.ErrorContains(t, err, "empty")
	})

	t.Run("no-downstream-workers", func(t *testing.T) {
		t.Parallel()
		r := newDataPlaneReceiver().(*dataPlaneReceiver)
		r.pool.close()
		_, err := r.DialDownstreamWorker(ctx, "w_a")
		require.ErrorContains(t, err, "closed")
	})

	t.Run("nil-listener", func(t *testing.T) {
		t.Parallel()
		r := newDataPlaneReceiver().(*dataPlaneReceiver)
		require.ErrorContains(t, r.ServeDataPlaneListener(ctx, nil), "nil")
	})
}

func TestNextDataPlaneDialBackoff(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 2*time.Second, nextDataPlaneDialBackoff(dataPlaneDialMinBackoff))
	assert.Equal(t, dataPlaneDialMaxBackoff, nextDataPlaneDialBackoff(dataPlaneDialMaxBackoff))
	assert.Equal(t, dataPlaneDialMaxBackoff, nextDataPlaneDialBackoff(dataPlaneDialMaxBackoff*2))
	assert.Equal(t, dataPlaneDialMinBackoff, nextDataPlaneDialBackoff(0))
}
