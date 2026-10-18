// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	// dataPlaneProtoVersion is the version of the worker to worker data plane
	// framing protocol.  It is sent as the first byte of every message and a
	// peer speaking an unknown version is dropped, which keeps a mixed version
	// deployment from misinterpreting a frame.
	dataPlaneProtoVersion byte = 1

	// dataPlaneHeaderSize is the size of the framing header: one protocol
	// version byte followed by a four byte big endian payload length.
	dataPlaneHeaderSize = 5

	// maxDataPlaneMessageSize bounds the size of a data plane control
	// message.  Control messages carry an address and a worker id list only,
	// so this is several orders of magnitude larger than anything legitimate.
	maxDataPlaneMessageSize = 8 * 1024

	// dataPlaneDialTimeout bounds how long a hop waits for the next hop or
	// the endpoint to accept a connection before giving up.
	dataPlaneDialTimeout = 15 * time.Second
)

// DataPlaneDialRequest is the control message an upstream worker sends over a
// data plane connection which a downstream worker previously established.
//
// The downstream worker which reads the request either dials the endpoint
// itself (when RemainingRoute is empty, meaning it is the egress worker of the
// route) or forwards the request, with the first entry of RemainingRoute
// stripped, to one of its own downstream workers.
type DataPlaneDialRequest struct {
	// Endpoint is the address the egress worker of the route has to dial,
	// in host:port form.
	Endpoint string `json:"endpoint"`

	// RemainingRoute holds the worker ids which still have to be traversed
	// once the hop handling this request has done its work.  It is empty
	// when the hop handling the request is the egress worker.
	RemainingRoute []string `json:"remaining_route,omitempty"`
}

// DataPlaneDialResponse is the answer to a DataPlaneDialRequest.  It is
// written by the egress worker of the route and relayed unchanged by every
// intermediate hop.
type DataPlaneDialResponse struct {
	// Error is non empty when the connection could not be established.  When
	// it is set the other fields are meaningless and the data plane
	// connection goes back to serving the next request.
	Error string `json:"error,omitempty"`

	// Ip and Port describe the address the egress worker connected to.  They
	// are relayed to the controller as the session's endpoint address, which
	// is why they are reported by the egress worker rather than derived by
	// the ingress worker: the ingress worker only ever sees its own tunnel.
	Ip   string `json:"ip,omitempty"`
	Port uint32 `json:"port,omitempty"`
}

// writeDataPlaneFrame writes a single framed message.
func writeDataPlaneFrame(w io.Writer, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("error marshaling data plane message: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxDataPlaneMessageSize {
		return fmt.Errorf("data plane message size %d is out of bounds", len(payload))
	}
	buf := make([]byte, dataPlaneHeaderSize+len(payload))
	buf[0] = dataPlaneProtoVersion
	binary.BigEndian.PutUint32(buf[1:dataPlaneHeaderSize], uint32(len(payload)))
	copy(buf[dataPlaneHeaderSize:], payload)
	_, err = w.Write(buf)
	return err
}

