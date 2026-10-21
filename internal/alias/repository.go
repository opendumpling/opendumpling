// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package alias

import (
	"context"
	"fmt"
	"strings"

	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/kms"
	"github.com/opendumpling/opendumpling/internal/oplog"
)

// Repository is the alias database repository.
type Repository struct {
	reader db.Reader
	writer db.Writer
	kms    *kms.Kms

	// defaultLimit provides a default for limiting the number of results returned from the repo
	defaultLimit int
}

// NewRepository creates a new alias Repository. Supports the WithLimit option
// which sets a default limit on results returned by repo operations.
func NewRepository(ctx context.Context, r db.Reader, w db.Writer, kms *kms.Kms, opt ...Option) (*Repository, error) {
	const op = "alias.NewRepository"
	if r == nil {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "nil reader")
	}
	if w == nil {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "nil writer")
	}
	if kms == nil {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "nil kms")
	}
	opts := getOpts(opt...)
	if opts.withLimit == 0 {
		opts.withLimit = db.DefaultLimit
	}
	return &Repository{
		reader:       r,
		writer:       w,
		kms:          kms,
		defaultLimit: opts.withLimit,
	}, nil
}

// CreateAlias creates a new alias in the repository.
func (r *Repository) CreateAlias(ctx context.Context, alias *Alias, _ ...Option) (*Alias, error) {
	const op = "alias.(Repository).CreateAlias"
	if alias == nil {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing alias")
	}
	if alias.PublicId != "" {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "public id not empty")
	}
	alias = alias.Clone().(*Alias)

	id, err := newAliasId(ctx)
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	alias.PublicId = id

	metadata, err := r.stdMetadata(ctx, alias)
	if err != nil {
		return nil, errors.Wrap(ctx, err, op, errors.WithMsg("error getting metadata"))
	}
	metadata["op-type"] = []string{oplog.OpType_OP_TYPE_CREATE.String()}

	scope, err := alias.GetScope(ctx, r.reader)
	if err != nil {
		return nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get scope"))
	}
	oplogWrapper, err := r.kms.GetWrapper(ctx, scope.GetPublicId(), kms.KeyPurposeOplog)
	if err != nil {
		return nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get oplog wrapper"))
	}

	var returnedAlias *Alias
	_, err = r.writer.DoTx(
		ctx,
		db.StdRetryCnt,
		db.ExpBackoff{},
		func(_ db.Reader, w db.Writer) error {
			returnedAlias = alias.Clone().(*Alias)
			if err := w.Create(ctx, returnedAlias, db.WithOplog(oplogWrapper, metadata)); err != nil {
				return errors.Wrap(ctx, err, op)
			}
			return nil
		},
	)
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	return returnedAlias, nil
}

// UpdateAlias updates an existing alias in the repository. It returns the
// updated alias and the number of rows updated.
func (r *Repository) UpdateAlias(ctx context.Context, alias *Alias, version uint32, fieldMaskPaths []string, _ ...Option) (*Alias, int, error) {
	const op = "alias.(Repository).UpdateAlias"
	switch {
	case alias == nil:
		return nil, db.NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing alias")
	case alias.PublicId == "":
		return nil, db.NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing public id")
	case version == 0:
		return nil, db.NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing version")
	}

	alias = alias.Clone().(*Alias)

	metadata, err := r.stdMetadata(ctx, alias)
	if err != nil {
		return nil, db.NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("error getting metadata"))
	}
	metadata["op-type"] = []string{oplog.OpType_OP_TYPE_UPDATE.String()}

	scope, err := alias.GetScope(ctx, r.reader)
	if err != nil {
		return nil, db.NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get scope"))
	}
	oplogWrapper, err := r.kms.GetWrapper(ctx, scope.GetPublicId(), kms.KeyPurposeOplog)
	if err != nil {
		return nil, db.NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get oplog wrapper"))
	}

	var rowsUpdated int
	var returnedAlias *Alias
	_, err = r.writer.DoTx(
		ctx,
		db.StdRetryCnt,
		db.ExpBackoff{},
		func(_ db.Reader, w db.Writer) error {
			returnedAlias = alias.Clone().(*Alias)
			var err error
			rowsUpdated, err = w.Update(
				ctx,
				returnedAlias,
				fieldMaskPaths,
				nil,
				db.WithVersion(&version),
				db.WithOplog(oplogWrapper, metadata),
			)
			if err != nil {
				return errors.Wrap(ctx, err, op)
			}
			if rowsUpdated > 1 {
				return errors.New(ctx, errors.MultipleRecords, op, "more than 1 alias would have been updated")
			}
			return nil
		},
	)
	if err != nil {
		return nil, db.NoRowsAffected, errors.Wrap(ctx, err, op)
	}
	return returnedAlias, rowsUpdated, nil
}

