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
	"io"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/multierr"

	"github.com/0x5a17ed/blitzortungc/internal/atomicval"
)

const (
	pongWait = 60 * time.Second

	pingPeriod = pongWait / 12

	writeWait = 3 * time.Second
)

type wsConn interface {
	io.Closer
	WriteControl(mType int, data []byte, deadline time.Time) error
	SetWriteDeadline(t time.Time) error
	ReadMessage() (messageType int, p []byte, err error)
	SetReadDeadline(t time.Time) error
	SetReadLimit(limit int64)
	SetPingHandler(h func(appData string) error)
	SetPongHandler(h func(appData string) error)
}

type messageWriter func() error

type errorFn func(err error)

// runner represents and handles a single connection.
type runner struct {
	wsConn  wsConn
	handler Handler
	errorFn errorFn

	writeCh chan messageWriter // writeCh is used to serialize writes to the websocket from goroutines.
	errorCh chan error

	nextPing atomicval.AtomicValue[time.Time] // Last message or pong received from the server.
}

func (r *runner) writeControl(mType int, data []byte) error {
	return r.wsConn.WriteControl(mType, data, time.Now().Add(writeWait))
}

// rearmPingTimer sets the next ping attempt to pingPeriod time in the future.
func (r *runner) rearmPingTimer() {
	r.nextPing.Store(time.Now().Add(pingPeriod))
}

// checkPing checks whenever the connection has been stale for too long
// and checks if the server is still reachable in case.
func (r *runner) checkPing() error {
	if time.Now().Before(r.nextPing.Load()) {
		return nil
	}

	// Haven't heard from the server in a while, try pinging it.
	if err := r.writeControl(websocket.PingMessage, nil); err != nil {
		return err
	}

	r.rearmPingTimer()
	return nil
}

// runReadLoop runs the websocket reading loop and acts on incoming messages.
func (r *runner) runReadLoop() error {
	r.wsConn.SetReadLimit(0xffff)

	r.wsConn.SetPongHandler(func(string) error {
		r.rearmPingTimer()
		return r.wsConn.SetReadDeadline(time.Now().Add(pongWait))
	})

	r.wsConn.SetPingHandler(func(m string) error {
		r.rearmPingTimer()
		r.writeCh <- func() error {
			return r.writeControl(websocket.PongMessage, []byte(m))
		}
		return nil
	})

	var val Strike
	for {
		if err := r.wsConn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			return err
		}

		if _, data, err := r.wsConn.ReadMessage(); err != nil {
			return err
		} else {
			// We received a message, this is as good as a pong,
			// reset the Ping timer.
			r.rearmPingTimer()

			inflated := Inflate(data)
			if err := json.Unmarshal(inflated, &val); err != nil {
				r.notifyError(&UnmarshalError{
					Wrapped: err,
					RawData: inflated,
				})
				continue
			}

			r.handler.HandleStrike(&val)
		}
	}
}

// runWriteLoop runs the writer loop.
func (r *runner) runWriteLoop(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case f := <-r.writeCh:
			if err := f(); err != nil {
				return err
			}
		case <-ticker.C:
			if err := r.checkPing(); err != nil {
				return err
			}
		case err := <-r.errorCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (r *runner) notifyError(err error) {
	if r.errorFn != nil {
		r.errorFn(err)
	}
}

// shutdown shuts the client down cleanly.
func (r *runner) shutdown() {
	r.writeCh <- func() error {
		// Cleanly close the connection by sending a close message and then
		// wait (with timeout) for the server to close the connection.
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
		if err := r.writeControl(websocket.CloseMessage, closeMsg); err != nil {
			// Informing the server that the connection is about
			// to be closed gracefully failed, just close the
			// underlying connection.
			return multierr.Append(err, r.wsConn.Close())
		}

		select {
		case err := <-r.errorCh:
			return err
		case <-time.After(2 * time.Second):
			return r.wsConn.Close()
		}
	}
}

func newRunner(wsConn wsConn, handler Handler, errorFn errorFn) *runner {
	c := &runner{
		wsConn:  wsConn,
		handler: handler,
		errorFn: errorFn,
		writeCh: make(chan messageWriter),
		errorCh: make(chan error),
	}

	go func() {
		defer close(c.errorCh)
		c.errorCh <- c.runReadLoop()
	}()

	return c
}
