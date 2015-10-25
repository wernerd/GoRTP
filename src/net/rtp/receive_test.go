package rtp

import (
    "net"
    "testing"
    "time"
    //    "encoding/hex"
    //    "fmt"
)

var rsRecv, rsSender *Session

var recvPort = 5220
var senderPort = 5222
var senderAddr *net.IPAddr
var dataReceiver DataReceiveChan

var localZone = ""
var remoteZone = ""

func initSessions() {
    recvAddr, _ := net.ResolveIPAddr("ip", "127.0.0.1")
    senderAddr, _ = net.ResolveIPAddr("ip", "127.0.0.2")

    // Create a UDP transport with "local" address and use this for a "local" RTP session
    // Not used in these tests, used to initialize and get a Session
    tpRecv, _ := NewTransportUDP(recvAddr, recvPort, localZone)

    // TransportUDP implements RtpTransportWrite and RtpTransportRecv interfaces thus
    // set it in the RtpSession for both interfaces
    rsRecv = NewSession(tpRecv, tpRecv)

    // Create and store the data receive channel.
    dataReceiver = rsRecv.CreateDataReceiveChan()

    // Create a media stream. 
    // The SSRC identifies the stream. Each stream has its own sequence number and other 
    // context. A RTP session can have several RTP stream for example to send several
    // streams of the same media. Need an output stream to test for collisions/loops
    //
    strIdx, _ := rsRecv.NewSsrcStreamOut(&Address{recvAddr.IP, recvPort, recvPort + 1, localZone}, 0x01020304, 0x4711)
    rsRecv.SsrcStreamOutForIndex(strIdx).SetSdesItem(SdesCname, "AAAAAA")
    rsRecv.SsrcStreamOutForIndex(strIdx).SetPayloadType(0)
    rsRecv.rtcpServiceActive = true // to simulate an active RTCP service

    tpSender, _ := NewTransportUDP(senderAddr, senderPort, remoteZone)
    rsSender = NewSession(tpSender, tpSender)
}

func receivePacket(t *testing.T, num int) {

    select {
    case rp := <-dataReceiver: // just get a packet - maybe we add some tests later
        rp.FreePacket()
    default: // no packet - should not happen, report this 
        t.Errorf("Unexpected case: data receiver channel is empty at %d.\n", num)
    }
}

// Create a RTP "sender" packet, no payload, just SSRC and address pair
func newSenderPacket(stamp uint32) (rp *DataPacket) {
    rp = rsSender.NewDataPacket(stamp)

    // initialize with "sender" address to enable all necessary checks
    rp.fromAddr.IpAddr = senderAddr.IP
    rp.fromAddr.DataPort = senderPort
    rp.fromAddr.CtrlPort = 0
    return
}

// The following tests are really white box tests - they check internal variables, manipulate
// internal variables to get the expected results. 

