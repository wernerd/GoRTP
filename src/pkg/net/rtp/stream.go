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
    "crypto/rand"
    "time"
    "sync"
)

const (
    maxDropout    = 3000
    minSequential = 0
    maxMisorder   = 100
)

// The type of the stream.
const (
    InputStream = iota
    OutputStream
)

const (
    idle = iota
    active
    isClosing
    isClosed
)

type SdesItemMap map[int]string

// ctrlStatistics holds data relevant to compute RTCP Receive Reports (RR) and this is part of SsrcStreamIn
type ctrlStatistics struct {
    //
    // all times are in nanoseconds
    lastPacketTime, // time the last RTP packet from this source was received
    lastRtcpPacketTime, // time the last RTCP packet was received.
    initialDataTime,
    lastRtcpSrTime int64 // time the last RTCP SR was received. Required for DLSR computation.

    // Data used to compute outgoing RR reports.
    packetCount, // number of packets received from this source.       
    octetCount, // number of octets received from this source.
    extendedMaxSeqNum,
    lastPacketTransitTime,
    initialDataTimestamp,
    cumulativePacketLost uint32
    maxSeqNum    uint16 // the highest sequence number seen from this source
    fractionLost uint8
    // for interarrivel jitter computation
    // interarrival jitter of packets from this source.
    jitter uint32

    // this flag assures we only call one gotHello and one gotGoodbye for this src.
    flag bool
    // for source validation:
    probation  int // packets in sequence before valid.
    baseSeqNum uint16
    expectedPrior,
    receivedPrior,
    badSeqNum,
    seqNumAccum uint32
}

// SenderInfoData stores the counters if used for an output stream, stores the received sender info data for an input stream.
type SenderInfoData struct {
    NtpTime int64
    RtpTimestamp,
    SenderPacketCnt,
    SenderOctectCnt uint32
}

type RecvReportData struct {
    FracLost uint8
    PacketsLost,
    HighestSeqNo,
    Jitter,
    LastSr,
    Dlsr uint32
}

type SsrcStream struct {
    streamType   int
    streamStatus int
    streamMutex  sync.Mutex
    Address      // Own if it is an output stream, remote address in case of input stream
    SenderInfoData // Sender reports if this is an input stream, read only.
    RecvReportData // Receiver reports if this is an output stream, read anly.
    SdesItems      SdesItemMap // SDES item map, indexed by the RTCP SDES item types constants. 
    // Read only, to set item use SetSdesItem()
    sdesChunkLen   int // pre-computed SDES chunk length - updated when setting a new name, relevant for output streams

    // the following two field are active for input streams ony
    prevConflictAddr *Address
    statistics       ctrlStatistics

    // The following two field are active for ouput streams only
    initialTime  int64
    initialStamp uint32

    sequenceNumber uint16
    ssrc           uint32
    payloadType    byte
    sender         bool // true if this source (ouput or input) was identified as active sender

    // For input streams: true if RTP packet seen after last RR
    dataAfterLastReport bool
}

const defaultCname = "GoRTP1.0.0@somewhere"

const seqNumMod = (1 << 16)

/* 
 * *****************************************************************
 * SsrcStream functions, valid for output and input streams
 * *****************************************************************
 */

// Ssrc returns the SSRC of this stream in host order.
func (str *SsrcStream) Ssrc() uint32 {
    return str.ssrc
}

// SequenceNo returns the current RTP packet sequence number of this stream in host order.
func (str *SsrcStream) SequenceNo() uint16 {
    return str.sequenceNumber
}

// SetPayloadType sets the payload type of this stream.
//
// According to RFC 3550 an application may change the payload type during a
// the lifetime of a RTP stream. Refer to rtp.PayloadFormat type.
//
// The method returns false and does not set the payload type if the payload format
// is not available in rtp.PayloadFormatMap. An application must either use a known
// format or set the new payload format at the correct index before it sets the
// payload type of the stream.
//
//  pt - the payload type number.
//
func (str *SsrcStream) SetPayloadType(pt byte) (ok bool) {
    if _, ok = PayloadFormatMap[int(pt)]; !ok {
        return
    }
    str.payloadType = pt
    return
}

