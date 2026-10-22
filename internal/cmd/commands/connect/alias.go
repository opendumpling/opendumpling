// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"

	"github.com/opendumpling/opendumpling/api"
	"github.com/opendumpling/opendumpling/api/aliases"
	"github.com/opendumpling/opendumpling/internal/types/scope"
)

// resolveAlias looks up the alias value in the given scope (or, if no scope was
// given, recursively from the global scope) and returns the id of the target it
// points at. Because a value only has to be unique within its scope, resolving
// without an explicit scope can find more than one match; in that case an error
// is returned asking the user to disambiguate.
func (c *Command) resolveAlias(ctx context.Context, client *api.Client) (string, error) {
	aliasClient := aliases.NewClient(client)

	scopeId := c.flagAliasScopeId
	recursive := false
	if scopeId == "" {
		scopeId = scope.Global.String()
		recursive = true
	}

	opts := []aliases.Option{
		aliases.WithFilter(fmt.Sprintf(`"/item/value" == %q`, c.flagAlias)),
	}
	if recursive {
		opts = append(opts, aliases.WithRecursive(true))
	}

	list, err := aliasClient.List(ctx, scopeId, opts...)
	if err != nil {
		if apiErr := api.AsServerError(err); apiErr != nil {
			return "", fmt.Errorf("Error from controller looking up alias %q: %w", c.flagAlias, apiErr)
		}
		return "", fmt.Errorf("Error looking up alias %q: %w", c.flagAlias, err)
	}

	items := list.GetItems()
	switch len(items) {
	case 0:
		return "", fmt.Errorf("No alias found with value %q", c.flagAlias)
	case 1:
		if items[0].DestinationId == "" {
			return "", fmt.Errorf("Alias %q did not include a destination target id", c.flagAlias)
		}
		return items[0].DestinationId, nil
	default:
		var scopes []string
		for _, item := range items {
			scopes = append(scopes, item.ScopeId)
		}
		return "", fmt.Errorf("Alias value %q matches aliases in more than one scope (%v); pass -alias-scope-id to disambiguate", c.flagAlias, scopes)
	}
}