// readDataPlaneFrame reads a single framed message and returns its payload.
func readDataPlaneFrame(r io.Reader) ([]byte, error) {
	var header [dataPlaneHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	if header[0] != dataPlaneProtoVersion {
		return nil, fmt.Errorf("unsupported data plane protocol version %d", header[0])
	}
	size := binary.BigEndian.Uint32(header[1:dataPlaneHeaderSize])
	if size == 0 || size > maxDataPlaneMessageSize {
		return nil, fmt.Errorf("data plane message size %d is out of bounds", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// readDataPlaneFrameInto reads a single framed message into v.
func readDataPlaneFrameInto(r io.Reader, v any) error {
	payload, err := readDataPlaneFrame(r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf("error unmarshaling data plane message: %w", err)
	}
	return nil
}

// DataPlaneNextHopFunc returns a data plane connection towards the downstream
// worker with the provided id.  It is supplied by the worker holding the
// connection pool of its downstream workers.
type DataPlaneNextHopFunc func(ctx context.Context, workerId string) (net.Conn, error)

// ServeDataPlaneConn serves a single data plane connection which an upstream
// worker holds towards this worker.
//
// The connection carries exactly one session.  The upstream worker writes a
// DataPlaneDialRequest; when RemainingRoute names other workers this hop
// forwards the request to the next hop and relays its answer, otherwise it
// dials the endpoint itself.  Once the answer reached the upstream worker the
// connection turns into a plain byte pipe between the upstream worker and the
// connection this hop opened, and the call returns when the session ended.
//
// A request which can be answered with an error (a dial failure for example)
// does not end the connection: it is answered and the loop reads the next
// request, so a downstream worker does not have to re-authenticate just
// because a single endpoint was unreachable.
func ServeDataPlaneConn(ctx context.Context, conn net.Conn, next DataPlaneNextHopFunc) error {
	const op = "cluster.ServeDataPlaneConn"

	if conn == nil {
		return fmt.Errorf("%s: data plane connection is nil", op)
	}
	defer conn.Close()

	for {
		var req DataPlaneDialRequest
		if err := readDataPlaneFrameInto(conn, &req); err != nil {
			// Including io.EOF: the upstream worker closed the connection.
			return err
		}
		switch {
		case req.Endpoint == "" && len(req.RemainingRoute) == 0:
			// Nothing to do and nothing sensible to answer, so the
			// connection is dropped rather than trusted further.
			return fmt.Errorf("%s: data plane request carries neither an endpoint nor a route", op)

		case len(req.RemainingRoute) == 0:
			// This hop is the egress worker of the route.
			epConn, err := net.DialTimeout("tcp", req.Endpoint, dataPlaneDialTimeout)
			if err != nil {
				if werr := writeDataPlaneFrame(conn, &DataPlaneDialResponse{Error: err.Error()}); werr != nil {
					return werr
				}
				continue
			}
			resp := &DataPlaneDialResponse{}
			if host, port, err := net.SplitHostPort(req.Endpoint); err == nil {
				resp.Ip = host
				if parsed, err := strconv.ParseUint(port, 10, 32); err == nil {
					resp.Port = uint32(parsed)
				}
			}
			if err := writeDataPlaneFrame(conn, resp); err != nil {
				epConn.Close()
				return err
			}
			proxyDataPlaneConn(conn, epConn)
			return nil

		default:
			// There is at least one more hop: forward the request towards
			// the next one and relay its answer back untouched.
			if next == nil {
				return fmt.Errorf("%s: no downstream connection available for worker %q", op, req.RemainingRoute[0])
			}
			nextHopId := req.RemainingRoute[0]
			nextConn, err := next(ctx, nextHopId)
			if err != nil {
				if werr := writeDataPlaneFrame(conn, &DataPlaneDialResponse{Error: err.Error()}); werr != nil {
					return werr
				}
				continue
			}
			forwarded := DataPlaneDialRequest{
				Endpoint:       req.Endpoint,
				RemainingRoute: req.RemainingRoute[1:],
			}
			if err := writeDataPlaneFrame(nextConn, &forwarded); err != nil {
				nextConn.Close()
				if werr := writeDataPlaneFrame(conn, &DataPlaneDialResponse{Error: err.Error()}); werr != nil {
					return werr
				}
				continue
			}
			resp, err := readDataPlaneFrame(nextConn)
			if err != nil {
				nextConn.Close()
				if werr := writeDataPlaneFrame(conn, &DataPlaneDialResponse{Error: err.Error()}); werr != nil {
					return werr
				}
				continue
			}
			if _, err := conn.Write(frameDataPlanePayload(resp)); err != nil {
				nextConn.Close()
				return err
			}
			proxyDataPlaneConn(conn, nextConn)
			return nil
		}
	}
}

// frameDataPlanePayload frames an already marshaled payload.
func frameDataPlanePayload(payload []byte) []byte {
	buf := make([]byte, dataPlaneHeaderSize+len(payload))
	buf[0] = dataPlaneProtoVersion
	binary.BigEndian.PutUint32(buf[1:dataPlaneHeaderSize], uint32(len(payload)))
	copy(buf[dataPlaneHeaderSize:], payload)
	return buf
}

// DataPlaneDial asks the downstream worker holding the other end of conn to
// establish the session connection described by req.
//
// On success the returned response carries the address the egress worker
// connected to and conn itself carries the session bytes.
func DataPlaneDial(ctx context.Context, conn net.Conn, req *DataPlaneDialRequest) (*DataPlaneDialResponse, error) {
	const op = "cluster.DataPlaneDial"

	switch {
	case conn == nil:
		return nil, fmt.Errorf("%s: data plane connection is nil", op)
	case req == nil:
		return nil, fmt.Errorf("%s: data plane request is nil", op)
	case req.Endpoint == "":
		return nil, fmt.Errorf("%s: data plane request carries no endpoint", op)
	}

	// The request has to be answered quickly; once it is, the deadlines are
	// cleared again because the connection then carries an arbitrarily long
	// lived session.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(dataPlaneDialTimeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("%s: error setting connection deadline: %w", op, err)
	}
	defer conn.SetDeadline(time.Time{})

	if err := writeDataPlaneFrame(conn, req); err != nil {
		return nil, fmt.Errorf("%s: error writing data plane request: %w", op, err)
	}
	raw, err := readDataPlaneFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("%s: error reading data plane response: %w", op, err)
	}
	resp := new(DataPlaneDialResponse)
	if err := json.Unmarshal(raw, resp); err != nil {
		return nil, fmt.Errorf("%s: error unmarshaling data plane response: %w", op, err)
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("%s: downstream worker was unable to reach the endpoint: %s", op, resp.Error)
	}
	return resp, nil
}

// closeWriter is implemented by connections which support closing one
// direction of the connection while leaving the other one open.
type closeWriter interface {
	CloseWrite() error
}

// proxyDataPlaneConn copies bytes in both directions until both halves ended
// and then closes the connections.
//
// Each half closes its destination as soon as its own copy returned, which is
// what unblocks the other half: a session ends with the client going away,
// and the endpoint only notices that once its side of the connection is
// closed.  Half closing is preferred where the underlying connection supports
// it so no bytes in flight are lost, but the connections handed around here
// are nodeenrollment connections which generally do not, so closing is the
// fallback.  Data lost that way is data the endpoint wanted to send to a
// client which already disconnected, which that client cannot read anyway.
func proxyDataPlaneConn(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	proxyDataPlaneHalf := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(closeWriter); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	go proxyDataPlaneHalf(b, a)
	go proxyDataPlaneHalf(a, b)
	wg.Wait()
	a.Close()
	b.Close()
}
