// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package static

import (
	"context"

	"github.com/opendumpling/opendumpling/globals"
	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/host"
	"github.com/opendumpling/opendumpling/internal/types/subtypes"
)

func init() {
	if err := subtypes.Register(host.Domain, Subtype, globals.StaticHostCatalogPrefix, globals.StaticHostSetPrefix, globals.StaticHostPrefix); err != nil {
		panic(err)
	}
}

// PublicId prefixes for the resources in the static package.
const (
	Subtype = subtypes.Subtype("static")
)

func newHostCatalogId(ctx context.Context) (string, error) {
	id, err := db.NewPublicId(ctx, globals.StaticHostCatalogPrefix)
	if err != nil {
		return "", errors.Wrap(ctx, err, "static.newHostCatalogId")
	}
	return id, nil
}

func newHostId(ctx context.Context) (string, error) {
	id, err := db.NewPublicId(ctx, globals.StaticHostPrefix)
	if err != nil {
		return "", errors.Wrap(ctx, err, "static.newHostId")
	}
	return id, nil
}

func newHostSetId(ctx context.Context) (string, error) {
	id, err := db.NewPublicId(ctx, globals.StaticHostSetPrefix)
	if err != nil {
		return "", errors.Wrap(ctx, err, "static.newHostSetId")
	}
	return id, nil
}