// PayloadType returns the payload type of this stream.
func (str *SsrcStream) PayloadType() byte {
    return str.payloadType
}


// StreamType returns stream's type, either input stream or otput stream.
//
func (str *SsrcStream) StreamType() int {
    return str.streamType
}

/* 
 * *****************************************************************
 * Processing for output streams
 * *****************************************************************
 */

func newSsrcStreamOut(own *Address, ssrc uint32, sequenceNo uint16) (so *SsrcStream) {
    so = new(SsrcStream)
    so.streamType = OutputStream
    so.ssrc = ssrc
    if ssrc == 0 {
        so.newSsrc()
    }
    so.sequenceNumber = sequenceNo
    if sequenceNo == 0 {
        so.newSequence()
    }
    so.IpAddr = own.IpAddr
    so.DataPort = own.DataPort
    so.CtrlPort = own.CtrlPort
    so.payloadType = 0xff // initialize to illegal payload type
    so.initialTime = time.Now().UnixNano()
    so.newInitialTimestamp()
    so.SdesItems = make(SdesItemMap, 2)
    so.SetSdesItem(SdesCname, defaultCname)
    return
}

// newDataPacket creates a new RTP packet suitable for use with the output stream.
//
// This method returns an initialized RTP packet that contains the correct SSRC, sequence
// number, the updated timestamp, and payload type if payload type was set in the stream.
//
// The application computes the next stamp based on the payload's frequency. The stamp usually
// advances by the number of samples contained in the RTP packet. 
//
// For example PCMU with a 8000Hz frequency sends 160 samples every 20m - thus the timestamp
// must adavance by 160 for each following packet. For fixed codecs, for example PCMU, the 
// number of samples correspond to the payload length. For variable codecs the number of samples
// has no direct relationship with the payload length.
//
//   stamp - the RTP timestamp for this packet. 
//
func (str *SsrcStream) newDataPacket(stamp uint32) (rp *DataPacket) {
    rp = newDataPacket()
    rp.SetSsrc(str.ssrc)
    rp.SetPayloadType(str.payloadType)
    rp.SetTimestamp(stamp + str.initialStamp)
    rp.SetSequence(str.sequenceNumber)
    str.sequenceNumber++
    return
}

// newCtrlPacket creates a new RTCP packet suitable for use with the specified output stream.
//
// This method returns an initialized RTCP packet that contains the correct SSRC, and the RTCP packet
// type. In addition the method returns the offset to the next position inside the buffer.
//  
//
//   streamindex - the index of the output stream as returned by NewSsrcStreamOut
//   stamp       - the RTP timestamp for this packet.
//
func (str *SsrcStream) newCtrlPacket(pktType int) (rp *CtrlPacket, offset int) {
    rp, offset = newCtrlPacket()
    rp.SetType(0, pktType)
    rp.SetSsrc(0, str.ssrc)
    return
}

// AddHeaderCtrl adds a new fixed RTCP header word into the compound, does not set SSRC after fixed header field
func (str *SsrcStream) addCtrlHeader(rp *CtrlPacket, offset, pktType int) (newOffset int) {
    newOffset = rp.addHeaderCtrl(offset)
    rp.SetType(offset, pktType)
    return
}

// newSsrc generates a random SSRC and sets it in stream
func (str *SsrcStream) newSsrc() {
    var randBuf [4]byte
    rand.Read(randBuf[:])
    ssrc := uint32(randBuf[0])
    ssrc |= uint32(randBuf[1]) << 8
    ssrc |= uint32(randBuf[2]) << 16
    ssrc |= uint32(randBuf[3]) << 24
    str.ssrc = ssrc

}

// newInitialTimestamp creates a random initiali timestamp for outgoing packets
func (str *SsrcStream) newInitialTimestamp() {
    var randBuf [4]byte
    rand.Read(randBuf[:])
    tmp := uint32(randBuf[0])
    tmp |= uint32(randBuf[1]) << 8
    tmp |= uint32(randBuf[2]) << 16
    tmp |= uint32(randBuf[3]) << 24
    str.initialStamp = (tmp & 0xFFFFFFF)
}

