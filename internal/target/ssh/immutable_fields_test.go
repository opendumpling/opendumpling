// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ssh_test

import (
	"context"
	"testing"

	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/db/timestamp"
	"github.com/opendumpling/opendumpling/internal/host/static"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/kms"
	"github.com/opendumpling/opendumpling/internal/target"
	"github.com/opendumpling/opendumpling/internal/target/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTarget_ImmutableFields(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	wrapper := db.TestWrapper(t)
	rw := db.New(conn)

	ts := timestamp.Timestamp{Timestamp: &timestamppb.Timestamp{Seconds: 0, Nanos: 0}}

	_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
	ctx := context.Background()
	new := ssh.TestTarget(ctx, t, conn, proj.PublicId, ssh.TestId(t))

	tests := []struct {
		name      string
		update    *ssh.Target
		fieldMask []string
	}{
		{
			name: "public_id",
			update: func() *ssh.Target {
				target := new.Clone().(*ssh.Target)
				target.PublicId = "p_thisIsNotAValidId"
				return target
			}(),
			fieldMask: []string{"PublicId"},
		},
		{
			name: "create time",
			update: func() *ssh.Target {
				target := new.Clone().(*ssh.Target)
				target.CreateTime = &ts
				return target
			}(),
			fieldMask: []string{"CreateTime"},
		},
		{
			name: "project_id",
			update: func() *ssh.Target {
				target := new.Clone().(*ssh.Target)
				target.ProjectId = "o_thisIsNotAValidId"
				return target
			}(),
			fieldMask: []string{"ProjectId"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)

			orig := new.Clone()
			orig.(*ssh.Target).SetTableName("target")
			err := rw.LookupById(context.Background(), orig)
			require.NoError(err)

			tt.update.SetTableName("target")
			rowsUpdated, err := rw.Update(context.Background(), tt.update, tt.fieldMask, nil, db.WithSkipVetForWrite(true))
			require.Error(err)
			assert.Equal(0, rowsUpdated)

			after := new.Clone()
			after.(*ssh.Target).SetTableName("target")
			err = rw.LookupById(context.Background(), after)
			require.NoError(err)

			assert.True(proto.Equal(orig.(*ssh.Target), after.(*ssh.Target)))
		})
	}
}

func TestTargetHostSet_ImmutableFields(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	wrapper := db.TestWrapper(t)
	testKms := kms.TestKms(t, conn, wrapper)

	ctx := context.Background()
	rw := db.New(conn)
	repo, err := target.NewRepository(ctx, rw, rw, testKms)
	require.NoError(t, err)

	ts := timestamp.Timestamp{Timestamp: &timestamppb.Timestamp{Seconds: 0, Nanos: 0}}

	_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
	projTarget := ssh.TestTarget(ctx, t, conn, proj.PublicId, ssh.TestId(t))
	testCats := static.TestCatalogs(t, conn, proj.PublicId, 1)
	hsets := static.TestSets(t, conn, testCats[0].GetPublicId(), 2)
	require.Equal(t, 2, len(hsets))

	updateTarget := ssh.TestTarget(ctx, t, conn, proj.PublicId, ssh.TestId(t))
	updateHset := hsets[1]

	gotTarget, err := repo.AddTargetHostSources(ctx, projTarget.GetPublicId(), 1, []string{hsets[0].PublicId})
	gotHostSources := gotTarget.GetHostSources()
	require.NoError(t, err)
	require.Equal(t, 1, len(gotHostSources))
	new, err := target.NewTargetHostSet(ctx, projTarget.GetPublicId(), gotHostSources[0].Id())
	require.NoError(t, err)

	tests := []struct {
		name      string
		update    *target.TargetHostSet
		fieldMask []string
	}{
		{
			name: "target_id",
			update: func() *target.TargetHostSet {
				target := new.Clone().(*target.TargetHostSet)
				target.TargetId = updateTarget.GetPublicId()
				return target
			}(),
			fieldMask: []string{"TargetId"},
		},
		{
			name: "create time",
			update: func() *target.TargetHostSet {
				target := new.Clone().(*target.TargetHostSet)
				target.CreateTime = &ts
				return target
			}(),
			fieldMask: []string{"CreateTime"},
		},
		{
			name: "host_set_id",
			update: func() *target.TargetHostSet {
				target := new.Clone().(*target.TargetHostSet)
				target.HostSetId = updateHset.PublicId
				return target
			}(),
			fieldMask: []string{"HostSetId"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			orig := new.Clone()
			err := rw.LookupWhere(context.Background(), orig, "target_id = ? and host_set_id = ?", []any{new.TargetId, new.HostSetId})
			require.NoError(err)

			rowsUpdated, err := rw.Update(context.Background(), tt.update, tt.fieldMask, nil, db.WithSkipVetForWrite(true))
			require.Error(err)
			assert.Equal(0, rowsUpdated)

			after := new.Clone()
			err = rw.LookupWhere(context.Background(), after, "target_id = ? and host_set_id = ?", []any{new.TargetId, new.HostSetId})
			require.NoError(err)
			assert.True(proto.Equal(orig.(*target.TargetHostSet), after.(*target.TargetHostSet)))
		})
	}
}
