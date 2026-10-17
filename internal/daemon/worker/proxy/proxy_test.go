// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/opendumpling/opendumpling/internal/gen/controller/servers/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRegisterHandler(t *testing.T) {
	assert, require := assert.New(t), require.New(t)

	fn := func(context.Context, context.Context, DecryptFn, net.Conn, *ProxyDialer, string, *anypb.Any, RecordingManager) (ProxyConnFn, error) {
		return nil, nil
	}
	oldHandler := handlers
	t.Cleanup(func() {
		handlers = oldHandler
	})
	handlers = sync.Map{}

	err := RegisterHandler("protocol", fn)
	require.NoError(err)

	// Register function with same protocol
	err = RegisterHandler("protocol", fn)
	require.Error(err)
	assert.ErrorIs(err, ErrProtocolAlreadyRegistered)

	err = RegisterHandler("new-protocol", fn)
	require.NoError(err)
}

func TestGetHandler(t *testing.T) {
	assert, require := assert.New(t), require.New(t)
	fn := func(context.Context, context.Context, DecryptFn, net.Conn, *ProxyDialer, string, *anypb.Any, RecordingManager) (ProxyConnFn, error) {
		return nil, nil
	}
	oldHandler := handlers
	t.Cleanup(func() {
		handlers = oldHandler
	})
	handlers = sync.Map{}

	// No handlers registered yet.
	_, err := protocolHandler("wid", nil)
	assert.ErrorIs(err, ErrUnknownProtocol)

	require.NoError(RegisterHandler(TcpHandlerName, fn))
	require.NoError(RegisterHandler(SshHandlerName, fn))

	// A nil protocol context falls back to the tcp handler.
	handler, err := protocolHandler("wid", nil)
	require.NoError(err)
	require.NotNil(handler)

	// An SshProtocolContext selects the ssh handler.
	sshCtx, err := anypb.New(&services.SshProtocolContext{})
	require.NoError(err)
	handler, err = protocolHandler("wid", sshCtx)
	require.NoError(err)
	require.NotNil(handler)

	// Any other protocol context uses the tcp handler.
	tcpCtx, err := anypb.New(wrapperspb.String("not-ssh"))
	require.NoError(err)
	handler, err = protocolHandler("wid", tcpCtx)
	require.NoError(err)
	require.NotNil(handler)
}
