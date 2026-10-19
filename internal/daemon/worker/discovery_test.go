// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/opendumpling/opendumpling/internal/cmd/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withLookupHost answers cluster id lookups for the duration of a test, so the
// tests do not depend on the DNS of whatever machine they run on. It cannot be
// combined with t.Parallel, since the resolver it replaces is process wide.
func withLookupHost(t *testing.T, fn func(ctx context.Context, name string) ([]string, error)) {
	t.Helper()
	orig := lookupHost
	lookupHost = fn
	t.Cleanup(func() { lookupHost = orig })
}

func TestResolveClusterAddressesByDns(t *testing.T) {
	ctx := context.Background()

	t.Run("resolves the cluster id against the discovery domain", func(t *testing.T) {
		withLookupHost(t, func(_ context.Context, name string) ([]string, error) {
			require.Equal(t, "c-1.dumpling.example.com", name)
			return []string{"10.0.0.5", "2001:db8::5"}, nil
		})

		addrs, err := ResolveClusterAddressesByDns(ctx, "c-1", "dumpling.example.com", nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"10.0.0.5", "2001:db8::5"}, addrs)
	})

	t.Run("defaults the discovery domain", func(t *testing.T) {
		withLookupHost(t, func(_ context.Context, name string) ([]string, error) {
			require.Equal(t, "c-1."+DefaultClusterDiscoveryDomain, name)
			return []string{"10.0.0.5"}, nil
		})

		addrs, err := ResolveClusterAddressesByDns(ctx, "c-1", "", nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"10.0.0.5"}, addrs)
	})

	t.Run("also looks up the cluster id without its int- prefix", func(t *testing.T) {
		seen := make([]string, 0, 2)
		withLookupHost(t, func(_ context.Context, name string) ([]string, error) {
			seen = append(seen, name)
			if name == "int-c-1.dumpling.example.com" {
				return nil, fmt.Errorf("no such host")
			}
			return []string{"10.0.0.5"}, nil
		})

		addrs, err := ResolveClusterAddressesByDns(ctx, "int-c-1", "dumpling.example.com", nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"10.0.0.5"}, addrs)
		assert.Equal(t, []string{
			"int-c-1.dumpling.example.com",
			"c-1.dumpling.example.com",
		}, seen)
	})

	t.Run("trailing dots do not produce an empty label", func(t *testing.T) {
		withLookupHost(t, func(_ context.Context, name string) ([]string, error) {
			require.Equal(t, "c-1.dumpling.example.com", name)
			return []string{"10.0.0.5"}, nil
		})

		_, err := ResolveClusterAddressesByDns(ctx, "c-1.", "dumpling.example.com.", nil)
		require.NoError(t, err)
	})

	t.Run("falls back when the name does not resolve", func(t *testing.T) {
		withLookupHost(t, func(_ context.Context, _ string) ([]string, error) {
			return nil, fmt.Errorf("no such host")
		})

		addrs, err := ResolveClusterAddressesByDns(ctx, "c-1", "dumpling.example.com", []string{"boundary.example.com:9201"})
		require.NoError(t, err)
		assert.Equal(t, []string{"boundary.example.com:9201"}, addrs)
	})

	t.Run("falls back when the name resolves to nothing", func(t *testing.T) {
		withLookupHost(t, func(_ context.Context, _ string) ([]string, error) {
			return nil, nil
		})

		addrs, err := ResolveClusterAddressesByDns(ctx, "c-1", "dumpling.example.com", []string{"boundary.example.com:9201"})
		require.NoError(t, err)
		assert.Equal(t, []string{"boundary.example.com:9201"}, addrs)
	})

	t.Run("fails when discovery fails and there is no fallback", func(t *testing.T) {
		withLookupHost(t, func(_ context.Context, _ string) ([]string, error) {
			return nil, fmt.Errorf("no such host")
		})

		_, err := ResolveClusterAddressesByDns(ctx, "c-1", "dumpling.example.com", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dumpling.example.com")
		assert.Contains(t, err.Error(), "discovery_fallback_addr")
	})

	t.Run("fails on an empty cluster id", func(t *testing.T) {
		_, err := ResolveClusterAddressesByDns(ctx, "", "dumpling.example.com", []string{"boundary.example.com:9201"})
		require.Error(t, err)
	})
}

func TestClusterUpstreams(t *testing.T) {
	ctx := context.Background()

	t.Run("is empty without a cluster id", func(t *testing.T) {
		withLookupHost(t, func(_ context.Context, _ string) ([]string, error) {
			t.Fatal("a worker without a cluster id must not look anything up")
			return nil, nil
		})

		w := &Worker{conf: &Config{RawConfig: &config.Config{}}}
		assert.Empty(t, w.clusterUpstreams(ctx))
	})

	t.Run("returns what the cluster id resolves to", func(t *testing.T) {
		withLookupHost(t, func(_ context.Context, _ string) ([]string, error) {
			return []string{"10.0.0.5"}, nil
		})

		w := &Worker{conf: &Config{RawConfig: &config.Config{ClusterId: "c-1"}}}
		assert.Equal(t, []string{"10.0.0.5"}, w.clusterUpstreams(ctx))
	})

	t.Run("swallows a discovery failure", func(t *testing.T) {
		withLookupHost(t, func(_ context.Context, _ string) ([]string, error) {
			return nil, fmt.Errorf("no such host")
		})

		w := &Worker{conf: &Config{RawConfig: &config.Config{ClusterId: "c-1"}}}
		assert.Empty(t, w.clusterUpstreams(ctx))
	})
}
