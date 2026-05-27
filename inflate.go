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
	"fmt"
	"unicode/utf8"
)

// Inflate decodes the LZW-style UTF-8 stream the upstream service
// uses for its strike messages. Codes are encoded as UTF-8 runes:
// runes below 256 emit themselves as a single byte, and runes from
// 256 upward index a dictionary that grows as the stream is consumed,
// with the standard KωK edge case for codes that reference the entry
// about to be defined.
//
// Returns an error if d contains an invalid UTF-8 byte sequence.
func Inflate(d []byte) ([]byte, error) {
	if len(d) == 0 {
		return nil, nil
	}

	const dictStart = 256

	// Decode the seed rune. It becomes the first output bytes and
	// the initial value of f (the "previous entry") used to build
	// dictionary entries.
	seed, seedLen := utf8.DecodeRune(d)
	if seed == utf8.RuneError && seedLen <= 1 {
		return nil, fmt.Errorf("blitzortungc inflate: invalid UTF-8 at offset 0")
	}

	// Output buffer. Decompressed payloads are always at least as
	// large as the input and typically larger; 2× is a heuristic that
	// avoids most reallocations without obviously overshooting. append
	// grows geometrically beyond this if the heuristic is too tight.
	out := make([]byte, 0, max(len(d)*2, 64))
	out = append(out, d[:seedLen]...)

	// Dictionary entries reference byte ranges in out via (offset,
	// length). Storing offsets rather than slices keeps the references
	// stable across reallocations of the output buffer. The first 256
	// codes are reserved for literal bytes, so dict[0] holds the
	// entry for rune 256 (the first dictionary entry).
	type entry struct{ off, length int }
	dict := make([]entry, 0, 64)

	// f tracks the previous decoded entry as (offset, length) into out.
	fOff, fLen := 0, seedLen

	for off := seedLen; off < len(d); {
		r, w := utf8.DecodeRune(d[off:])
		if r == utf8.RuneError && w <= 1 {
			return nil, fmt.Errorf("blitzortungc inflate: invalid UTF-8 at offset %d", off)
		}

		// Resolve the current code to a byte range in out, appending
		// the bytes as a side effect. aOff is the start of the
		// appended range; the invariant aOff == fOff + fLen holds
		// because each iteration appends exactly the new entry to
		// the end of out.
		var aOff, aLen int
		switch {
		case int(r) < dictStart:
			// Literal: a single UTF-8 rune below 256 emits the
			// corresponding input bytes verbatim.
			aOff = len(out)
			out = append(out, d[off:off+w]...)
			aLen = w
		case int(r)-dictStart < len(dict):
			e := dict[int(r)-dictStart]
			aOff = len(out)
			out = append(out, out[e.off:e.off+e.length]...)
			aLen = e.length
		default:
			// KωK: the code references the entry about to be
			// defined. The decoded bytes are f + first_rune_of_f,
			// which we append to out in two steps. Between the
			// appends, out may have grown; fOff remains a valid
			// index into the (possibly new) backing array.
			_, fFirstRuneLen := utf8.DecodeRune(out[fOff:])
			aOff = len(out)
			out = append(out, out[fOff:fOff+fLen]...)
			out = append(out, out[fOff:fOff+fFirstRuneLen]...)
			aLen = fLen + fFirstRuneLen
		}
		off += w

		// Define the next dictionary entry: f + first_rune_of_a.
		// By the aOff == fOff + fLen invariant, those bytes are
		// already contiguous in out at [fOff : fOff + fLen +
		// aFirstRuneLen], so no copy is needed.
		_, aFirstRuneLen := utf8.DecodeRune(out[aOff:])
		dict = append(dict, entry{off: fOff, length: fLen + aFirstRuneLen})

		fOff, fLen = aOff, aLen
	}
	return out, nil
}
