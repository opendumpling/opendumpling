// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ssh_test

import (
	"context"
	"testing"
	"time"

	"github.com/opendumpling/opendumpling/globals"
	"github.com/opendumpling/opendumpling/internal/credential"
	"github.com/opendumpling/opendumpling/internal/db"
	dbassert "github.com/opendumpling/opendumpling/internal/db/assert"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/oplog"
	"github.com/opendumpling/opendumpling/internal/target"
	"github.com/opendumpling/opendumpling/internal/target/ssh"
	targetstore "github.com/opendumpling/opendumpling/internal/target/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestTarget_Create(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	wrapper := db.TestWrapper(t)
	_, prj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
	ctx := context.Background()
	type args struct {
		projectId string
		opt       []target.Option
	}
	tests := []struct {
		name          string
		args          args
		want          target.Target
		wantErr       bool
		wantIsErr     errors.Code
		create        bool
		wantCreateErr bool
	}{
		{
			name:      "empty-projectId",
			args:      args{},
			wantErr:   true,
			wantIsErr: errors.InvalidParameter,
		},
		{
			name: "valid-proj-id",
			args: args{
				projectId: prj.PublicId,
				opt:       []target.Option{target.WithName("valid-proj-id")},
			},
			want: func() target.Target {
				t, _ := target.New(
					ctx,
					ssh.Subtype,
					prj.PublicId,
					target.WithName("valid-proj-id"),
					target.WithSessionMaxSeconds(uint32((8 * time.Hour).Seconds())),
					target.WithSessionConnectionLimit(-1),
				)
				return t
			}(),
			create: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			got, err := target.New(ctx, ssh.Subtype, tt.args.projectId, tt.args.opt...)
			if tt.wantErr {
				require.Error(err)
				assert.True(errors.Match(errors.T(tt.wantIsErr), err))
				return
			}
			require.NoError(err)
			assert.Equal(tt.want, got)
			if tt.create {
				id, err := db.NewPublicId(ctx, globals.SshTargetPrefix)
				require.NoError(err)
				got.SetPublicId(ctx, id)
				err = db.New(conn).Create(ctx, got)
				if tt.wantCreateErr {
					assert.Error(err)
					return
				}

				assert.NoError(err)
			}
		})
	}
}

func TestTarget_DefaultPort(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name string
		opt  []target.Option
		want uint32
	}{
		{
			name: "defaults to 22",
			opt:  nil,
			want: ssh.DefaultSSHPort,
		},
		{
			name: "explicit port is preserved",
			opt:  []target.Option{target.WithDefaultPort(2222)},
			want: 2222,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			tar, err := target.New(ctx, ssh.Subtype, "testScope", tt.opt...)
			require.NoError(err)
			assert.Equal(tt.want, tar.GetDefaultPort())
		})
	}
}

func TestTarget_Delete(t *testing.T) {
	t.Parallel()
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
	ctx := context.Background()

	tests := []struct {
		name            string
		target          target.Target
		wantRowsDeleted int
		wantErr         bool
		wantErrMsg      string
	}{
		{
			name:            "valid",
			target:          ssh.TestTarget(ctx, t, conn, proj.PublicId, ssh.TestTargetName(t, proj.PublicId)),
			wantErr:         false,
			wantRowsDeleted: 1,
		},
		{
			name: "bad-id",
			target: func() target.Target {
				tar, _ := target.New(ctx, ssh.Subtype, proj.PublicId)

				id, err := db.NewPublicId(ctx, globals.SshTargetPrefix)
				require.NoError(t, err)
				tar.SetPublicId(ctx, id)
				tar.SetName(ssh.TestTargetName(t, proj.PublicId))
				return tar
			}(),
			wantErr:         false,
			wantRowsDeleted: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			deleteTarget := ssh.NewTestTarget(ctx, "")
			deleteTarget.SetPublicId(ctx, tt.target.GetPublicId())
			deletedRows, err := rw.Delete(context.Background(), deleteTarget)
			if tt.wantErr {
				require.Error(err)
				return
			}
			require.NoError(err)
			if tt.wantRowsDeleted == 0 {
				assert.Equal(tt.wantRowsDeleted, deletedRows)
				return
			}
			assert.Equal(tt.wantRowsDeleted, deletedRows)
			foundTarget := ssh.NewTestTarget(ctx, "")
			foundTarget.SetPublicId(ctx, tt.target.GetPublicId())
			err = rw.LookupById(context.Background(), foundTarget)
			require.Error(err)
			assert.True(errors.IsNotFoundError(err))
		})
	}
}

