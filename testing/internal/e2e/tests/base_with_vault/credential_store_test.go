// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package base_with_vault_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/opendumpling/opendumpling/api/credentiallibraries"
	"github.com/opendumpling/opendumpling/api/credentialstores"
	"github.com/opendumpling/opendumpling/api/scopes"
	"github.com/opendumpling/opendumpling/api/targets"
	"github.com/opendumpling/opendumpling/testing/internal/e2e"
	"github.com/opendumpling/opendumpling/testing/internal/e2e/dumpling"
	"github.com/opendumpling/opendumpling/testing/internal/e2e/vault"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCliVaultCredentialStore uses the cli to perform a number of credential store operations with
// vault
func TestCliVaultCredentialStore(t *testing.T) {
	e2e.MaybeSkipTest(t)
	c, err := loadTestConfig()
	require.NoError(t, err)

	ctx := context.Background()
	dumpling.AuthenticateAdminCli(t, ctx)
	newOrgId := dumpling.CreateNewOrgCli(t, ctx)
	t.Cleanup(func() {
		ctx := context.Background()
		dumpling.AuthenticateAdminCli(t, ctx)
		output := e2e.RunCommand(ctx, "dumpling", e2e.WithArgs("scopes", "delete", "-id", newOrgId))
		require.NoError(t, output.Err, string(output.Stderr))
	})
	newProjectId := dumpling.CreateNewProjectCli(t, ctx, newOrgId)
	newHostCatalogId := dumpling.CreateNewHostCatalogCli(t, ctx, newProjectId)
	newHostSetId := dumpling.CreateNewHostSetCli(t, ctx, newHostCatalogId)
	newHostId := dumpling.CreateNewHostCli(t, ctx, newHostCatalogId, c.TargetIp)
	dumpling.AddHostToHostSetCli(t, ctx, newHostSetId, newHostId)
	newTargetId := dumpling.CreateNewTargetCli(t, ctx, newProjectId, c.TargetPort)
	dumpling.AddHostSourceToTargetCli(t, ctx, newTargetId, newHostSetId)

	// Configure vault
	boundaryPolicyName, kvPolicyFilePath := vault.Setup(t)
	t.Cleanup(func() {
		output := e2e.RunCommand(ctx, "vault",
			e2e.WithArgs("policy", "delete", boundaryPolicyName),
		)
		require.NoError(t, output.Err, string(output.Stderr))
	})

	output := e2e.RunCommand(ctx, "vault",
		e2e.WithArgs("secrets", "enable", "-path="+c.VaultSecretPath, "kv-v2"),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	t.Cleanup(func() {
		output := e2e.RunCommand(ctx, "vault",
			e2e.WithArgs("secrets", "disable", c.VaultSecretPath),
		)
		require.NoError(t, output.Err, string(output.Stderr))
	})

	// Create credentials in vault
	privateKeySecretName := vault.CreateKvPrivateKeyCredential(t, c.VaultSecretPath, c.TargetSshUser, c.TargetSshKeyPath, kvPolicyFilePath)
	passwordSecretName, password := vault.CreateKvPasswordCredential(t, c.VaultSecretPath, c.TargetSshUser, kvPolicyFilePath)
	kvPolicyName := vault.WritePolicy(t, ctx, kvPolicyFilePath)
	t.Cleanup(func() {
		output := e2e.RunCommand(ctx, "vault",
			e2e.WithArgs("policy", "delete", kvPolicyName),
		)
		require.NoError(t, output.Err, string(output.Stderr))
	})
	t.Log("Created Vault Credentials")

	// Create vault token for boundary
	output = e2e.RunCommand(ctx, "vault",
		e2e.WithArgs(
			"token", "create",
			"-no-default-policy=true",
			"-policy="+boundaryPolicyName,
			"-policy="+kvPolicyName,
			"-orphan=true",
			"-period=20m",
			"-renewable=true",
			"-format=json",
		),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	var tokenCreateResult vault.CreateTokenResponse
	err = json.Unmarshal(output.Stdout, &tokenCreateResult)
	require.NoError(t, err)
	credStoreToken := tokenCreateResult.Auth.Client_Token
	t.Log("Created Vault Cred Store Token")

	// Create a credential store
	newCredentialStoreId := dumpling.CreateNewCredentialStoreVaultCli(t, ctx, newProjectId, c.VaultAddr, credStoreToken)

	// Create a credential library for the private key
	output = e2e.RunCommand(ctx, "dumpling",
		e2e.WithArgs(
			"credential-libraries", "create", "vault",
			"-credential-store-id", newCredentialStoreId,
			"-vault-path", c.VaultSecretPath+"/data/"+privateKeySecretName,
			"-name", "e2e Automated Test Vault Credential Library - Private Key",
			"-credential-type", "ssh_private_key",
			"-format", "json",
		),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	var newCredentialLibraryResult credentiallibraries.CredentialLibraryCreateResult
	err = json.Unmarshal(output.Stdout, &newCredentialLibraryResult)
	require.NoError(t, err)
	newPrivateKeyCredentialLibraryId := newCredentialLibraryResult.Item.Id
	t.Logf("Created Credential Library: %s", newPrivateKeyCredentialLibraryId)

	// Create a credential library for the password
	output = e2e.RunCommand(ctx, "dumpling",
		e2e.WithArgs(
			"credential-libraries", "create", "vault",
			"-credential-store-id", newCredentialStoreId,
			"-vault-path", c.VaultSecretPath+"/data/"+passwordSecretName,
			"-name", "e2e Automated Test Vault Credential Library - Password",
			"-credential-type", "username_password",
			"-format", "json",
		),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	err = json.Unmarshal(output.Stdout, &newCredentialLibraryResult)
	require.NoError(t, err)
	newPasswordCredentialLibraryId := newCredentialLibraryResult.Item.Id
	t.Logf("Created Credential Library: %s", newPasswordCredentialLibraryId)

	// Get credentials for target (expect empty)
	output = e2e.RunCommand(ctx, "dumpling",
		e2e.WithArgs("targets", "authorize-session", "-id", newTargetId, "-format", "json"),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	var newSessionAuthorizationResult targets.SessionAuthorizationResult
	err = json.Unmarshal(output.Stdout, &newSessionAuthorizationResult)
	require.NoError(t, err)
	require.True(t, newSessionAuthorizationResult.Item.Credentials == nil)

	// Add credentials to target
	dumpling.AddBrokeredCredentialSourceToTargetCli(t, ctx, newTargetId, newPrivateKeyCredentialLibraryId)
	dumpling.AddBrokeredCredentialSourceToTargetCli(t, ctx, newTargetId, newPasswordCredentialLibraryId)

	// Get credentials for target
	output = e2e.RunCommand(ctx, "dumpling",
		e2e.WithArgs("targets", "authorize-session", "-id", newTargetId, "-format", "json"),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	err = json.Unmarshal(output.Stdout, &newSessionAuthorizationResult)
	require.NoError(t, err)

	newSessionAuthorization := newSessionAuthorizationResult.Item
	require.Len(t, newSessionAuthorization.Credentials, 2)

	for _, v := range newSessionAuthorization.Credentials {
		if v.CredentialSource.CredentialType == "ssh_private_key" {
			retrievedUser, ok := v.Credential["username"].(string)
			require.True(t, ok)
			retrievedKey, ok := v.Credential["private_key"].(string)
			require.True(t, ok)
			assert.Equal(t, c.TargetSshUser, retrievedUser)

			keyData, err := os.ReadFile(c.TargetSshKeyPath)
			require.NoError(t, err)
			require.Equal(t, string(keyData), retrievedKey)
			t.Log("Successfully retrieved private key credentials for target")
		} else if v.CredentialSource.CredentialType == "username_password" {
			retrievedUser, ok := v.Credential["username"].(string)
			require.True(t, ok)
			retrievedPassword, ok := v.Credential["password"].(string)
			require.True(t, ok)
			assert.Equal(t, c.TargetSshUser, retrievedUser)
			assert.Equal(t, password, retrievedPassword)
			t.Log("Successfully retrieved password credentials for target")
		}
	}
}

// TestApiVaultCredentialStore uses the Go api to perform a number of credential store operations
// with vault
func TestApiVaultCredentialStore(t *testing.T) {
	e2e.MaybeSkipTest(t)
	c, err := loadTestConfig()
	require.NoError(t, err)

	client, err := dumpling.NewApiClient()
	require.NoError(t, err)
	ctx := context.Background()

	newOrgId := dumpling.CreateNewOrgApi(t, ctx, client)
	t.Cleanup(func() {
		scopeClient := scopes.NewClient(client)
		_, err := scopeClient.Delete(ctx, newOrgId)
		require.NoError(t, err)
	})
	newProjectId := dumpling.CreateNewProjectApi(t, ctx, client, newOrgId)
	newHostCatalogId := dumpling.CreateNewHostCatalogApi(t, ctx, client, newProjectId)
	newHostSetId := dumpling.CreateNewHostSetApi(t, ctx, client, newHostCatalogId)
	newHostId := dumpling.CreateNewHostApi(t, ctx, client, newHostCatalogId, c.TargetIp)
	dumpling.AddHostToHostSetApi(t, ctx, client, newHostSetId, newHostId)
	newTargetId := dumpling.CreateNewTargetApi(t, ctx, client, newProjectId, c.TargetPort)
	dumpling.AddHostSourceToTargetApi(t, ctx, client, newTargetId, newHostSetId)

	// Configure vault
	boundaryPolicyName, kvPolicyFilePath := vault.Setup(t)
	output := e2e.RunCommand(ctx, "vault",
		e2e.WithArgs("secrets", "enable", "-path="+c.VaultSecretPath, "kv-v2"),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	t.Cleanup(func() {
		output := e2e.RunCommand(ctx, "vault",
			e2e.WithArgs("secrets", "disable", c.VaultSecretPath),
		)
		require.NoError(t, output.Err, string(output.Stderr))
	})

	// Create credential in vault
	privateKeySecretName := vault.CreateKvPrivateKeyCredential(t, c.VaultSecretPath, c.TargetSshUser, c.TargetSshKeyPath, kvPolicyFilePath)
	passwordSecretName, password := vault.CreateKvPasswordCredential(t, c.VaultSecretPath, c.TargetSshUser, kvPolicyFilePath)
	kvPolicyName := vault.WritePolicy(t, ctx, kvPolicyFilePath)
	t.Log("Created Vault Credentials")

	// Create vault token for boundary
	output = e2e.RunCommand(ctx, "vault",
		e2e.WithArgs(
			"token", "create",
			"-no-default-policy=true",
			"-policy="+boundaryPolicyName,
			"-policy="+kvPolicyName,
			"-orphan=true",
			"-period=20m",
			"-renewable=true",
			"-format=json",
		),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	var tokenCreateResult vault.CreateTokenResponse
	err = json.Unmarshal(output.Stdout, &tokenCreateResult)
	require.NoError(t, err)
	credStoreToken := tokenCreateResult.Auth.Client_Token
	t.Log("Created Vault Cred Store Token")

	// Create a credential store
	csClient := credentialstores.NewClient(client)
	newCredentialStoreResult, err := csClient.Create(ctx, "vault", newProjectId,
		credentialstores.WithVaultCredentialStoreAddress(c.VaultAddr),
		credentialstores.WithVaultCredentialStoreToken(credStoreToken),
	)
	require.NoError(t, err)
	newCredentialStoreId := newCredentialStoreResult.Item.Id
	t.Logf("Created Credential Store: %s", newCredentialStoreId)

	// Create a credential library for the private key
	clClient := credentiallibraries.NewClient(client)
	newCredentialLibraryResult, err := clClient.Create(ctx, "vault", newCredentialStoreId,
		credentiallibraries.WithVaultCredentialLibraryPath(c.VaultSecretPath+"/data/"+privateKeySecretName),
		credentiallibraries.WithCredentialType("ssh_private_key"),
	)
	require.NoError(t, err)
	newPrivateKeyCredentialLibraryId := newCredentialLibraryResult.Item.Id
	t.Logf("Created Credential Library: %s", newPrivateKeyCredentialLibraryId)

	// Create a credential library for the password
	newCredentialLibraryResult, err = clClient.Create(ctx, "vault", newCredentialStoreId,
		credentiallibraries.WithVaultCredentialLibraryPath(c.VaultSecretPath+"/data/"+passwordSecretName),
		credentiallibraries.WithCredentialType("username_password"),
	)
	require.NoError(t, err)
	newPasswordCredentialLibraryId := newCredentialLibraryResult.Item.Id
	t.Logf("Created Credential Library: %s", newPasswordCredentialLibraryId)

	// Add private key brokered credentials to target
	tClient := targets.NewClient(client)
	_, err = tClient.AddCredentialSources(ctx, newTargetId, 0,
		targets.WithBrokeredCredentialSourceIds([]string{newPrivateKeyCredentialLibraryId}),
		targets.WithAutomaticVersioning(true),
	)
	require.NoError(t, err)

	// Add password brokered credentials to target
	_, err = tClient.AddCredentialSources(ctx, newTargetId, 0,
		targets.WithBrokeredCredentialSourceIds([]string{newPasswordCredentialLibraryId}),
		targets.WithAutomaticVersioning(true),
	)
	require.NoError(t, err)

	// Get credentials for target
	newSessionAuthorizationResult, err := tClient.AuthorizeSession(ctx, newTargetId)
	require.NoError(t, err)
	newSessionAuthorization := newSessionAuthorizationResult.Item
	require.Len(t, newSessionAuthorization.Credentials, 2)

	for _, v := range newSessionAuthorization.Credentials {
		if v.CredentialSource.CredentialType == "ssh_private_key" {
			retrievedUser, ok := v.Credential["username"].(string)
			require.True(t, ok)
			assert.Equal(t, c.TargetSshUser, retrievedUser)

			retrievedKey, ok := v.Credential["private_key"].(string)
			require.True(t, ok)
			k, err := os.ReadFile(c.TargetSshKeyPath)
			require.NoError(t, err)
			keysMatch := string(k) == retrievedKey // This is done to prevent printing out key info
			require.True(t, keysMatch, "Key retrieved from vault does not match expected value")
			t.Log("Successfully retrieved credentials for target")
		} else if v.CredentialSource.CredentialType == "username_password" {
			retrievedUser, ok := v.Credential["username"].(string)
			require.True(t, ok)
			retrievedPassword, ok := v.Credential["password"].(string)
			require.True(t, ok)
			assert.Equal(t, c.TargetSshUser, retrievedUser)
			assert.Equal(t, password, retrievedPassword)
			t.Log("Successfully retrieved password credentials for target")
		}
	}
}
