// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"context"

	"github.com/opendumpling/opendumpling/internal/db"
	"github.com/opendumpling/opendumpling/internal/errors"
)

// PublicId prefixes for the resources in the plugin package.
const (
	PluginPrefix = "pl"
)

func newPluginId(ctx context.Context) (string, error) {
	id, err := db.NewPublicId(ctx, PluginPrefix)
	if err != nil {
		return "", errors.Wrap(ctx, err, "plugin.newPluginId")
	}
	return id, nil
}
