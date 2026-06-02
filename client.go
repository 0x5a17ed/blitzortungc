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
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"

	"github.com/0x5a17ed/blitzortungc/internal/upstream"
)

// Baked-in fallback values, used when remote config is disabled or
// fails. Kept in sync with the upstream JS by hand.
var (
	defaultServers = []string{
		"ws1.blitzortung.org",
		"ws2.blitzortung.org",
		"ws7.blitzortung.org",
		"ws8.blitzortung.org",
	}
	defaultSubscribePayload = []byte(`{"a":111}`)
)

// remoteConfigTimeout bounds the per-call duration of the upstream JS
// fetch on Run startup. It must complete before the WebSocket connect
// is attempted, so keep it short enough that a slow CDN doesn't stall
// the whole client.
const remoteConfigTimeout = 10 * time.Second

// Dialer establishes the underlying WebSocket connection. Provide a
// custom implementation to inject HTTP headers, a custom HTTP client,
// or to mock the transport in tests. DefaultDialer dials with
// websocket.Dial and the package-level UserAgent applied.
type Dialer interface {
	Dial(ctx context.Context, urlStr string) (*websocket.Conn, *http.Response, error)
}

// UserAgent is the User-Agent header DefaultDialer sends on the
// WebSocket upgrade request. Override it at init time to identify
// your application to the upstream service. Custom Dialer
// implementations are responsible for setting their own header.
var UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// DefaultDialer is the Dialer used when none is configured: it calls
// websocket.Dial with the package-level UserAgent applied.
var DefaultDialer Dialer = dialerFunc(func(ctx context.Context, urlStr string) (
	*websocket.Conn,
	*http.Response,
	error,
) {
	return websocket.Dial(ctx, urlStr, &websocket.DialOptions{
		HTTPHeader: http.Header{"User-Agent": []string{UserAgent}},
	})
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

	// DisableRemoteConfig disables fetching the live server list and
	// subscribe payload from blitzortung.org's JS at Run start. With
	// it set, the baked-in defaults are used unconditionally.
	DisableRemoteConfig bool

	// HTTPClient is used to fetch the upstream JS for remote config.
	// If nil, http.DefaultClient is used. Ignored when
	// DisableRemoteConfig is true.
	HTTPClient *http.Client

	backOff *backoff.ExponentialBackOff
	runner  atomic.Pointer[runner]

	// Resolved at Run start from upstream.Fetch (or the baked-in
	// defaults on failure / when remote config is disabled).
	servers          []string
	subscribePayload []byte

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

func (c *Client) pickServer() string {
	return c.servers[rand.IntN(len(c.servers))]
}

// resolveConfig populates c.servers and c.subscribePayload, preferring
// the values extracted from upstream JS over the baked-in defaults.
// Failures are surfaced via ErrorHook but never block startup.
func (c *Client) resolveConfig(ctx context.Context) {
	c.servers = defaultServers
	c.subscribePayload = defaultSubscribePayload
	if c.DisableRemoteConfig {
		return
	}

	fetchCtx, cancel := context.WithTimeout(ctx, remoteConfigTimeout)
	defer cancel()

	cfg, err := upstream.Fetch(fetchCtx, c.HTTPClient, UserAgent)
	if err != nil {
		c.notifyError(fmt.Errorf("remote config: %w", err))
		return
	}
	c.servers = cfg.Servers
	c.subscribePayload = cfg.SubscribePayload
}

func (c *Client) runOnce(ctx context.Context, dialer Dialer) (err error) {
	u := url.URL{Scheme: "wss", Host: c.pickServer(), Path: "/"}

	conn, _, err := dialer.Dial(ctx, u.String())
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()

	subscribeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = conn.Write(subscribeCtx, websocket.MessageText, c.subscribePayload)
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
	c.resolveConfig(ctx)

	_, err := backoff.Retry(
		ctx,
		func() (struct{}, error) {
			return struct{}{}, c.runOnce(ctx, dialer)
		},
		backoff.WithBackOff(c.backOff),
		backoff.WithNotify(func(err error, _ time.Duration) {
			c.notifyError(err)
		}),
	)
	return err
}
