// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"fmt"
	"net"

	"github.com/opendumpling/opendumpling/internal/daemon/cluster"
	"github.com/opendumpling/opendumpling/internal/errors"
	pbs "github.com/opendumpling/opendumpling/internal/gen/controller/servers/services"
	"google.golang.org/protobuf/proto"
)

// downstreamDataPlaneDialer is implemented by a worker holding the data plane
// connections its downstream workers established towards it.  It is what the
// session dialer of an ingress worker uses to reach the next hop of a multi
// hop route.
type downstreamDataPlaneDialer interface {
	DialDownstreamWorker(ctx context.Context, workerId string) (net.Conn, error)
}

// routeTunneledConn is the connection an ingress worker gets when the session
// travels through more than one worker.  Its bytes are tunneled through the
// downstream worker which in turn either forwards them further down the route
// or dials the endpoint.
//
// It reports the address the egress worker connected to rather than anything
// the ingress worker can see, so the controller records the endpoint the
// session actually reached.
type routeTunneledConn struct {
	net.Conn
	ip   string
	port uint32
}

func (c *routeTunneledConn) GetIp() string   { return c.ip }
func (c *routeTunneledConn) GetPort() uint32 { return c.port }

// routeDialer returns a ProxyDialer which reaches the endpoint through the
// route the controller computed for the session.
//
// The controller returns the route in the authorize session response: it
// starts with the worker handling the client connection and ends with the
// egress worker which dials the endpoint.  A route of a single worker means
// the handling worker dials the endpoint itself, which is handed to the direct
// dialer so a single hop deployment behaves exactly as it always did.
func routeDialer(ctx context.Context, endpoint string, _ string, acResp proto.Message, downstream interface{}) (*ProxyDialer, error) {
	const op = "proxy.routeDialer"

	if len(endpoint) == 0 {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "endpoint is empty")
	}
	authzResp, ok := acResp.(*pbs.AuthorizeConnectionResponse)
	if !ok || authzResp == nil || len(authzResp.GetRoute()) <= 1 {
		// No route, or a response which does not carry one at all (an older
		// controller for example): the handling worker dials the endpoint
		// itself.
		return directDialer(ctx, endpoint, "", acResp, downstream)
	}

	nextHop := authzResp.GetRoute()[1]
	// The remaining route is what the next hop has to forward, which is
	// everything past itself; an empty remainder tells the egress worker that
	// it is the one dialing the endpoint.
	remaining := make([]string, 0, len(authzResp.GetRoute())-2)
	remaining = append(remaining, authzResp.GetRoute()[2:]...)

	dialer, ok := downstream.(downstreamDataPlaneDialer)
	if !ok || dialer == nil {
		return nil, errors.New(ctx, errors.Internal, op,
			fmt.Sprintf("worker holds no data plane connection towards the next hop %q", nextHop))
	}

	d, err := NewProxyDialer(ctx, func(opt ...Option) (net.Conn, error) {
		conn, err := dialer.DialDownstreamWorker(ctx, nextHop)
		if err != nil {
			return nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to reach the next hop of the route"))
		}
		resp, err := cluster.DataPlaneDial(ctx, conn, &cluster.DataPlaneDialRequest{
			Endpoint:       endpoint,
			RemainingRoute: remaining,
		})
		if err != nil {
			conn.Close()
			return nil, errors.Wrap(ctx, err, op, errors.WithMsg("unable to reach the endpoint through the route"))
		}
		opts := GetOpts(opt...)
		if opts.WithPostConnectionHook != nil {
			opts.WithPostConnectionHook(conn)
		}
		return &routeTunneledConn{
			Conn: conn,
			ip:   resp.Ip,
			port: resp.Port,
		}, nil
	})
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	return d, nil
}
