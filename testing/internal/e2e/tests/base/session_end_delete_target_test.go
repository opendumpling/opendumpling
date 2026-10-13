// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package base_test

import (
	"context"
	"testing"
	"time"

	"github.com/opendumpling/opendumpling/internal/session"
	"github.com/opendumpling/opendumpling/testing/internal/e2e"
	"github.com/opendumpling/opendumpling/testing/internal/e2e/dumpling"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCliSessionEndWhenTargetIsDeleted tests that an active session is canceled when the respective
// target for the session is deleted.
func TestCliSessionEndWhenTargetIsDeleted(t *testing.T) {
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

	// Delete Target
	t.Log("Deleting target...")
	output := e2e.RunCommand(ctx, "dumpling", e2e.WithArgs("targets", "delete", "-id", newTargetId))
	require.NoError(t, output.Err, string(output.Stderr))

	// Check if session has terminated
	t.Log("Waiting for session to be canceling/terminated...")
	select {
	case output := <-errChan:
		// `boundary connect` returns a 255 when cancelled
		require.Equal(t, 255, output.ExitCode, string(output.Stdout), string(output.Stderr))
	case <-time.After(time.Second * 5):
		t.Fatal("Timed out waiting for session command to exit")
	}

	dumpling.WaitForSessionStatusCli(t, ctx, s.Id, session.StatusTerminated.String())
	t.Log("Session successfully ended after target was deleted")
}