func rtpReceive(t *testing.T) {
    // ******************** New session setup to have fresh data *************************** 
    initSessions()

    pay := make([]byte, 160)
    // Create a RTP "sender" stream, with defined SSRC, sequence and payload type (PCMU in this case)
    // The defined sequence number (maxDropout-1) tests one path of sequence number initialization for
    // the input stream.
    seqNum := uint16(maxDropout - 1)
    strIdx, _ := rsSender.NewSsrcStreamOut(&Address{senderAddr.IP, senderPort, senderPort + 1, remoteZone}, 0x04030201, seqNum)
    strOut := rsSender.SsrcStreamOutForIndex(strIdx)
    strOut.SetPayloadType(0)

    // Test the SDES management stuff 
    strOut.SetSdesItem(SdesCname, "AAAAAA")
    strOut.SetSdesItem(SdesEmail, "BBBBBBB")
    if strOut.sdesChunkLen != 24 { // Chunk length does not include SDES header (4 bytes)
        t.Errorf("SDES chunk length check 1 failed. Expected: 24, got: %d\n", strOut.sdesChunkLen)
        return
    }
    strOut.SetSdesItem(SdesEmail, "BBBBBB") // reset e-mail name, on char less, total length must stay (padding)
    if strOut.sdesChunkLen != 24 {
        t.Errorf("SDES chunk length check 2 failed. Expected: 24, got: %d\n", strOut.sdesChunkLen)
        return
    }
    rpSender := newSenderPacket(160)
    rpSender.SetPayload(pay)

    // Feed into receiver session, then check if packet was processed correctly
    rsRecv.OnRecvData(rpSender)

    // Expect one new input stream with SSRC and address of sender packet
    idx := rsRecv.streamInIndex
    if idx != 1 {
        t.Errorf("StreamIn index check failed. Expected: 1, got: %d\n", idx)
        return
    }
    // get the new (default) input stream
    strIn := rsRecv.SsrcStreamIn()
    ssrc := strIn.Ssrc()
    if ssrc != 0x04030201 {
        t.Errorf("StreamIn SSRC check failed. Expected: 0x04030201, got: %x\n", ssrc)
        return
    }
    maxSeq := strIn.statistics.maxSeqNum
    if maxSeq != seqNum {
        t.Errorf("First maxSeqNum check failed. Expected: %d, got: %d\n", seqNum, maxSeq)
        return
    }
    badSeq := strIn.statistics.badSeqNum
    if badSeq != seqNumMod+1 {
        t.Errorf("First badSeqNum check failed. Expected: 0x%x, got: 0x%x\n", seqNumMod+1, badSeq)
        return
    }
    receivePacket(t, 0)

    // The 20/15ms sleeps simulate a jitter at the receiver's end. The expected jitter range takes some
    // additional delays into account. "go thread" switching introduces additional delays
    time.Sleep(20e6)

    rpSender = newSenderPacket(320)
    seqNum++
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 1)
    time.Sleep(15e6)

    rpSender = newSenderPacket(480)
    seqNum++
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 2)
    time.Sleep(20e6)

    rpSender = newSenderPacket(640)
    seqNum++
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 3)
    time.Sleep(15e6)

    rpSender = newSenderPacket(800)
    seqNum++
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 4)
    time.Sleep(20e6)

    rpSender = newSenderPacket(960)
    seqNum++
    rsRecv.OnRecvData(rpSender)

    maxSeq = strIn.statistics.maxSeqNum
    if maxSeq != seqNum {
        t.Errorf("Second maxSeqNum check failed. Expected: %d, got: %d\n", seqNum, maxSeq)
        return
    }
    badSeq = strIn.statistics.badSeqNum
    if badSeq != seqNumMod+1 {
        t.Errorf("Second badSeqNum check failed. Expected: 0x%x, got: 0x%x\n", seqNumMod+1, badSeq)
        return
    }
    jitter := strIn.statistics.jitter >> 4
    if jitter <= 0 && jitter > 10 {
        t.Errorf("Jitter test failed. Expected jitter range: 0 < jitter < 10, got: %d\n", jitter)
        return
    }
    receivePacket(t, 5)

    // Create a RTCP packet and fill in senderInfo of the output stream
    rcTime, offset := strOut.newCtrlPacket(RtcpSR)
    rcTime.addHeaderSsrc(offset, strOut.Ssrc())

    newInfo, _ := rcTime.newSenderInfo()
    strOut.fillSenderInfo(newInfo) // create a sender info block after fixed header and SSRC.

    // the above jitter test took 90ms. Thus the difference between session start and now is about 90ms and
    // the RTP timestamp should be 720 units for the selected payload (PCMU, 8000Hz). To get this we have
    // to subtract the random initial timestamp.
    tm := time.Now().UnixNano()
    info := rcTime.toSenderInfo(rtcpHeaderLength + rtcpSsrcLength)
    stamp := info.rtpTimeStamp() - strOut.initialStamp

    if stamp != 720 {
        t.Logf("rtpTimeStamp test out of range - logged only. Expected rtpTimeStamp: 720, got: %d\n", stamp)
    }
    high, low := info.ntpTimeStamp()
    tm1 := fromNtp(high, low)
    diff := tm - tm1
    // check if it is in a reasonable range. Take some thread switching into account. tm1 must be smaller than
    // tm because tm was taken after makeSenderInfo that computes the timestamp in the senderInfo.
    if diff > 30000 {
        t.Errorf("NTP time check in senderInfo failed. Expected range: +30000, got: %d\n", diff)
        return
    }

    // Create a RTCP compound packet that will contain: one RTCP header, one recvReport, one SDES with 
    // chunk length 28 which gives a compound total of 8 + 24 + 28 = 60 bytes

    // First get a new ctrl packet and initialize it so that we can "send" it to some internal "receiver" methods.

    // Set the receiver as "sender" as well, thus we will have a senderInfo part in the packet as well
    rsRecv.streamsOut[0].sender = true

    // build a RTCP packet for the standard output stream
    rcSender := rsRecv.buildRtcpPkt(rsRecv.SsrcStreamOut(), 31)

    rcSender.fromAddr.IpAddr = senderAddr.IP
    rcSender.fromAddr.DataPort = 0
    rcSender.fromAddr.CtrlPort = senderPort + 1

    // ***    fmt.Printf("1st Ctrl buffer: %s\n", hex.EncodeToString(rcSender.buffer[:rcSender.InUse()]))
    rcTotalLength := rcSender.InUse()
    if rcTotalLength != rtcpHeaderLength+rtcpSsrcLength+senderInfoLen+reportBlockLen+20 { // 20: SDES header plus SDES chunk
        t.Errorf("rcSender packet length check failed. Expected: %d, got: %d\n",
            rtcpHeaderLength+rtcpSsrcLength+senderInfoLen+reportBlockLen+20, rcTotalLength)
        return
    }
    if !rsRecv.OnRecvCtrl(rcSender) {
        t.Errorf("OnRecvCtrl failed for RTCP packet.\n")
        return
    }
    // Need to perform a lookup here: with the last OnRecvCtrl we have produced a collision. Now the receiver has two
    // input streams: one with 0x04030201 and one with 0x01020304. This happened because, for this test,
    // we have produced the control packet from the receiver session and fed that packet into the receiver.
    // The receiver now has a newly initialized output stream (one only) with new random SSRC and sequence numbers.

    if rsRecv.streamInIndex != 2 {
        t.Errorf("Input stream index check failed. Expected: 2, got: %d\n", rsRecv.streamInIndex)
        return
    }
    if rsRecv.streamOutIndex != 1 {
        t.Errorf("Output stream index check failed. Expected: 1, got: %d\n", rsRecv.streamOutIndex)
        return
    }
    // lookup and get the new input stream and check if SDES was parsed correctly
    inx, _, _ := rsRecv.lookupSsrcMapIn(rcSender.Ssrc(0))
    if inx.SdesItems[SdesCname] != "AAAAAA" {
        t.Errorf("SDES chunk parsing failed. Expected: 'AAAAAA', got: %s\n", strIn.SdesItems[SdesCname])
        return
    }

    // Now set sender to false, only RR packet plus SDES
    rsRecv.streamsOut[0].sender = false
    rsRecv.streamsOut[0].streamStatus = active // just to pass the active check during onRecvCtrl()

    rsRecv.streamsIn[0].dataAfterLastReport = true // just to simulate received RTP data to generate correct RR

    rcSender = rsRecv.buildRtcpPkt(rsRecv.SsrcStreamOut(), 31)

    rcSender.fromAddr.IpAddr = senderAddr.IP
    rcSender.fromAddr.DataPort = 0
    rcSender.fromAddr.CtrlPort = senderPort + 3 // just to avoid an addtional conflict - but collosion will happen

    // ***    fmt.Printf("2nd Ctrl buffer: %s\n", hex.EncodeToString(rcSender.buffer[:rcSender.InUse()]))
    rcTotalLength = rcSender.InUse()

    // we have still have 1 receiver report here because the second receiver generated (see test above) never
    // sent an RTP packet, thus is not included in RR
    if rcTotalLength != rtcpHeaderLength+rtcpSsrcLength+reportBlockLen+20 { // 20: SDES header plus SDES chunk
        t.Errorf("rcSender packet length check failed. Expected: %d, got: %d\n",
            rtcpHeaderLength+rtcpSsrcLength+reportBlockLen+20, rcTotalLength)
        return
    }
    if !rsRecv.OnRecvCtrl(rcSender) {
        t.Errorf("OnRecvCtrl failed for RTCP packet.\n")
        return
    }
    // Need to perform a lookup here: with this test we have produced another collision. Now the receiver has three
    // input streams: one with 0x04030201, one with 0x01020304, one with random SSRC (see test above). This 
    // happened because, for this test, we have produced the control packet from the receiver session and fed that
    // packet into the receiver.
    // The receiver now again has a newly initialized output stream (one only) with new random SSRC and sequence numbers.

    if rsRecv.streamInIndex != 3 {
        t.Errorf("Input stream index check failed. Expected: 3, got: %d\n", rsRecv.streamInIndex)
        return
    }
    if rsRecv.streamOutIndex != 1 {
        t.Errorf("Output stream index check failed. Expected: 1, got: %d\n", rsRecv.streamOutIndex)
        return
    }
    // lookup and get the new input stream and check if SDES was parsed correctly
    inx, _, _ = rsRecv.lookupSsrcMapIn(rcSender.Ssrc(0))
    if inx.SdesItems[SdesCname] != "AAAAAA" {
        t.Errorf("SDES chunk parsing failed. Expected: 'AAAAAA', got: %s\n", strIn.SdesItems[SdesCname])
        return
    }
    // The receiver has three input streams: one with 0x04030201, one with 0x01020304, one with random 
    // SSRC (see test above) - the latest one with random SSRC is ommited from receiver reports because
    // it was no "active", neither sent or received a packet

    rcSender = rsRecv.buildRtcpByePkt(rsRecv.SsrcStreamOut(), "CCCCCC")
    rcTotalLength = rcSender.InUse()
    // ***    fmt.Printf("3rd Ctrl buffer: %s\n", hex.EncodeToString(rcSender.buffer[:rcSender.InUse()]))

    // BYE packet has empty RR; 20: SDES header plus SDES chunk; 16: BYE RTCP packet
    if rcTotalLength != rtcpHeaderLength+rtcpSsrcLength+20+16 {
        t.Errorf("rcSender packet length check failed. Expected: %d, got: %d\n",
            rtcpHeaderLength+rtcpSsrcLength+20+16, rcTotalLength)
        return
    }

    // ******************** New session setup to have fresh data *************************** 
    initSessions()
    // Create a RTP "sender" stream, with defined SSRC, sequence and payload type. Define the sequence number to
    // check second if-path when initalizing the sequence number for input stream
    seqNum = uint16(maxDropout)
    strIdx, _ = rsSender.NewSsrcStreamOut(&Address{senderAddr.IP, senderPort, senderPort + 1, remoteZone}, 0x04030201, seqNum)
    strOut = rsSender.SsrcStreamOutForIndex(strIdx)
    strOut.SetPayloadType(0)

    rpSender = newSenderPacket(160)
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 6)

    strIn = rsRecv.SsrcStreamIn()
    maxSeq = strIn.statistics.maxSeqNum
    if maxSeq != seqNum {
        t.Errorf("Third maxSeqNum check failed. Expected: %d, got: %d\n", seqNum, maxSeq)
        return
    }
    badSeq = strIn.statistics.badSeqNum
    if badSeq != uint32(seqNum+1) {
        t.Errorf("Third badSeqNum check failed. Expected: %d, got: %d\n", seqNum+1, badSeq)
        return
    }

    // After receiving a packet with a sequence number "maxDropout" now simulate a large step in the sequence
    // number. Expected result: the old sequence nummber (maxSeq) stays, badSeq is the new (higher) sequence plus one
    rpSender = newSenderPacket(160)

    // Force a large jump in sequence number which causes the receiver to drop this packet, so don't try to receive it.
    seqNum = uint16(maxDropout * 2)
    rpSender.SetSequence(seqNum)
    rsRecv.OnRecvData(rpSender)
    //    receivePacket(t, 7)

    maxSeq = strIn.statistics.maxSeqNum
    if maxSeq != maxDropout {
        t.Errorf("Forth maxSeqNum check failed. Expected: %d, got: %d\n", maxDropout, maxSeq)
        return
    }
    badSeq = strIn.statistics.badSeqNum
    if badSeq != uint32(seqNum+1) {
        t.Errorf("Forth badSeqNum check failed. Expected: %d, got: %d\n", seqNum+1, badSeq)
        return
    }

    // Now send a packet which is in sequence to the first (lower, maxDropout+1) sequence. This simluates
    // a lingering RTP packet after the sender switched to new higher sequence numbers.
    // Expected result: sequence nummber maxSeq is maxDropout+1, badSeq is the new (higer) sequence plus one
    rpSender = newSenderPacket(160)
    seqNum = uint16(maxDropout + 1)
    rpSender.SetSequence(seqNum)
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 8)

    maxSeq = strIn.statistics.maxSeqNum
    if maxSeq != maxDropout+1 {
        t.Errorf("Fifth maxSeqNum check failed. Expected: %d, got: %d\n", maxDropout+1, maxSeq)
        return
    }
    badSeq = strIn.statistics.badSeqNum
    if badSeq != 2*maxDropout+1 {
        t.Errorf("Fifth badSeqNum check failed. Expected: %d, got: %d\n", 2*maxDropout+1, badSeq)
        return
    }
    // Now send a packet which is in sequence with the new higher (maxDropout*2+1) sequence. This simluates
    // a RTP packet in sequence after the sender switched to new higher sequence numbers
    // Expected result: sequence nummber maxSeq is maxDropout*2+1, badSeq is seqNumMod + 1, a resync happened
    rpSender = newSenderPacket(160)
    seqNum = uint16(maxDropout*2 + 1)
    rpSender.SetSequence(seqNum)
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 9)

    maxSeq = strIn.statistics.maxSeqNum
    if maxSeq != seqNum {
        t.Errorf("Sixth maxSeqNum check failed. Expected: %d, got: %d\n", seqNum, maxSeq)
        return
    }
    badSeq = strIn.statistics.badSeqNum
    if badSeq != seqNumMod+1 {
        t.Errorf("Sixth badSeqNum check failed. Expected: %d, got: %d\n", seqNumMod+1, badSeq)
        return
    }

    // ******************** New session setup to have fresh data *************************** 
    initSessions()
    // Create a RTP "sender" stream, with defined SSRC, sequence and payload type. Define the sequence number to
    // enable checks if sequence number wraps. First use a sequence number near wrap but small enough to go through
    // the initial tests
    seqNum = uint16(seqNumMod - maxMisorder - 2)
    strIdx, _ = rsSender.NewSsrcStreamOut(&Address{senderAddr.IP, senderPort, senderPort + 1, remoteZone}, 0x04030201, seqNum)
    strOut = rsSender.SsrcStreamOutForIndex(strIdx)
    strOut.SetPayloadType(0)

    rpSender = newSenderPacket(160)
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 10)

    strIn = rsRecv.SsrcStreamIn()
    maxSeq = strIn.statistics.maxSeqNum
    if maxSeq != seqNum {
        t.Errorf("Seventh maxSeqNum check failed. Expected: %d, got: %d\n", seqNum, maxSeq)
        return
    }
    badSeq = strIn.statistics.badSeqNum
    if badSeq != uint32(seqNum+1) {
        t.Errorf("Seventh badSeqNum check failed. Expected: %d, got: %d\n", seqNum+1, badSeq)
        return
    }
    rpSender = newSenderPacket(160)

    // Now step up sequence number near wrapping value (2^16-1), this step is small, thus it is considered "in sequence"
    // and badSeqNum will not change from its value above
    seqNum = uint16(seqNumMod - 1)
    rpSender.SetSequence(seqNum)
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 11)

    strIn = rsRecv.SsrcStreamIn()
    maxSeq = strIn.statistics.maxSeqNum
    if maxSeq != seqNum {
        t.Errorf("Eighth maxSeqNum check failed. Expected: %d, got: %d\n", seqNum, maxSeq)
        return
    }
    rpSender = newSenderPacket(160)

    // Now step up sequence number so it wraps from 2^16-1 to 2^16 (i.e. 0), it is considered "in sequence" and
    // the sequence numbers will wrap to 0, the warp-counter (accum) is enhanced by seqNumMod (was zero in this case)
    seqNum++
    rpSender.SetSequence(seqNum)
    rsRecv.OnRecvData(rpSender)
    receivePacket(t, 12)

    strIn = rsRecv.SsrcStreamIn()
    maxSeq = strIn.statistics.maxSeqNum
    if maxSeq != seqNum {
        t.Errorf("Nineth maxSeqNum check failed. Expected: %d, got: %d\n", seqNum, maxSeq)
        return
    }
    accu := strIn.statistics.seqNumAccum
    if accu != seqNumMod {
        t.Errorf("Sequence number wrapping check failed. Expected: %d, got: %d\n", seqNumMod, accu)
        return
    }
    select {
    case <-dataReceiver: // Here we have a lingering packet.
        t.Errorf("Unexpected packet received after all tests done.\n")
    default: // no packet - should not happen, report this 
    }
}

func TestReceive(t *testing.T) {
    parseFlags()
    rtpReceive(t)
}