// newSequence generates a random sequence and sets it in stream
func (str *SsrcStream) newSequence() {
    var randBuf [2]byte
    rand.Read(randBuf[:])
    sequenceNo := uint16(randBuf[0])
    sequenceNo |= uint16(randBuf[1]) << 8
    sequenceNo &= 0xEFFF
    str.sequenceNumber = sequenceNo
}

// readRecvReport reads data from receive report and fills it into output stream RecvReportData structure
func (so *SsrcStream) readRecvReport(report recvReport) {
    so.FracLost = report.packetsLostFrac()
    so.PacketsLost = report.packetsLost()
    so.HighestSeqNo = report.highestSeq()
    so.Jitter = report.jitter()
    so.LastSr = report.lsr()
    so.Dlsr = report.dlsr()
}

// fillSenderInfo fills in the senderInfo.
func (so *SsrcStream) fillSenderInfo(info senderInfo) {
    info.setOctetCount(so.SenderOctectCnt)
    info.setPacketCount(so.SenderPacketCnt)
    tm := time.Now().UnixNano()
    sec, frac := toNtpStamp(tm)
    info.setNtpTimeStamp(sec, frac)

    tm1 := uint32(tm-so.initialTime) / 1e6                               // time since session creation in ms
    tm1 *= uint32(PayloadFormatMap[int(so.payloadType)].ClockRate / 1e3) // compute number of samples
    tm1 += so.initialStamp
    info.setRtpTimeStamp(tm1)
}

// makeSdesChunk creates an SDES chunk at the current inUse position and returns offset that points after the chunk.
func (so *SsrcStream) makeSdesChunk(rc *CtrlPacket) (newOffset int) {
    chunk, newOffset := rc.newSdesChunk(so.sdesChunkLen)
    copy(chunk, nullArray[:]) // fill with zeros before using
    chunk.setSsrc(so.ssrc)
    itemOffset := 4
    for itemType, name := range so.SdesItems {
        itemOffset += chunk.setItemData(itemOffset, byte(itemType), name)
    }
    return
}

// SetSdesItem set a new SDES item or overwrites an existing one with new text (string).
// An application shall set at least a CNAME SDES item text otherwise w use a default string.
func (so *SsrcStream) SetSdesItem(itemType int, itemText string) bool {
    if itemType <= SdesEnd || itemType >= sdesMax {
        return false
    }
    so.SdesItems[itemType] = itemText
    length := 4 // Initialize with SSRC length
    for _, name := range so.SdesItems {
        length += 2 + len(name) // add length of each item
    }
    if rem := length & 0x3; rem == 0 { // if already multiple of 4 add another 4 that holds "end" marker byte plus 3 bytes padding
        length += 4
    } else {
        length += 4 - rem
    }
    so.sdesChunkLen = length
    return true
}

// makeByeData creates a by data block after the BYE RTCP header field.
// Currently only one SSRC for bye data supported. Additional CSRCs requiere addiitonal data structure in output stream.
func (so *SsrcStream) makeByeData(rc *CtrlPacket, reason string) (newOffset int) {
    length := 4
    if len(reason) > 0 {
        length += (len(reason) + 3 + 1) & ^3 // plus one is the length field
    }
    bye, newOffset := rc.newByeData(length)
    bye.setSsrc(0, so.ssrc)
    if len(reason) > 0 {
        bye.setReason(reason, 1)
    }
    return
}

/* 
 * *****************************************************************
 *  Processing for input streams
 * *****************************************************************
 */

// newSsrcStreamIn creates a new input stream and sets the variables required for 
// RTP and RTCP processing to well defined initial values.
func newSsrcStreamIn(from *Address, ssrc uint32) (si *SsrcStream) {
    si = new(SsrcStream)
    si.streamType = InputStream
    si.ssrc = ssrc
    si.IpAddr = from.IpAddr
    si.DataPort = from.DataPort
    si.CtrlPort = from.CtrlPort
    si.SdesItems = make(SdesItemMap, 2)
    si.initStats()
    return
}

