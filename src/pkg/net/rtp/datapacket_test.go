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
    "net"
    "testing"
    "flag"
    "time"
)

var verbose *bool = flag.Bool("verbose", false, "Verbose output during tests")
var parsed bool

var payload = []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x20}
var payloadNull = []byte{}

var initialPacket = []byte{0x80, 0x03, 0x47, 0x11, 0xf0, 0xe0, 0xd0, 0xc0, 0x01, 0x02, 0x03, 0x04,
    0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x20}

var csrc_1 = []uint32{0x21435465, 0x65544322}
var csrc_2 = []uint32{0x23445566, 0x66554423, 0x87766554}
var csrc_3 = []uint32{}

//                 profile ID     length 
var ext_1 = []byte{0x77, 0x88, 0x00, 0x02, 0x01, 0x02, 0x03, 0x04, 0x04, 0x03, 0x02, 0x01}                         // len: 12
var ext_2 = []byte{0x77, 0x89, 0x00, 0x03, 0x01, 0x02, 0x03, 0x04, 0x04, 0x03, 0x02, 0x01, 0x11, 0x22, 0x33, 0x44} // len: 16
var ext_3 = []byte{0x77, 0x8a, 0x00, 0x00}                                                                         // len: 4
var ext_4 = []byte{}

func headerCheck(rp *DataPacket, t *testing.T) (result bool) {
    result = false

    ssrc := rp.Ssrc()
    if ssrc != 0x01020304 {
        t.Error(fmt.Sprintf("SSRC check failed. Expected: 0x1020304, got: %x\n", ssrc))
        return
    }
    seq := rp.Sequence()
    if seq != 0x4711 {
        t.Error(fmt.Sprintf("Sequence check failed. Expected: 0x4711, got: %x\n", seq))
        return
    }
    ts := rp.Timestamp()
    if ts != 0xF0E0D0C0 {
        t.Error(fmt.Sprintf("Timestamp check failed. Expected: 0xF0E0D0C0, got: %x\n", ts))
        return
    }

    result = true
    return
}

func csrcTest(rp *DataPacket, t *testing.T, csrc []uint32, run int) (result bool) {
    result = false
    rp.SetCsrcList(csrc)

    if !headerCheck(rp, t) {
        return
    }
    ssrcLen := rp.CsrcCount()
    if int(ssrcLen) != len(csrc) {
        t.Error(fmt.Sprintf("CSRC-%d length check failed. Expected: %d, got: %d\n", run, len(csrc), ssrcLen))
        return
    }
    csrcTmp := rp.CsrcList()
    for i, v := range csrcTmp {
        if v != csrc[i] {
            t.Error(fmt.Sprintf("CSRC-%d check failed at %i. Expected: %x, got: %x\n", run, i, csrc[i], csrcTmp[i]))
            return
        }
    }
    use := rp.InUse()
    expected := rtpHeaderLength + len(payload) + len(csrc)*4 + rp.ExtensionLength()
    if use != expected {
        t.Error(fmt.Sprintf("Payload-CSRC-%d length check failed. Expected: %d, got: %d\n", run, expected, use))
        return
    }
    pay := rp.Payload()
    for i, v := range payload {
        if v != pay[i] {
            t.Error(fmt.Sprintf("Payload-CSRC-%d check failed at %i. Expected: %x, got: %x\n", run, i, payload[i], pay[i]))
            return
        }
    }
    if *verbose {
        rp.Print(fmt.Sprintf("CSRC-%d check", run))
    }
    result = true
    return
}

func extTest(rp *DataPacket, t *testing.T, ext []byte, run int) (result bool) {
    result = false
    rp.SetExtension(ext)

    if !headerCheck(rp, t) {
        return
    }
    extLen := rp.ExtensionLength()
    if extLen != len(ext) {
        t.Error(fmt.Sprintf("EXT-%d length check failed. Expected: %d, got: %d\n", run, len(ext), extLen))
        return
    }
    extTmp := rp.Extension()
    for i, v := range extTmp {
        if v != ext[i] {
            t.Error(fmt.Sprintf("EXT-%d check failed at %i. Expected: %x, got: %x\n", run, i, ext[i], extTmp[i]))
            return
        }
    }
    use := rp.InUse()
    expected := rtpHeaderLength + len(payload) + int(rp.CsrcCount()*4) + len(ext)
    if use != expected {
        t.Error(fmt.Sprintf("Payload-EXT-%d length check failed. Expected: %d, got: %d\n", run, expected, use))
        return
    }
    pay := rp.Payload()
    for i, v := range payload {
        if v != pay[i] {
            t.Error(fmt.Sprintf("Payload-EXT-%d check failed at %i. Expected: %x, got: %x\n", run, i, payload[i], pay[i]))
            return
        }
    }
    if *verbose {
        rp.Print(fmt.Sprintf("EXT-%d check", run))
    }
    result = true
    return
}

