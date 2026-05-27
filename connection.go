// Copyright 2022 individual contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     <http://www.apache.org/licenses/LICENSE-2.0>
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied. See the License for the
// specific language governing permissions and limitations
// under the License.

package blitzortungc

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	// pongWait bounds how long we'll wait for a ping reply (or any
	// other traffic) before considering the connection dead.
	pongWait = 60 * time.Second

	// pingPeriod is how often, in the absence of incoming traffic, we
	// send an application-level ping to keep the connection alive.
	pingPeriod = pongWait / 12

	// readLimit caps incoming WebSocket messages. Strikes inflate to
	// at most a few KiB; 64 KiB is comfortably above that.
	readLimit = 64 * 1024
)

type errorFn func(err error)

// runner represents and handles a single connection.
type runner struct {
	conn    *websocket.Conn
	handler Handler
	errorFn errorFn

	// lastTrafficUnixNano is updated on every successful read or
	// ping. The ping loop uses it to skip pinging when traffic has
	// arrived recently.
	lastTrafficUnixNano atomic.Int64
}

func newRunner(conn *websocket.Conn, handler Handler, errorFn errorFn) *runner {
	r := &runner{
		conn:    conn,
		handler: handler,
		errorFn: errorFn,
	}
	r.markTraffic()
	return r
}

func (r *runner) markTraffic() {
	r.lastTrafficUnixNano.Store(time.Now().UnixNano())
}

func (r *runner) notifyError(err error) {
	if r.errorFn != nil {
		r.errorFn(err)
	}
}

// runReadLoop consumes incoming messages until the connection errors out.
func (r *runner) runReadLoop(ctx context.Context) error {
	for {
		_, data, err := r.conn.Read(ctx)
		if err != nil {
			return err
		}
		r.markTraffic()

		inflated := Inflate(data)
		val := &Strike{}
		if err := json.Unmarshal(inflated, val); err != nil {
			r.notifyError(&UnmarshalError{
				Wrapped: err,
				RawData: inflated,
			})
			continue
		}
		r.handler.HandleStrike(val)
	}
}

// runPingLoop issues keepalive pings when no traffic has arrived for
// pingPeriod. Each ping is bounded by pongWait via a sub-context;
// coder/websocket's Ping returns once the matching pong arrives.
func (r *runner) runPingLoop(ctx context.Context) error {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			last := time.Unix(0, r.lastTrafficUnixNano.Load())
			if now.Sub(last) < pingPeriod {
				continue
			}
			pingCtx, cancel := context.WithTimeout(ctx, pongWait)
			err := r.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return err
			}
			r.markTraffic()
		}
	}
}

// run drives the read and ping loops in tandem. It returns when either
// loop terminates (graceful close, ctx cancel, or transport error),
// canceling the other loop and waiting for it to unwind so the conn
// can be closed cleanly afterwards.
func (r *runner) run(ctx context.Context) error {
	r.conn.SetReadLimit(readLimit)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)
	go func() { errCh <- r.runReadLoop(ctx) }()
	go func() { errCh <- r.runPingLoop(ctx) }()

	first := <-errCh
	cancel()
	second := <-errCh

	// Prefer the most informative error: a CloseError reported by the
	// read loop usually carries the server's status code, which the
	// caller may want to inspect. Otherwise return whichever isn't
	// a context error.
	return pickError(first, second)
}

func pickError(a, b error) error {
	for _, e := range []error{a, b} {
		if e == nil {
			continue
		}
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			continue
		}
		return e
	}
	if a != nil {
		return a
	}
	return b
}
