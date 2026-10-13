// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package credentialstorescmd

import (
	"github.com/opendumpling/opendumpling/internal/cmd/base"
)

func (c *StaticCommand) extraStaticHelpFunc(helpMap map[string]func() string) string {
	var helpStr string
	switch c.Func {
	case "create":
		helpStr = base.WrapForHelpText([]string{
			"Usage: dumpling credential-stores create static [options] [args]",
			"",
			"  Create a static-type credential store. Example:",
			"",
			`    $ dumpling credential-stores create static -scope-id p_1234567890`,
			"",
			"",
		})

	case "update":
		helpStr = base.WrapForHelpText([]string{
			"Usage: dumpling credential-stores update static [options] [args]",
			"",
			"  Update a static-type credential store given its ID. Example:",
			"",
			`    $ dumpling credential-stores update static -name devops -description "For DevOps usage"`,
			"",
			"",
		})
	}
	return helpStr + c.Flags().Help()
}
