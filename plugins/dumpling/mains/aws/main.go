// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"

	aws "github.com/opendumpling/opendumpling/plugins/dumpling/mains/aws/plugin"
	hp "github.com/opendumpling/opendumpling/sdk/plugins"
)

func main() {
	if err := hp.ServePlugin(aws.NewAwsPlugin()); err != nil {
		fmt.Println("Error serving plugin", err)
		os.Exit(1)
	}
	os.Exit(0)
}
