// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/opendumpling/opendumpling/internal/credential"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/target"
	"github.com/opendumpling/opendumpling/internal/target/ssh/store"
)

type targetHooks struct{}

func init() {
	target.Register(Subtype, targetHooks{}, TargetPrefix)
}

const (
	// TargetPrefix is the prefix for public ids of a ssh.Target.
	TargetPrefix = "tssh"
)

func (h targetHooks) Vet(ctx context.Context, t target.Target) error {
	const op = "ssh.vet"

	tt, ok := t.(*Target)
	if !ok {
		return errors.New(ctx, errors.InvalidParameter, op, "target is not a ssh.Target")
	}

	if tt == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "missing target")
	}

	if tt.Target == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "missing target store")
	}
	if tt.GetDefaultPort() > math.MaxUint16 {
		return errors.New(ctx, errors.InvalidParameter, op, "invalid default port number")
	}
	if tt.GetDefaultClientPort() > math.MaxUint16 {
		return errors.New(ctx, errors.InvalidParameter, op, "invalid default client port number")
	}
	return nil
}

func (h targetHooks) VetForUpdate(ctx context.Context, t target.Target, paths []string) error {
	const op = "ssh.vetForUpdate"

	tt, ok := t.(*Target)
	if !ok {
		return errors.New(ctx, errors.InvalidParameter, op, "target is not a ssh.Target")
	}

	switch {
	case tt == nil:
		return errors.New(ctx, errors.InvalidParameter, op, "missing target")
	case tt.Target == nil:
		return errors.New(ctx, errors.InvalidParameter, op, "missing target store")
	}

	for _, f := range paths {
		if strings.EqualFold("defaultport", f) {
			if tt.GetDefaultPort() > math.MaxUint16 {
				return errors.New(ctx, errors.InvalidParameter, op, "invalid default port number")
			}
		}
		if strings.EqualFold("defaultclientport", f) {
			if tt.GetDefaultClientPort() > math.MaxUint16 {
				return errors.New(ctx, errors.InvalidParameter, op, "invalid default client port number")
			}
		}
	}

	return nil
}

// VetCredentialSources checks that credential sources are valid for SSH targets.
// SSH targets support both brokered and injected credentials.
// For injected purposes (worker-side injection), credentials like ssh_certificate are valid.
func (h targetHooks) VetCredentialSources(ctx context.Context, libs []*target.CredentialLibrary, creds []*target.StaticCredential) error {
	const op = "ssh.VetCredentialSources"

	for _, c := range libs {
		purpose := c.GetCredentialPurpose()
		// Accept both brokered and injected purposes for ssh targets
		if purpose != string(credential.BrokeredPurpose) && purpose != string(credential.InjectedApplicationPurpose) {
			return errors.New(ctx, errors.InvalidParameter, op, fmt.Sprintf("ssh.Target only supports credential purposes: %q or %q", credential.BrokeredPurpose, credential.InjectedApplicationPurpose))
		}
	}
	for _, c := range creds {
		purpose := c.GetCredentialPurpose()
		if purpose != string(credential.BrokeredPurpose) && purpose != string(credential.InjectedApplicationPurpose) {
			return errors.New(ctx, errors.InvalidParameter, op, fmt.Sprintf("ssh.Target only supports credential purposes: %q or %q", credential.BrokeredPurpose, credential.InjectedApplicationPurpose))
		}
	}
	return nil
}

// NewTarget creates a new in memory ssh target.
func (h targetHooks) NewTarget(ctx context.Context, projectId string, opt ...target.Option) (target.Target, error) {
	const op = "ssh.NewTarget"
	opts := target.GetOpts(opt...)
	if projectId == "" {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing project id")
	}
	// SSH targets default to port 22 when no port is provided.
	defaultPort := opts.WithDefaultPort
	if defaultPort == 0 {
		defaultPort = DefaultSSHPort
	}
	t := &Target{
		Target: &store.Target{
			ProjectId:              projectId,
			Name:                   opts.WithName,
			Description:            opts.WithDescription,
			DefaultPort:            defaultPort,
			DefaultClientPort:      opts.WithDefaultClientPort,
			SessionConnectionLimit: opts.WithSessionConnectionLimit,
			SessionMaxSeconds:      opts.WithSessionMaxSeconds,
			WorkerFilter:           opts.WithWorkerFilter,
			EgressWorkerFilter:     opts.WithEgressWorkerFilter,
			IngressWorkerFilter:    opts.WithIngressWorkerFilter,
			EnableSessionRecording: opts.WithEnableSessionRecording,
			StorageBucketId:        opts.WithStorageBucketId,
		},
		Address: opts.WithAddress,
	}
	return t, nil
}