// checkSsrcIncomingData checks for collision or loops on incoming data packets.
// Implements th algorithm found in chap 8.2 in RFC 3550 
func (si *SsrcStream) checkSsrcIncomingData(existingStream bool, rs *Session, rp *DataPacket) (result bool) {
    result = true

    // Test if the source is new and its SSRC is not already used in an output stream.
    // Thus a new input stream without collision.
    if !existingStream && !rs.isOutputSsrc(si.ssrc) {
        return result
    }

    // Found an existing input stream. Check if it is still same address/port.
    // if yes, no conflicts, no further checks required.
    if si.DataPort != rp.fromAddr.DataPort || !si.IpAddr.Equal(rp.fromAddr.IpAddr) {
        // SSRC collision or a loop has happened
        strOut, _, localSsrc := rs.lookupSsrcMapOut(si.ssrc)
        if !localSsrc { // Not a SSRC in use for own output (local SSRC)
            // TODO: Optional error counter: Known SSRC stream changed address or port
            // Note this differs from the default in the RFC. Discard packet only when the collision is
            // repeating (to avoid flip-flopping)
            if si.prevConflictAddr != nil &&
                si.prevConflictAddr.IpAddr.Equal(rp.fromAddr.IpAddr) &&
                si.prevConflictAddr.DataPort == rp.fromAddr.DataPort {
                result = false // discard packet and do not flip-flop
            } else {
                // Record who has collided so that in the future we can know if the collision repeats.
                si.prevConflictAddr = &Address{rp.fromAddr.IpAddr, rp.fromAddr.DataPort, 0}
                // Change sync source transport address
                si.IpAddr = rp.fromAddr.IpAddr
                si.DataPort = rp.fromAddr.DataPort
            }
        } else {
            // Collision or loop of own packets. In this case si was found in ouput stream map, 
            // thus cannot be in input stream map
            if rs.checkConflictData(&rp.fromAddr) {
                // Optional error counter.
                result = false
            } else {
                // New collision
                // dispatch a BYE for a new confilicting output stream, using old SSRC
                // renew the output stream's SSRC
                rs.WriteCtrl(rs.buildRtcpByePkt(strOut, "SSRC collision detected when receiving RTCP packet."))
                rs.replaceStream(strOut)
                si.IpAddr = rp.fromAddr.IpAddr
                si.DataPort = rp.fromAddr.DataPort
                si.CtrlPort = 0
                si.initStats()
            }
        }
    }
    return
}

