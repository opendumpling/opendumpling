// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package ssh implements the worker side of proxying an SSH session.
//
// Unlike the tcp proxy, the worker does not blindly copy bytes between the
// client and the endpoint. Instead it terminates the SSH connection coming from
// the client and establishes a new SSH connection to the endpoint using the
// credentials injected by the controller via the SshProtocolContext. This
// allows Dumpling to sign short lived SSH certificates (or broker any other
// credential) without ever exposing the private key to the user's client.
package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"sync"
	"time"

	"github.com/opendumpling/opendumpling/internal/daemon/worker/proxy"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/gen/controller/servers/services"
	gssh "golang.org/x/crypto/ssh"
	"google.golang.org/protobuf/types/known/anypb"
)

func init() {
	if err := proxy.RegisterHandler(proxy.SshHandlerName, handleProxy); err != nil {
		panic(err)
	}
}

// handleProxy terminates the SSH connection from the client and establishes an
// SSH connection to the endpoint using the injected credentials.
//
// When handleProxy returns the connection to the endpoint has been established
// (both the SSH handshake with the client and the SSH handshake with the
// endpoint are complete). The returned ProxyConnFn bridges the two connections
// and blocks until either side closes.
func handleProxy(controlCtx context.Context, _ context.Context, _ proxy.DecryptFn, conn net.Conn, out *proxy.ProxyDialer, connId string, pb *anypb.Any, _ proxy.RecordingManager) (proxy.ProxyConnFn, error) {
	const op = "ssh.handleProxy"
	switch {
	case conn == nil:
		return nil, errors.New(controlCtx, errors.InvalidParameter, op, "conn is nil")
	case out == nil:
		return nil, errors.New(controlCtx, errors.InvalidParameter, op, "proxy dialer is nil")
	case len(connId) == 0:
		return nil, errors.New(controlCtx, errors.InvalidParameter, op, "connection id is empty")
	case pb == nil:
		return nil, errors.New(controlCtx, errors.InvalidParameter, op, "protocol context is nil")
	}

	protoCtx := &services.SshProtocolContext{}
	if err := pb.UnmarshalTo(protoCtx); err != nil {
		return nil, errors.Wrap(controlCtx, err, op, errors.WithMsg("unable to unmarshal ssh protocol context"))
	}

	// The worker impersonates an SSH server to the client. The client used by
	// `dumpling connect ssh` disables host key verification for localhost, so
	// an ephemeral host key is sufficient.
	serverConfig, err := newServerConfig()
	if err != nil {
		return nil, errors.Wrap(controlCtx, err, op, errors.WithMsg("unable to create ssh server config"))
	}
	clientConn, clientChans, clientReqs, err := gssh.NewServerConn(conn, serverConfig)
	if err != nil {
		return nil, errors.Wrap(controlCtx, err, op, errors.WithMsg("unable to establish ssh connection with client"))
	}

	// Dial the endpoint and establish an SSH connection to it using the
	// injected credentials.
	remoteConn, err := out.Dial(controlCtx)
	if err != nil {
		_ = clientConn.Close()
		return nil, errors.Wrap(controlCtx, err, op, errors.WithMsg("unable to dial endpoint"))
	}
	clientConfig, err := endpointClientConfig(controlCtx, protoCtx, clientConn.User())
	if err != nil {
		_ = clientConn.Close()
		_ = remoteConn.Close()
		return nil, errors.Wrap(controlCtx, err, op, errors.WithMsg("unable to create ssh client config"))
	}
	endpointConn, endpointChans, endpointReqs, err := gssh.NewClientConn(remoteConn, remoteConn.RemoteAddr().String(), clientConfig)
	if err != nil {
		_ = clientConn.Close()
		_ = remoteConn.Close()
		return nil, errors.Wrap(controlCtx, err, op, errors.WithMsg("unable to establish ssh connection with endpoint"))
	}

	return func() {
		defer func() {
			_ = clientConn.Close()
			_ = endpointConn.Close()
			_ = remoteConn.Close()
		}()
		bridge(clientConn, clientChans, clientReqs, endpointConn, endpointChans, endpointReqs)
	}, nil
}

// newServerConfig returns an SSH server config with an ephemeral host key. The
// client's host key verification is disabled for localhost connections, so the
// host key only needs to be well formed.
func newServerConfig() (*gssh.ServerConfig, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	signer, err := gssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	config := &gssh.ServerConfig{
		// Authentication of the user is performed by Dumpling before the
		// session is authorized, so the worker accepts the client's "none"
		// authentication. This prevents the client from being prompted for
		// credentials it does not have.
		NoClientAuth: true,
	}
	config.AddHostKey(signer)
	return config, nil
}

// bridge proxies channels and global requests between the client connection
// (where the worker acts as the SSH server) and the endpoint connection (where
// the worker acts as the SSH client).
func bridge(clientConn gssh.Conn, clientChans <-chan gssh.NewChannel, clientReqs <-chan *gssh.Request, endpointConn gssh.Conn, endpointChans <-chan gssh.NewChannel, endpointReqs <-chan *gssh.Request) {
	done := make(chan struct{}, 2)

	// Global requests are forwarded in both directions (e.g. keepalives).
	go forwardGlobalRequests(clientReqs, endpointConn)
	go forwardGlobalRequests(endpointReqs, clientConn)

	// Channels opened by the client are opened on the endpoint and bridged.
	go func() {
		for newChan := range clientChans {
			go handleChannel(newChan, endpointConn)
		}
		done <- struct{}{}
	}()

	// Channels opened by the endpoint are opened on the client and bridged.
	// This is uncommon but covers cases like agent forwarding.
	go func() {
		for newChan := range endpointChans {
			go handleChannel(newChan, clientConn)
		}
		done <- struct{}{}
	}()

	// Return once either connection has been closed. The deferred Close in the
	// caller tears down the other side.
	<-done
}

