// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"sort"
	"sync"

	"github.com/opendumpling/opendumpling/internal/errors"
)

// DownstreamersGraph is an in-memory graph of the workers that live below a
// controller in a multi-hop deployment.
//
// The root node represents the controller itself.  Every worker that reports
// status to the controller is added to the set of reporters, and the worker
// ids it reports as connected directly downstream become edges below that
// worker.  A worker which connects through an intermediate worker shows up
// both as a reporter (it still sends its own status) and as a child of the
// intermediate worker, so a worker is only considered a direct child of the
// root when no other worker reports it as being connected downstream.
//
// The graph is populated from the worker status calls the controller receives
// and is therefore eventually consistent: an edge learned from one worker and
// not yet confirmed by another can briefly produce an incomplete route.
//
// A DownstreamersGraph is safe for concurrent use.  The zero value is not
// usable; construct one with NewDownstreamers.
type DownstreamersGraph struct {
	rootId string

	mu sync.RWMutex
	// direct maps a worker id to the sorted ids of the workers that connect
	// directly to it.
	direct map[string][]string
	// reporters is the set of worker ids that have reported status.
	reporters map[string]struct{}
	// children is the set of worker ids reported as connecting directly to
	// some other worker.
	children map[string]struct{}
}

var _ Downstreamers = (*DownstreamersGraph)(nil)

// NewDownstreamers returns an empty DownstreamersGraph rooted at rootId.  The
// controller uses "root" for the root of its graph.
func NewDownstreamers(rootId string) *DownstreamersGraph {
	return &DownstreamersGraph{
		rootId:    rootId,
		direct:    make(map[string][]string),
		reporters: make(map[string]struct{}),
		children:  make(map[string]struct{}),
	}
}

// RootId returns the id of the root node of the graph.
func (d *DownstreamersGraph) RootId() string {
	if d == nil {
		return ""
	}
	return d.rootId
}

// SetDownstreamWorkers records that the reporter worker has the provided
// worker ids connected directly downstream of it.  An empty list is
// meaningful: it replaces any previously reported ids and makes the worker a
// leaf of the graph.
func (d *DownstreamersGraph) SetDownstreamWorkers(ctx context.Context, reporter string, downstream []string) error {
	const op = "common.(*DownstreamersGraph).SetDownstreamWorkers"
	if d == nil {
		return nil
	}
	if ctx == nil {
		return errors.New(context.Background(), errors.InvalidParameter, op, "context is nil")
	}
	if reporter == "" {
		return errors.New(ctx, errors.InvalidParameter, op, "reporter worker id is empty")
	}
	ids := make([]string, 0, len(downstream))
	for _, id := range downstream {
		// Self references and empty ids would corrupt the traversal below.
		if id == "" || id == reporter {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	d.mu.Lock()
	defer d.mu.Unlock()

	d.reporters[reporter] = struct{}{}
	if len(ids) == 0 {
		delete(d.direct, reporter)
	} else {
		d.direct[reporter] = ids
	}

	// The children set is derived from every reported edge; rebuilding it
	// wholesale keeps the two in sync when a worker loses a downstream.
	d.children = make(map[string]struct{}, len(ids))
	for _, children := range d.direct {
		for _, child := range children {
			d.children[child] = struct{}{}
		}
	}
	return nil
}

// Children returns the direct downstream worker ids of the provided worker
// id.  The root id returns the workers that connect directly to the
// controller.  An unknown worker id returns a nil slice rather than an error.
func (d *DownstreamersGraph) Children(ctx context.Context, id string) ([]string, error) {
	const op = "common.(*DownstreamersGraph).Children"
	if d == nil {
		return nil, nil
	}
	if ctx == nil {
		return nil, errors.New(context.Background(), errors.InvalidParameter, op, "context is nil")
	}
	if id == "" {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "worker id is empty")
	}

	d.mu.RLock()
	defer d.mu.RUnlock()
	children := d.childrenOf(id)
	if len(children) == 0 {
		return nil, nil
	}
	ret := make([]string, len(children))
	copy(ret, children)
	return ret, nil
}

// Routes returns every path of worker ids starting at the provided worker id
// and walking down to a leaf worker.  The provided id itself is always the
// first element of each route.  A worker without downstream workers produces
// a single route containing only that worker, which represents the requester
// acting as its own egress worker.
//
// When called with the root id the routes start at the root id, which is a
// controller rather than a worker; callers working with worker routes should
// pass a worker id.
func (d *DownstreamersGraph) Routes(ctx context.Context, id string) ([][]string, error) {
	const op = "common.(*DownstreamersGraph).Routes"
	if d == nil {
		return nil, nil
	}
	if ctx == nil {
		return nil, errors.New(context.Background(), errors.InvalidParameter, op, "context is nil")
	}
	if id == "" {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "worker id is empty")
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	seen := map[string]struct{}{id: {}}
	var routes [][]string
	d.walk(id, []string{id}, seen, &routes)
	return routes, nil
}

// childrenOf returns the direct downstream ids of id.  The lock must be held
// by the caller.
func (d *DownstreamersGraph) childrenOf(id string) []string {
	if id == d.rootId {
		var ret []string
		for reporter := range d.reporters {
			if _, ok := d.children[reporter]; !ok {
				ret = append(ret, reporter)
			}
		}
		sort.Strings(ret)
		return ret
	}
	return d.direct[id]
}

// walk performs a depth first traversal from id, appending every path that
// reaches a leaf to out.  seen guards against a malformed graph producing an
// infinite recursion; ids already on the current path are skipped.
func (d *DownstreamersGraph) walk(id string, path []string, seen map[string]struct{}, out *[][]string) {
	children := d.childrenOf(id)
	extended := false
	for _, child := range children {
		if _, ok := seen[child]; ok {
			continue
		}
		seen[child] = struct{}{}
		childPath := make([]string, len(path), len(path)+1)
		copy(childPath, path)
		childPath = append(childPath, child)

		extended = true
		d.walk(child, childPath, seen, out)
		delete(seen, child)
	}
	if !extended {
		*out = append(*out, path)
	}
}
