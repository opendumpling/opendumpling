// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package host

import "github.com/opendumpling/opendumpling/internal/dumpling"

// Domain defines the domain for the host package
const Domain = "host"

// Catalog contains the common methods across all the different types of host
// catalogs.
type Catalog interface {
	dumpling.Resource
	GetProjectId() string
}

// Set contains the common methods across all the different types of host sets.
type Set interface {
	dumpling.Resource
	GetCatalogId() string
}

// Host contains the common methods across all the different types of hosts.
type Host interface {
	dumpling.Resource
	GetCatalogId() string
	GetAddress() string
	GetIpAddresses() []string
	GetDnsNames() []string
	GetSetIds() []string
}