// LookupAlias returns the alias for the provided public id. If the alias is
// not found it returns nil, nil.
func (r *Repository) LookupAlias(ctx context.Context, publicId string, _ ...Option) (*Alias, error) {
	const op = "alias.(Repository).LookupAlias"
	if publicId == "" {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing public id")
	}
	a := allocAlias()
	a.PublicId = publicId
	if err := r.reader.LookupByPublicId(ctx, &a); err != nil {
		if errors.IsNotFoundError(err) {
			return nil, nil
		}
		return nil, errors.Wrap(ctx, err, op, errors.WithMsg(fmt.Sprintf("failed for %s", publicId)))
	}
	return &a, nil
}

// LookupAliasByValue returns the alias in the provided scope whose value
// matches, or an error if there is none. Both the value and the scope id are
// required because a value only has to be unique within its scope.
func (r *Repository) LookupAliasByValue(ctx context.Context, scopeId, value string, _ ...Option) (*Alias, error) {
	const op = "alias.(Repository).LookupAliasByValue"
	switch {
	case scopeId == "":
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing scope id")
	case value == "":
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing value")
	}
	a := allocAlias()
	if err := r.reader.LookupWhere(ctx, &a, "scope_id = ? and value = ?", []any{scopeId, value}); err != nil {
		if errors.IsNotFoundError(err) {
			return nil, nil
		}
		return nil, errors.Wrap(ctx, err, op, errors.WithMsg(fmt.Sprintf("failed for %s in scope %s", value, scopeId)))
	}
	return &a, nil
}

// DeleteAlias deletes the alias with the provided public id and returns the
// number of rows deleted.
func (r *Repository) DeleteAlias(ctx context.Context, publicId string, _ ...Option) (int, error) {
	const op = "alias.(Repository).DeleteAlias"
	if publicId == "" {
		return db.NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing public id")
	}
	a := allocAlias()
	a.PublicId = publicId

	metadata, err := r.stdMetadata(ctx, &a)
	if err != nil {
		return db.NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("error getting metadata"))
	}
	metadata["op-type"] = []string{oplog.OpType_OP_TYPE_DELETE.String()}

	scope, err := a.GetScope(ctx, r.reader)
	if err != nil {
		return db.NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get scope"))
	}
	oplogWrapper, err := r.kms.GetWrapper(ctx, scope.GetPublicId(), kms.KeyPurposeOplog)
	if err != nil {
		return db.NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get oplog wrapper"))
	}

	var rowsDeleted int
	_, err = r.writer.DoTx(
		ctx,
		db.StdRetryCnt,
		db.ExpBackoff{},
		func(_ db.Reader, w db.Writer) error {
			var err error
			rowsDeleted, err = w.Delete(ctx, &a, db.WithOplog(oplogWrapper, metadata))
			if err != nil {
				return errors.Wrap(ctx, err, op)
			}
			if rowsDeleted > 1 {
				return errors.New(ctx, errors.MultipleRecords, op, "more than 1 alias would have been deleted")
			}
			return nil
		},
	)
	if err != nil {
		return db.NoRowsAffected, errors.Wrap(ctx, err, op)
	}
	return rowsDeleted, nil
}

// ListAliases returns a slice of aliases in the provided scopes, up to the
// repository limit.
func (r *Repository) ListAliases(ctx context.Context, scopeIds []string, opt ...Option) ([]*Alias, error) {
	const op = "alias.(Repository).ListAliases"
	if len(scopeIds) == 0 {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing scope ids")
	}
	opts := getOpts(opt...)
	limit := r.defaultLimit
	if opts.withLimit != 0 {
		limit = opts.withLimit
	}

	var aliases []*Alias
	where := "scope_id in (?)"
	args := []any{scopeIds}
	if err := r.reader.SearchWhere(ctx, &aliases, where, args, db.WithLimit(limit)); err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	return aliases, nil
}

func (r *Repository) stdMetadata(ctx context.Context, a *Alias) (oplog.Metadata, error) {
	const op = "alias.(Repository).stdMetadata"
	scope, err := a.GetScope(ctx, r.reader)
	if err != nil {
		return nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get scope"))
	}
	return oplog.Metadata{
		"resource-public-id": []string{a.GetPublicId()},
		"scope-id":           []string{scope.PublicId},
		"scope-type":         []string{scope.Type},
		"resource-type":      []string{a.ResourceType().String()},
	}, nil
}

func contains(ss []string, t string) bool {
	for _, s := range ss {
		if strings.EqualFold(s, t) {
			return true
		}
	}
	return false
}
