// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package vault_test

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/opendumpling/opendumpling/api/credentiallibraries"
	"github.com/opendumpling/opendumpling/api/credentialstores"
	"github.com/opendumpling/opendumpling/api/hosts"
	"github.com/opendumpling/opendumpling/api/targets"
	"github.com/opendumpling/opendumpling/internal/cmd/config"
	"github.com/opendumpling/opendumpling/internal/credential/vault"
	"github.com/opendumpling/opendumpling/internal/daemon/controller"
	"github.com/opendumpling/opendumpling/internal/daemon/worker"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"

	_ "github.com/opendumpling/opendumpling/internal/daemon/controller/handlers/targets/tcp"
)

func TestSetupSleepyDevEnvironment(t *testing.T) {
	if os.Getenv("DUMPLING_SLEEPY_TESTS") == "" {
		t.Skip("This test exists to make manual testing and debugging easier by setting up an environment to test against.\n" +
			"Set DUMPLING_SLEEPY_TESTS to something non-zero to run the environment establishing test.")
	}

	logger := hclog.New(&hclog.LoggerOptions{
		Level: hclog.Info,
	})
	conf, err := config.DevController()
	require.NoError(t, err)
	c1 := controller.NewTestController(t, &controller.TestControllerOpts{
		Config:                 conf,
		InitialResourcesSuffix: "1234567890",
		Logger:                 logger.Named("c1"),
		DefaultPassword:        "password",
	})
	defer c1.Shutdown()
	ctx := c1.Context()
	// Ensure target is valid
	client := c1.Client()
	client.SetToken(c1.Token().Token)
	tcl := targets.NewClient(client)
	tgt, err := tcl.Read(ctx, "ttcp_1234567890")
	require.NoError(t, err)
	require.NotNil(t, tgt)
	// Setup credentials on target.
	v := vault.NewTestVaultServer(t, vault.WithDockerNetwork(true))
	// Mount database secrets
	testDb := v.MountDatabase(t)
	// It comes as a template so need to populate that before it can be parsed
	dbUrl, err := url.Parse(fmt.Sprintf(string(testDb.URL), "username", "password"))
	require.NoError(t, err)
	splitHost := strings.Split(dbUrl.Host, ":")
	host, portStr := splitHost[0], splitHost[1]
	hcl := hosts.NewClient(client)
	_, err = hcl.Update(ctx, "hst_1234567890", 0, hosts.WithStaticHostAddress(host), hosts.WithAutomaticVersioning(true))
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	_, err = tcl.Update(ctx, "ttcp_1234567890", 0, targets.WithTcpTargetDefaultPort(uint32(port)), targets.WithAutomaticVersioning(true))
	require.NoError(t, err)
	sec, _ := v.CreateToken(t, vault.WithPolicies([]string{"default", "dumpling-controller", "database"}))
	storeClient := credentialstores.NewClient(client)
	store, err := storeClient.Create(ctx, "vault", tgt.Item.ScopeId,
		credentialstores.WithVaultCredentialStoreAddress(v.Addr),
		credentialstores.WithVaultCredentialStoreToken(sec.Auth.ClientToken))
	require.NoError(t, err)
	require.NotNil(t, store)
	libClient := credentiallibraries.NewClient(client)
	lib, err := libClient.Create(ctx, store.Item.Type, store.Item.Id, credentiallibraries.WithVaultCredentialLibraryPath(path.Join("database", "creds", "opened")),
		credentiallibraries.WithVaultCredentialLibraryHttpMethod("GET"),
	)
	require.NoError(t, err)
	require.NotNil(t, lib)
	tgt, err = tcl.AddCredentialSources(ctx, tgt.Item.Id, 0, targets.WithAutomaticVersioning(true), targets.WithBrokeredCredentialSourceIds([]string{lib.Item.Id}))
	require.NoError(t, err)
	require.NotNil(t, tgt)
	// Worker 1
	conf, err = config.DevWorker()
	require.NoError(t, err)
	conf.Worker.Name = "w1"
	conf.Worker.Tags = map[string][]string{
		"region": {"east"},
		"foo":    {"bar"},
	}
	w1 := worker.NewTestWorker(t, &worker.TestWorkerOpts{
		Config:           conf,
		WorkerAuthKms:    c1.Config().WorkerAuthKms,
		InitialUpstreams: c1.ClusterAddrs(),
		Logger:           logger.Named("w1"),
	})
	defer w1.Shutdown()
	addr := c1.ApiAddrs()[0]
	t.Logf("Dumpling ready\n"+
		"dumpling authenticate password -addr %q -auth-method-id ampw_1234567890 -login-name admin -password password\n"+
		"dumpling connect -addr %q -target-id ttcp_1234567890 -format json\n", addr, addr)
	time.Sleep(time.Hour)
}
