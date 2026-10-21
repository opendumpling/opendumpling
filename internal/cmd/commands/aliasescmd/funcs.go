// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package aliasescmd

import (
	"fmt"
	"time"

	"github.com/opendumpling/opendumpling/api"
	"github.com/opendumpling/opendumpling/api/aliases"
	"github.com/opendumpling/opendumpling/internal/cmd/base"
)

func init() {
	extraActionsFlagsMapFunc = extraActionsFlagsMapFuncImpl
	extraSynopsisFunc = extraSynopsisFuncImpl
	extraFlagsFunc = extraFlagsFuncImpl
	extraFlagsHandlingFunc = extraFlagsHandlingFuncImpl
	executeExtraActions = executeExtraActionsImpl
}

func extraActionsFlagsMapFuncImpl() map[string][]string {
	return nil
}

func extraSynopsisFuncImpl(_ *Command) string {
	return ""
}

func extraFlagsFuncImpl(_ *Command, _ *base.FlagSets, _ *base.FlagSet) {
}

func (c *Command) extraHelpFunc(helpMap map[string]func() string) string {
	switch c.Func {
	case "":
		return helpMap["base"]()
	}
	return c.Flags().Help()
}

func extraFlagsHandlingFuncImpl(c *Command, _ *base.FlagSets, _ *[]aliases.Option) bool {
	return true
}

func executeExtraActionsImpl(c *Command, origResp *api.Response, origItem *aliases.Alias, origItems []*aliases.Alias, origError error, _ *aliases.Client, _ uint32, _ []aliases.Option) (*api.Response, *aliases.Alias, []*aliases.Alias, error) {
	return origResp, origItem, origItems, origError
}

func (c *Command) printListTable(items []*aliases.Alias) string {
	if len(items) == 0 {
		return "No aliases found"
	}
	var output []string
	output = []string{
		"",
		"Alias information:",
	}
	for i, item := range items {
		if i > 0 {
			output = append(output, "")
		}
		if item.Id != "" {
			output = append(output,
				fmt.Sprintf("  ID:                    %s", item.Id),
			)
		} else {
			output = append(output,
				fmt.Sprintf("  ID:                    %s", "(not available)"),
			)
		}
		if c.FlagRecursive && item.ScopeId != "" {
			output = append(output,
				fmt.Sprintf("    Scope ID:            %s", item.ScopeId),
			)
		}
		if item.Version > 0 {
			output = append(output,
				fmt.Sprintf("    Version:             %d", item.Version),
			)
		}
		if item.Name != "" {
			output = append(output,
				fmt.Sprintf("    Name:                %s", item.Name),
			)
		}
		if item.Value != "" {
			output = append(output,
				fmt.Sprintf("    Value:               %s", item.Value),
			)
		}
		if item.Description != "" {
			output = append(output,
				fmt.Sprintf("    Description:         %s", item.Description),
			)
		}
		if item.DestinationId != "" {
			output = append(output,
				fmt.Sprintf("    Destination ID:      %s", item.DestinationId),
			)
		}
		if len(item.AuthorizedActions) > 0 {
			output = append(output,
				"    Authorized Actions:",
				base.WrapSlice(6, item.AuthorizedActions),
			)
		}
	}

	return base.WrapForHelpText(output)
}

func printItemTable(item *aliases.Alias, resp *api.Response) string {
	nonAttributeMap := map[string]any{}
	if item.Id != "" {
		nonAttributeMap["ID"] = item.Id
	}
	if item.Version != 0 {
		nonAttributeMap["Version"] = item.Version
	}
	if !item.CreatedTime.IsZero() {
		nonAttributeMap["Created Time"] = item.CreatedTime.Local().Format(time.RFC1123)
	}
	if !item.UpdatedTime.IsZero() {
		nonAttributeMap["Updated Time"] = item.UpdatedTime.Local().Format(time.RFC1123)
	}
	if item.Name != "" {
		nonAttributeMap["Name"] = item.Name
	}
	if item.Value != "" {
		nonAttributeMap["Value"] = item.Value
	}
	if item.Description != "" {
		nonAttributeMap["Description"] = item.Description
	}
	if item.DestinationId != "" {
		nonAttributeMap["Destination ID"] = item.DestinationId
	}
	if item.DestinationType != "" {
		nonAttributeMap["Destination Type"] = item.DestinationType
	}

	maxLength := base.MaxAttributesLength(nonAttributeMap, nil, nil)

	ret := []string{
		"",
		"Alias information:",
		base.WrapMap(2, maxLength+2, nonAttributeMap),
	}

	if item.Scope != nil {
		ret = append(ret,
			"",
			"  Scope:",
			base.ScopeInfoForOutput(item.Scope, maxLength),
		)
	}

	if len(item.AuthorizedActions) > 0 {
		ret = append(ret,
			"",
			"  Authorized Actions:",
			base.WrapSlice(4, item.AuthorizedActions),
		)
	}

	return base.WrapForHelpText(ret)
}
