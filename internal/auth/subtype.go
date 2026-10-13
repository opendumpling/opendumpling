// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"github.com/opendumpling/opendumpling/internal/dumpling"
)

// Domain defines the domain for the auth package.
const Domain = "auth"

// AuthMethod contains the common methods across all the different types of auth methods.
type AuthMethod interface {
	dumpling.Resource
	GetScopeId() string
	GetIsPrimaryAuthMethod() bool
}

type Account interface {
	dumpling.Resource
	GetAuthMethodId() string
	GetLoginName() string
	GetEmail() string
	GetSubject() string
}

type ManagedGroup interface {
	dumpling.Resource
	GetAuthMethodId() string
}