// recordReceptionData checks validity (probation), sequence numbers, computes jitter, and records the statistics for incoming data packets.
// See algorithms in chapter A.1 (sequence number handling) and A.8 (jitter computation)
func (si *SsrcStream) recordReceptionData(rp *DataPacket, rs *Session, recvTime int64) (result bool) {
    result = true

    seq := rp.Sequence()

    if si.statistics.probation != 0 {
        // source is not yet valid.
        if seq == si.statistics.maxSeqNum+1 {
            // packet in sequence.
            si.statistics.probation--
            if si.statistics.probation == 0 {
                si.statistics.seqNumAccum = 0
            } else {
                result = false
            }
        } else {
            // packet not in sequence.
            si.statistics.probation = minSequential - 1
            result = false
        }
        si.statistics.maxSeqNum = seq
    } else {
        // source was already valid.
        step := seq - si.statistics.maxSeqNum
        if step < maxDropout {
            // Ordered, with not too high step.
            if seq < si.statistics.maxSeqNum {
                // sequene number wrapped.
                si.statistics.seqNumAccum += seqNumMod
            }
            si.statistics.maxSeqNum = seq
        } else if step <= (seqNumMod - maxMisorder) {
            // too high step of the sequence number.
            // TODO: check usage of baseSeqNum
            if uint32(seq) == si.statistics.badSeqNum {
                // Here we saw two sequential packets - assume other side restarted, so just re-sync
                // and treat this packet as first packet
                si.statistics.maxSeqNum = seq
                si.statistics.baseSeqNum = seq
                si.statistics.seqNumAccum = 0
                si.statistics.badSeqNum = seqNumMod + 1
            } else {
                si.statistics.badSeqNum = uint32((seq + 1) & (seqNumMod - 1))
                // This additional check avoids that the very first packet from a source be discarded.
                if si.statistics.packetCount > 0 {
                    result = false
                } else {
                    si.statistics.maxSeqNum = seq
                }
            }
        } else {
            // duplicate or reordered packet
        }
    }

    if result {
        si.sequenceNumber = si.statistics.maxSeqNum
        // the packet is considered valid.
        si.statistics.packetCount++
        si.statistics.octetCount += uint32(len(rp.Payload()))
        if si.statistics.packetCount == 1 {
            si.statistics.initialDataTimestamp = rp.Timestamp()
            si.statistics.baseSeqNum = seq
        }
        si.streamMutex.Lock()
        si.statistics.lastPacketTime = recvTime
        if !si.sender && rs.rtcpCtrlChan != nil {
            rs.rtcpCtrlChan <- rtcpIncrementSender
        }
        si.sender = true // Stream is sender. If it was false new stream or no RTP packets for some time
        si.dataAfterLastReport = true
        si.streamMutex.Unlock()

        // compute the interarrival jitter estimation.
        pt := int(rp.PayloadType())
        // compute lastPacketTime to ms and clockrate as kHz 
        arrival := uint32(si.statistics.lastPacketTime / 1e6 * int64(PayloadFormatMap[pt].ClockRate/1e3))
        transitTime := arrival - rp.Timestamp()
        if si.statistics.lastPacketTransitTime != 0 {
            delta := int32(transitTime - si.statistics.lastPacketTransitTime)
            if delta < 0 {
                delta = -delta
            }
            si.statistics.jitter += uint32(delta) - ((si.statistics.jitter + 8) >> 4)
        }
        si.statistics.lastPacketTransitTime = transitTime
    }
    return
}

// checkSsrcIncomingData checks for collision or loops on incoming data packets.
// Implements th algorithm found in chap 8.2 in RFC 3550 
func (si *SsrcStream) checkSsrcIncomingCtrl(existingStream bool, rs *Session, from *Address) (result bool) {
    result = true

    // Test if the source is new and its SSRC is not already used in an output stream.
    // Thus a new input stream without collision.
    if !existingStream && !rs.isOutputSsrc(si.ssrc) {
        return result
    }
    // Found an existing input stream. Check if it is still same address/port.
    // if yes, no conflicts, no further checks required.
    if si.CtrlPort != from.CtrlPort || !si.IpAddr.Equal(from.IpAddr) {
        // SSRC collision or a loop has happened
        strOut, _, localSsrc := rs.lookupSsrcMapOut(si.ssrc)
        if !localSsrc { // Not a SSRC in use for own output (local SSRC)
            // TODO: Optional error counter: Know SSRC stream changed address or port
            // Note this differs from the default in the RFC. Discard packet only when the collision is
            // repeating (to avoid flip-flopping)
            if si.prevConflictAddr != nil &&
                si.prevConflictAddr.IpAddr.Equal(from.IpAddr) &&
                si.prevConflictAddr.CtrlPort == from.CtrlPort {
                result = false // discard packet and do not flip-flop
            } else {
                // Record who has collided so that in the future we can know if the collision repeats.
                si.prevConflictAddr = &Address{from.IpAddr, 0, from.CtrlPort}
                // Change sync source transport address
                si.IpAddr = from.IpAddr
                si.CtrlPort = from.CtrlPort
            }
        } else {
            // Collision or loop of own packets. In this case strOut == si.
            if rs.checkConflictCtrl(from) {
                // Optional error counter.
                result = false
            } else {
                // New collision, dispatch a BYE using old SSRC, renew the output stream's SSRC
                rs.WriteCtrl(rs.buildRtcpByePkt(strOut, "SSRC collision detected when receiving RTCP packet."))
                rs.replaceStream(strOut)
                si.IpAddr = from.IpAddr
                si.DataPort = 0
                si.CtrlPort = from.CtrlPort
                si.initStats()
            }
        }
    }
    return
}

