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

// TestCliSessionCancelAdmin uses the boundary cli to start and then cancel a session
func TestCliSessionCancelAdmin(t *testing.T) {
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

	// Connect to target to create a session
	ctxCancel, cancel := context.WithCancel(context.Background())
	errChan := make(chan *e2e.CommandResult)
	go func() {
		t.Log("Starting session...")
		errChan <- e2e.RunCommand(ctxCancel, "dumpling",
			e2e.WithArgs(
				"connect",
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
		)
	}()
	t.Cleanup(cancel)
	s := dumpling.WaitForSessionCli(t, ctx, newProjectId)
	dumpling.WaitForSessionStatusCli(t, ctx, s.Id, session.StatusActive.String())
	assert.Equal(t, newTargetId, s.TargetId)
	assert.Equal(t, newHostId, s.HostId)

	// Cancel session
	t.Log("Canceling session...")
	output := e2e.RunCommand(ctx, "dumpling",
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
