// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package version

import (
	"testing"

	gvers "github.com/hashicorp/go-version"
	"github.com/stretchr/testify/assert"
)

func TestFromVersionString(t *testing.T) {
	cases := []struct {
		input string
		want  *Info
	}{
		{
			input: "OpenDumpling v0.12.0-beta+dev.int (revision)",
			want: &Info{
				Revision:          "revision",
				Version:           "0.12.0",
				VersionPrerelease: "beta",
				VersionMetadata:   "dev.int",
			},
		},
		{
			input: "OpenDumpling v0.12.0+dev.int",
			want: &Info{
				Version:         "0.12.0",
				VersionMetadata: "dev.int",
			},
		},
		{
			input: "OpenDumpling v0.12.0",
			want: &Info{
				Version: "0.12.0",
			},
		},
		{
			input: "0.12.0-alpha+dev.int (revision)",
			want: &Info{
				Revision:          "revision",
				Version:           "0.12.0",
				VersionPrerelease: "alpha",
				VersionMetadata:   "dev.int",
			},
		},
		{
			input: " 0.12.0-dev+int ",
			want: &Info{
				Version:           "0.12.0",
				VersionPrerelease: "dev",
				VersionMetadata:   "int",
			},
		},
		{
			input: "0.12.0- spaces in the prerelease are invalid",
			want:  nil,
		},
		{
			input: "OpenDumpling (version unknown)",
			want:  nil,
		},
		{
			input: "(version unknown)",
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := FromVersionString(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSemver(t *testing.T) {
	cases := []struct {
		name   string
		toTest *Info
		want   *gvers.Version
	}{
		{
			name: "0.12.0+dev.int",
			toTest: &Info{
				Version:         "0.12.0",
				VersionMetadata: "dev.int",
			},
			want: gvers.Must(gvers.NewSemver("0.12.0+dev.int")),
		},
		{
			name: "0.12.0+dev.int (revision)",
			toTest: &Info{
				Version:         "0.12.0",
				VersionMetadata: "dev.int",
				Revision:        "revision",
			},
			want: gvers.Must(gvers.NewSemver("0.12.0+dev.int")),
		},
		{
			name: "unparsable",
			toTest: &Info{
				Version:         "some unparsable string",
				VersionMetadata: "dev.int",
			},
			want: nil,
		},
		{
			name:   "nil info",
			toTest: nil,
			want:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.toTest.Semver()
			assert.Equal(t, tc.want, got)
		})
	}
}
