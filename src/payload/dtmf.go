// Copyright (C) 2021 Homin Lee
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.
//
// Authors: Homin Lee <homin.lee@suapapa.net>
//

package payload

import (
	"encoding/binary"
	"fmt"
)

// RFC2333, RTP Payload for DTMF Digits, Telephony Tones and Telephony Signals
/*
   Payload format
    0                   1                   2                   3
    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |     event     |E|R| volume    |          duration             |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/

type DTMF []byte

// IsValid checks DTMF is valid DTMF payload
func (dp DTMF) IsValid() bool {
	if len(dp) != 4 {
		return false
	}
	// check R field
	if (dp[1] & 0b0100_0000) != 0 {
		return false
	}

	return true
}

// Event returns event char for received event
func (dp DTMF) Event() byte {
	return dp[0]
}

// ASCIIEvent returns ASCII char for received event
func (dp DTMF) ASCIIEvent() byte {
	switch dp[0] {
	case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9:
		return '0' + dp[0]
	case 10:
		return '*'
	case 11:
		return '#'
	}
	return 0
}

// Volume returns volume of the DTMF
func (dp DTMF) Volume() int {
	return int(dp[1] & 0b0011_1111)
}

// Duration returns duration of the DTMF
func (dp DTMF) Duration() int {
	return int(binary.BigEndian.Uint16(dp[2:4]))
}

// IsEnd returns the end bit is set or not
func (dp DTMF) IsEnd() bool {
	return (dp[1] & 0b1000_0000) == 0b1000_0000
}

// String for Stringer interface (for debug)
func (dp DTMF) String() string {
	return fmt.Sprintf("DTMF[Evt=%v Vol=%v Dur=%v IsEnd=%v]",
		dp.Event(), dp.Volume(), dp.Duration(), dp.IsEnd())
}