func TestTarget_Update(t *testing.T) {
	t.Parallel()
	id := ssh.TestId(t)
	conn, _ := db.TestSetup(t, "postgres")
	rw := db.New(conn)
	wrapper := db.TestWrapper(t)
	_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))

	type args struct {
		name           string
		description    string
		fieldMaskPaths []string
		nullPaths      []string
		ProjectId      string
	}
	tests := []struct {
		name           string
		args           args
		wantRowsUpdate int
		wantErr        bool
		wantErrMsg     string
		wantDup        bool
	}{
		{
			name: "valid",
			args: args{
				name:           "valid" + id,
				fieldMaskPaths: []string{"Name"},
				ProjectId:      proj.PublicId,
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
		{
			name: "proj-id-not-in-mask",
			args: args{
				name:           "proj-id" + id,
				fieldMaskPaths: []string{"Name"},
				ProjectId:      proj.PublicId,
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
		{
			name: "empty-project-id",
			args: args{
				name:           "empty-project-id" + id,
				fieldMaskPaths: []string{"Name"},
				ProjectId:      "",
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
		{
			name: "dup-name",
			args: args{
				name:           "dup-name" + id,
				fieldMaskPaths: []string{"Name"},
				ProjectId:      proj.PublicId,
			},
			wantErr:    true,
			wantDup:    true,
			wantErrMsg: `db.Update: duplicate key value violates unique constraint "target_ssh_project_id_name_uq": unique constraint violation: integrity violation: error #1002`,
		},
		{
			name: "set description null",
			args: args{
				name:           "set description null" + id,
				fieldMaskPaths: []string{"Name"},
				nullPaths:      []string{"Description"},
				ProjectId:      proj.PublicId,
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
		{
			name: "set name null",
			args: args{
				description:    "set description null" + id,
				fieldMaskPaths: []string{"Description"},
				nullPaths:      []string{"Name"},
				ProjectId:      proj.PublicId,
			},
			wantErr:    true,
			wantErrMsg: `db.Update: name must not be empty: not null constraint violated: integrity violation: error #1001`,
		},
		{
			name: "set description null",
			args: args{
				name:           "set name null" + id,
				fieldMaskPaths: []string{"Name"},
				nullPaths:      []string{"Description"},
				ProjectId:      proj.PublicId,
			},
			wantErr:        false,
			wantRowsUpdate: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			ctx := context.Background()
			if tt.wantDup {
				target := ssh.TestTarget(ctx, t, conn, proj.PublicId, ssh.TestTargetName(t, proj.PublicId))
				target.SetName(tt.args.name)
				_, err := rw.Update(ctx, target, tt.args.fieldMaskPaths, tt.args.nullPaths)
				require.NoError(err)
			}

			id := ssh.TestId(t)
			tar := ssh.TestTarget(ctx, t, conn, proj.PublicId, id, target.WithDescription(id))

			updateTarget := ssh.NewTestTarget(ctx, tt.args.ProjectId)
			updateTarget.SetPublicId(ctx, tar.GetPublicId())
			updateTarget.SetName(tt.args.name)
			updateTarget.SetDescription(tt.args.description)

			updatedRows, err := rw.Update(ctx, updateTarget, tt.args.fieldMaskPaths, tt.args.nullPaths)
			if tt.wantErr {
				require.Error(err)
				assert.Equal(0, updatedRows)
				assert.Equal(tt.wantErrMsg, err.Error())
				err = db.TestVerifyOplog(t, rw, tar.GetPublicId(), db.WithOperation(oplog.OpType_OP_TYPE_UPDATE), db.WithCreateNotBefore(10*time.Second))
				require.Error(err)
				assert.Contains(err.Error(), "record not found")
				return
			}
			require.NoError(err)
			assert.Equal(tt.wantRowsUpdate, updatedRows)
			assert.NotEqual(tar.GetUpdateTime(), updateTarget.GetUpdateTime())
			foundTarget := ssh.NewTestTarget(ctx, tt.args.ProjectId)
			foundTarget.SetPublicId(ctx, tar.GetPublicId())
			err = rw.LookupByPublicId(ctx, foundTarget)
			require.NoError(err)
			assert.True(proto.Equal(updateTarget.(*ssh.Target).Target, foundTarget.(*ssh.Target).Target))
			if len(tt.args.nullPaths) != 0 {
				underlyingDB, err := conn.SqlDB(ctx)
				require.NoError(err)
				dbassert := dbassert.New(t, underlyingDB)
				for _, f := range tt.args.nullPaths {
					ft := foundTarget.(*ssh.Target)
					dbassert.IsNull(&ft, f)
				}
			}
		})
	}
	t.Run("update dup names in diff projects", func(t *testing.T) {
		ctx := context.Background()
		assert, require := assert.New(t), require.New(t)
		id := ssh.TestId(t)
		_, proj2 := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
		_ = ssh.TestTarget(ctx, t, conn, proj2.PublicId, id, target.WithDescription(id))
		projTarget := ssh.TestTarget(ctx, t, conn, proj.PublicId, id)
		projTarget.SetName(id)
		updatedRows, err := rw.Update(ctx, projTarget, []string{"Name"}, nil)
		require.NoError(err)
		assert.Equal(1, updatedRows)

		foundTarget, _ := target.New(ctx, ssh.Subtype, proj2.PublicId)
		foundTarget.SetPublicId(ctx, projTarget.GetPublicId())
		err = rw.LookupByPublicId(ctx, foundTarget)
		require.NoError(err)
		assert.Equal(id, projTarget.GetName())
	})
}

func TestTarget_Clone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn, _ := db.TestSetup(t, "postgres")
	wrapper := db.TestWrapper(t)
	t.Run("valid", func(t *testing.T) {
		assert := assert.New(t)
		_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
		tar := ssh.TestTarget(ctx, t, conn, proj.PublicId, ssh.TestTargetName(t, proj.PublicId),
			target.WithAddress("8.8.8.8"),
		)
		cp := tar.Clone()
		assert.True(proto.Equal(cp.(*ssh.Target).Target, tar.(*ssh.Target).Target))
	})
	t.Run("not-equal", func(t *testing.T) {
		assert := assert.New(t)
		_, proj := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
		_, proj2 := iam.TestScopes(t, iam.TestRepo(t, conn, wrapper))
		target1 := ssh.TestTarget(ctx, t, conn, proj.PublicId, ssh.TestTargetName(t, proj.PublicId))
		target2 := ssh.TestTarget(ctx, t, conn, proj2.PublicId, ssh.TestTargetName(t, proj2.PublicId))

		cp := target1.Clone()
		assert.True(!proto.Equal(cp.(*ssh.Target).Target, target2.(*ssh.Target).Target))
	})
}

func TestTable_SetTableName(t *testing.T) {
	t.Parallel()
	defaultTableName := ssh.DefaultTableName
	ctx := context.Background()
	tests := []struct {
		name      string
		setNameTo string
		want      string
	}{
		{
			name:      "new-name",
			setNameTo: "new-name",
			want:      "new-name",
		},
		{
			name:      "reset to default",
			setNameTo: "",
			want:      defaultTableName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			def, _ := target.New(ctx, ssh.Subtype, "testScope")
			require.Equal(defaultTableName, def.(*ssh.Target).TableName())
			ss, _ := target.New(ctx, ssh.Subtype, "testScope")
			s := ss.(*ssh.Target)
			s.SetTableName(tt.setNameTo)
			assert.Equal(tt.want, s.TableName())
		})
	}
}

func TestTarget_oplog(t *testing.T) {
	ctx := context.Background()
	id := ssh.TestId(t)
	tests := []struct {
		name   string
		target target.Target
		op     oplog.OpType
		want   oplog.Metadata
	}{
		{
			name: "simple",
			target: func() target.Target {
				tar, _ := target.New(ctx, ssh.Subtype, id)
				if err := tar.SetPublicId(ctx, id); err != nil {
					t.Fatalf("failed to set public id: %s", err)
				}
				return tar
			}(),
			op: oplog.OpType_OP_TYPE_CREATE,
			want: oplog.Metadata{
				"resource-public-id": []string{id},
				"resource-type":      []string{"ssh target"},
				"op-type":            []string{oplog.OpType_OP_TYPE_CREATE.String()},
				"project-id":         []string{id},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			got := tt.target.Oplog(tt.op)
			assert.Equal(got, tt.want)
		})
	}
}

func TestVetCredentialSources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name    string
		purpose credential.Purpose
		wantErr bool
	}{
		{
			name:    "brokered is allowed",
			purpose: credential.BrokeredPurpose,
			wantErr: false,
		},
		{
			name:    "injected application is allowed",
			purpose: credential.InjectedApplicationPurpose,
			wantErr: false,
		},
		{
			name:    "other purpose is rejected",
			purpose: credential.Purpose("invalid"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			tar, err := target.New(ctx, ssh.Subtype, "testScope")
			require.NoError(err)
			src := &target.TargetCredentialSource{
				CredentialSource: &targetstore.CredentialSource{
					TargetId:           tar.GetPublicId(),
					CredentialSourceId: "clvlt_1234567890",
					CredentialPurpose:  string(tt.purpose),
					Type:               string(target.LibraryCredentialSourceType),
				},
			}
			err = target.VetCredentialSources(ctx, ssh.Subtype, []target.CredentialSource{src})
			if tt.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)
		})
	}
}
