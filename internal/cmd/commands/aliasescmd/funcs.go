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

type extraCmdVars struct {
	flagValue         string
	flagDestinationId string
}

func extraActionsFlagsMapFuncImpl() map[string][]string {
	return map[string][]string{
		"create": {"value", "destination-id"},
		"update": {"value", "destination-id"},
	}
}

func extraSynopsisFuncImpl(_ *Command) string {
	return ""
}

func extraFlagsFuncImpl(c *Command, _ *base.FlagSets, f *base.FlagSet) {
	for _, name := range flagsMap[c.Func] {
		switch name {
		case "value":
			f.StringVar(&base.StringVar{
				Name:   "value",
				Target: &c.flagValue,
				Usage:  "The alias value used to look up its destination.",
			})
		case "destination-id":
			f.StringVar(&base.StringVar{
				Name:   "destination-id",
				Target: &c.flagDestinationId,
				Usage:  "The ID of the target this alias points to.",
			})
		}
	}
}

func (c *Command) extraHelpFunc(helpMap map[string]func() string) string {
	switch c.Func {
	case "":
		return helpMap["base"]()
	}
	return c.Flags().Help()
}

func extraFlagsHandlingFuncImpl(c *Command, _ *base.FlagSets, opts *[]aliases.Option) bool {
	switch c.Func {
	case "create":
		if c.flagValue == "" {
			c.UI.Error("Alias value must be passed in via -value")
			return false
		}
		if c.flagDestinationId == "" {
			c.UI.Error("Destination target ID must be passed in via -destination-id")
			return false
		}
		*opts = append(*opts,
			aliases.WithValue(c.flagValue),
			aliases.WithDestinationId(c.flagDestinationId),
		)
	case "update":
		if c.flagValue != "" {
			*opts = append(*opts, aliases.WithValue(c.flagValue))
		}
		if c.flagDestinationId != "" {
			*opts = append(*opts, aliases.WithDestinationId(c.flagDestinationId))
		}
	}
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
