// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"testing"
	"time"

	"github.com/opendumpling/opendumpling/internal/daemon/worker/proxy"
	"github.com/opendumpling/opendumpling/internal/gen/controller/servers/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gssh "golang.org/x/crypto/ssh"
	"google.golang.org/protobuf/types/known/anypb"
	"nhooyr.io/websocket"
)

// TestHandleProxy_SSHCertificateInjection exercises the full worker side flow:
// an SSH client connects to the worker (which acts as an SSH server), the worker
// signs into the endpoint using the injected SSH certificate and bridges the
// session channels.
func TestHandleProxy_SSHCertificateInjection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The CA which signs the injected user certificate.
	caPub, caPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	caSigner, err := gssh.NewSignerFromKey(caPriv)
	require.NoError(t, err)
	caPublicKey, err := gssh.NewPublicKey(caPub)
	require.NoError(t, err)

	// The user key pair; the private key is injected while the certificate is
	// signed by the CA above.
	userPub, userPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	userPublicKey, err := gssh.NewPublicKey(userPub)
	require.NoError(t, err)

	cert := &gssh.Certificate{
		Key:             userPublicKey,
		CertType:        gssh.UserCert,
		KeyId:           "test",
		ValidPrincipals: []string{"injected-user"},
		ValidAfter:      uint64(time.Now().Add(-time.Minute).Unix()),
		ValidBefore:     uint64(time.Now().Add(time.Hour).Unix()),
	}
	require.NoError(t, cert.SignCert(rand.Reader, caSigner))

	pkcs8, err := x509.MarshalPKCS8PrivateKey(userPriv)
	require.NoError(t, err)
	privateKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
	certificate := string(gssh.MarshalAuthorizedKey(cert))

	// Fake endpoint SSH server which only accepts certificates signed by the CA
	// and echoes any data sent on a session channel.
	endpointAddr, endpointErrs := startEndpointSSHServer(t, caPublicKey)
	t.Cleanup(func() {
		select {
		case err := <-endpointErrs:
			assert.NoError(t, err)
		default:
		}
	})

	protoCtx := &services.SshProtocolContext{
		Credentials: []*services.Credential{
			{
				Credential: &services.Credential_SshCertificate{
					SshCertificate: &services.SshCertificate{
						Username:    "injected-user",
						PrivateKey:  privateKeyPEM,
						Certificate: certificate,
					},
				},
			},
		},
	}
	anyCtx, err := anypb.New(protoCtx)
	require.NoError(t, err)

	dialer, err := proxy.NewProxyDialer(ctx, func(...proxy.Option) (net.Conn, error) {
		return net.Dial("tcp", endpointAddr)
	})
	require.NoError(t, err)

	// Simulate the local websocket proxy created during `dumpling connect`.
	clientWebsocket, proxyWebsocket := proxy.TestWsConn(t, ctx)
	workerConn := websocket.NetConn(ctx, proxyWebsocket, websocket.MessageBinary)

	type handleResult struct {
		fn  proxy.ProxyConnFn
		err error
	}
	resultCh := make(chan handleResult, 1)
	go func() {
		fn, err := handleProxy(ctx, ctx, nil, workerConn, dialer, "test-connection-id", anyCtx, nil)
		resultCh <- handleResult{fn: fn, err: err}
	}()

	// Connect an SSH client to the worker. The worker acts as the SSH server
	// and accepts the client without authentication.
	clientNetConn := websocket.NetConn(ctx, clientWebsocket, websocket.MessageBinary)
	clientSSHConn, chans, reqs, err := gssh.NewClientConn(clientNetConn, "localhost:2222", &gssh.ClientConfig{
		User:            "client-user",
		HostKeyCallback: gssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	require.NoError(t, err)
	client := gssh.NewClient(clientSSHConn, chans, reqs)
	t.Cleanup(func() {
		_ = client.Close()
	})

	var fn proxy.ProxyConnFn
	select {
	case res := <-resultCh:
		require.NoError(t, res.err)
		fn = res.fn
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for handleProxy to complete the handshakes")
	}
	require.NotNil(t, fn)
	go fn()

	// Open a session through the worker and verify the data round trips to the
	// endpoint.
	session, err := client.NewSession()
	require.NoError(t, err)
	stdin, err := session.StdinPipe()
	require.NoError(t, err)
	stdout, err := session.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, session.Shell())

	msg := []byte("hello over ssh")
	_, err = stdin.Write(msg)
	require.NoError(t, err)

	got := make([]byte, len(msg))
	_, err = io.ReadFull(stdout, got)
	require.NoError(t, err)
	assert.Equal(t, msg, got)
}

// startEndpointSSHServer starts an in-process SSH server which trusts the
// provided CA, accepts a single connection and echoes session channel data. It
// returns the address to dial and a channel which receives the terminal error
// (if any) once the connection has been served.
func startEndpointSSHServer(t *testing.T, caPublicKey gssh.PublicKey) (string, <-chan error) {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostSigner, err := gssh.NewSignerFromKey(hostPriv)
	require.NoError(t, err)

	checker := &gssh.CertChecker{
		IsUserAuthority: func(auth gssh.PublicKey) bool {
			return bytes.Equal(auth.Marshal(), caPublicKey.Marshal())
		},
	}
	serverConfig := &gssh.ServerConfig{
		PublicKeyCallback: func(conn gssh.ConnMetadata, key gssh.PublicKey) (*gssh.Permissions, error) {
			if _, err := checker.Authenticate(conn, key); err != nil {
				return nil, err
			}
			return nil, nil
		},
	}
	serverConfig.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	errCh := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		serverConn, chans, reqs, err := gssh.NewServerConn(conn, serverConfig)
		if err != nil {
			errCh <- err
			return
		}
		defer serverConn.Close()
		go gssh.DiscardRequests(reqs)

		for newChan := range chans {
			if newChan.ChannelType() != "session" {
				_ = newChan.Reject(gssh.UnknownChannelType, "only session channels are supported")
				continue
			}
			ch, chReqs, err := newChan.Accept()
			if err != nil {
				errCh <- err
				return
			}
			go func() {
				for req := range chReqs {
					if req.WantReply {
						_ = req.Reply(true, nil)
					}
				}
			}()
			_, _ = io.Copy(ch, ch)
			_ = ch.Close()
		}
		errCh <- nil
	}()

	return listener.Addr().String(), errCh
}
