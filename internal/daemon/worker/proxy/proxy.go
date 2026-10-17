// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/opendumpling/opendumpling/internal/gen/controller/servers/services"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	TcpHandlerName = "tcp"
	SshHandlerName = "ssh"

	// handlers is the map of registered handlers
	handlers sync.Map

	// ErrUnknownProtocol specifies the provided protocol has no registered handler
	ErrUnknownProtocol = errors.New("proxy: handler not found for protocol")

	// ErrProtocolAlreadyRegistered specifies the provided protocol has already been registered
	ErrProtocolAlreadyRegistered = errors.New("proxy: protocol already registered")

	// GetHandler returns the handler registered for the provided worker and
	// protocolContext. If a protocol cannot be determined or the protocol is
	// not registered nil, ErrUnknownProtocol is returned.
	GetHandler = protocolHandler
)

// protocolHandler selects the proxy handler to use based on the protocol
// context provided by the controller. A SshProtocolContext indicates the worker
// must terminate the SSH connection from the client and inject credentials when
// connecting to the endpoint; any other (or absent) protocol context is treated
// as a straightforward tcp proxy.
func protocolHandler(_ string, protocolCtx proto.Message) (Handler, error) {
	if a, ok := protocolCtx.(*anypb.Any); ok && a != nil && a.MessageIs(&services.SshProtocolContext{}) {
		return loadHandler(SshHandlerName)
	}
	return loadHandler(TcpHandlerName)
}

// loadHandler returns the handler registered for the provided protocol.
func loadHandler(protocol string) (Handler, error) {
	handler, ok := handlers.Load(protocol)
	if !ok {
		return nil, ErrUnknownProtocol
	}
	return handler.(Handler), nil
}

// RecordingManager allows a handler for a protocol that supports recording.
type RecordingManager any

// DecryptFn decrypts the provided bytes into a proto.Message
type DecryptFn func(ctx context.Context, from []byte, to proto.Message) error

// ProxyConnFn is called after the call to ConnectConnection on the cluster.
// ProxyConnFn blocks until the specific request that is being proxied is finished
type ProxyConnFn func()

// Handler is the type that all proxies need to implement to be called by the worker
// when a new client connection is created.  If there is an error ProxyConnFn must
// be nil. If there is no error ProxyConnFn must be set.  When Handler has
// returned, it is expected that the initial connection to the endpoint has been
// established.
type Handler func(controlCtx context.Context, dataCtx context.Context, df DecryptFn, c net.Conn, pd *ProxyDialer, connId string, pb *anypb.Any, rm RecordingManager) (ProxyConnFn, error)

func RegisterHandler(protocol string, handler Handler) error {
	_, loaded := handlers.LoadOrStore(protocol, handler)
	if loaded {
		return ErrProtocolAlreadyRegistered
	}
	return nil
}
