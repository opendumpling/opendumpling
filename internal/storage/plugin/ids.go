// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"context"

	"github.com/opendumpling/opendumpling/globals"
	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/types/subtypes"
)

const StorageDomain = "storage"

func init() {
	if err := subtypes.Register(StorageDomain, Subtype, globals.PluginStorageBucketPrefix); err != nil {
		panic(err)
	}
}

const (
	Subtype = subtypes.Subtype("plugin")
)

func newStorageBucketId(ctx context.Context) (string, error) {
	const op = "plugin.newStorageBucketId"
	id, err := db.NewPublicId(ctx, globals.PluginStorageBucketPrefix)
	if err != nil {
		return "", errors.Wrap(ctx, err, op)
	}
	return id, nil
}
