// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package handlers

import (
	"context"
	"fmt"
	"testing"

	"github.com/opendumpling/opendumpling/internal/authtoken"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/common"
	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/host/static"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/kms"
	"github.com/opendumpling/opendumpling/internal/server"
	"github.com/opendumpling/opendumpling/internal/session"
	"github.com/opendumpling/opendumpling/internal/target"
	"github.com/opendumpling/opendumpling/internal/target/tcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultiHopConnectionRoute exercises the route the ingress worker receives
// when a connection is authorized: a path of worker ids starting at the
// requesting worker and ending at the worker which will dial the endpoint.
func TestMultiHopConnectionRoute(t *testing.T) {
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	kms := kms.TestKms(t, conn, wrapper)

	serversRepoFn := func() (*server.Repository, error) {
		return server.NewRepository(ctx, rw, rw, kms)
	}
	repo, err := serversRepoFn()
	require.NoError(t, err)

	org, prj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
	at := authtoken.TestAuthToken(t, conn, kms, org.GetPublicId())
	hc := static.TestCatalogs(t, conn, prj.GetPublicId(), 1)[0]
	hs := static.TestSets(t, conn, hc.GetPublicId(), 1)[0]
	h := static.TestHosts(t, conn, hc.GetPublicId(), 1)[0]
	static.TestSetMembers(t, conn, hs.GetPublicId(), []*static.Host{h})
	tar := tcp.TestTarget(ctx, t, conn, prj.GetPublicId(), "test", target.WithHostSources([]string{hs.GetPublicId()}))

	newSession := func(t *testing.T, egressFilter string) *session.Session {
		return session.TestSession(t, conn, wrapper, session.ComposedOf{
			UserId:             at.GetIamUserId(),
			HostId:             h.GetPublicId(),
			TargetId:           tar.GetPublicId(),
			HostSetId:          hs.GetPublicId(),
			AuthTokenId:        at.GetPublicId(),
			ProjectId:          prj.GetPublicId(),
			Endpoint:           "tcp://127.0.0.1:22",
			EgressWorkerFilter: egressFilter,
		})
	}

	// ingress connects to the controller and has mid and sibling below it; mid
	// has egress below it.
	ingress := server.TestKmsWorker(t, conn, wrapper, server.WithName("ingress"))
	mid := server.TestKmsWorker(t, conn, wrapper, server.WithName("mid"))
	egress := server.TestKmsWorker(t, conn, wrapper, server.WithName("egress"))
	sibling := server.TestKmsWorker(t, conn, wrapper, server.WithName("sibling"))

	newGraph := func(t *testing.T) *common.DownstreamersGraph {
		g := common.NewDownstreamers("root")
		require.NoError(t, g.SetDownstreamWorkers(ctx, ingress.GetPublicId(), []string{mid.GetPublicId(), sibling.GetPublicId()}))
		require.NoError(t, g.SetDownstreamWorkers(ctx, sibling.GetPublicId(), nil))
		require.NoError(t, g.SetDownstreamWorkers(ctx, mid.GetPublicId(), []string{egress.GetPublicId()}))
		require.NoError(t, g.SetDownstreamWorkers(ctx, egress.GetPublicId(), nil))
		return g
	}

	egressOnly := fmt.Sprintf("%q matches %q", "/name", "egress")
	ingressOnly := fmt.Sprintf("%q matches %q", "/name", "ingress")

	t.Run("no egress filter is a single hop", func(t *testing.T) {
		route, err := multiHopConnectionRoute(ctx, ingress, newSession(t, ""), nil, repo, newGraph(t))
		require.NoError(t, err)
		assert.Equal(t, []string{ingress.GetPublicId()}, route)
	})

	t.Run("no graph is a single hop", func(t *testing.T) {
		route, err := multiHopConnectionRoute(ctx, ingress, newSession(t, egressOnly), nil, repo, nil)
		require.NoError(t, err)
		assert.Equal(t, []string{ingress.GetPublicId()}, route)
	})

	t.Run("requester matching the filter is a single hop", func(t *testing.T) {
		route, err := multiHopConnectionRoute(ctx, ingress, newSession(t, ingressOnly), nil, repo, newGraph(t))
		require.NoError(t, err)
		assert.Equal(t, []string{ingress.GetPublicId()}, route)
	})

	t.Run("route walks down to the egress worker", func(t *testing.T) {
		route, err := multiHopConnectionRoute(ctx, ingress, newSession(t, egressOnly), nil, repo, newGraph(t))
		require.NoError(t, err)
		assert.Equal(t, []string{ingress.GetPublicId(), mid.GetPublicId(), egress.GetPublicId()}, route)
	})

	t.Run("shortest matching route wins", func(t *testing.T) {
		// Two workers match the filter: one two hops away and one three hops
		// away.  The closest one must be picked.
		nearEgress := server.TestKmsWorker(t, conn, wrapper, server.WithName("egress-near"))
		g := newGraph(t)
		require.NoError(t, g.SetDownstreamWorkers(ctx, ingress.GetPublicId(), []string{mid.GetPublicId(), sibling.GetPublicId(), nearEgress.GetPublicId()}))
		require.NoError(t, g.SetDownstreamWorkers(ctx, nearEgress.GetPublicId(), nil))

		filter := fmt.Sprintf("%q matches %q", "/name", "egress.*")
		route, err := multiHopConnectionRoute(ctx, ingress, newSession(t, filter), nil, repo, g)
		require.NoError(t, err)
		assert.Equal(t, []string{ingress.GetPublicId(), nearEgress.GetPublicId()}, route)
	})

	t.Run("no matching egress worker is an error", func(t *testing.T) {
		filter := fmt.Sprintf("%q matches %q", "/name", "does-not-exist")
		_, err := multiHopConnectionRoute(ctx, ingress, newSession(t, filter), nil, repo, newGraph(t))
		require.Error(t, err)
	})

	t.Run("unreachable egress worker is an error", func(t *testing.T) {
		// egress exists and matches, but lives outside of the requesting
		// worker's subtree.
		g := common.NewDownstreamers("root")
		require.NoError(t, g.SetDownstreamWorkers(ctx, ingress.GetPublicId(), nil))
		require.NoError(t, g.SetDownstreamWorkers(ctx, sibling.GetPublicId(), []string{egress.GetPublicId()}))
		require.NoError(t, g.SetDownstreamWorkers(ctx, egress.GetPublicId(), nil))

		_, err := multiHopConnectionRoute(ctx, ingress, newSession(t, egressOnly), nil, repo, g)
		require.Error(t, err)
	})
}

// TestIngressFilterSelector pins the fallback used when a session carries no
// ingress filter, which is what keeps single hop deployments validating
// against the same filter they always have.
func TestIngressFilterSelector(t *testing.T) {
	tests := []struct {
		name string
		sess *session.Session
		want string
	}{
		{
			name: "ingress filter wins",
			sess: &session.Session{IngressWorkerFilter: "ingress", EgressWorkerFilter: "egress", WorkerFilter: "worker"},
			want: "ingress",
		},
		{
			name: "falls back to the egress filter",
			sess: &session.Session{EgressWorkerFilter: "egress", WorkerFilter: "worker"},
			want: "egress",
		},
		{
			name: "falls back to the deprecated worker filter",
			sess: &session.Session{WorkerFilter: "worker"},
			want: "worker",
		},
		{
			name: "no filters",
			sess: &session.Session{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ingressFilterSelector(tt.sess))
			assert.Equal(t, tt.want, workerFilterSelectionFn(tt.sess))
		})
	}
}
