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
	"math/big"
	"time"
)

// Station represents a single station that reported a given lightning strike.
type Station struct {
	// Station that has reported the signal.
	ID int `json:"sta"`

	// Status of the signal.
	// bit 1 = polarity negative
	// bit 2 = polarity positive
	// bit 3 = signal is used for the computation
	Status int `json:"status"`

	// Time difference to the time of the lightning strike in nanoseconds.
	TimeDeltaValue int `json:"time"`

	Latitude  float64 `json:"lat"` // Latitude of the detector in degree (decimal).
	Longitude float64 `json:"lon"` // Longitude of the detector in degree (decimal).
	Altitude  int     `json:"alt"` // Altitude of the detector in meter.
}

// Strike represents a single lightning strike as reported by the remote server.
type Strike struct {
	Status   *int `json:"status,omitempty"`
	Polarity int  `json:"pol"`

	Signals []Station `json:"sig"`

	TimeValue  big.Int `json:"time"`
	DelayValue float64 `json:"delay"`

	// MaxDeviationSpan is the maximal deviation span in nanoseconds.
	MaxDeviationSpan int `json:"mds"`
	// MaxCircularGap is the maximal circular gap in degree between two stations.
	MaxCircularGap *int `json:"mcg,omitempty"`

	Region    int     `json:"region"`
	Latitude  float64 `json:"lat"` // Latitude in degree (decimal).
	Longitude float64 `json:"lon"` // Longitude in degree (decimal).
	Altitude  int     `json:"alt"` // Altitude in meter.
}

// Delay interprets the DelayValue field as a time.Duration value.
func (s Strike) Delay() time.Duration {
	return time.Duration(s.DelayValue*1e3) * time.Millisecond
}

// Time interprets the TimeValue field as a time.Time value.
func (s Strike) Time() time.Time {
	var t, m big.Int
	t.DivMod(&s.TimeValue, big.NewInt(1e9), &m)
	return time.Unix(t.Int64(), m.Int64())
}
