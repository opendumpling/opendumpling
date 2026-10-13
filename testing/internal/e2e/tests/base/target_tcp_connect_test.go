// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package base_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opendumpling/opendumpling/internal/target"
	"github.com/opendumpling/opendumpling/testing/internal/e2e"
	"github.com/opendumpling/opendumpling/testing/internal/e2e/dumpling"
)

// TestCliTcpTargetConnectTargetBasic uses the boundary cli to create a number of
// supporting objects to connect to a target. It then attempts to connect to
// that target and verifies that the connection was successful.
func TestCliTcpTargetConnectTargetBasic(t *testing.T) {
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

	// Connect to target and print host's IP address
	output := e2e.RunCommand(ctx, "dumpling",
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
			"hostname", "-i",
		),
	)
	require.NoError(t, output.Err, string(output.Stderr))

	parts := strings.Fields(string(output.Stdout))
	hostIp := parts[len(parts)-1]
	require.Equal(t, c.TargetIp, hostIp, "SSH session did not return expected output")
	t.Log("Successfully connected to target")
}

// TestCliTcpTargetConnectTargetBasic uses the boundary cli to create a number of
// supporting objects to connect to a target. It then attempts to connect to
// that target via the combination of target name and target scope name,
// and verifies that the connection was successful.
func TestCliTcpTargetConnectTargetViaTargetAndScopeNames(t *testing.T) {
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
	testProjectName := `E2E/Project-With\Name`
	testTargetName := `E2E/Test-Target-With\Name`
	newProjectId := dumpling.CreateNewProjectCli(t, ctx, newOrgId, dumpling.WithName(testProjectName))
	newHostCatalogId := dumpling.CreateNewHostCatalogCli(t, ctx, newProjectId)
	newHostSetId := dumpling.CreateNewHostSetCli(t, ctx, newHostCatalogId)
	newHostId := dumpling.CreateNewHostCli(t, ctx, newHostCatalogId, c.TargetIp)
	dumpling.AddHostToHostSetCli(t, ctx, newHostSetId, newHostId)
	newTargetId := dumpling.CreateNewTargetCli(t, ctx, newProjectId, c.TargetPort, target.WithName(testTargetName))
	dumpling.AddHostSourceToTargetCli(t, ctx, newTargetId, newHostSetId)

	// Connect to target via target and scope names, and print host's IP address
	output := e2e.RunCommand(ctx, "dumpling",
		e2e.WithArgs(
			"connect",
			"-target-name", testTargetName,
			"-target-scope-name", testProjectName,
			"-exec", "/usr/bin/ssh", "--",
			"-l", c.TargetSshUser,
			"-i", c.TargetSshKeyPath,
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "StrictHostKeyChecking=no",
			"-o", "IdentitiesOnly=yes", // forces the use of the provided key
			"-p", "{{dumpling.port}}", // this is provided by boundary
			"{{dumpling.ip}}",
			"hostname", "-i",
		),
	)
	require.NoError(t, output.Err, string(output.Stderr))

	parts := strings.Fields(string(output.Stdout))
	hostIp := parts[len(parts)-1]
	require.Equal(t, c.TargetIp, hostIp, "SSH session did not return expected output")
	t.Log("Successfully connected to target by its name and scope name")

	// Connect to target via target name and scope ID, and print host's IP address
	output = e2e.RunCommand(ctx, "dumpling",
		e2e.WithArgs(
			"connect",
			"-target-name", testTargetName,
			"-target-scope-id", newProjectId,
			"-exec", "/usr/bin/ssh", "--",
			"-l", c.TargetSshUser,
			"-i", c.TargetSshKeyPath,
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "StrictHostKeyChecking=no",
			"-o", "IdentitiesOnly=yes", // forces the use of the provided key
			"-p", "{{dumpling.port}}", // this is provided by boundary
			"{{dumpling.ip}}",
			"hostname", "-i",
		),
	)
	require.NoError(t, output.Err, string(output.Stderr))

	parts = strings.Fields(string(output.Stdout))
	hostIp = parts[len(parts)-1]
	require.Equal(t, c.TargetIp, hostIp, "SSH session did not return expected output")
	t.Log("Successfully connected to target by its name and scope ID")
}

func TestCliTcpTargetConnectTargetWithTargetClientPort(t *testing.T) {
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

	var expPort uint32 = 8356

	newProjectId := dumpling.CreateNewProjectCli(t, ctx, newOrgId)
	newHostCatalogId := dumpling.CreateNewHostCatalogCli(t, ctx, newProjectId)
	newHostSetId := dumpling.CreateNewHostSetCli(t, ctx, newHostCatalogId)
	newHostId := dumpling.CreateNewHostCli(t, ctx, newHostCatalogId, c.TargetIp)
	dumpling.AddHostToHostSetCli(t, ctx, newHostSetId, newHostId)
	newTargetId := dumpling.CreateNewTargetCli(t, ctx, newProjectId, c.TargetPort, target.WithDefaultClientPort(expPort))
	dumpling.AddHostSourceToTargetCli(t, ctx, newTargetId, newHostSetId)

	// Connect to target and print host's IP address
	output := e2e.RunCommand(ctx, "dumpling",
		e2e.WithArgs(
			"connect",
			"-target-id", newTargetId,
			"-exec", "/bin/echo", "--",
			"{{dumpling.port}}",
		),
	)
	require.NoError(t, output.Err, string(output.Stderr))

	parts := strings.Fields(string(output.Stdout))
	port := parts[len(parts)-1]

	require.Equal(t, fmt.Sprintf("%d", expPort), port, "specified port was not found")
}
