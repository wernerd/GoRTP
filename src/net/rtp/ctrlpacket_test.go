// Copyright (C) 2011 Werner Dittmann
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
// Authors: Werner Dittmann <Werner.Dittmann@t-online.de>
//

package rtp

import (
    "fmt"
    //    "net"
    "testing"
)

// V=2, P=0, chunks=2;   PT=SDES; length=5 32bit words (24 bytes)
var sdes_1 = []byte{0x81, 202, 0x00, 0x05,
    0x01, 0x02, 0x03, 0x04, // SSRC 0x01020304
    0x01, 0x01, 0x02, // CNAME, len=1, content=2
    0x00,                   // END (chunk length: 8)
    0x05, 0x06, 0x07, 0x08, // SSRC 0x05060707
    0x01, 0x03, 0x05, 0x06, 0x07, // CNAME, len=3, content=5,6,7
    0x00, 0x00, 0x00} // END plus 2 padding (chunk length: 12)
// V=2, P=0, chunks=2;   PT=SDES; length=8 32bit words (36 bytes)
var sdes_2 = []byte{0x81, 202, 0x00, 0x08,
    // first chunk, two items: CNAME, NAME, END)
    0x01, 0x02, 0x03, 0x04, // SSRC 0x01020304
    0x01, 0x01, 0x02, // CNAME, len=1, content=2 (item length: 3)
    0x02, 0x07, // NAME, len=7
    0x05, 0x06, 0x07, 0x07, 0x06, 0x05, 0x04, // content 5,6,7,7,6,5,4 (item length 9)
    0x00, 0x00, 0x00, 0x00, // END plus 3 padding (chunk length: 20)
    // second chunk, one item (CNAME, END)
    0x05, 0x06, 0x07, 0x08, // SSRC 0x05060707
    0x01, 0x03, 0x05, 0x06, 0x07, // CNAME, len=3, content=5,6,7
    0x00, 0x00, 0x00} // END plus 2 padding (chunk length: 12)

// V=2, P=0, chunks=2;   PT=SDES; length=5 32bit words (24 bytes)
var sdes_1_wrong = []byte{0x81, 202, 0x00, 0x05,
    0x01, 0x02, 0x03, 0x04, // SSRC 0x01020304
    0x01, 0x03, 0x02, // CNAME, len=3 (wrong, should be 1), content=2
    0x00,                   // END (chunk length: 8)
    0x05, 0x06, 0x07, 0x08, // SSRC 0x05060707
    0x01, 0x03, 0x05, 0x06, 0x07, // CNAME, len=3, content=5,6,7
    0x00, 0x00, 0x00} // END plus 2 padding (chunk length: 12)

func sdesCheck(t *testing.T) (result bool) {
    result = false

    rp := new(CtrlPacket) // allocate a new CTRL packet.
    rp.buffer = sdes_1

    cnt := int((rp.Length(0) + 1) * 4) // SDES Length incl. header word
    if cnt != 24 {
        t.Error(fmt.Sprintf("Basic first SDES length check failed. Expected: 24, got: %d\n", cnt))
        return
    }
    offset := 4
    // first SDES chunk starts ofter first header word
    sdesChunk := rp.toSdesChunk(offset, cnt-4)

    // Get length of first chunk - must be 8
    chunkLen, _ := sdesChunk.chunkLen()
    if chunkLen != 8 {
        t.Error(fmt.Sprintf("Basic first SDES chunk length check failed. Expected: 8, got: %d\n", chunkLen))
        return
    }
    offset += chunkLen
    cnt -= chunkLen
    // second SDES chunk starts ofter first chunk :-) 
    sdesChunk = rp.toSdesChunk(offset, cnt-4)

    // Get length of second chunk - must be 12
    chunkLen, _ = sdesChunk.chunkLen()
    if chunkLen != 12 {
        t.Error(fmt.Sprintf("Basic first SDES chunk length check failed. Expected: 12, got: %d\n", chunkLen))
        return
    }

    rp.buffer = sdes_2
    cnt = int((rp.Length(0) + 1) * 4) // SDES Length incl. header word
    if cnt != 36 {
        t.Error(fmt.Sprintf("Basic second SDES length check failed. Expected: 36, got: %d\n", cnt))
        return
    }
    offset = 4
    // first SDES chunk starts ofter first header word
    sdesChunk = rp.toSdesChunk(offset, cnt-4)

    // Get length of first chunk - must be 20
    chunkLen, _ = sdesChunk.chunkLen()
    if chunkLen != 20 {
        t.Error(fmt.Sprintf("Basic second SDES chunk length check failed. Expected: 20, got: %d\n", chunkLen))
        return
    }
    offset += chunkLen
    cnt -= chunkLen
    // second SDES chunk starts ofter first chunk :-) 
    sdesChunk = rp.toSdesChunk(offset, cnt-4)

    // Get length of second chunk - must be 12
    chunkLen, _ = sdesChunk.chunkLen()
    if chunkLen != 12 {
        t.Error(fmt.Sprintf("Basic second SDES chunk length check failed. Expected: 12, got: %d\n", chunkLen))
        return
    }

    rp.buffer = sdes_1_wrong
    offset = 4
    // SDES chunk starts ofter first header word
    sdesChunk = rp.toSdesChunk(offset, cnt-4)

    // Get length of chunk - must fail
    chunkLen, ok := sdesChunk.chunkLen()
    if ok {
        t.Errorf("Chunk length error handling failed, expected: 0, false; got: %d, %v\n", chunkLen, ok)
        return
    }

    result = true
    return
}

func rtcpPacketBasic(t *testing.T) {
    sdesCheck(t)
}

func TestRtcpPacket(t *testing.T) {
    parseFlags()
    rtcpPacketBasic(t)
}
