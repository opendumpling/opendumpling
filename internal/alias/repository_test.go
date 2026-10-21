// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package alias

import (
	"context"
	"testing"

	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/target/tcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testScopeAndTarget(t *testing.T, conn *db.DB, iamRepo *iam.Repository) (org, prj *iam.Scope, targetId string) {
	t.Helper()
	org, prj = iam.TestScopes(t, iamRepo)
	tar := tcp.TestTarget(context.Background(), t, conn, prj.PublicId, "alias-dest-"+testId(t))
	return org, prj, tar.GetPublicId()
}

func TestRepository_CreateAlias(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	repo, iamRepo := TestRepoWithIam(t, conn, db.TestWrapper(t))
	org, prj, targetId := testScopeAndTarget(t, conn, iamRepo)
	id := testId(t)

	tests := []struct {
		name        string
		scopeId     string
		value       string
		destId      string
		wantErr     bool
		wantIsError errors.Code
	}{
		{
			name:    "valid-org",
			scopeId: org.PublicId,
			value:   "valid-org-" + id,
			destId:  targetId,
		},
		{
			name:    "valid-proj",
			scopeId: prj.PublicId,
			value:   "valid-proj-" + id,
			destId:  targetId,
		},
		{
			name:        "empty-scope",
			scopeId:     "",
			value:       "empty-" + id,
			destId:      targetId,
			wantErr:     true,
			wantIsError: errors.InvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			a, err := NewAlias(ctx, tt.scopeId, WithValue(tt.value), WithDestinationId(tt.destId))
			if tt.wantErr {
				require.Error(err)
				assert.Truef(errors.Match(errors.T(tt.wantIsError), err), "unexpected error %q", err)
				return
			}
			require.NoError(err)
			got, err := repo.CreateAlias(ctx, a)
			require.NoError(err)
			require.NotNil(got)
			assert.NotEmpty(got.GetPublicId())
			assert.Equal(tt.value, got.GetValue())
			assert.Equal(tt.destId, got.GetDestinationId())
		})
	}
}

func TestRepository_LookupAliasByValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	repo, iamRepo := TestRepoWithIam(t, conn, db.TestWrapper(t))
	org, prj, targetId := testScopeAndTarget(t, conn, iamRepo)

	// Same value in two different scopes must resolve independently.
	value := "shared-" + testId(t)
	orgAlias, err := NewAlias(ctx, org.PublicId, WithValue(value), WithDestinationId(targetId))
	require.NoError(t, err)
	created, err := repo.CreateAlias(ctx, orgAlias)
	require.NoError(t, err)

	prjAlias, err := NewAlias(ctx, prj.PublicId, WithValue(value), WithDestinationId(targetId))
	require.NoError(t, err)
	_, err = repo.CreateAlias(ctx, prjAlias)
	require.NoError(t, err)

	got, err := repo.LookupAliasByValue(ctx, org.PublicId, value)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, created.GetPublicId(), got.GetPublicId())

	// A duplicate value in the same scope must fail.
	dup, err := NewAlias(ctx, org.PublicId, WithValue(value), WithDestinationId(targetId))
	require.NoError(t, err)
	_, err = repo.CreateAlias(ctx, dup)
	require.Error(t, err)
}

func TestRepository_DeleteAlias(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	repo, iamRepo := TestRepoWithIam(t, conn, db.TestWrapper(t))
	org, _, targetId := testScopeAndTarget(t, conn, iamRepo)

	a, err := NewAlias(ctx, org.PublicId, WithValue("delete-"+testId(t)), WithDestinationId(targetId))
	require.NoError(t, err)
	created, err := repo.CreateAlias(ctx, a)
	require.NoError(t, err)

	deleted, err := repo.DeleteAlias(ctx, created.GetPublicId())
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	got, err := repo.LookupAlias(ctx, created.GetPublicId())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestRepository_ListAliases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	repo, iamRepo := TestRepoWithIam(t, conn, db.TestWrapper(t))
	org, _, targetId := testScopeAndTarget(t, conn, iamRepo)

	for i := 0; i < 3; i++ {
		a, err := NewAlias(ctx, org.PublicId, WithValue("list-"+testId(t)), WithDestinationId(targetId))
		require.NoError(t, err)
		_, err = repo.CreateAlias(ctx, a)
		require.NoError(t, err)
	}

	aliases, err := repo.ListAliases(ctx, []string{org.PublicId})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(aliases), 3)
}
