// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/opendumpling/opendumpling/internal/errors"
)

const (
	// DefaultClusterDiscoveryDomain is the DNS domain a worker resolves a
	// cluster id against when the configuration does not name one. A cluster
	// which does not serve discovery under this name has to say so, either by
	// setting discovery_domain or by providing a fallback address.
	DefaultClusterDiscoveryDomain = "dumpling.local"

	// intClusterIdPrefix is a prefix some cluster ids carry which is not part
	// of the name they are published under.
	intClusterIdPrefix = "int-"
)

// ClusterIdResolver turns a cluster id into the addresses a worker can dial to
// reach that cluster. It defaults to DNS discovery and exists as a variable so
// a build which reaches a cluster some other way can replace it.
var ClusterIdResolver = ResolveClusterAddressesByDns

// lookupHost is a variable so tests can answer for a name they control instead
// of depending on the DNS of whatever machine they run on.
var lookupHost = net.DefaultResolver.LookupHost

// ResolveClusterAddressesByDns resolves a cluster id by looking up
// "<cluster id>.<discovery domain>" in DNS, the same way a managed worker
// finds the control plane of its cluster without being told an address.
//
// Every address the name resolves to is returned without a port, so the caller
// applies the same port default it applies to initial_upstreams. Discovery
// failing is not fatal on its own: the configured fallback addresses are
// returned instead, which is what makes the cluster id usable in networks
// where the discovery DNS is not reachable, or not set up at all.
func ResolveClusterAddressesByDns(ctx context.Context, clusterId, discoveryDomain string, fallback []string) ([]string, error) {
	const op = "worker.(ResolveClusterAddressesByDns)"
	switch {
	case clusterId == "":
		return nil, errors.New(ctx, errors.InvalidParameter, op, "empty cluster id")
	case discoveryDomain == "":
		discoveryDomain = DefaultClusterDiscoveryDomain
	}

	// Cluster ids may carry an "int-" prefix which the published name does
	// not, so both forms are looked up before giving up on discovery.
	names := []string{clusterId}
	if trimmed := strings.TrimPrefix(clusterId, intClusterIdPrefix); trimmed != clusterId {
		names = append(names, trimmed)
	}

	var (
		addrs    []string
		lookupErr error
	)
	for _, name := range names {
		fqdn := strings.TrimSuffix(name, ".") + "." + strings.TrimSuffix(discoveryDomain, ".")
		hosts, err := lookupHost(ctx, fqdn)
		switch {
		case err != nil:
			lookupErr = fmt.Errorf("error resolving %s: %w", fqdn, err)
		case len(hosts) == 0:
			lookupErr = fmt.Errorf("%s resolved to no addresses", fqdn)
		default:
			addrs = append(addrs, hosts...)
		}
		if len(addrs) > 0 {
			break
		}
	}
	if len(addrs) > 0 {
		return addrs, nil
	}

	// Discovery came up empty, and the loop above sets a reason every time it
	// does, so there is always something to report.
	if len(fallback) == 0 {
		if lookupErr != nil {
			return nil, errors.Wrap(ctx, lookupErr, op,
				errors.WithMsg("cluster discovery failed and no discovery_fallback_addr is configured"))
		}
		return nil, errors.New(ctx, errors.InvalidParameter, op,
			"cluster id resolved to no addresses and no discovery_fallback_addr is configured")
	}
	return fallback, nil
}

// clusterUpstreams returns the addresses the worker's cluster id currently
// resolves to. It is the discovery counterpart of initial_upstreams: a worker
// configured with a cluster id has no addresses of its own, so whenever the
// connection to the controllers has to be re-seeded this is where they come
// from. Discovery failing is not worth reporting here, the caller already has
// a connection problem of its own to deal with.
func (w *Worker) clusterUpstreams(ctx context.Context) []string {
	if w.conf.RawConfig.ClusterId == "" {
		return nil
	}
	addrs, err := ClusterIdResolver(
		ctx,
		w.conf.RawConfig.ClusterId,
		w.conf.RawConfig.DiscoveryDomain,
		w.conf.RawConfig.DiscoveryFallbackAddr,
	)
	if err != nil {
		return nil
	}
	return addrs
}