func rtpPacket(t *testing.T) {

    // Prepare some data to create a RP session, RTP stream and then RTP packets
    port := 5220
    local, _ := net.ResolveIPAddr("ip", "127.0.0.1")

    // Create a UDP transport with "local" address and use this for a "local" RTP session
    // The RTP session uses the transport to receive and send RTP packets to the remote peers.
    tpLocal, _ := NewTransportUDP(local, port)

    // TransportUDP implements RtpTransportWrite and RtpTransportRecv interfaces thus
    // set it in the RtpSession for both interfaces
    rsLocal := NewSession(tpLocal, tpLocal)

    // Create a media stream. 
    // The SSRC identifies the stream. Each stream has its own sequence number and other 
    // context. A RTP session can have several RTP stream for example to send several
    // streams of the same media.
    //
    strIdx, _ := rsLocal.NewSsrcStreamOut(&Address{local.IP, port, port+1}, 0x01020304, 0x4711)
    rsLocal.SsrcStreamOutForIndex(strIdx).SetPayloadType(3)

    // Create a RTP packet suitable for standard stream (index 0) with a payload length of 160 bytes
    // The method initializes the RTP packet with SSRC, sequence number, and RTP version number. 
    // If the payload type was set with the RTP stream then the payload type is also set in
    // the RTP packet   
    rp := rsLocal.NewDataPacket(160)
    rp.SetTimestamp(0xF0E0D0C0)

    if !headerCheck(rp, t) {
        return
    }
    use := rp.InUse()
    if use != rtpHeaderLength {
        t.Error(fmt.Sprintf("RTP header length check failed. Expected: 12, got: %d\n", use))
        return
    }

    // Check basic payload handling
    rp.SetPayload(payload)

    if *verbose {
        rp.Print("Basic payload")
    }
    use = rp.InUse()
    if use != rtpHeaderLength+len(payload) {
        t.Error(fmt.Sprintf("Packet length check failed. Expected: %d, got: %d\n", rtpHeaderLength+len(payload), use))
        return
    }
    pay := rp.Payload()
    if len(pay) != len(payload) {
        t.Error(fmt.Sprintf("Payload length check failed. Expected: %d, got: %d\n", len(payload), len(pay)))
        return
    }
    for i, v := range payload {
        if v != pay[i] {
            t.Error(fmt.Sprintf("Payload check failed at %i. Expected: %x, got: %x\n", i, payload[i], pay[i]))
            return
        }
    }
    buf := rp.Buffer()
    for i, v := range buf[0:use] {
        if v != initialPacket[i] {
            t.Error(fmt.Sprintf("Basic header buffer check failed at %d. Expected: %x, got: %x\n", i, initialPacket[i], buf[i]))
            return
        }
    }
    rp.SetMarker(true)
    if buf[markerPtOffset] != 0x83 {
        t.Error(fmt.Sprintf("Marker/PT check 1 failed. Expected: 0x83, got: %x\n", buf[markerPtOffset]))
        return
    }
    pt := rp.PayloadType()
    if pt != 3 {
        t.Error(fmt.Sprintf("PT-after-Marker check 1 failed. Expected: 3, got: %x\n", pt))
        return
    }
    rp.SetMarker(false)
    if buf[markerPtOffset] != 3 {
        t.Error(fmt.Sprintf("Marker/PT check 2 failed. Expected: 3, got: %x\n", buf[markerPtOffset]))
        return
    }
    pt = rp.PayloadType()
    if pt != 3 {
        t.Error(fmt.Sprintf("PT-after-Marker check 2 failed. Expected: 3, got: %x\n", pt))
        return
    }

    // Delete payload, prepare for padding tests
    rp.SetPayload(payloadNull)

    // Check padding
    rp.SetPadding(true, 0) // zero defaults to multiple of int32 (4)

    // Check payload handling with padding; len(payload) +  rtpHeaderLength is 22, thus 2 bytes padding 
    rp.SetPayload(payload)
    use = rp.InUse()
    if use != (rtpHeaderLength + len(payload) + 2) {
        t.Error(fmt.Sprintf("Padding packet length check failed. Expected: %d, got: %d\n", rtpHeaderLength+len(payload)+2, use))
        return
    }
    pay = rp.Payload()
    if len(pay) != len(payload) {
        t.Error(fmt.Sprintf("Padding payload length check failed. Expected: %d, got: %d\n", len(payload), len(pay)))
        return
    }
    for i, v := range payload {
        if v != pay[i] {
            t.Error(fmt.Sprintf("Payload check failed at %i. Expected: %x, got: %x\n", i, payload[i], pay[i]))
            return
        }
    }
    if *verbose {
        rp.Print("Payload with padding")
    }

    // delete padded payload and switch off padding
    rp.SetPayload(payloadNull)
    rp.SetPadding(false, 0)

    // set normal payload without padding to perform other tests
    rp.SetPayload(payload)

    // Now check CSRC list handling. These modify InUse and shift the payload inside the packet buffer.
    if !csrcTest(rp, t, csrc_1, 1) || !csrcTest(rp, t, csrc_2, 2) || !csrcTest(rp, t, csrc_3, 3) {
        return
    }
    // After last CSCR test the packet shall be in initial state, check it.
    use = rp.InUse()
    if use != rtpHeaderLength+len(payload) {
        t.Error(fmt.Sprintf("Packet length check afer CSRC failed. Expected: %d, got: %d\n", rtpHeaderLength+len(payload), use))
        return
    }
    buf = rp.Buffer()
    for i, v := range buf[0:use] {
        if v != initialPacket[i] {
            t.Error(fmt.Sprintf("Basic header buffer check after CSRC failed at %d. Expected: %x, got: %x\n", i, initialPacket[i], buf[i]))
            return
        }
    }
    if !extTest(rp, t, ext_1, 1) || !extTest(rp, t, ext_2, 2) || !extTest(rp, t, ext_3, 3) || !extTest(rp, t, ext_4, 4) {
        return
    }
    // After last EXT test the packet shall be in initial state, check it.
    use = rp.InUse()
    if use != rtpHeaderLength+len(payload) {
        t.Error(fmt.Sprintf("Packet length check afer EXT failed. Expected: %d, got: %d\n", rtpHeaderLength+len(payload), use))
        return
    }
    buf = rp.Buffer()
    for i, v := range buf[0:use] {
        if v != initialPacket[i] {
            t.Error(fmt.Sprintf("Basic header buffer check after CSRC failed at %d. Expected: %x, got: %x\n", i, initialPacket[i], buf[i]))
            return
        }
    }
    if !csrcTest(rp, t, csrc_1, 1) || !extTest(rp, t, ext_1, 1) {
        return
    }
    if *verbose {
        rp.Print("CSCR/EXT combined")
    }
    use = rp.InUse()
    expected := rtpHeaderLength + len(payload) + len(csrc_1)*4 + len(ext_1)
    if use != expected {
        t.Error(fmt.Sprintf("Packet length check afer CSRC/EXT failed. Expected: %d, got: %d\n", expected, use))
        return
    }
}

func intervalCheck(t *testing.T) {
//                     members, senders, RTCP bandwidth, packet length, weSent, initial
    tm, _ := rtcpInterval(1,       0,        3500.0,         80.0,       false,  true)
    fmt.Printf("Interval: %d\n", tm)
    tm, _ = rtcpInterval(100,     0,        3500.0,         160.0,       false,  true)
    fmt.Printf("Interval: %d\n", tm)
}

func ntpCheck(t *testing.T) {
    tm := time.Now().UnixNano()
    high, low := toNtpStamp(tm)
    tm1 := fromNtp(high, low)
    diff := tm - tm1
    if diff < -1 || diff > 1 {
        t.Error(fmt.Sprintf("NTP time conversion check failed. Expected range: +/-1 got: %d\n", diff))
        return
    }
}

func parseFlags() {
    if !parsed {
        flag.Parse()
        parsed = true
    }
}

func TestRtpPacket(t *testing.T) {
    parseFlags()
    rtpPacket(t)
    ntpCheck(t)
//    intervalCheck(t)
}
