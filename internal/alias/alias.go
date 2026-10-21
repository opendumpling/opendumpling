// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package alias

import (
	"context"

	"github.com/opendumpling/opendumpling/internal/alias/store"
	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/types/action"
	"github.com/opendumpling/opendumpling/internal/types/resource"
	"google.golang.org/protobuf/proto"
)

const (
	defaultAliasTableName = "alias"
)

// Alias points a DNS-like value at a destination resource, which is a target.
// It exists so a user can reach a target with a short, memorable string
// instead of its public id.
type Alias struct {
	*store.Alias
	tableName string `gorm:"-"`
}

// ensure that Alias implements the interfaces of: iam.Resource, iam.Cloneable,
// and db.VetForWriter.
var (
	_ iam.Resource    = (*Alias)(nil)
	_ iam.Cloneable   = (*Alias)(nil)
	_ db.VetForWriter = (*Alias)(nil)
)

// NewAlias creates a new in memory alias in the provided scope. Supported
// options are: WithValue, WithDestinationId, WithDescription.
func NewAlias(ctx context.Context, scopeId string, opt ...Option) (*Alias, error) {
	const op = "alias.NewAlias"
	if scopeId == "" {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing scope id")
	}
	opts := getOpts(opt...)
	a := &Alias{
		Alias: &store.Alias{
			ScopeId:       scopeId,
			Name:          opts.withName,
			Value:         opts.withValue,
			Description:   opts.withDescription,
			DestinationId: opts.withDestinationId,
		},
	}
	return a, nil
}

// Clone creates a clone of the Alias.
func (a *Alias) Clone() any {
	cp := proto.Clone(a.Alias)
	return &Alias{
		Alias: cp.(*store.Alias),
	}
}

func allocAlias() Alias {
	return Alias{
		Alias: &store.Alias{},
	}
}

// VetForWrite implements db.VetForWrite() and validates the alias before it is
// written.
func (a *Alias) VetForWrite(ctx context.Context, r db.Reader, opType db.OpType, opt ...db.Option) error {
	const op = "alias.(Alias).VetForWrite"
	if a.PublicId == "" {
		return errors.New(ctx, errors.InvalidParameter, op, "missing public id")
	}
	if err := validateScopeForWrite(ctx, r, a, opType, opt...); err != nil {
		return errors.Wrap(ctx, err, op)
	}
	return nil
}

// GetName returns the alias value. iam.Resource requires a GetName; for an
// alias the value is the closest equivalent of a name and is what the CLI
// resolves.
func (a *Alias) GetName() string { return a.GetValue() }

// GetScope returns the scope for the Alias.
func (a *Alias) GetScope(ctx context.Context, r db.Reader) (*iam.Scope, error) {
	return lookupScope(ctx, r, a)
}

// ResourceType returns the type of the Alias.
func (*Alias) ResourceType() resource.Type { return resource.Alias }

// Actions returns the available actions for Alias.
func (*Alias) Actions() map[string]action.Type {
	return iam.CrudlActions()
}

// TableName returns the tablename to override the default gorm table name.
func (a *Alias) TableName() string {
	if a.tableName != "" {
		return a.tableName
	}
	return defaultAliasTableName
}

// SetTableName sets the tablename and satisfies the ReplayableMessage
// interface. If the caller attempts to set the name to "" the name will be
// reset to the default name.
func (a *Alias) SetTableName(n string) {
	a.tableName = n
}
