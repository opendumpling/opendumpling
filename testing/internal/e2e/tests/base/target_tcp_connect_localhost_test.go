// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package base_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/opendumpling/opendumpling/testing/internal/e2e"
	"github.com/opendumpling/opendumpling/testing/internal/e2e/dumpling"
	"github.com/stretchr/testify/require"
)

func TestCliTcpTargetConnectTargetWithLocalhost(t *testing.T) {
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

	// Start a session
	ctxCancel, cancel := context.WithCancel(context.Background())
	port := "12345"
	cmdChan := make(chan *e2e.CommandResult)
	go func() {
		t.Log("Starting session...")
		cmdChan <- e2e.RunCommand(ctxCancel, "dumpling",
			e2e.WithArgs(
				"connect",
				"-target-id", newTargetId,
				"-listen-port", port,
				"-format", "json",
			),
		)
	}()
	t.Cleanup(cancel)

	dumpling.WaitForSessionCli(t, ctx, newProjectId)

	// Connect to target and print host's IP address
	output := e2e.RunCommand(ctx, "ssh",
		e2e.WithArgs(
			"localhost",
			"-p", port,
			"-l", c.TargetSshUser,
			"-i", c.TargetSshKeyPath,
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "StrictHostKeyChecking=no",
			"-o", "IdentitiesOnly=yes", // forces the use of the provided key
			"hostname -i",
		),
	)
	require.NoError(t, output.Err, string(output.Stderr))
	require.Equal(t, c.TargetIp, strings.TrimSpace(string(output.Stdout)))

	// Cancel session
	cancel()

	// Check output from session
	select {
	case output := <-cmdChan:
		type connectOutput struct {
			Port int
		}
		var response connectOutput
		err = json.Unmarshal(output.Stdout, &response)
		require.NoError(t, err)
		require.Equal(t, port, fmt.Sprint(response.Port))
	case <-time.After(time.Second * 5):
		t.Fatal("Timed out waiting for session command to exit")
	}
}
