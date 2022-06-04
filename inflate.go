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
	"unicode/utf8"
)

func concatBytes(a, b []byte) []byte { return append(append([]byte(nil), a...), b...) }

func Inflate(d []byte) (out []byte) {
	if len(d) == 0 {
		return nil
	}

	var c [8]byte
	var cl int
	_, cl = utf8.DecodeRune(d[:])
	copy(c[:], d[:cl])

	f := d[:cl]

	out = make([]byte, cl, len(d))
	copy(out, d[:cl])

	m := make([][]byte, 256, 256+len(d))

	for off := cl; off < len(d); {
		r, w := utf8.DecodeRune(d[off:])

		var a []byte
		if r <= 0xff {
			a = d[off : off+w]
		} else if int(r) < len(m) {
			a = m[r]
		} else {
			a = concatBytes(f, c[:cl])
		}
		off += w

		out = append(out, a...)

		r, _ = utf8.DecodeRune(a)
		cl = utf8.EncodeRune(c[:], r)
		m = append(m, concatBytes(f, c[:cl]))

		f = a
	}
	return
}
