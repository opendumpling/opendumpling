// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package ssh provides a Target subtype for a SSH Target.
// Importing this package will register it with the target package and
// allow the target.Repository to support ssh.Targets.
package ssh

import (
	"context"
	"fmt"
	"strings"

	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/db/timestamp"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/oplog"
	"github.com/opendumpling/opendumpling/internal/target"
	"github.com/opendumpling/opendumpling/internal/target/ssh/store"
	"github.com/opendumpling/opendumpling/internal/types/subtypes"
	"google.golang.org/protobuf/proto"
)

const (
	defaultTableName = "target_ssh"
	Subtype          = subtypes.Subtype("ssh")

	// DefaultSSHPort is applied to an SSH target when no default port is
	// specified.
	DefaultSSHPort = uint32(22)
)

// Target is a resource that represents an SSH networked service.
// It is a subtype of target.Target.
type Target struct {
	*store.Target
	Address           string                    `json:"address,omitempty" gorm:"-"`
	tableName         string                    `gorm:"-"`
	HostSource        []target.HostSource       `gorm:"-"`
	CredentialSources []target.CredentialSource `gorm:"-"`
}

// Ensure Target implements interfaces
var (
	_ target.Target           = (*Target)(nil)
	_ db.VetForWriter         = (*Target)(nil)
	_ oplog.ReplayableMessage = (*Target)(nil)
)

// AllocTarget will allocate an ssh target
func (h targetHooks) AllocTarget() target.Target {
	return &Target{
		Target: &store.Target{},
	}
}

// Clone creates a clone of the Target
func (t *Target) Clone() target.Target {
	cp := proto.Clone(t.Target)
	return &Target{
		Target:            cp.(*store.Target),
		Address:           t.Address,
		HostSource:        t.HostSource,
		CredentialSources: t.CredentialSources,
	}
}

// VetForWrite implements db.VetForWrite() interface and validates the ssh target
// before it's written.
func (t *Target) VetForWrite(ctx context.Context, _ db.Reader, opType db.OpType, _ ...db.Option) error {
	const op = "ssh.(Target).VetForWrite"
	if t.PublicId == "" {
		return errors.New(ctx, errors.InvalidParameter, op, "missing public id")
	}
	if opType == db.CreateOp {
		if t.ProjectId == "" {
			return errors.New(ctx, errors.InvalidParameter, op, "missing project id")
		}
		if t.Name == "" {
			return errors.New(ctx, errors.InvalidParameter, op, "missing name")
		}
	}
	return nil
}

func (t *Target) TableName() string {
	if t.tableName != "" {
		return t.tableName
	}
	return defaultTableName
}

func (t *Target) SetTableName(n string) {
	if n == "" {
		n = defaultTableName
	}
	t.tableName = n
}

func (t *Target) Oplog(op oplog.OpType) oplog.Metadata {
	metadata := oplog.Metadata{
		"resource-public-id": []string{t.PublicId},
		"resource-type":      []string{"ssh target"},
		"op-type":            []string{op.String()},
		"project-id":         []string{t.ProjectId},
	}
	return metadata
}

func (t *Target) GetType() subtypes.Subtype { return Subtype }

func (t *Target) GetAddress() string                  { return t.Address }
func (t *Target) GetHostSources() []target.HostSource { return t.HostSource }
func (t *Target) GetCredentialSources() []target.CredentialSource {
	return t.CredentialSources
}

func (t *Target) GetEnableSessionRecording() bool { return t.EnableSessionRecording }
func (t *Target) GetStorageBucketId() string      { return t.StorageBucketId }

func (t *Target) SetPublicId(ctx context.Context, publicId string) error {
	const op = "ssh.(Target).SetPublicId"
	if !strings.HasPrefix(publicId, TargetPrefix+"_") {
		return errors.New(ctx, errors.InvalidParameter, op, fmt.Sprintf("passed-in public ID %q has wrong prefix, should be %q", publicId, TargetPrefix))
	}
	t.PublicId = publicId
	return nil
}

func (t *Target) SetProjectId(projectId string) { t.ProjectId = projectId }
func (t *Target) SetName(name string)           { t.Name = name }
func (t *Target) SetDescription(description string) {
	t.Description = description
}
func (t *Target) SetVersion(v uint32)        { t.Version = v }
func (t *Target) SetDefaultPort(port uint32) { t.DefaultPort = port }
func (t *Target) SetDefaultClientPort(port uint32) {
	t.DefaultClientPort = port
}
func (t *Target) SetCreateTime(ts *timestamp.Timestamp) { t.CreateTime = ts }
func (t *Target) SetUpdateTime(ts *timestamp.Timestamp) { t.UpdateTime = ts }
func (t *Target) SetSessionMaxSeconds(s uint32)         { t.SessionMaxSeconds = s }
func (t *Target) SetSessionConnectionLimit(limit int32) { t.SessionConnectionLimit = limit }
func (t *Target) SetWorkerFilter(filter string)         { t.WorkerFilter = filter }
func (t *Target) SetEgressWorkerFilter(filter string)   { t.EgressWorkerFilter = filter }
func (t *Target) SetIngressWorkerFilter(filter string)  { t.IngressWorkerFilter = filter }
func (t *Target) SetAddress(address string)             { t.Address = address }
func (t *Target) SetHostSources(sources []target.HostSource) {
	t.HostSource = sources
}
func (t *Target) SetCredentialSources(sources []target.CredentialSource) {
	t.CredentialSources = sources
}
func (t *Target) SetEnableSessionRecording(enable bool) { t.EnableSessionRecording = enable }
func (t *Target) SetStorageBucketId(id string)          { t.StorageBucketId = id }
