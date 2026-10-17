// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"context"
	"math"

	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/handlers"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/handlers/targets"
	"github.com/opendumpling/opendumpling/internal/session"
	"github.com/opendumpling/opendumpling/internal/target"
	"github.com/opendumpling/opendumpling/internal/target/ssh"
	sshStore "github.com/opendumpling/opendumpling/internal/target/ssh/store"
	"github.com/opendumpling/opendumpling/internal/target/store"
	pb "github.com/opendumpling/opendumpling/sdk/pbs/controller/api/resources/targets"
)

const (
	defaultPortField       = "attributes.default_port"
	defaultClientPortField = "attributes.default_client_port"
)

type attribute struct {
	*pb.SshTargetAttributes
}

func (a *attribute) Options() []target.Option {
	var opts []target.Option

	// SSH targets default to port 22 when no default port is provided.
	if a.GetDefaultPort() == nil || a.GetDefaultPort().GetValue() == 0 {
		opts = append(opts, target.WithDefaultPort(ssh.DefaultSSHPort))
	} else {
		opts = append(opts, target.WithDefaultPort(a.GetDefaultPort().GetValue()))
	}

	if a.GetDefaultClientPort().GetValue() != 0 {
		opts = append(opts, target.WithDefaultClientPort(a.GetDefaultClientPort().GetValue()))
	}
	if a.GetStorageBucketId() != nil {
		opts = append(opts, target.WithStorageBucketId(a.GetStorageBucketId().GetValue()))
	}
	if a.GetEnableSessionRecording() != nil {
		opts = append(opts, target.WithEnableSessionRecording(a.GetEnableSessionRecording().GetValue()))
	}
	return opts
}

func (a *attribute) Vet() map[string]string {
	badFields := map[string]string{}

	// default_port is optional for SSH targets; when provided it must be a
	// valid, non-zero port number.
	if a.GetDefaultPort() != nil {
		if a.GetDefaultPort().GetValue() == 0 {
			badFields[defaultPortField] = "This field cannot be set to zero."
		}
		if a.GetDefaultPort().GetValue() > math.MaxUint16 {
			badFields[defaultPortField] = "Value is greater than maximum port number."
		}
	}
	if a.GetDefaultClientPort() != nil {
		if a.GetDefaultClientPort().GetValue() == 0 {
			badFields[defaultClientPortField] = "This field cannot be set to zero."
		}
		if a.GetDefaultClientPort().GetValue() > math.MaxUint16 {
			badFields[defaultClientPortField] = "Value is greater than maximum port number."
		}
	}
	return badFields
}

func (a *attribute) VetForUpdate(p []string) map[string]string {
	badFields := map[string]string{}
	if handlers.MaskContains(p, defaultPortField) {
		if a.GetDefaultPort() == nil {
			badFields[defaultPortField] = "This field is required."
		} else {
			if a.GetDefaultPort().GetValue() == 0 {
				badFields[defaultPortField] = "This cannot be set to zero."
			}
			if a.GetDefaultPort().GetValue() > math.MaxUint16 {
				badFields[defaultPortField] = "Value is greater than maximum port number."
			}
		}
	}
	if handlers.MaskContains(p, defaultClientPortField) && a.GetDefaultClientPort() != nil {
		if a.GetDefaultClientPort().GetValue() == 0 {
			badFields[defaultClientPortField] = "This cannot be set to zero."
		}
		if a.GetDefaultClientPort().GetValue() > math.MaxUint16 {
			badFields[defaultClientPortField] = "Value is greater than maximum port number."
		}
	}
	return badFields
}

func newAttribute(m any) targets.Attributes {
	a := &attribute{
		&pb.SshTargetAttributes{},
	}
	if sshAttr, ok := m.(*pb.Target_SshTargetAttributes); ok {
		a.SshTargetAttributes = sshAttr.SshTargetAttributes
	}
	return a
}

func setAttributes(t target.Target, out *pb.Target) error {
	if t == nil {
		return nil
	}

	attrs := &pb.Target_SshTargetAttributes{
		SshTargetAttributes: &pb.SshTargetAttributes{},
	}
	if t.GetDefaultPort() > 0 {
		attrs.SshTargetAttributes.DefaultPort = &wrappers.UInt32Value{Value: t.GetDefaultPort()}
	}
	if t.GetDefaultClientPort() > 0 {
		attrs.SshTargetAttributes.DefaultClientPort = &wrappers.UInt32Value{Value: t.GetDefaultClientPort()}
	}
	if t.GetStorageBucketId() != "" {
		attrs.SshTargetAttributes.StorageBucketId = &wrappers.StringValue{Value: t.GetStorageBucketId()}
	}
	if t.GetEnableSessionRecording() {
		attrs.SshTargetAttributes.EnableSessionRecording = &wrappers.BoolValue{Value: t.GetEnableSessionRecording()}
	}

	out.Attrs = attrs
	return nil
}

func noopSessionValidation(context.Context, *session.Session) error { return nil }

func init() {
	var maskManager handlers.MaskManager
	var err error

	if maskManager, err = handlers.NewMaskManager(
		context.Background(),
		handlers.MaskDestination{&sshStore.Target{}, &store.TargetAddress{}},
		handlers.MaskSource{&pb.Target{}, &pb.SshTargetAttributes{}},
	); err != nil {
		panic(err)
	}

	targets.Register(ssh.Subtype, maskManager, newAttribute, setAttributes, noopSessionValidation)
}
