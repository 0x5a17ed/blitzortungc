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
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gorilla/websocket"
	"go.uber.org/multierr"

	"github.com/0x5a17ed/blitzortungc/internal/atomicval"
)

func pickServer() string {
	l := []string{"ws1.blitzortung.org", "ws7.blitzortung.org", "ws8.blitzortung.org"}
	return l[rand.Intn(len(l))]
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
	runner  atomicval.AtomicValue[*runner]

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

type Dialer interface {
	DialContext(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error)
}

func (c *Client) runOnce(ctx context.Context, dialer Dialer) (err error) {
	u := url.URL{Scheme: "wss", Host: pickServer(), Path: "/"}

	wc, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	defer multierr.AppendInvoke(&err, multierr.Close(wc))

	if err := wc.WriteJSON(map[string]int{"a": 542}); err != nil {
		return err
	}

	c.backOff.Reset()

	r := newRunner(wc, c.Handler, c.notifyError)
	c.runner.Store(r)
	if err := r.runWriteLoop(ctx); err != nil {
		isShuttingDown := c.getIsShuttingDown()
		switch {
		case websocket.IsCloseError(err, websocket.CloseNormalClosure) && isShuttingDown:
			return nil
		case errors.Is(err, context.Canceled) || isShuttingDown:
			return backoff.Permanent(err)
		default:
			return err
		}
	}
	return nil
}

// Shutdown shuts the client down cleanly and prevents it from
// reconnecting to the data source again.
func (c *Client) Shutdown() {
	c.m.Lock()
	defer c.m.Unlock()

	if r := c.runner.Load(); r != nil && !c.isShuttingDown {
		c.isShuttingDown = true
		r.shutdown()
	}
}

// Run runs the given client, connecting to the lightning events
// source server and tries to keep the connection alive.  Calls
// Handler.HandleStrike for incoming events.
func (c *Client) Run(ctx context.Context, dialer Dialer) error {
	c.backOff = backoff.NewExponentialBackOff()

	return backoff.RetryNotify(func() error {
		return c.runOnce(ctx, dialer)
	}, c.backOff, func(err error, _ time.Duration) {
		c.notifyError(err)
	})
}
