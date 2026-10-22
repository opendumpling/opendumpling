// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package sshconfigcmd

import (
	"strings"
	"testing"
)

func TestRenderSSHConfig(t *testing.T) {
	got := renderSSHConfig([]configAlias{
		{value: "database", scopeId: "p_project123"},
		{value: "web", scopeId: "o_org123456"},
		{value: "database", scopeId: "o_org123456"},
	}, "/opt/dumpling cli")

	for _, want := range []string{
		"Host database-o_org123456",
		"Host database-p_project123",
		"Host web",
		"ProxyCommand '/opt/dumpling cli' connect -format ssh-proxy -alias 'database' -alias-scope-id 'o_org123456'",
		"ProxyCommand '/opt/dumpling cli' connect -format ssh-proxy -alias 'web' -alias-scope-id 'o_org123456'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderSSHConfig() missing %q in output:\n%s", want, got)
		}
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("dumpling's cli")
	want := "'dumpling'\\''s cli'"
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}
