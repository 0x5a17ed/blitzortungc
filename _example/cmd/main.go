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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/0x5a17ed/blitzortungc"
	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/websocket"
)

func main() {
	var verbose bool
	flag.BoolVar(&verbose, "verbose", false, "print lightning strike data")

	var limit int
	flag.IntVar(&limit, "limit", 0, "limit output to the given number")

	flag.Parse()

	ctx, cancelFn := context.WithCancel(context.Background())
	defer cancelFn()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() { <-sig; cancelFn() }()

	var events int
	var c *blitzortungc.Client
	c = &blitzortungc.Client{
		Handler: blitzortungc.HandlerFunc(func(s *blitzortungc.Strike) {
			s.Signals = nil
			if verbose {
				spew.Dump(events, s)
			} else {
				fmt.Printf(".")
			}

			events += 1
			if limit > 0 && events > limit {
				c.Shutdown()
			}
		}),
		ErrorHook: func(err error) {
			fmt.Println("error detected: ", err)
		},
	}

	if err := c.Run(ctx, websocket.DefaultDialer); err != nil {
		fmt.Printf("error: %s\n", err.Error())
	}

	if !verbose {
		fmt.Println()
	}
}
