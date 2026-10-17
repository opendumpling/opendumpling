// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	// Enable tcp target support.
	_ "github.com/opendumpling/opendumpling/internal/daemon/controller/handlers/targets/tcp"
	_ "github.com/opendumpling/opendumpling/internal/target/tcp"

	// Enable ssh target support.
	_ "github.com/opendumpling/opendumpling/internal/daemon/controller/handlers/targets/ssh"
	_ "github.com/opendumpling/opendumpling/internal/target/ssh"
)
