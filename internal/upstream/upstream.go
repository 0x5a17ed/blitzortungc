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

// Package upstream extracts the WebSocket server list and the
// initial subscribe payload from the blitzortung.org browser JS.
// The site doesn't expose these values through a stable data
// endpoint, so we parse the JS files that the live map ships with.
package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

// Default URLs of the two JS files we parse.
const (
	LBRURL     = "https://www.blitzortung.org/JS/lbr.js"
	DynamicURL = "https://www.blitzortung.org/en/JS/live_dynamic_maps3.js"
)

// Config bundles values extracted from the upstream JS.
type Config struct {
	// Servers is the deduplicated list of hostnames matching
	// ws<digits>.blitzortung.org found in lbr.js, in first-seen order.
	Servers []string

	// SubscribePayload is the JSON message the client sends after
	// the WebSocket handshake. Derived from the mode/key literals
	// in live_dynamic_maps3.js's startWebSocket function.
	SubscribePayload []byte
}

// Fetch fetches both upstream JS files via httpClient and extracts
// the server list and subscribe payload. If httpClient is nil,
// http.DefaultClient is used. If userAgent is non-empty, it's sent
// as the User-Agent header on both requests. Either fetch or
// extraction failing returns a non-nil error; callers should fall
// back to baked-in defaults in that case.
func Fetch(ctx context.Context, httpClient *http.Client, userAgent string) (*Config, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	servers, err := fetchServers(ctx, httpClient, userAgent, LBRURL)
	if err != nil {
		return nil, fmt.Errorf("servers: %w", err)
	}
	payload, err := fetchSubscribe(ctx, httpClient, userAgent, DynamicURL)
	if err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	return &Config{Servers: servers, SubscribePayload: payload}, nil
}

func fetchServers(ctx context.Context, c *http.Client, userAgent, url string) ([]string, error) {
	src, err := fetchSource(ctx, c, userAgent, url)
	if err != nil {
		return nil, err
	}
	program, err := parser.ParseFile(nil, url, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	servers := ExtractServers(program)
	if len(servers) == 0 {
		return nil, errors.New("no server hostnames found")
	}
	return servers, nil
}

func fetchSubscribe(ctx context.Context, c *http.Client, userAgent, url string) ([]byte, error) {
	src, err := fetchSource(ctx, c, userAgent, url)
	if err != nil {
		return nil, err
	}
	program, err := parser.ParseFile(nil, url, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return ExtractSubscribePayload(program)
}

func fetchSource(ctx context.Context, c *http.Client, userAgent, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

var serverHostRE = regexp.MustCompile(`^ws\d+\.blitzortung\.org$`)

// ExtractServers walks the program for string literals matching the
// ws<digits>.blitzortung.org pattern, returning them deduplicated and
// in first-seen order. Exported so callers can parse pre-fetched
// source if they want to short-circuit the HTTP path.
func ExtractServers(program *ast.Program) []string {
	seen := map[string]bool{}
	var out []string
	walk(program, func(n ast.Node) {
		lit, ok := n.(*ast.StringLiteral)
		if !ok {
			return
		}
		v := lit.Value.String()
		if !serverHostRE.MatchString(v) || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	})
	return out
}

// ExtractSubscribePayload walks the program for `var mode = "..."`
// and `var key = <number>` declarations and returns the JSON-encoded
// payload `{"<mode>":<key>}`.
func ExtractSubscribePayload(program *ast.Program) ([]byte, error) {
	var (
		mode             string
		key              int64
		haveMode, haveKey bool
	)
	walk(program, func(n ast.Node) {
		vs, ok := n.(*ast.VariableStatement)
		if !ok {
			return
		}
		for _, b := range vs.List {
			id, ok := b.Target.(*ast.Identifier)
			if !ok {
				continue
			}
			switch id.Name.String() {
			case "mode":
				if s, ok := b.Initializer.(*ast.StringLiteral); ok {
					mode = s.Value.String()
					haveMode = true
				}
			case "key":
				if n, ok := b.Initializer.(*ast.NumberLiteral); ok {
					switch v := n.Value.(type) {
					case int64:
						key = v
						haveKey = true
					case float64:
						key = int64(v)
						haveKey = true
					}
				}
			}
		}
	})
	if !haveMode {
		return nil, errors.New("could not extract mode")
	}
	if !haveKey {
		return nil, errors.New("could not extract key")
	}
	return json.Marshal(map[string]int64{mode: key})
}

// walk is a minimal AST visitor covering the node types we need to
// reach mode/key var declarations inside startWebSocket() and server
// hostname literals inside if-branch assignments. It is intentionally
// not a full ECMAScript walker: unhandled node types are treated as
// leaves.
func walk(n ast.Node, fn func(ast.Node)) {
	if n == nil {
		return
	}
	fn(n)
	switch v := n.(type) {
	case *ast.Program:
		for _, s := range v.Body {
			walk(s, fn)
		}
	case *ast.FunctionDeclaration:
		if v.Function != nil {
			walk(v.Function, fn)
		}
	case *ast.FunctionLiteral:
		if v.Body != nil {
			walk(v.Body, fn)
		}
	case *ast.BlockStatement:
		for _, s := range v.List {
			walk(s, fn)
		}
	case *ast.IfStatement:
		walk(v.Test, fn)
		walk(v.Consequent, fn)
		if v.Alternate != nil {
			walk(v.Alternate, fn)
		}
	case *ast.ExpressionStatement:
		walk(v.Expression, fn)
	case *ast.AssignExpression:
		walk(v.Left, fn)
		walk(v.Right, fn)
	case *ast.BinaryExpression:
		walk(v.Left, fn)
		walk(v.Right, fn)
	case *ast.SequenceExpression:
		for _, e := range v.Sequence {
			walk(e, fn)
		}
	case *ast.VariableStatement:
		for _, b := range v.List {
			if b.Initializer != nil {
				walk(b.Initializer, fn)
			}
		}
	case *ast.CallExpression:
		walk(v.Callee, fn)
		for _, a := range v.ArgumentList {
			walk(a, fn)
		}
	case *ast.TryStatement:
		if v.Body != nil {
			walk(v.Body, fn)
		}
		if v.Catch != nil {
			walk(v.Catch, fn)
		}
		if v.Finally != nil {
			walk(v.Finally, fn)
		}
	case *ast.CatchStatement:
		walk(v.Body, fn)
	case *ast.ReturnStatement:
		if v.Argument != nil {
			walk(v.Argument, fn)
		}
	case *ast.ForStatement:
		walk(v.Body, fn)
	case *ast.WhileStatement:
		walk(v.Body, fn)
	}
}