// forwardGlobalRequests forwards connection level requests to the provided
// connection, replying with the result.
func forwardGlobalRequests(reqs <-chan *gssh.Request, to gssh.Conn) {
	for req := range reqs {
		ok, payload, err := to.SendRequest(req.Type, req.WantReply, req.Payload)
		if err != nil {
			return
		}
		if req.WantReply {
			if err := req.Reply(ok, payload); err != nil {
				return
			}
		}
	}
}

// handleChannel opens the channel described by newChan on the remote connection
// and copies data and channel requests between the two channels until they are
// closed.
func handleChannel(newChan gssh.NewChannel, to gssh.Conn) {
	remoteChan, remoteReqs, err := to.OpenChannel(newChan.ChannelType(), newChan.ExtraData())
	if err != nil {
		_ = newChan.Reject(gssh.ConnectionFailed, "unable to open channel on remote")
		return
	}
	localChan, localReqs, err := newChan.Accept()
	if err != nil {
		_ = remoteChan.Close()
		return
	}

	// Requests sent by the client (shell, exec, pty-req, window-change, ...)
	// are forwarded to the endpoint and vice versa (exit-status, exit-signal).
	localReqsDone := make(chan struct{})
	remoteReqsDone := make(chan struct{})
	go func() {
		defer close(localReqsDone)
		forwardChannelRequests(localReqs, remoteChan)
	}()
	go func() {
		defer close(remoteReqsDone)
		forwardChannelRequests(remoteReqs, localChan)
	}()

	var copyWg sync.WaitGroup
	copyWg.Add(2)
	go func() {
		defer copyWg.Done()
		_, _ = io.Copy(remoteChan, localChan)
		_ = remoteChan.CloseWrite()
	}()
	go func() {
		defer copyWg.Done()
		_, _ = io.Copy(localChan, remoteChan)
		_ = localChan.CloseWrite()
	}()
	copyWg.Wait()

	// Drain the endpoint's requests before closing the client channel so that
	// exit-status and exit-signal reach the client.
	<-remoteReqsDone
	_ = localChan.Close()
	_ = remoteChan.Close()
	<-localReqsDone
}

// forwardChannelRequests forwards channel requests to the provided channel,
// replying with the result.
func forwardChannelRequests(reqs <-chan *gssh.Request, to gssh.Channel) {
	for req := range reqs {
		ok, err := to.SendRequest(req.Type, req.WantReply, req.Payload)
		if err != nil {
			return
		}
		if req.WantReply {
			if err := req.Reply(ok, nil); err != nil {
				return
			}
		}
	}
}

// endpointClientConfig builds the SSH client configuration used to connect to
// the endpoint from the credentials injected by the controller. If a credential
// does not carry a username the username requested by the client is used.
func endpointClientConfig(ctx context.Context, protoCtx *services.SshProtocolContext, requestedUser string) (*gssh.ClientConfig, error) {
	const op = "ssh.endpointClientConfig"
	var (
		username string
		auths    []gssh.AuthMethod
	)
	for _, cred := range protoCtx.GetCredentials() {
		switch c := cred.GetCredential().(type) {
		case *services.Credential_SshCertificate:
			if c.SshCertificate.GetPrivateKey() == "" || c.SshCertificate.GetCertificate() == "" {
				continue
			}
			signer, err := signerWithCertificate(c.SshCertificate.GetPrivateKey(), c.SshCertificate.GetCertificate())
			if err != nil {
				return nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to create signer from ssh certificate credential"))
			}
			auths = append(auths, gssh.PublicKeys(signer))
			if u := c.SshCertificate.GetUsername(); u != "" {
				username = u
			}
		case *services.Credential_SshPrivateKey:
			if c.SshPrivateKey.GetPrivateKey() == "" {
				continue
			}
			signer, err := signerFromPrivateKey(c.SshPrivateKey.GetPrivateKey(), c.SshPrivateKey.GetPrivateKeyPassphrase())
			if err != nil {
				return nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to create signer from ssh private key credential"))
			}
			auths = append(auths, gssh.PublicKeys(signer))
			if u := c.SshPrivateKey.GetUsername(); u != "" {
				username = u
			}
		case *services.Credential_UsernamePassword:
			auths = append(auths, gssh.Password(c.UsernamePassword.GetPassword()))
			if u := c.UsernamePassword.GetUsername(); u != "" {
				username = u
			}
		}
	}
	if username == "" {
		username = requestedUser
	}
	return &gssh.ClientConfig{
		User: username,
		Auth: auths,
		// TODO: Make host key verification configurable.
		HostKeyCallback: gssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}, nil
}

// signerFromPrivateKey parses a PEM encoded private key, decrypting it with the
// provided passphrase when necessary.
func signerFromPrivateKey(privateKey, passphrase string) (gssh.Signer, error) {
	if passphrase != "" {
		return gssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
	}
	return gssh.ParsePrivateKey([]byte(privateKey))
}

// signerWithCertificate parses an OpenSSH certificate and the private key it was
// signed for and returns a signer that presents the certificate to the endpoint.
func signerWithCertificate(privateKey, certificate string) (gssh.Signer, error) {
	keySigner, err := gssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, err
	}
	pubKey, _, _, _, err := gssh.ParseAuthorizedKey([]byte(certificate))
	if err != nil {
		return nil, err
	}
	cert, ok := pubKey.(*gssh.Certificate)
	if !ok {
		return nil, errors.New(context.Background(), errors.InvalidParameter, "ssh.signerWithCertificate", "provided certificate is not an ssh certificate")
	}
	return gssh.NewCertSigner(cert, keySigner)
}
