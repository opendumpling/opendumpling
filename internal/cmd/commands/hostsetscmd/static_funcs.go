// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package hostsetscmd

import (
	"github.com/opendumpling/opendumpling/internal/cmd/base"
)

func (c *StaticCommand) extraStaticHelpFunc(helpMap map[string]func() string) string {
	var helpStr string
	switch c.Func {
	case "create":
		helpStr = base.WrapForHelpText([]string{
			"Usage: dumpling host-sets create static [options] [args]",
			"",
			"  Create a static-type host set. Example:",
			"",
			`    $ dumpling host-sets create static -name prodops -description "Static host-set for ProdOps"`,
			"",
			"",
		})

	case "update":
		helpStr = base.WrapForHelpText([]string{
			"Usage: dumpling host-sets update static [options] [args]",
			"",
			"  Update a static-type host set given its ID. Example:",
			"",
			`    $ dumpling host-sets update static -id hsst_1234567890 -name "devops" -description "Static host-set for DevOps"`,
			"",
			"",
		})
	default:
		helpStr = helpMap[c.Func]()
	}
	return helpStr + c.Flags().Help()
}
