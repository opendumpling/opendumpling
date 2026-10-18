// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package workerscmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/opendumpling/opendumpling/api"
	"github.com/opendumpling/opendumpling/api/workers"
	"github.com/opendumpling/opendumpling/internal/cmd/base"
	"github.com/opendumpling/opendumpling/internal/cmd/common"
	"github.com/posener/complete"
)

var registerFlagsMap = map[string][]string{
	"register": {"scope-id", "name", "description"},
}

// RegisterCommand registers a worker from the registration request that worker
// printed when it started. It is the same call as
// "workers create worker-led", but reads the request from wherever the worker
// stored it, so that nothing has to be copied out of a log by hand.
type RegisterCommand struct {
	*base.Command

	flagAuthRequestFile string
}

func (c *RegisterCommand) Synopsis() string {
	return "Register a worker from the registration request it printed at startup"
}

func (c *RegisterCommand) Help() string {
	return base.WrapForHelpText([]string{
		"Usage: dumpling workers register [options]",
		"",
		"  Register a worker which has already been started. A worker writes its",
		"  Worker Auth Registration Request to the auth_request_token file in its",
		"  auth storage path, so registering it is a matter of pointing at that",
		"  file. Example:",
		"",
		"    $ dumpling workers register -name ingress-1 -auth-request-file /opt/dumpling/ingress/auth_request_token",
		"",
		"  Once the worker is registered it connects on its next status report;",
		"  it does not have to be restarted.",
		"",
	}) + c.Flags().Help()
}

func (c *RegisterCommand) Flags() *base.FlagSets {
	set := c.FlagSet(base.FlagSetHTTP | base.FlagSetClient | base.FlagSetOutputFormat)
	f := set.NewFlagSet("Command Options")
	common.PopulateCommonFlags(c.Command, f, "worker", registerFlagsMap, "register")

	f.StringVar(&base.StringVar{
		Name:       "auth-request-file",
		Target:     &c.flagAuthRequestFile,
		EnvVar:     "DUMPLING_WORKER_AUTH_REQUEST_FILE",
		Completion: complete.PredictAnything,
		Usage:      "Path of the file the worker wrote its Worker Auth Registration Request to. Use - to read the request from standard input instead.",
	})

	return set
}

func (c *RegisterCommand) Run(args []string) int {
	f := c.Flags()
	if err := f.Parse(args); err != nil {
		c.PrintCliError(err)
		return base.CommandUserError
	}

	authRequest, err := c.readAuthRequest()
	if err != nil {
		c.PrintCliError(err)
		return base.CommandUserError
	}

	client, err := c.Client()
	if c.WrapperCleanupFunc != nil {
		defer func() {
			if err := c.WrapperCleanupFunc(); err != nil {
				c.PrintCliError(fmt.Errorf("Error cleaning kms wrapper: %w", err))
			}
		}()
	}
	if err != nil {
		c.PrintCliError(fmt.Errorf("Error creating API client: %w", err))
		return base.CommandCliError
	}
	workersClient := workers.NewClient(client)

	var opts []workers.Option
	switch c.FlagName {
	case "":
	case "null":
		opts = append(opts, workers.DefaultName())
	default:
		opts = append(opts, workers.WithName(c.FlagName))
	}

	switch c.FlagDescription {
	case "":
	case "null":
		opts = append(opts, workers.DefaultDescription())
	default:
		opts = append(opts, workers.WithDescription(c.FlagDescription))
	}

	result, err := workersClient.CreateWorkerLed(c.Context, authRequest, c.FlagScopeId, opts...)
	switch {
	case err == nil:
	case api.AsServerError(err) != nil:
		c.PrintApiError(api.AsServerError(err), "Error from controller when registering worker")
		return base.CommandApiError
	default:
		c.PrintCliError(fmt.Errorf("Error trying to register worker: %s", err.Error()))
		return base.CommandCliError
	}

	switch base.Format(c.UI) {
	case "table":
		c.UI.Output(printItemTable(result.GetItem(), result.GetResponse()))
	case "json":
		if ok := c.PrintJsonItem(result.GetResponse()); !ok {
			return base.CommandCliError
		}
	}

	return base.CommandSuccess
}

func (c *RegisterCommand) readAuthRequest() (string, error) {
	switch c.flagAuthRequestFile {
	case "":
		return "", errors.New("a worker auth registration request is required; pass -auth-request-file or set DUMPLING_WORKER_AUTH_REQUEST_FILE")

	case "-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("error reading worker auth registration request from standard input: %w", err)
		}
		return checkAuthRequest(string(data))

	default:
		data, err := os.ReadFile(c.flagAuthRequestFile)
		if err != nil {
			return "", fmt.Errorf("error reading worker auth registration request file: %w", err)
		}
		return checkAuthRequest(string(data))
	}
}

// checkAuthRequest guards against the two ways the request is most commonly
// mangled on its way into the file: leftover whitespace from whatever wrote it,
// and the whole log line rather than just the token itself.
func checkAuthRequest(in string) (string, error) {
	switch in = strings.TrimSpace(in); {
	case in == "":
		return "", errors.New("worker auth registration request is empty")
	case strings.ContainsAny(in, " \t\r\n"):
		return "", errors.New("worker auth registration request contains whitespace; expected only the token itself")
	default:
		return in, nil
	}
}
