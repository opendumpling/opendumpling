// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package authtokenscmd

import "github.com/opendumpling/opendumpling/internal/cmd/base"

func (c *Command) extraHelpFunc(helpMap map[string]func() string) string {
	var helpStr string
	switch c.Func {
	case "":
		return base.WrapForHelpText([]string{
			"Usage: dumpling auth-tokens [sub command] [options] [args]",
			"",
			"  This command allows operations on Dumpling auth token resources. Example:",
			"",
			"    List all auth tokens:",
			"",
			`      $ dumpling auth-tokens list -recursive `,
			"",
			"  Please see the auth-tokens subcommand help for detailed usage information.",
			"  Note: To create an auth token, see the authenticate subcommand.",
		})

	default:
		helpStr = helpMap["base"]()
	}
	return helpStr
}
