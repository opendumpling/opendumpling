// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package base_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/opendumpling/opendumpling/api/accounts"
	"github.com/opendumpling/opendumpling/api/roles"
	"github.com/opendumpling/opendumpling/api/scopes"
	"github.com/opendumpling/opendumpling/api/users"
	"github.com/opendumpling/opendumpling/internal/session"
	"github.com/opendumpling/opendumpling/testing/internal/e2e"
	"github.com/opendumpling/opendumpling/testing/internal/e2e/dumpling"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCliSessionCancelUser uses the cli to create a new user and sets up the right permissions for
// the user to connect to the created target. The test also confirms that an admin can cancel the
// user's session.
func TestCliSessionCancelUser(t *testing.T) {
	e2e.MaybeSkipTest(t)
	c, err := loadTestConfig()
	require.NoError(t, err)
	bc, err := dumpling.LoadConfig()
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
	acctName := "e2e-account"
	newAccountId, acctPassword := dumpling.CreateNewAccountCli(t, ctx, bc.AuthMethodId, acctName)
	t.Cleanup(func() {
		dumpling.AuthenticateAdminCli(t, context.Background())
		output := e2e.RunCommand(ctx, "dumpling",
			e2e.WithArgs("accounts", "delete", "-id", newAccountId),
		)
		require.NoError(t, output.Err, string(output.Stderr))
	})
	newUserId := dumpling.CreateNewUserCli(t, ctx, "global")
	t.Cleanup(func() {
		dumpling.AuthenticateAdminCli(t, context.Background())
		output := e2e.RunCommand(ctx, "dumpling",
			e2e.WithArgs("users", "delete", "-id", newUserId),
		)
		require.NoError(t, output.Err, string(output.Stderr))
	})
	dumpling.SetAccountToUserCli(t, ctx, newUserId, newAccountId)

	// Try to connect to the target as a user without permissions
	dumpling.AuthenticateCli(t, ctx, bc.AuthMethodId, acctName, acctPassword)
	output := e2e.RunCommand(ctx, "dumpling",
		e2e.WithArgs(
			"connect",
			"-target-id", newTargetId,
			"-format", "json",
			"-exec", "/usr/bin/ssh", "--",
			"-l", c.TargetSshUser,
			"-i", c.TargetSshKeyPath,
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "StrictHostKeyChecking=no",
			"-o", "IdentitiesOnly=yes", // forces the use of the provided key
			"-p", "{{dumpling.port}}", // this is provided by boundary
			"{{dumpling.ip}}",
			"hostname -i",
		),
	)
	require.Error(t, output.Err, string(output.Stdout), string(output.Stderr))
	var response dumpling.CliError
	err = json.Unmarshal(output.Stderr, &response)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, int(response.Status))
	t.Log("Successfully received an error when connecting to target as a user without permissions")

	// Create a role for user
	dumpling.AuthenticateAdminCli(t, ctx)
	newRoleId := dumpling.CreateNewRoleCli(t, ctx, newProjectId)
	dumpling.AddGrantToRoleCli(t, ctx, newRoleId, "id=*;type=target;actions=authorize-session")
	dumpling.AddPrincipalToRoleCli(t, ctx, newRoleId, newUserId)

	// Connect to target to create a session
	ctxCancel, cancel := context.WithCancel(context.Background())
	errChan := make(chan *e2e.CommandResult)
	go func() {
		token := dumpling.GetAuthenticationTokenCli(t, ctx, acctName, acctPassword)
		t.Log("Starting session as user...")
		errChan <- e2e.RunCommand(ctxCancel, "dumpling",
			e2e.WithArgs(
				"connect",
				"-token", "env://E2E_AUTH_TOKEN",
				"-target-id", newTargetId,
				"-exec", "/usr/bin/ssh", "--",
				"-l", c.TargetSshUser,
				"-i", c.TargetSshKeyPath,
				"-o", "UserKnownHostsFile=/dev/null",
				"-o", "StrictHostKeyChecking=no",
				"-o", "IdentitiesOnly=yes", // forces the use of the provided key
				"-p", "{{dumpling.port}}", // this is provided by boundary
				"{{dumpling.ip}}",
				"hostname -i; sleep 60",
			),
			e2e.WithEnv("E2E_AUTH_TOKEN", token),
		)
	}()
	t.Cleanup(cancel)
	s := dumpling.WaitForSessionCli(t, ctx, newProjectId)
	dumpling.WaitForSessionStatusCli(t, ctx, s.Id, session.StatusActive.String())
	assert.Equal(t, newTargetId, s.TargetId)
	assert.Equal(t, newHostId, s.HostId)

	// Cancel session
	t.Log("Canceling session...")
	output = e2e.RunCommand(ctx, "dumpling",
		e2e.WithArgs("sessions", "cancel", "-id", s.Id),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	dumpling.WaitForSessionStatusCli(t, ctx, s.Id, session.StatusTerminated.String())

	// Check output from session
	select {
	case output := <-errChan:
		// `boundary connect` returns a 255 when cancelled
		require.Equal(t, 255, output.ExitCode, string(output.Stdout), string(output.Stderr))
	case <-time.After(time.Second * 5):
		t.Fatal("Timed out waiting for session command to exit")
	}

	t.Log("Successfully cancelled session")
}

// TestApiCreateUser uses the Go api to create a new user and add some grants to the user
func TestApiCreateUser(t *testing.T) {
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

	acctName := "e2e-account"
	newAcctId, _ := dumpling.CreateNewAccountApi(t, ctx, client, acctName)
	t.Cleanup(func() {
		aClient := accounts.NewClient(client)
		_, err := aClient.Delete(ctx, newAcctId)
		require.NoError(t, err)
	})
	newUserId := dumpling.CreateNewUserApi(t, ctx, client, "global")
	t.Cleanup(func() {
		uClient := users.NewClient(client)
		_, err := uClient.Delete(ctx, newUserId)
		require.NoError(t, err)
	})
	uClient := users.NewClient(client)
	uClient.SetAccounts(ctx, newUserId, 0, []string{newAcctId})

	rClient := roles.NewClient(client)
	newRoleResult, err := rClient.Create(ctx, newProjectId)
	require.NoError(t, err)
	newRoleId := newRoleResult.Item.Id
	t.Logf("Created Role: %s", newRoleId)

	_, err = rClient.AddGrants(ctx, newRoleId, 0, []string{"id=*;type=target;actions=authorize-session"},
		roles.WithAutomaticVersioning(true),
	)
	require.NoError(t, err)

	_, err = rClient.AddPrincipals(ctx, newRoleId, 0, []string{newUserId},
		roles.WithAutomaticVersioning(true),
	)
	require.NoError(t, err)
}
