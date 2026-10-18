// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownstreamersGraph_Children(t *testing.T) {
	ctx := context.Background()
	g := NewDownstreamers("root")

	// Nothing has reported yet.
	children, err := g.Children(ctx, "root")
	require.NoError(t, err)
	assert.Empty(t, children)
	children, err = g.Children(ctx, "w1")
	require.NoError(t, err)
	assert.Empty(t, children)

	// Two workers reporting directly to the controller and nothing else:
	// both are children of the root.
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w1", nil))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w2", nil))
	children, err = g.Children(ctx, "root")
	require.NoError(t, err)
	assert.Equal(t, []string{"w1", "w2"}, children)

	// w3 connects through w1, so it moves below w1 even though it still
	// reports status itself.
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w3", nil))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w1", []string{"w3"}))
	children, err = g.Children(ctx, "root")
	require.NoError(t, err)
	assert.Equal(t, []string{"w1", "w2"}, children)
	children, err = g.Children(ctx, "w1")
	require.NoError(t, err)
	assert.Equal(t, []string{"w3"}, children)

	// Losing a downstream worker moves it back to the root.
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w1", nil))
	children, err = g.Children(ctx, "root")
	require.NoError(t, err)
	assert.Equal(t, []string{"w1", "w2", "w3"}, children)
	children, err = g.Children(ctx, "w1")
	require.NoError(t, err)
	assert.Empty(t, children)
}

func TestDownstreamersGraph_Routes(t *testing.T) {
	ctx := context.Background()
	g := NewDownstreamers("root")

	//          root
	//        /   |   \
	//      w1    w2   w3
	//      |
	//     w4
	//    /
	//   w5
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w1", []string{"w4"}))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w2", nil))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w3", nil))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w4", []string{"w5"}))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w5", nil))

	tests := []struct {
		name string
		id   string
		want [][]string
	}{
		{
			name: "root spans every direct worker",
			id:   "root",
			want: [][]string{{"root", "w1", "w4", "w5"}, {"root", "w2"}, {"root", "w3"}},
		},
		{
			name: "ingress worker reaches every worker below it",
			id:   "w1",
			want: [][]string{{"w1", "w4", "w5"}},
		},
		{
			name: "leaf worker is its own only route",
			id:   "w3",
			want: [][]string{{"w3"}},
		},
		{
			name: "unknown worker is a leaf",
			id:   "w9",
			want: [][]string{{"w9"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes, err := g.Routes(ctx, tt.id)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, routes)
		})
	}
}

func TestDownstreamersGraph_RoutesNested(t *testing.T) {
	ctx := context.Background()
	g := NewDownstreamers("root")
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w1", []string{"w2", "w3"}))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w2", []string{"w4"}))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w3", nil))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w4", nil))

	routes, err := g.Routes(ctx, "w1")
	require.NoError(t, err)
	assert.ElementsMatch(t, [][]string{
		{"w1", "w3"},
		{"w1", "w2", "w4"},
	}, routes)
}

func TestDownstreamersGraph_CycleDoesNotRecurseForever(t *testing.T) {
	ctx := context.Background()
	g := NewDownstreamers("root")
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w1", []string{"w2"}))
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w2", []string{"w1"}))

	routes, err := g.Routes(ctx, "w1")
	require.NoError(t, err)
	assert.ElementsMatch(t, [][]string{{"w1", "w2"}}, routes)
}

func TestDownstreamersGraph_SelfReferenceIgnored(t *testing.T) {
	ctx := context.Background()
	g := NewDownstreamers("root")
	require.NoError(t, g.SetDownstreamWorkers(ctx, "w1", []string{"w1", "w2", ""}))

	children, err := g.Children(ctx, "w1")
	require.NoError(t, err)
	assert.Equal(t, []string{"w2"}, children)
}

func TestDownstreamersGraph_InvalidInput(t *testing.T) {
	ctx := context.Background()
	g := NewDownstreamers("root")

	require.Error(t, g.SetDownstreamWorkers(ctx, "", []string{"w1"}))
	_, err := g.Children(ctx, "")
	require.Error(t, err)
	_, err = g.Routes(ctx, "")
	require.Error(t, err)
}

func TestDownstreamersGraph_NilReceiverIsNoOp(t *testing.T) {
	var g *DownstreamersGraph
	require.Equal(t, "", g.RootId())
	require.NoError(t, g.SetDownstreamWorkers(context.Background(), "w1", nil))

	children, err := g.Children(context.Background(), "w1")
	require.NoError(t, err)
	assert.Nil(t, children)

	routes, err := g.Routes(context.Background(), "w1")
	require.NoError(t, err)
	assert.Nil(t, routes)
}

func TestDownstreamersGraph_ConcurrentUpdates(t *testing.T) {
	ctx := context.Background()
	g := NewDownstreamers("root")

	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				_ = g.SetDownstreamWorkers(ctx, "w1", []string{"w2"})
				_, _ = g.Routes(ctx, "root")
				_, _ = g.Children(ctx, "w1")
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}
