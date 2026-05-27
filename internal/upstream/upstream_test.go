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

package upstream

import (
	"testing"

	"github.com/dop251/goja/parser"
	assertpkg "github.com/stretchr/testify/assert"
	requirepkg "github.com/stretchr/testify/require"
)

func TestExtractServers_LBRShape(t *testing.T) {
	// Mirrors the shape of the relevant block of lbr.js: a default
	// assignment followed by an if/else-if chain branching on a
	// random number. The hostnames are the only string literals in
	// the file that match the ws<digits>.blitzortung.org pattern.
	src := `
		var ws_server;
		ws_server = "ws1.blitzortung.org";
		if (have_cookie) {
			ws_server = "ws1.blitzortung.org"; // duplicate
		} else {
			var rnd = pickRandom();
			if (rnd < 1)      { ws_server = "ws1.blitzortung.org"; }
			else if (rnd < 2) { ws_server = "ws2.blitzortung.org"; }
			else if (rnd < 3) { ws_server = "ws7.blitzortung.org"; }
			else if (rnd < 4) { ws_server = "ws8.blitzortung.org"; }
		}
		var other = "not-a-blitz-host.example";
	`
	program, err := parser.ParseFile(nil, "snippet.js", src, 0)
	requirepkg.NoError(t, err)

	got := ExtractServers(program)
	assertpkg.Equal(t, []string{
		"ws1.blitzortung.org",
		"ws2.blitzortung.org",
		"ws7.blitzortung.org",
		"ws8.blitzortung.org",
	}, got)
}

func TestExtractServers_None(t *testing.T) {
	program, err := parser.ParseFile(nil, "snippet.js", `var x = "hello";`, 0)
	requirepkg.NoError(t, err)
	assertpkg.Empty(t, ExtractServers(program))
}

func TestExtractSubscribePayload_StartWebSocketShape(t *testing.T) {
	// Mirrors live_dynamic_maps3.js: mode/key are local vars inside
	// the startWebSocket function declaration.
	src := `
		function startWebSocket() {
			var mode = 'a';
			var key = 111;
			ws.send('{"'+mode+'":'+key+'}');
		}
	`
	program, err := parser.ParseFile(nil, "snippet.js", src, 0)
	requirepkg.NoError(t, err)

	got, err := ExtractSubscribePayload(program)
	requirepkg.NoError(t, err)
	assertpkg.JSONEq(t, `{"a":111}`, string(got))
}

func TestExtractSubscribePayload_TopLevel(t *testing.T) {
	src := `var mode = 'b'; var key = 42;`
	program, err := parser.ParseFile(nil, "snippet.js", src, 0)
	requirepkg.NoError(t, err)

	got, err := ExtractSubscribePayload(program)
	requirepkg.NoError(t, err)
	assertpkg.JSONEq(t, `{"b":42}`, string(got))
}

func TestExtractSubscribePayload_MissingKey(t *testing.T) {
	program, err := parser.ParseFile(nil, "snippet.js", `var mode = 'a';`, 0)
	requirepkg.NoError(t, err)

	_, err = ExtractSubscribePayload(program)
	assertpkg.Error(t, err)
}

func TestExtractSubscribePayload_MissingMode(t *testing.T) {
	program, err := parser.ParseFile(nil, "snippet.js", `var key = 111;`, 0)
	requirepkg.NoError(t, err)

	_, err = ExtractSubscribePayload(program)
	assertpkg.Error(t, err)
}
