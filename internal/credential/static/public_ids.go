// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package static

import (
	"context"

	"github.com/opendumpling/opendumpling/globals"
	"github.com/opendumpling/opendumpling/internal/credential"
	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/types/subtypes"
)

func init() {
	if err := subtypes.Register(credential.Domain, Subtype, globals.StaticCredentialStorePrefix, globals.StaticCredentialStorePreviousPrefix); err != nil {
		panic(err)
	}
}

// PublicId prefixes for the resources in the static package.
const (
	Subtype = subtypes.Subtype("static")
)

func newCredentialStoreId(ctx context.Context) (string, error) {
	id, err := db.NewPublicId(ctx, globals.StaticCredentialStorePrefix)
	if err != nil {
		return "", errors.Wrap(ctx, err, "static.newCredentialStoreId")
	}
	return id, nil
}
