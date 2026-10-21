// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package alias

import (
	"context"

	"github.com/opendumpling/opendumpling/globals"
	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/errors"
)

func newAliasId(ctx context.Context) (string, error) {
	const op = "alias.newAliasId"
	id, err := db.NewPublicId(ctx, globals.AliasPrefix)
	if err != nil {
		return "", errors.Wrap(ctx, err, op)
	}
	return id, nil
}