// makeRecvReport fills a receiver report at the current inUse position and returns offset that points after the report.
// See chapter A.3 in RFC 3550 regarding the packet lost algorithm, end of chapter 6.4.1 regarding LSR, DLSR stuff.
//
func (si *SsrcStream) makeRecvReport(rp *CtrlPacket) (newOffset int) {

    report, newOffset := rp.newRecvReport()

    extMaxSeq := si.statistics.seqNumAccum + uint32(si.statistics.maxSeqNum)
    expected := extMaxSeq - uint32(si.statistics.baseSeqNum) + 1
    lost := expected - si.statistics.packetCount
    if si.statistics.packetCount == 0 {
        lost = 0
    }
    expectedDelta := expected - si.statistics.expectedPrior
    si.statistics.expectedPrior = expected

    receivedDelta := si.statistics.packetCount - si.statistics.receivedPrior
    si.statistics.receivedPrior = si.statistics.packetCount

    lostDelta := expectedDelta - receivedDelta

    var fracLost byte
    if expectedDelta != 0 && lostDelta > 0 {
        fracLost = byte((lostDelta << 8) / expectedDelta)
    }

    var lsr, dlsr uint32
    if si.statistics.lastRtcpSrTime != 0 {
        sec, frac := toNtpStamp(si.statistics.lastRtcpSrTime)
        ntp := (sec << 32) | frac
        lsr = ntp >> 16
        sec, frac = toNtpStamp(time.Now().UnixNano() - si.statistics.lastRtcpSrTime)
        ntp = (sec << 32) | frac
        dlsr = ntp >> 16
    }

    report.setSsrc(si.ssrc)
    report.setPacketsLost(lost)
    report.setPacketsLostFrac(fracLost)
    report.setHighestSeq(extMaxSeq)
    report.setJitter(si.statistics.jitter >> 4)
    report.setLsr(lsr)
    report.setDlsr(dlsr)

    return
}

func (si *SsrcStream) readSenderInfo(info senderInfo) {
    seconds, fraction := info.ntpTimeStamp()
    si.NtpTime = fromNtp(seconds, fraction)
    si.RtpTimestamp = info.rtpTimeStamp()
    si.SenderPacketCnt = info.packetCount()
    si.SenderOctectCnt = info.octetCount()
}

// goodBye marks this source as having sent a BYE control packet.
func (si *SsrcStream) goodbye() bool {
    if !si.statistics.flag {
        return false
    }
    si.statistics.flag = false
    return true
}

// hello marks this source as having sent some packet.
func (si *SsrcStream) hello() bool {
    if si.statistics.flag {
        return false
    }
    si.statistics.flag = true
    return true
}

// initStats initializes all RTCP statistic counters and other relevant data. 
func (si *SsrcStream) initStats() {
    si.statistics.lastPacketTime = 0
    si.statistics.lastRtcpPacketTime = 0
    si.statistics.lastRtcpSrTime = 0

    si.statistics.packetCount = 0
    si.statistics.octetCount = 0
    si.statistics.maxSeqNum = 0
    si.statistics.extendedMaxSeqNum = 0
    si.statistics.cumulativePacketLost = 0
    si.statistics.fractionLost = 0
    si.statistics.jitter = 0
    si.statistics.initialDataTimestamp = 0
    si.statistics.initialDataTime = 0
    si.statistics.flag = false

    si.statistics.badSeqNum = seqNumMod + 1
    si.statistics.probation = minSequential
    si.statistics.baseSeqNum = 0
    si.statistics.expectedPrior = 0
    si.statistics.receivedPrior = 0
    si.statistics.seqNumAccum = 0
}

func (si *SsrcStream) parseSdesChunk(sc sdesChunk) {
    offset := 4 // points after SSRC field of this chunk    

    for {
        itemType := sc.getItemType(offset)
        if itemType <= SdesEnd || itemType >= sdesMax {
            return
        }

        txtLen := sc.getItemLen(offset)
        itemTxt := sc.getItemText(offset, txtLen)
        offset += 2 + txtLen
        if name, ok := si.SdesItems[itemType]; ok && name == itemTxt {
            continue
        }
        txt := make([]byte, len(itemTxt))
        copy(txt, itemTxt)
        si.SdesItems[itemType] = string(txt)
    }
}
