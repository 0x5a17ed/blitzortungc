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
	"errors"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/coder/websocket"
)

func pickServer() string {
	l := []string{"ws1.blitzortung.org", "ws2.blitzortung.org", "ws7.blitzortung.org", "ws8.blitzortung.org"}
	return l[rand.IntN(len(l))]
}

// Dialer establishes the underlying WebSocket connection. Provide a
// custom implementation to inject HTTP headers, a custom HTTP client,
// or to mock the transport in tests. DefaultDialer dials with
// websocket.Dial and zero options.
type Dialer interface {
	Dial(ctx context.Context, urlStr string) (*websocket.Conn, *http.Response, error)
}

// DefaultDialer is the Dialer used when none is configured: it calls
// websocket.Dial with no options.
var DefaultDialer Dialer = dialerFunc(func(ctx context.Context, urlStr string) (*websocket.Conn, *http.Response, error) {
	return websocket.Dial(ctx, urlStr, nil)
})

type dialerFunc func(ctx context.Context, urlStr string) (*websocket.Conn, *http.Response, error)

func (f dialerFunc) Dial(ctx context.Context, urlStr string) (*websocket.Conn, *http.Response, error) {
	return f(ctx, urlStr)
}

// Client represents a client for the service of https://www.blitzortung.org/en/ for
// tracking lightning and thunderstorms in real time.
type Client struct {
	// Handler is called for handling lightning strike events.
	Handler Handler

	// ErrorHook is called when the read hit an error while
	// processing server data.
	ErrorHook func(error)

	backOff *backoff.ExponentialBackOff
	runner  atomic.Pointer[runner]

	m              sync.Mutex
	isShuttingDown bool
}

func (c *Client) notifyError(err error) {
	if c.ErrorHook != nil {
		c.ErrorHook(err)
	}
}

func (c *Client) getIsShuttingDown() bool {
	c.m.Lock()
	defer c.m.Unlock()

	return c.isShuttingDown
}

func (c *Client) runOnce(ctx context.Context, dialer Dialer) (err error) {
	u := url.URL{Scheme: "wss", Host: pickServer(), Path: "/"}

	conn, _, err := dialer.Dial(ctx, u.String())
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()

	subscribeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = subscribe(subscribeCtx, conn)
	cancel()
	if err != nil {
		return err
	}

	c.backOff.Reset()

	r := newRunner(conn, c.Handler, c.notifyError)
	c.runner.Store(r)
	if runErr := r.run(ctx); runErr != nil {
		isShuttingDown := c.getIsShuttingDown()
		switch {
		case websocket.CloseStatus(runErr) == websocket.StatusNormalClosure && isShuttingDown:
			return nil
		case errors.Is(runErr, context.Canceled) || isShuttingDown:
			return backoff.Permanent(runErr)
		default:
			return runErr
		}
	}
	return nil
}

// subscribe sends the initial subscription frame the upstream service
// expects after the WebSocket handshake.
func subscribe(ctx context.Context, conn *websocket.Conn) error {
	const payload = `{"a":111}`
	return conn.Write(ctx, websocket.MessageText, []byte(payload))
}

// Shutdown shuts the client down cleanly and prevents it from
// reconnecting to the data source again.
func (c *Client) Shutdown() {
	c.m.Lock()
	defer c.m.Unlock()

	if c.isShuttingDown {
		return
	}
	c.isShuttingDown = true
	if r := c.runner.Load(); r != nil {
		_ = r.conn.Close(websocket.StatusNormalClosure, "")
	}
}

// Run runs the given client, connecting to the lightning events
// source server and tries to keep the connection alive. Calls
// Handler.HandleStrike for incoming events.
func (c *Client) Run(ctx context.Context, dialer Dialer) error {
	if dialer == nil {
		dialer = DefaultDialer
	}
	c.backOff = backoff.NewExponentialBackOff()

	return backoff.RetryNotify(func() error {
		return c.runOnce(ctx, dialer)
	}, c.backOff, func(err error, _ time.Duration) {
		c.notifyError(err)
	})
}
