// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package alias

import (
	"context"
	"testing"

	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/kms"
	"github.com/opendumpling/opendumpling/internal/types/scope"
	wrapping "github.com/hashicorp/go-kms-wrapping/v2"
	"github.com/hashicorp/go-uuid"
	"github.com/stretchr/testify/require"
)

// TestRepo creates a new alias repository backed by the provided database and
// root wrapper, along with the iam repository that shares its kms. Sharing the
// kms means tests only create the global scope keys once.
func TestRepo(t testing.TB, conn *db.DB, rootWrapper wrapping.Wrapper) *Repository {
	t.Helper()
	repo, _ := TestRepoWithIam(t, conn, rootWrapper)
	return repo
}

// TestRepoWithIam is like TestRepo but also returns the iam repository built on
// the same kms cache.
func TestRepoWithIam(t testing.TB, conn *db.DB, rootWrapper wrapping.Wrapper) (*Repository, *iam.Repository) {
	t.Helper()
	ctx := context.Background()
	require := require.New(t)
	rw := db.New(conn)
	kmsCache := kms.TestKms(t, conn, rootWrapper)
	wrapper, err := kmsCache.GetWrapper(ctx, scope.Global.String(), kms.KeyPurposeOplog)
	if err != nil {
		require.NoError(kmsCache.CreateKeys(ctx, scope.Global.String()))
		wrapper, err = kmsCache.GetWrapper(ctx, scope.Global.String(), kms.KeyPurposeOplog)
		require.NoError(err)
	}
	require.NotNil(wrapper)

	repo, err := NewRepository(ctx, rw, rw, kmsCache)
	require.NoError(err)
	iamRepo, err := iam.NewRepository(ctx, rw, rw, kmsCache)
	require.NoError(err)
	return repo, iamRepo
}

func testId(t testing.TB) string {
	t.Helper()
	id, err := uuid.GenerateUUID()
	require.NoError(t, err)
	return id
}
