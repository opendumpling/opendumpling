// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package workerscmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendumpling/opendumpling/internal/cmd/base"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRegisterCommand(t *testing.T) *RegisterCommand {
	t.Helper()
	return &RegisterCommand{Command: base.NewCommand(cli.NewMockUi())}
}

func TestCheckAuthRequest(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		exp          string
		expErrSubstr string
	}{
		{
			name: "a bare token",
			in:   "GzusqckarbczHoLGQ4UA25uSQrUxChDeeEbizy5XUx8g",
			exp:  "GzusqckarbczHoLGQ4UA25uSQrUxChDeeEbizy5XUx8g",
		},
		{
			name: "surrounding whitespace is dropped",
			in:   "\nGzusqckarbczHoLGQ4UA25uSQrUxChDeeEbizy5XUx8g\n",
			exp:  "GzusqckarbczHoLGQ4UA25uSQrUxChDeeEbizy5XUx8g",
		},
		{
			name:         "an empty request",
			in:           "   \n",
			expErrSubstr: "empty",
		},
		{
			name:         "the whole log line instead of the token",
			in:           "==> Worker Auth Registration Request: GzusqckarbczHoLGQ4UA25uSQrUxChDeeEbizy5XUx8g",
			expErrSubstr: "whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checkAuthRequest(tt.in)
			if tt.expErrSubstr != "" {
				require.ErrorContains(t, err, tt.expErrSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.exp, got)
		})
	}
}

func TestReadAuthRequestRequiresAFile(t *testing.T) {
	_, err := newRegisterCommand(t).readAuthRequest()
	require.ErrorContains(t, err, "auth-request-file")
}

func TestReadAuthRequestFromStdin(t *testing.T) {
	orig := os.Stdin
	t.Cleanup(func() { os.Stdin = orig })

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r
	_, err = w.WriteString("GzusqckarbczHoLGQ4UA25uSQrUxChDeeEbizy5XUx8g\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	c := newRegisterCommand(t)
	c.flagAuthRequestFile = "-"

	got, err := c.readAuthRequest()
	require.NoError(t, err)
	assert.Equal(t, "GzusqckarbczHoLGQ4UA25uSQrUxChDeeEbizy5XUx8g", got)
}

func TestReadAuthRequestFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth_request_token")
	require.NoError(t, os.WriteFile(path, []byte("GzusqckarbczHoLGQ4UA25uSQrUxChDeeEbizy5XUx8g\n"), 0o600))

	c := newRegisterCommand(t)
	c.flagAuthRequestFile = path

	got, err := c.readAuthRequest()
	require.NoError(t, err)
	assert.Equal(t, "GzusqckarbczHoLGQ4UA25uSQrUxChDeeEbizy5XUx8g", got)
}

func TestReadAuthRequestReportsAMissingFile(t *testing.T) {
	c := newRegisterCommand(t)
	c.flagAuthRequestFile = filepath.Join(t.TempDir(), "does-not-exist")

	_, err := c.readAuthRequest()
	require.ErrorContains(t, err, "does-not-exist")
}
