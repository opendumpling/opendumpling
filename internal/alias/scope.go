// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package alias

import (
	"context"
	"fmt"

	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/types/scope"
)

// lookupScope returns the iam.Scope which owns the alias. It is a local copy of
// iam.LookupScope because that function requires an unexported method on the
// resource.
func lookupScope(ctx context.Context, reader db.Reader, a *Alias) (*iam.Scope, error) {
	const op = "alias.lookupScope"
	switch {
	case reader == nil:
		return nil, errors.New(ctx, errors.InvalidParameter, op, "nil reader")
	case a == nil:
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing alias")
	case a.GetPublicId() == "":
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing public id")
	}
	if a.GetScopeId() == "" {
		if err := reader.LookupById(ctx, a); err != nil {
			return nil, errors.Wrap(ctx, err, op)
		}
		if a.GetScopeId() == "" {
			return nil, errors.New(ctx, errors.InvalidParameter, op, "missing scope id")
		}
	}
	var s iam.Scope
	if err := reader.LookupWhere(ctx, &s, "public_id = ?", []any{a.GetScopeId()}); err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	return &s, nil
}

// validScopeTypes returns the scope types an alias may live in.
func (a *Alias) validScopeTypes() []scope.Type {
	return []scope.Type{scope.Global, scope.Org, scope.Project}
}

// validateScopeForWrite validates that the scope is valid for the write
// operation.
func validateScopeForWrite(ctx context.Context, r db.Reader, a *Alias, opType db.OpType, opt ...db.Option) error {
	const op = "alias.validateScopeForWrite"
	opts := db.GetOpts(opt...)

	if opType == db.CreateOp {
		if a.GetScopeId() == "" {
			return errors.New(ctx, errors.InvalidParameter, op, "error scope id not set for write")
		}
		ps, err := lookupScope(ctx, r, a)
		if err != nil {
			if errors.IsNotFoundError(err) {
				return errors.New(ctx, errors.RecordNotFound, op, "scope is not found")
			}
			return errors.Wrap(ctx, err, op)
		}
		validScopeType := false
		for _, t := range a.validScopeTypes() {
			if ps.Type == t.String() {
				validScopeType = true
			}
		}
		if !validScopeType {
			return errors.New(ctx, errors.InvalidParameter, op, fmt.Sprintf("%s not a valid scope type for this resource", ps.Type))
		}
	}
	if opType == db.UpdateOp && a.GetScopeId() != "" {
		if contains(opts.WithFieldMaskPaths, "ScopeId") || contains(opts.WithNullPaths, "ScopeId") {
			return errors.New(ctx, errors.InvalidParameter, op, "not allowed to change a resource's scope")
		}
	}
	return nil
}
