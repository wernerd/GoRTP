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
	"sync"
	"time"
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

// SdesItemMap item map, indexed by the RTCP SDES item types constants.
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
	streamType     int
	streamStatus   int
	streamMutex    sync.Mutex
	Address                    // Own if it is an output stream, remote address in case of input stream
	SenderInfoData             // Sender reports if this is an input stream, read only.
	RecvReportData             // Receiver reports if this is an output stream, read anly.
	SdesItems      SdesItemMap // SDES item map, indexed by the RTCP SDES item types constants.
	// Read only, to set item use SetSdesItem()
	sdesChunkLen int // pre-computed SDES chunk length - updated when setting a new name, relevant for output streams

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
func (strm *SsrcStream) Ssrc() uint32 {
	return strm.ssrc
}

// SequenceNo returns the current RTP packet sequence number of this stream in host order.
func (strm *SsrcStream) SequenceNo() uint16 {
	return strm.sequenceNumber
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
func (strm *SsrcStream) SetPayloadType(pt byte) (ok bool) {
	if _, ok = PayloadFormatMap[int(pt)]; !ok {
		return
	}
	strm.payloadType = pt
	return
}

// PayloadType returns the payload type of this stream.
func (strm *SsrcStream) PayloadType() byte {
	return strm.payloadType
}

// StreamType returns stream's type, either input stream or otput stream.
//
func (strm *SsrcStream) StreamType() int {
	return strm.streamType
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
	so.IPAddr = own.IPAddr
	so.DataPort = own.DataPort
	so.CtrlPort = own.CtrlPort
	so.Zone = own.Zone
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
func (strm *SsrcStream) newDataPacket(stamp uint32) (rp *DataPacket) {
	rp = newDataPacket()
	rp.SetSsrc(strm.ssrc)
	rp.SetPayloadType(strm.payloadType)
	rp.SetTimestamp(stamp + strm.initialStamp)
	rp.SetSequence(strm.sequenceNumber)
	strm.sequenceNumber++
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
func (strm *SsrcStream) newCtrlPacket(pktType int) (rp *CtrlPacket, offset int) {
	rp, offset = newCtrlPacket()
	rp.SetType(0, pktType)
	rp.SetSsrc(0, strm.ssrc)
	return
}

// AddHeaderCtrl adds a new fixed RTCP header word into the compound, does not set SSRC after fixed header field
func (strm *SsrcStream) addCtrlHeader(rp *CtrlPacket, offset, pktType int) (newOffset int) {
	newOffset = rp.addHeaderCtrl(offset)
	rp.SetType(offset, pktType)
	return
}

// newSsrc generates a random SSRC and sets it in stream
func (strm *SsrcStream) newSsrc() {
	var randBuf [4]byte
	rand.Read(randBuf[:])
	ssrc := uint32(randBuf[0])
	ssrc |= uint32(randBuf[1]) << 8
	ssrc |= uint32(randBuf[2]) << 16
	ssrc |= uint32(randBuf[3]) << 24
	strm.ssrc = ssrc

}

// newInitialTimestamp creates a random initiali timestamp for outgoing packets
func (strm *SsrcStream) newInitialTimestamp() {
	var randBuf [4]byte
	rand.Read(randBuf[:])
	tmp := uint32(randBuf[0])
	tmp |= uint32(randBuf[1]) << 8
	tmp |= uint32(randBuf[2]) << 16
	tmp |= uint32(randBuf[3]) << 24
	strm.initialStamp = (tmp & 0xFFFFFFF)
}

// newSequence generates a random sequence and sets it in stream
func (strm *SsrcStream) newSequence() {
	var randBuf [2]byte
	rand.Read(randBuf[:])
	sequenceNo := uint16(randBuf[0])
	sequenceNo |= uint16(randBuf[1]) << 8
	sequenceNo &= 0xEFFF
	strm.sequenceNumber = sequenceNo
}

// readRecvReport reads data from receive report and fills it into output stream RecvReportData structure
func (strm *SsrcStream) readRecvReport(report recvReport) {
	strm.FracLost = report.packetsLostFrac()
	strm.PacketsLost = report.packetsLost()
	strm.HighestSeqNo = report.highestSeq()
	strm.Jitter = report.jitter()
	strm.LastSr = report.lsr()
	strm.Dlsr = report.dlsr()
}

// fillSenderInfo fills in the senderInfo.
func (strm *SsrcStream) fillSenderInfo(info senderInfo) {
	info.setOctetCount(strm.SenderOctectCnt)
	info.setPacketCount(strm.SenderPacketCnt)
	tm := time.Now().UnixNano()
	sec, frac := toNtpStamp(tm)
	info.setNtpTimeStamp(sec, frac)

	tm1 := uint32(tm-strm.initialTime) / 1e6 // time since session creation in ms
	if v, ok := PayloadFormatMap[int(strm.payloadType)]; ok {
		tm1 *= uint32(v.ClockRate / 1e3) // compute number of samples
		tm1 += strm.initialStamp
		info.setRtpTimeStamp(tm1)
	}
}

// makeSdesChunk creates an SDES chunk at the current inUse position and returns offset that points after the chunk.
func (strm *SsrcStream) makeSdesChunk(rc *CtrlPacket) (newOffset int) {
	chunk, newOffset := rc.newSdesChunk(strm.sdesChunkLen)
	copy(chunk, nullArray[:]) // fill with zeros before using
	chunk.setSsrc(strm.ssrc)
	itemOffset := 4
	for itemType, name := range strm.SdesItems {
		itemOffset += chunk.setItemData(itemOffset, byte(itemType), name)
	}
	return
}

// SetSdesItem set a new SDES item or overwrites an existing one with new text (string).
// An application shall set at least a CNAME SDES item text otherwise w use a default string.
func (strm *SsrcStream) SetSdesItem(itemType int, itemText string) bool {
	if itemType <= SdesEnd || itemType >= sdesMax {
		return false
	}
	strm.SdesItems[itemType] = itemText
	length := 4 // Initialize with SSRC length
	for _, name := range strm.SdesItems {
		length += 2 + len(name) // add length of each item
	}
	if rem := length & 0x3; rem == 0 { // if already multiple of 4 add another 4 that holds "end" marker byte plus 3 bytes padding
		length += 4
	} else {
		length += 4 - rem
	}
	strm.sdesChunkLen = length
	return true
}

// makeByeData creates a by data block after the BYE RTCP header field.
// Currently only one SSRC for bye data supported. Additional CSRCs requiere addiitonal data structure in output stream.
func (strm *SsrcStream) makeByeData(rc *CtrlPacket, reason string) (newOffset int) {
	length := 4
	if len(reason) > 0 {
		length += (len(reason) + 3 + 1) & ^3 // plus one is the length field
	}
	bye, newOffset := rc.newByeData(length)
	bye.setSsrc(0, strm.ssrc)
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
func newSsrcStreamIn(from *Address, ssrc uint32) (strm *SsrcStream) {
	strm = new(SsrcStream)
	strm.streamType = InputStream
	strm.ssrc = ssrc
	strm.IPAddr = from.IPAddr
	strm.DataPort = from.DataPort
	strm.CtrlPort = from.CtrlPort
	strm.SdesItems = make(SdesItemMap, 2)
	strm.initStats()
	return
}

// checkSsrcIncomingData checks for collision or loops on incoming data packets.
// Implements th algorithm found in chap 8.2 in RFC 3550
func (strm *SsrcStream) checkSsrcIncomingData(existingStream bool, rs *Session, rp *DataPacket) (result bool) {
	result = true

	// Test if the source is new and its SSRC is not already used in an output stream.
	// Thus a new input stream without collision.
	if !existingStream && !rs.isOutputSsrc(strm.ssrc) {
		return result
	}

	// Found an existing input stream. Check if it is still same address/port.
	// if yes, no conflicts, no further checks required.
	if strm.DataPort != rp.fromAddr.DataPort || !strm.IPAddr.Equal(rp.fromAddr.IPAddr) {
		// SSRC collision or a loop has happened
		strOut, _, localSsrc := rs.lookupSsrcMapOut(strm.ssrc)
		if !localSsrc { // Not a SSRC in use for own output (local SSRC)
			// TODO: Optional error counter: Known SSRC stream changed address or port
			// Note this differs from the default in the RFC. Discard packet only when the collision is
			// repeating (to avoid flip-flopping)
			if strm.prevConflictAddr != nil &&
				strm.prevConflictAddr.IPAddr.Equal(rp.fromAddr.IPAddr) &&
				strm.prevConflictAddr.DataPort == rp.fromAddr.DataPort {
				result = false // discard packet and do not flip-flop
			} else {
				// Record who has collided so that in the future we can know if the collision repeats.
				strm.prevConflictAddr = &Address{rp.fromAddr.IPAddr, rp.fromAddr.DataPort, 0, rp.fromAddr.Zone}
				// Change sync source transport address
				strm.IPAddr = rp.fromAddr.IPAddr
				strm.DataPort = rp.fromAddr.DataPort
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
				strm.IPAddr = rp.fromAddr.IPAddr
				strm.DataPort = rp.fromAddr.DataPort
				strm.CtrlPort = 0
				strm.initStats()
			}
		}
	}
	return
}

// recordReceptionData checks validity (probation), sequence numbers, computes jitter, and records the statistics for incoming data packets.
// See algorithms in chapter A.1 (sequence number handling) and A.8 (jitter computation)
func (strm *SsrcStream) recordReceptionData(rp *DataPacket, rs *Session, recvTime int64) (result bool) {
	result = true

	seq := rp.Sequence()

	if strm.statistics.probation != 0 {
		// source is not yet valid.
		if seq == strm.statistics.maxSeqNum+1 {
			// packet in sequence.
			strm.statistics.probation--
			if strm.statistics.probation == 0 {
				strm.statistics.seqNumAccum = 0
			} else {
				result = false
			}
		} else {
			// packet not in sequence.
			strm.statistics.probation = minSequential - 1
			result = false
		}
		strm.statistics.maxSeqNum = seq
	} else {
		// source was already valid.
		step := seq - strm.statistics.maxSeqNum
		if step < maxDropout {
			// Ordered, with not too high step.
			if seq < strm.statistics.maxSeqNum {
				// sequene number wrapped.
				strm.statistics.seqNumAccum += seqNumMod
			}
			strm.statistics.maxSeqNum = seq
		} else if step <= (seqNumMod - maxMisorder) {
			// too high step of the sequence number.
			// TODO: check usage of baseSeqNum
			if uint32(seq) == strm.statistics.badSeqNum {
				// Here we saw two sequential packets - assume other side restarted, so just re-sync
				// and treat this packet as first packet
				strm.statistics.maxSeqNum = seq
				strm.statistics.baseSeqNum = seq
				strm.statistics.seqNumAccum = 0
				strm.statistics.badSeqNum = seqNumMod + 1
			} else {
				strm.statistics.badSeqNum = uint32((seq + 1) & (seqNumMod - 1))
				// This additional check avoids that the very first packet from a source be discarded.
				if strm.statistics.packetCount > 0 {
					result = false
				} else {
					strm.statistics.maxSeqNum = seq
				}
			}
		} // else {
		// duplicate or reordered packet
		// }
	}

	if result {
		strm.sequenceNumber = strm.statistics.maxSeqNum
		// the packet is considered valid.
		strm.statistics.packetCount++
		strm.statistics.octetCount += uint32(len(rp.Payload()))
		if strm.statistics.packetCount == 1 {
			strm.statistics.initialDataTimestamp = rp.Timestamp()
			strm.statistics.baseSeqNum = seq
		}
		strm.streamMutex.Lock()
		strm.statistics.lastPacketTime = recvTime
		if !strm.sender && rs.rtcpCtrlChan != nil {
			rs.rtcpCtrlChan <- rtcpIncrementSender
		}
		strm.sender = true // Stream is sender. If it was false new stream or no RTP packets for some time
		strm.dataAfterLastReport = true
		strm.streamMutex.Unlock()

		// compute the interarrival jitter estimation.
		pt := int(rp.PayloadType())
		// compute lastPacketTime to ms and clockrate as kHz
		arrival := uint32(strm.statistics.lastPacketTime / 1e6 * int64(PayloadFormatMap[pt].ClockRate/1e3))
		transitTime := arrival - rp.Timestamp()
		if strm.statistics.lastPacketTransitTime != 0 {
			delta := int32(transitTime - strm.statistics.lastPacketTransitTime)
			if delta < 0 {
				delta = -delta
			}
			strm.statistics.jitter += uint32(delta) - ((strm.statistics.jitter + 8) >> 4)
		}
		strm.statistics.lastPacketTransitTime = transitTime
	}
	return
}

// checkSsrcIncomingData checks for collision or loops on incoming data packets.
// Implements th algorithm found in chap 8.2 in RFC 3550
func (strm *SsrcStream) checkSsrcIncomingCtrl(existingStream bool, rs *Session, from *Address) (result bool) {
	result = true

	// Test if the source is new and its SSRC is not already used in an output stream.
	// Thus a new input stream without collision.
	if !existingStream && !rs.isOutputSsrc(strm.ssrc) {
		return result
	}
	// Found an existing input stream. Check if it is still same address/port.
	// if yes, no conflicts, no further checks required.
	if strm.CtrlPort != from.CtrlPort || !strm.IPAddr.Equal(from.IPAddr) {
		// SSRC collision or a loop has happened
		strOut, _, localSsrc := rs.lookupSsrcMapOut(strm.ssrc)
		if !localSsrc { // Not a SSRC in use for own output (local SSRC)
			// TODO: Optional error counter: Know SSRC stream changed address or port
			// Note this differs from the default in the RFC. Discard packet only when the collision is
			// repeating (to avoid flip-flopping)
			if strm.prevConflictAddr != nil &&
				strm.prevConflictAddr.IPAddr.Equal(from.IPAddr) &&
				strm.prevConflictAddr.CtrlPort == from.CtrlPort {
				result = false // discard packet and do not flip-flop
			} else {
				// Record who has collided so that in the future we can know if the collision repeats.
				strm.prevConflictAddr = &Address{from.IPAddr, 0, from.CtrlPort, from.Zone}
				// Change sync source transport address
				strm.IPAddr = from.IPAddr
				strm.CtrlPort = from.CtrlPort
			}
		} else {
			// Collision or loop of own packets. In this case strOut == strm.
			if rs.checkConflictCtrl(from) {
				// Optional error counter.
				result = false
			} else {
				// New collision, dispatch a BYE using old SSRC, renew the output stream's SSRC
				rs.WriteCtrl(rs.buildRtcpByePkt(strOut, "SSRC collision detected when receiving RTCP packet."))
				rs.replaceStream(strOut)
				strm.IPAddr = from.IPAddr
				strm.DataPort = 0
				strm.CtrlPort = from.CtrlPort
				strm.initStats()
			}
		}
	}
	return
}

// makeRecvReport fills a receiver report at the current inUse position and returns offset that points after the report.
// See chapter A.3 in RFC 3550 regarding the packet lost algorithm, end of chapter 6.4.1 regarding LSR, DLSR stuff.
//
func (strm *SsrcStream) makeRecvReport(rp *CtrlPacket) (newOffset int) {

	report, newOffset := rp.newRecvReport()

	extMaxSeq := strm.statistics.seqNumAccum + uint32(strm.statistics.maxSeqNum)
	expected := extMaxSeq - uint32(strm.statistics.baseSeqNum) + 1
	lost := expected - strm.statistics.packetCount
	if strm.statistics.packetCount == 0 {
		lost = 0
	}
	expectedDelta := expected - strm.statistics.expectedPrior
	strm.statistics.expectedPrior = expected

	receivedDelta := strm.statistics.packetCount - strm.statistics.receivedPrior
	strm.statistics.receivedPrior = strm.statistics.packetCount

	lostDelta := expectedDelta - receivedDelta

	var fracLost byte
	if expectedDelta != 0 && lostDelta > 0 {
		fracLost = byte((lostDelta << 8) / expectedDelta)
	}

	var lsr, dlsr uint32
	if strm.statistics.lastRtcpSrTime != 0 {
		sec, frac := toNtpStamp(strm.statistics.lastRtcpSrTime)
		ntp := (sec << 32) | frac
		lsr = ntp >> 16
		sec, frac = toNtpStamp(time.Now().UnixNano() - strm.statistics.lastRtcpSrTime)
		ntp = (sec << 32) | frac
		dlsr = ntp >> 16
	}

	report.setSsrc(strm.ssrc)
	report.setPacketsLost(lost)
	report.setPacketsLostFrac(fracLost)
	report.setHighestSeq(extMaxSeq)
	report.setJitter(strm.statistics.jitter >> 4)
	report.setLsr(lsr)
	report.setDlsr(dlsr)

	return
}

func (strm *SsrcStream) readSenderInfo(info senderInfo) {
	seconds, fraction := info.ntpTimeStamp()
	strm.NtpTime = fromNtp(seconds, fraction)
	strm.RtpTimestamp = info.rtpTimeStamp()
	strm.SenderPacketCnt = info.packetCount()
	strm.SenderOctectCnt = info.octetCount()
}

// goodBye marks this source as having sent a BYE control packet.
func (strm *SsrcStream) goodbye() bool {
	if !strm.statistics.flag {
		return false
	}
	strm.statistics.flag = false
	return true
}

// hello marks this source as having sent some packet.
func (strm *SsrcStream) hello() bool {
	if strm.statistics.flag {
		return false
	}
	strm.statistics.flag = true
	return true
}

// initStats initializes all RTCP statistic counters and other relevant data.
func (strm *SsrcStream) initStats() {
	strm.statistics.lastPacketTime = 0
	strm.statistics.lastRtcpPacketTime = 0
	strm.statistics.lastRtcpSrTime = 0

	strm.statistics.packetCount = 0
	strm.statistics.octetCount = 0
	strm.statistics.maxSeqNum = 0
	strm.statistics.extendedMaxSeqNum = 0
	strm.statistics.cumulativePacketLost = 0
	strm.statistics.fractionLost = 0
	strm.statistics.jitter = 0
	strm.statistics.initialDataTimestamp = 0
	strm.statistics.initialDataTime = 0
	strm.statistics.flag = false

	strm.statistics.badSeqNum = seqNumMod + 1
	strm.statistics.probation = minSequential
	strm.statistics.baseSeqNum = 0
	strm.statistics.expectedPrior = 0
	strm.statistics.receivedPrior = 0
	strm.statistics.seqNumAccum = 0
}

func (strm *SsrcStream) parseSdesChunk(sc sdesChunk) {
	offset := 4 // points after SSRC field of this chunk

	for {
		itemType := sc.getItemType(offset)
		if itemType <= SdesEnd || itemType >= sdesMax {
			return
		}

		txtLen := sc.getItemLen(offset)
		itemTxt := sc.getItemText(offset, txtLen)
		offset += 2 + txtLen
		if name, ok := strm.SdesItems[itemType]; ok && name == itemTxt {
			continue
		}
		txt := make([]byte, len(itemTxt))
		copy(txt, itemTxt)
		strm.SdesItems[itemType] = string(txt)
	}
}
