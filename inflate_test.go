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
	"encoding/hex"
	"testing"

	assertpkg "github.com/stretchr/testify/assert"
)

func TestInflate(t *testing.T) {
	tests := []struct {
		name string
		inp  string
		want string
	}{
		{"", "6162636465666768696a6b6c6d6e6f707172737475767778797a", "abcdefghijklmnopqrstuvwxyz"},
		{"", "c3a4", "ä"},
		{"", "61c480c481c480", "aaaaaaaa"},
		{"", "61c4806162c48362c481c482c48462", "aaaabbbbaaaabbbbb"},
		{"", "544f42454f524e4f54c480c482c484c489c483c485c48723", "TOBEORNOTTOBEORTOBEORNOT#"},

		{"", "4c6f72656d2069707375c484646f6cc4812073697420616d65742c20636f6e73" +
			"656374c4967572c49364c486697363696e6720656cc4912e204e756c6cc49420" +
			"c481c4a920c48dc4826dc4986d616c65c488616461c4ad7520c4a5c4b7c4aac4" +
			"bf617474c4a72076c4b4707574c58f65c49375677565c4b15175c4a771c59e20" +
			"c580c582c584c5862e",
			"Lorem ipsum dolor sit amet, consectetur adipiscing elit. Nullam orci lorem, malesuada eu diam in, mattis vulputate augue. Quisque malesuada."},
		{"", "4475697320616c697175657420626c616e6469c48b617263752075c48b677261" +
			"766964612e204375c49c62c492c4a520c49e74616520c491616dc48463206fc4" +
			"956920636f6e67c4892070c4b67474c492c4b6c484c48b696e206cc4b6656dc4" +
			"a2566573c58362756c75c4b26672c58867696cc48e207268c4bbc496c4836ec5" +
			"95c59f61c48b74c58d707573c4a24d61c4a5c482c4b3206ac5ae746fc484c495" +
			"75c4a24e756ec4b476656c20c5abc59ec5ae20c48a206ec482c685c59574c59a" +
			"63c591c4bf72c48a69c59720c49fc484c68ac48674c4a250c6846c656ec5ab73" +
			"c488c4aec58073c489c696206dc5b2c59ac483c4992e",
			"Duis aliquet blandit arcu ut gravida. Curabitur vitae diam ac orci congue porttitor at in lorem. Vestibulum fringilla rhoncus nulla at tempus. Mauris ac justo arcu. Nunc vel tellus et nisl ultrices pretium id a elit. Pellentesque posuere mauris ut."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assertpkg.New(t)

			data, err := hex.DecodeString(tt.inp)
			if !assert.NoError(err) {
				return
			}

			assertpkg.Equal(t, tt.want, string(Inflate(data)))
		})
	}
}
