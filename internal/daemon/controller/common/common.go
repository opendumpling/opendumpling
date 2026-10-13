// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"github.com/opendumpling/opendumpling/internal/auth/ldap"
	"github.com/opendumpling/opendumpling/internal/auth/oidc"
	"github.com/opendumpling/opendumpling/internal/auth/password"
	credstatic "github.com/opendumpling/opendumpling/internal/credential/static"
	"github.com/opendumpling/opendumpling/internal/credential/vault"
	pluginhost "github.com/opendumpling/opendumpling/internal/host/plugin"
	"github.com/opendumpling/opendumpling/internal/host/static"
	"github.com/opendumpling/opendumpling/internal/iam"
	"github.com/opendumpling/opendumpling/internal/plugin"
	"github.com/opendumpling/opendumpling/internal/server"
	"github.com/opendumpling/opendumpling/internal/session"
	pluginstorage "github.com/opendumpling/opendumpling/internal/storage/plugin"
)

type (
	AuthTokenRepoFactory           = oidc.AuthTokenRepoFactory
	VaultCredentialRepoFactory     = func() (*vault.Repository, error)
	StaticCredentialRepoFactory    = func() (*credstatic.Repository, error)
	IamRepoFactory                 = iam.IamRepoFactory
	OidcAuthRepoFactory            = oidc.OidcRepoFactory
	LdapAuthRepoFactory            = ldap.RepoFactory
	PasswordAuthRepoFactory        func() (*password.Repository, error)
	ServersRepoFactory             func() (*server.Repository, error)
	StaticRepoFactory              func() (*static.Repository, error)
	PluginHostRepoFactory          func() (*pluginhost.Repository, error)
	PluginRepoFactory              func() (*plugin.Repository, error)
	ConnectionRepoFactory          func() (*session.ConnectionRepository, error)
	WorkerAuthRepoStorageFactory   func() (*server.WorkerAuthRepositoryStorage, error)
	PluginStorageBucketRepoFactory func() (*pluginstorage.Repository, error)
)

// Downstreamers provides at least a minimum interface that must be met by a
// Controller.downstreamWorkers field which is far better than allowing any (empty
// interface)
type Downstreamers interface {
	// RootId returns the root ID of the downstreamers' graph
	RootId() string
}
