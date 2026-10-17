// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ssh_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
	wrapping "github.com/hashicorp/go-kms-wrapping/v2"
	"github.com/opendumpling/opendumpling/globals"
	"github.com/opendumpling/opendumpling/internal/authtoken"
	credstatic "github.com/opendumpling/opendumpling/internal/credential/static"
	"github.com/opendumpling/opendumpling/internal/credential/vault"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/auth"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/handlers"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/handlers/targets"
	"github.com/opendumpling/opendumpling/internal/db"
	pbs "github.com/opendumpling/opendumpling/internal/gen/controller/api/services"
	authpb "github.com/opendumpling/opendumpling/internal/gen/controller/auth"
	hostplugin "github.com/opendumpling/opendumpling/internal/host/plugin"
	"github.com/opendumpling/opendumpling/internal/host/static"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/kms"
	"github.com/opendumpling/opendumpling/internal/requests"
	"github.com/opendumpling/opendumpling/internal/scheduler"
	"github.com/opendumpling/opendumpling/internal/server"
	"github.com/opendumpling/opendumpling/internal/session"
	"github.com/opendumpling/opendumpling/internal/target"
	sshtarget "github.com/opendumpling/opendumpling/internal/target/ssh"
	"github.com/opendumpling/opendumpling/internal/types/scope"
	"github.com/opendumpling/opendumpling/sdk/pbs/controller/api/resources/scopes"
	pb "github.com/opendumpling/opendumpling/sdk/pbs/controller/api/resources/targets"
	plgpb "github.com/opendumpling/opendumpling/sdk/pbs/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/protobuf/field_mask"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	_ "github.com/opendumpling/opendumpling/internal/daemon/controller/handlers/targets/ssh"
)

var testAuthorizedActions = []string{
	"no-op",
	"read",
	"update",
	"delete",
	"add-host-sources",
	"set-host-sources",
	"remove-host-sources",
	"add-credential-sources",
	"set-credential-sources",
	"remove-credential-sources",
	"authorize-session",
}

func testService(t *testing.T, ctx context.Context, conn *db.DB, kms *kms.Kms, wrapper wrapping.Wrapper) (targets.Service, error) {
	rw := db.New(conn)
	sche := scheduler.TestScheduler(t, conn, wrapper)
	statusGracePeriod := new(atomic.Int64)
	statusGracePeriod.Store(int64(server.DefaultLiveness))
	repoFn := func(o ...target.Option) (*target.Repository, error) {
		return target.NewRepository(ctx, rw, rw, kms, o...)
	}
	iamRepoFn := func() (*iam.Repository, error) {
		return iam.TestRepo(t, conn, wrapper), nil
	}
	serversRepoFn := func() (*server.Repository, error) {
		return server.NewRepository(ctx, rw, rw, kms)
	}
	sessionRepoFn := func(opts ...session.Option) (*session.Repository, error) {
		return session.NewRepository(ctx, rw, rw, kms, opts...)
	}
	staticHostRepoFn := func() (*static.Repository, error) {
		return static.NewRepository(ctx, rw, rw, kms)
	}
	pluginHostRepoFn := func() (*hostplugin.Repository, error) {
		return hostplugin.NewRepository(ctx, rw, rw, kms, sche, map[string]plgpb.HostPluginServiceClient{})
	}
	vaultCredRepoFn := func() (*vault.Repository, error) {
		return vault.NewRepository(ctx, rw, rw, kms, sche)
	}
	staticCredRepoFn := func() (*credstatic.Repository, error) {
		return credstatic.NewRepository(context.Background(), rw, rw, kms)
	}
	return targets.NewService(ctx, kms, repoFn, iamRepoFn, serversRepoFn, sessionRepoFn, pluginHostRepoFn, staticHostRepoFn, vaultCredRepoFn, staticCredRepoFn, nil, statusGracePeriod, nil)
}

func TestCreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	wrapper := db.TestWrapper(t)
	kms := kms.TestKms(t, conn, wrapper)

	rw := db.New(conn)

	iamRepo := iam.TestRepo(t, conn, wrapper)
	iamRepoFn := func() (*iam.Repository, error) {
		return iamRepo, nil
	}
	tokenRepoFn := func() (*authtoken.Repository, error) {
		return authtoken.NewRepository(ctx, rw, rw, kms)
	}
	serversRepoFn := func() (*server.Repository, error) {
		return server.NewRepository(ctx, rw, rw, kms)
	}

	org, proj := iam.TestScopes(t, iamRepo)
	at := authtoken.TestAuthToken(t, conn, kms, org.GetPublicId())
	r := iam.TestRole(t, conn, proj.GetPublicId())
	_ = iam.TestUserRole(t, conn, r.GetPublicId(), at.GetIamUserId())
	_ = iam.TestRoleGrant(t, conn, r.GetPublicId(), "id=*;type=*;actions=*")

	cases := []struct {
		name   string
		req    *pbs.CreateTargetRequest
		res    *pbs.CreateTargetResponse
		err    error
		errStr string
	}{
		{
			name: "Create a valid ssh target",
			req: &pbs.CreateTargetRequest{Item: &pb.Target{
				ScopeId:     proj.GetPublicId(),
				Name:        wrapperspb.String("name"),
				Description: wrapperspb.String("desc"),
				Type:        sshtarget.Subtype.String(),
				Attrs: &pb.Target_SshTargetAttributes{
					SshTargetAttributes: &pb.SshTargetAttributes{
						DefaultPort:       wrapperspb.UInt32(2222),
						DefaultClientPort: wrapperspb.UInt32(3),
					},
				},
				EgressWorkerFilter: wrapperspb.String(`type == "bar"`),
			}},
			res: &pbs.CreateTargetResponse{
				Uri: fmt.Sprintf("targets/%s_", globals.SshTargetPrefix),
				Item: &pb.Target{
					ScopeId:     proj.GetPublicId(),
					Scope:       &scopes.ScopeInfo{Id: proj.GetPublicId(), Type: scope.Project.String(), ParentScopeId: org.GetPublicId()},
					Name:        wrapperspb.String("name"),
					Description: wrapperspb.String("desc"),
					Type:        sshtarget.Subtype.String(),
					Attrs: &pb.Target_SshTargetAttributes{
						SshTargetAttributes: &pb.SshTargetAttributes{
							DefaultPort:       wrapperspb.UInt32(2222),
							DefaultClientPort: wrapperspb.UInt32(3),
						},
					},
					SessionMaxSeconds:      wrapperspb.UInt32(28800),
					SessionConnectionLimit: wrapperspb.Int32(-1),
					AuthorizedActions:      testAuthorizedActions,
					EgressWorkerFilter:     wrapperspb.String(`type == "bar"`),
					Address:                &wrapperspb.StringValue{},
				},
			},
		},
		{
			name: "Create an ssh target with no port defaults to 22",
			req: &pbs.CreateTargetRequest{Item: &pb.Target{
				ScopeId: proj.GetPublicId(),
				Name:    wrapperspb.String("no-port"),
				Type:    sshtarget.Subtype.String(),
			}},
			res: &pbs.CreateTargetResponse{
				Uri: fmt.Sprintf("targets/%s_", globals.SshTargetPrefix),
				Item: &pb.Target{
					ScopeId: proj.GetPublicId(),
					Scope:   &scopes.ScopeInfo{Id: proj.GetPublicId(), Type: scope.Project.String(), ParentScopeId: org.GetPublicId()},
					Name:    wrapperspb.String("no-port"),
					Type:    sshtarget.Subtype.String(),
					Attrs: &pb.Target_SshTargetAttributes{
						SshTargetAttributes: &pb.SshTargetAttributes{
							DefaultPort: wrapperspb.UInt32(22),
						},
					},
					SessionMaxSeconds:      wrapperspb.UInt32(28800),
					SessionConnectionLimit: wrapperspb.Int32(-1),
					AuthorizedActions:      testAuthorizedActions,
					Address:                &wrapperspb.StringValue{},
				},
			},
		},
		{
			name: "Create with default port 0",
			req: &pbs.CreateTargetRequest{Item: &pb.Target{
				ScopeId: proj.GetPublicId(),
				Name:    wrapperspb.String("name"),
				Type:    sshtarget.Subtype.String(),
				Attrs: &pb.Target_SshTargetAttributes{
					SshTargetAttributes: &pb.SshTargetAttributes{
						DefaultPort: wrapperspb.UInt32(0),
					},
				},
			}},
			err: handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
		{
			name: "Create with unknown type",
			req: &pbs.CreateTargetRequest{Item: &pb.Target{
				Name:        wrapperspb.String("name"),
				Description: wrapperspb.String("desc"),
				Type:        "ThisIsMadeUp",
			}},
			err: handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
		{
			name: "Create with no type",
			req: &pbs.CreateTargetRequest{Item: &pb.Target{
				ScopeId:     proj.GetPublicId(),
				Name:        wrapperspb.String("name"),
				Description: wrapperspb.String("desc"),
			}},
			err: handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
		{
			name: "Can't specify Id",
			req: &pbs.CreateTargetRequest{Item: &pb.Target{
				Id: "not allowed to be set",
			}},
			res: nil,
			err: handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
		{
			name: "Can't specify Created Time",
			req: &pbs.CreateTargetRequest{Item: &pb.Target{
				CreatedTime: timestamppb.Now(),
			}},
			res: nil,
			err: handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
		{
			name: "Can't specify Update Time",
			req: &pbs.CreateTargetRequest{Item: &pb.Target{
				UpdatedTime: timestamppb.Now(),
			}},
			res: nil,
			err: handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)

			s, err := testService(t, context.Background(), conn, kms, wrapper)
			require.NoError(err, "Failed to create a new target service.")

			requestInfo := authpb.RequestInfo{
				TokenFormat: uint32(auth.AuthTokenTypeBearer),
				PublicId:    at.GetPublicId(),
				Token:       at.GetToken(),
			}
			requestContext := context.WithValue(context.Background(), requests.ContextRequestInformationKey, &requests.RequestContext{})
			ctx := auth.NewVerifierContext(requestContext, iamRepoFn, tokenRepoFn, serversRepoFn, kms, &requestInfo)

			got, gErr := s.CreateTarget(ctx, tc.req)
			if tc.err != nil {
				require.Error(gErr)
				assert.True(errors.Is(gErr, tc.err), "CreateTarget(%+v) got error %v, wanted %v", tc.req, gErr, tc.err)
				if tc.errStr != "" {
					require.ErrorContains(gErr, tc.errStr)
				}
				return
			}
			require.NoError(gErr, "Unexpected error: %v", gErr)

			assert.Contains(got.GetUri(), tc.res.GetUri())
			assert.True(strings.HasPrefix(got.GetItem().GetId(), globals.SshTargetPrefix), got.GetItem().GetId())

			// Clear all values which are hard to compare against.
			got.Uri, tc.res.Uri = "", ""
			got.Item.Id, tc.res.Item.Id = "", ""
			got.Item.CreatedTime, got.Item.UpdatedTime, tc.res.Item.CreatedTime, tc.res.Item.UpdatedTime = nil, nil, nil, nil
			tc.res.Item.Version = 1
			assert.Empty(cmp.Diff(got, tc.res, protocmp.Transform()), "CreateTarget(%q)\n got response %q\n, wanted %q\n", tc.req, got, tc.res)
		})
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	wrapper := db.TestWrapper(t)
	kms := kms.TestKms(t, conn, wrapper)

	rw := db.New(conn)

	iamRepo := iam.TestRepo(t, conn, wrapper)
	iamRepoFn := func() (*iam.Repository, error) {
		return iamRepo, nil
	}
	tokenRepoFn := func() (*authtoken.Repository, error) {
		return authtoken.NewRepository(ctx, rw, rw, kms)
	}
	serversRepoFn := func() (*server.Repository, error) {
		return server.NewRepository(ctx, rw, rw, kms)
	}

	o, proj := iam.TestScopes(t, iamRepo)
	at := authtoken.TestAuthToken(t, conn, kms, o.GetPublicId())
	r := iam.TestRole(t, conn, proj.GetPublicId())
	_ = iam.TestUserRole(t, conn, r.GetPublicId(), at.GetIamUserId())
	_ = iam.TestRoleGrant(t, conn, r.GetPublicId(), "id=*;type=*;actions=*")

	tar := sshtarget.TestTarget(ctx, t, conn, proj.GetPublicId(), "test")

	want := &pb.Target{
		Id:      tar.GetPublicId(),
		ScopeId: proj.GetPublicId(),
		Name:    wrapperspb.String("test"),
		Scope:   &scopes.ScopeInfo{Id: proj.GetPublicId(), Type: scope.Project.String(), ParentScopeId: o.GetPublicId()},
		Type:    sshtarget.Subtype.String(),
		Attrs: &pb.Target_SshTargetAttributes{
			SshTargetAttributes: &pb.SshTargetAttributes{
				DefaultPort: wrapperspb.UInt32(22),
			},
		},
		CreatedTime:            tar.GetCreateTime().GetTimestamp(),
		UpdatedTime:            tar.GetUpdateTime().GetTimestamp(),
		SessionMaxSeconds:      wrapperspb.UInt32(28800),
		SessionConnectionLimit: wrapperspb.Int32(-1),
		AuthorizedActions:      testAuthorizedActions,
		Address:                &wrapperspb.StringValue{},
	}

	cases := []struct {
		name string
		req  *pbs.GetTargetRequest
		res  *pbs.GetTargetResponse
		err  error
	}{
		{
			name: "Get an Existing Target",
			req:  &pbs.GetTargetRequest{Id: tar.GetPublicId()},
			res:  &pbs.GetTargetResponse{Item: want},
		},
		{
			name: "Get a non existing Target",
			req:  &pbs.GetTargetRequest{Id: globals.SshTargetPrefix + "_DoesntExis"},
			err:  handlers.ApiErrorWithCode(codes.NotFound),
		},
		{
			name: "Wrong id prefix",
			req:  &pbs.GetTargetRequest{Id: "j_1234567890"},
			err:  handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)

			s, err := testService(t, ctx, conn, kms, wrapper)
			require.NoError(err, "Couldn't create a new target service.")

			requestInfo := authpb.RequestInfo{
				TokenFormat: uint32(auth.AuthTokenTypeBearer),
				PublicId:    at.GetPublicId(),
				Token:       at.GetToken(),
			}
			requestContext := context.WithValue(context.Background(), requests.ContextRequestInformationKey, &requests.RequestContext{})
			ctx := auth.NewVerifierContext(requestContext, iamRepoFn, tokenRepoFn, serversRepoFn, kms, &requestInfo)
			got, gErr := s.GetTarget(ctx, tc.req)
			if tc.err != nil {
				require.Error(gErr)
				assert.True(errors.Is(gErr, tc.err), "GetTarget(%+v) got error %v, wanted %v", tc.req, gErr, tc.err)
				return
			}
			require.NoError(gErr)
			if tc.res != nil {
				tc.res.Item.Version = 1
			}
			assert.Empty(cmp.Diff(got, tc.res, protocmp.Transform()), "GetTarget(%q) got response %q, wanted %q", tc.req, got, tc.res)
		})
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	wrapper := db.TestWrapper(t)
	kms := kms.TestKms(t, conn, wrapper)

	rw := db.New(conn)

	iamRepo := iam.TestRepo(t, conn, wrapper)
	iamRepoFn := func() (*iam.Repository, error) {
		return iamRepo, nil
	}
	tokenRepoFn := func() (*authtoken.Repository, error) {
		return authtoken.NewRepository(ctx, rw, rw, kms)
	}
	serversRepoFn := func() (*server.Repository, error) {
		return server.NewRepository(ctx, rw, rw, kms)
	}

	o, proj := iam.TestScopes(t, iamRepo)
	at := authtoken.TestAuthToken(t, conn, kms, o.GetPublicId())
	r := iam.TestRole(t, conn, proj.GetPublicId())
	_ = iam.TestUserRole(t, conn, r.GetPublicId(), at.GetIamUserId())
	_ = iam.TestRoleGrant(t, conn, r.GetPublicId(), "id=*;type=*;actions=*")

	tar := sshtarget.TestTarget(ctx, t, conn, proj.GetPublicId(), "test")

	version := tar.GetVersion()
	cases := []struct {
		name string
		req  *pbs.UpdateTargetRequest
		res  *pbs.UpdateTargetResponse
		err  error
	}{
		{
			name: "Update name and default port",
			req: &pbs.UpdateTargetRequest{
				Id: tar.GetPublicId(),
				Item: &pb.Target{
					Name: wrapperspb.String("new name"),
					Attrs: &pb.Target_SshTargetAttributes{
						SshTargetAttributes: &pb.SshTargetAttributes{
							DefaultPort: wrapperspb.UInt32(2222),
						},
					},
					Version: version,
				},
				UpdateMask: &field_mask.FieldMask{Paths: []string{"name", "attributes.default_port"}},
			},
			res: &pbs.UpdateTargetResponse{
				Item: &pb.Target{
					Id:      tar.GetPublicId(),
					ScopeId: proj.GetPublicId(),
					Name:    wrapperspb.String("new name"),
					Scope:   &scopes.ScopeInfo{Id: proj.GetPublicId(), Type: scope.Project.String(), ParentScopeId: o.GetPublicId()},
					Type:    sshtarget.Subtype.String(),
					Attrs: &pb.Target_SshTargetAttributes{
						SshTargetAttributes: &pb.SshTargetAttributes{
							DefaultPort: wrapperspb.UInt32(2222),
						},
					},
					CreatedTime:            tar.GetCreateTime().GetTimestamp(),
					SessionMaxSeconds:      wrapperspb.UInt32(28800),
					SessionConnectionLimit: wrapperspb.Int32(-1),
					AuthorizedActions:      testAuthorizedActions,
					Address:                &wrapperspb.StringValue{},
				},
			},
		},
		{
			name: "Update default port to zero is rejected",
			req: &pbs.UpdateTargetRequest{
				Id: tar.GetPublicId(),
				Item: &pb.Target{
					Attrs: &pb.Target_SshTargetAttributes{
						SshTargetAttributes: &pb.SshTargetAttributes{
							DefaultPort: wrapperspb.UInt32(0),
						},
					},
					Version: version,
				},
				UpdateMask: &field_mask.FieldMask{Paths: []string{"attributes.default_port"}},
			},
			err: handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)

			s, err := testService(t, ctx, conn, kms, wrapper)
			require.NoError(err, "Couldn't create a new target service.")

			requestInfo := authpb.RequestInfo{
				TokenFormat: uint32(auth.AuthTokenTypeBearer),
				PublicId:    at.GetPublicId(),
				Token:       at.GetToken(),
			}
			requestContext := context.WithValue(context.Background(), requests.ContextRequestInformationKey, &requests.RequestContext{})
			ctx := auth.NewVerifierContext(requestContext, iamRepoFn, tokenRepoFn, serversRepoFn, kms, &requestInfo)

			got, gErr := s.UpdateTarget(ctx, tc.req)
			if tc.err != nil {
				require.Error(gErr)
				assert.True(errors.Is(gErr, tc.err), "UpdateTarget(%+v) got error %v, wanted %v", tc.req, gErr, tc.err)
				return
			}
			require.NoError(gErr)
			got.Item.UpdatedTime, tc.res.Item.UpdatedTime = nil, nil
			tc.res.Item.Version = got.Item.GetVersion()
			assert.Empty(cmp.Diff(got, tc.res, protocmp.Transform()), "UpdateTarget(%q) got response %q, wanted %q", tc.req, got, tc.res)
		})
	}
}
