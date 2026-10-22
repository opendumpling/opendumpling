// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/opendumpling/opendumpling/internal/proxy"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wspb"
)

// runStdioProxy opens a single proxied connection to the worker and pipes the
// process's stdin/stdout through it. This is what backs the ssh-proxy mode: an
// SSH client runs this command as its ProxyCommand and speaks the SSH transport
// over our stdin/stdout.
func (c *Command) runStdioProxy(workerAddr string, transport *http.Transport, tofuToken string) error {
	wsConn, err := c.getWsConn(c.proxyCtx, workerAddr, transport)
	if err != nil {
		return err
	}
	defer wsConn.Close(websocket.StatusNormalClosure, "")

	// Perform the same proxy handshake the TCP listener path uses. The
	// handshake is written to the websocket, not stdout, so it does not corrupt
	// the SSH stream.
	handshake := proxy.ClientHandshake{TofuToken: tofuToken}
	if err := wspb.Write(c.proxyCtx, wsConn, &handshake); err != nil {
		return fmt.Errorf("error sending handshake to worker: %w", err)
	}
	var handshakeResult proxy.HandshakeResult
	if err := wspb.Read(c.proxyCtx, wsConn, &handshakeResult); err != nil {
		switch {
		case strings.Contains(err.Error(), "unable to authorize connection"):
			return fmt.Errorf("unable to authorize connection")
		case strings.Contains(err.Error(), "tofu token not allowed"):
			return fmt.Errorf("session is already in use")
		default:
			return fmt.Errorf("error reading handshake result: %w", err)
		}
	}

	netConn := websocket.NetConn(c.proxyCtx, wsConn, websocket.MessageBinary)
	// Keep stdin->worker asynchronous so a worker-side EOF also terminates the
	// ProxyCommand instead of waiting forever for more local input.
	go func() {
		_, _ = io.Copy(netConn, os.Stdin)
		netConn.Close()
	}()
	_, _ = io.Copy(os.Stdout, netConn)
	_ = netConn.Close()

	// End the authorization promptly after SSH disconnects instead of leaving
	// the session active until its certificate expires.
	if time.Now().Before(c.expiration.Add(-5 * time.Minute)) {
		ctx, cancel := context.WithTimeout(context.Background(), sessionCancelTimeout)
		defer cancel()
		cancelConn, cancelErr := c.getWsConn(ctx, workerAddr, transport)
		if cancelErr == nil {
			cancelErr = c.sendSessionTeardown(ctx, cancelConn, tofuToken)
			_ = cancelConn.Close(websocket.StatusNormalClosure, "")
		}
		if cancelErr != nil {
			return fmt.Errorf("failed to cancel session: %w", cancelErr)
		}
	}
	return nil
}
