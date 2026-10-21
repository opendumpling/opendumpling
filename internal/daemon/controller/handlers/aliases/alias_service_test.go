// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package aliases_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/opendumpling/opendumpling/globals"
	"github.com/opendumpling/opendumpling/internal/alias"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/auth"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/handlers"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/handlers/aliases"
	"github.com/opendumpling/opendumpling/internal/db"
	pbs "github.com/opendumpling/opendumpling/internal/gen/controller/api/services"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/target/tcp"
	"github.com/opendumpling/opendumpling/internal/types/scope"
	pb "github.com/opendumpling/opendumpling/sdk/pbs/controller/api/resources/aliases"
	"github.com/opendumpling/opendumpling/sdk/pbs/controller/api/resources/scopes"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testAuthorizedActions = []string{"no-op", "read", "update", "delete"}

func createDefaultAliasAndRepo(t *testing.T) (*alias.Alias, *alias.Alias, string, func() (*alias.Repository, error), func() (*iam.Repository, error)) {
	t.Helper()
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	wrap := db.TestWrapper(t)
	repo, iamRepo := alias.TestRepoWithIam(t, conn, wrap)
	aliasRepoFn := func() (*alias.Repository, error) {
		return repo, nil
	}
	iamRepoFn := func() (*iam.Repository, error) {
		return iamRepo, nil
	}
	o, p := iam.TestScopes(t, iamRepo)
	tar := tcp.TestTarget(ctx, t, conn, p.GetPublicId(), "alias-handler-dest")

	oa, err := alias.NewAlias(ctx, o.GetPublicId(), alias.WithName("default"), alias.WithValue("default-org"), alias.WithDestinationId(tar.GetPublicId()))
	require.NoError(t, err)
	oa, err = repo.CreateAlias(ctx, oa)
	require.NoError(t, err)

	pa, err := alias.NewAlias(ctx, p.GetPublicId(), alias.WithName("default"), alias.WithValue("default-proj"), alias.WithDestinationId(tar.GetPublicId()))
	require.NoError(t, err)
	pa, err = repo.CreateAlias(ctx, pa)
	require.NoError(t, err)

	return oa, pa, tar.GetPublicId(), aliasRepoFn, iamRepoFn
}

func TestGet(t *testing.T) {
	ctx := context.Background()
	oa, pa, destId, aliasRepoFn, iamRepoFn := createDefaultAliasAndRepo(t)

	wantOrg := &pb.Alias{
		Id:                oa.GetPublicId(),
		ScopeId:           oa.GetScopeId(),
		Scope:             &scopes.ScopeInfo{Id: oa.GetScopeId(), Type: scope.Org.String(), ParentScopeId: scope.Global.String()},
		Name:              &wrapperspb.StringValue{Value: oa.GetName()},
		Value:             &wrapperspb.StringValue{Value: oa.GetValue()},
		DestinationId:     &wrapperspb.StringValue{Value: destId},
		DestinationType:   "target",
		CreatedTime:       oa.GetCreateTime().GetTimestamp(),
		UpdatedTime:       oa.GetUpdateTime().GetTimestamp(),
		Version:           1,
		AuthorizedActions: testAuthorizedActions,
	}

	cases := []struct {
		name string
		req  *pbs.GetAliasRequest
		res  *pbs.GetAliasResponse
		err  error
	}{
		{
			name: "Get an existing alias",
			req:  &pbs.GetAliasRequest{Id: oa.GetPublicId()},
			res:  &pbs.GetAliasResponse{Item: wantOrg},
		},
		{
			name: "Get a non existent alias",
			req:  &pbs.GetAliasRequest{Id: globals.AliasPrefix + "_DoesntExis"},
			err:  handlers.ApiErrorWithCode(codes.NotFound),
		},
		{
			name: "Wrong id prefix",
			req:  &pbs.GetAliasRequest{Id: "j_1234567890"},
			err:  handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			s, err := aliases.NewService(ctx, aliasRepoFn, iamRepoFn)
			require.NoError(err, "Couldn't create new alias service.")

			got, gErr := s.GetAlias(auth.DisabledAuthTestContext(iamRepoFn, oa.GetScopeId()), tc.req)
			if tc.err != nil {
				require.Error(gErr)
				assert.True(errors.Is(gErr, tc.err), "GetAlias(%+v) got error %v, wanted %v", tc.req, gErr, tc.err)
			} else {
				require.NoError(gErr)
			}
			_ = pa
			assert.Empty(cmp.Diff(got, tc.res, protocmp.Transform()), "GetAlias(%q) got response\n%q, wanted\n%q", tc.req, got, tc.res)
		})
	}
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	_, _, destId, aliasRepoFn, iamRepoFn := createDefaultAliasAndRepo(t)

	cases := []struct {
		name string
		req  *pbs.CreateAliasRequest
		err  error
	}{
		{
			name: "Create a valid org alias",
			req: &pbs.CreateAliasRequest{Item: &pb.Alias{
				ScopeId:       scope.Global.String(),
				Name:          wrapperspb.String("created"),
				Value:         wrapperspb.String("created-value-org"),
				DestinationId: wrapperspb.String(destId),
			}},
		},
		{
			name: "Create with a missing value",
			req: &pbs.CreateAliasRequest{Item: &pb.Alias{
				ScopeId:       scope.Global.String(),
				DestinationId: wrapperspb.String(destId),
			}},
			err: handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
		{
			name: "Create with a bad destination",
			req: &pbs.CreateAliasRequest{Item: &pb.Alias{
				ScopeId:       scope.Global.String(),
				Value:         wrapperspb.String("created-value-bad"),
				DestinationId: wrapperspb.String("w_1234567890"),
			}},
			err: handlers.ApiErrorWithCode(codes.InvalidArgument),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			s, err := aliases.NewService(ctx, aliasRepoFn, iamRepoFn)
			require.NoError(err, "Couldn't create new alias service.")

			got, gErr := s.CreateAlias(auth.DisabledAuthTestContext(iamRepoFn, scope.Global.String()), tc.req)
			if tc.err != nil {
				require.Error(gErr)
				assert.Truef(errors.Is(gErr, tc.err), "CreateAlias(%+v) got error %v, wanted %v", tc.req, gErr, tc.err)
				return
			}
			require.NoError(gErr)
			require.NotNil(got)
			assert.NotEmpty(got.GetItem().GetId())
			assert.Equal(tc.req.GetItem().GetValue().GetValue(), got.GetItem().GetValue().GetValue())
			assert.Equal(tc.req.GetItem().GetDestinationId().GetValue(), got.GetItem().GetDestinationId().GetValue())
		})
	}
}
