// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controller

import (
	"context"

	"github.com/opendumpling/opendumpling/internal/daemon/controller/common"
)

func init() {
	// Install the in-memory graph of downstream workers.  The controller keeps
	// it up to date from the worker status calls it receives and uses it to
	// calculate ingress to egress routes when authorizing a session.
	downstreamersFactory = func(_ context.Context, rootId, _ string) (common.Downstreamers, error) {
		return common.NewDownstreamers(rootId), nil
	}
}
