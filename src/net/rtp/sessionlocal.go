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

/*
 * This source file contains the local types, constants, methods and functions for the Session type
 */

import (
    "crypto/rand"
    "time"
)

const (
    dataReceiveChanLen = 3
    ctrlEventChanLen   = 3
)

const (
    maxNumberOutStreams = 5
    maxNumberInStreams  = 30
)
// conflictAddr stores conflicting address detected during loop and collision check
// It also stores the time of the latest conflict-
type conflictAddr struct {
    Address
    seenAt int64
}

// The RTCP control commands a a simple uint32: the MSB defines the command, the lower
// 3 bytes the value for the command, if a value is necssary for the command.
//
const (
    rtcpCtrlCmdMask     = 0xff000000
    rtcpStopService     = 0x01000000
    rtcpModifyInterval  = 0x02000000 // Modify RTCP timer interval, low 3 bytes contain new tick time in ms
    rtcpIncrementSender = 0x03000000 // a stream became an active sender, count this globally   
)

// rtcpCtrlChan sends control data to the RTCP service.
// USe this channel to send a stop service command or a modify time command to the
// rtcpService. With this technique the rtcpService can modify its time between
// service runs without being stopped.
//
type rtcpCtrlChan chan uint32

// Manages the output SSRC streams. 
// 
// Refer to RFC 3550: do not use SSRC to multiplex different media types on one session. One RTP session
// shall handle one media type only. However, a RTP session can have several SSRC output streams for the
// same media types, for example sending video data from two or more cameras.
// The output streams are identified with our "own" SSRCs, thus a RTP session may have several "own" SSRCs.
type streamOutMap map[uint32]*SsrcStream

// Manages the input SSRC streams.
type streamInMap map[uint32]*SsrcStream

// The remote peers.
type remoteMap map[uint32]*Address

type conflictMap map[uint32]*conflictAddr


// rtcpService provides the RTCP service and sends RTCP reports at computed intervals.
//
func (rs *Session) rtcpService(ti, td int64) {

    granularity := time.Duration(250e6) // 250 ms
    ssrcTimeout := 5 * td
    dataTimeout := 2 * ti

    rs.rtcpServiceActive = true
    ticker := time.NewTicker(granularity)
    var cmd uint32
    for cmd != rtcpStopService {
        select {
        case <-ticker.C:
            now := time.Now().UnixNano()
            if now < rs.tnext {
                continue
            }

            var outActive, inActive int // Counts all members in active state
            var inActiveSinceLastRR int

            for idx, str := range rs.streamsIn {
                switch str.streamStatus {
                case active:
                    str.streamMutex.Lock()
                    // Manage number of active senders on input streams. 
                    // Every time this stream receives a packet it updates the last packet time. If the input stream
                    // did not receive a RTP packet for 2 RTCP intervals its sender status is set to false and the 
                    // number of active senders in this session is decremented if not already zero. See chapter 6.3.5
                    rtpDiff := now - str.statistics.lastPacketTime
                    if str.sender {
                        if str.dataAfterLastReport {
                            inActiveSinceLastRR++
                        }
                        if rtpDiff > dataTimeout {
                            str.sender = false
                            if rs.activeSenders > 0 {
                                rs.activeSenders--
                            }
                        }
                    }
                    // SSRC timeout processing: check for inactivity longer than 5*non-random interval time 
                    // (both RTP/RTCP inactivity) chapter 6.3.5
                    rtcpDiff := now - str.statistics.lastRtcpPacketTime
                    if rtpDiff > rtcpDiff {
                        rtpDiff = rtcpDiff
                    }
                    if rtpDiff > ssrcTimeout {
                        delete(rs.streamsIn, idx)
                    }
                    str.streamMutex.Unlock()

                case isClosing:
                    str.streamStatus = isClosed

                case isClosed:
                    delete(rs.streamsOut, idx)
                }
            }

            var rc *CtrlPacket
            var streamForRR *SsrcStream
            var outputSenders int

            for idx, str := range rs.streamsOut {
                switch str.streamStatus {
                case active:
                    outActive++
                    streamForRR = str // remember one active stream in case there is no sending output stream

                    // Manage number of active senders. Every time this stream sends a packet the output stream
                    // sender updates the last packet time. If the output stream did not send RTP for 2 RTCP 
                    // intervals its sender status is set to false and the number of active senders in this session
                    // is decremented if not already zero. See chapter 6.3.8
                    //
                    str.streamMutex.Lock()
                    rtpDiff := now - str.statistics.lastPacketTime
                    if str.sender {
                        outputSenders++
                        if rtpDiff > dataTimeout {
                            str.sender = false
                            outputSenders--
                            if rs.activeSenders > 0 {
                                rs.activeSenders--
                            }
                        }
                    }
                    str.streamMutex.Unlock()
                    if str.sender {
                        if rc == nil {
                            rc = rs.buildRtcpPkt(str, inActiveSinceLastRR)
                        } else {
                            rs.addSenderReport(str, rc)
                        }
                    }

                case isClosing:
                    str.streamStatus = isClosed

                case isClosed:
                    delete(rs.streamsOut, idx)
                }
            }
            // If no active output stream is left then weSent becomes false
            rs.weSent = outputSenders > 0

            // if rc is nil then we found no sending stream and havent't build a control packet. Just use 
            // one active output stream as proxy to create at least an RR and the proxy's SDES (RR may be 
            // empty as well). If also no active output stream - don't create and send RTCP report. In this
            // case the RTP stack in completey inactive.
            if rc == nil && streamForRR != nil {
                rc = rs.buildRtcpPkt(streamForRR, inActiveSinceLastRR)
            }
            if rc != nil {
                rs.WriteCtrl(rc)
                rs.tprev = now
                size := float64(rc.InUse() + 20 + 8) // TODO: get real values for IP and transport from transport module
                rs.avrgPacketLength = (1.0/16.0)*size + (15.0/16.0)*rs.avrgPacketLength

                ti, td := rtcpInterval(outActive+inActive, int(rs.activeSenders), rs.RtcpSessionBandwidth,
                    rs.avrgPacketLength, rs.weSent, false)
                rs.tnext = ti + now
                dataTimeout = 2 * ti
                ssrcTimeout = 5 * td
                rc.FreePacket()
            }
            outActive = 0
            inActive = 0

        case cmd = <-rs.rtcpCtrlChan:
            switch cmd & rtcpCtrlCmdMask {
            case rtcpStopService:
                ticker.Stop()

            case rtcpModifyInterval:
                ticker.Stop()
                granularity = time.Duration(cmd &^ rtcpCtrlCmdMask)
                ticker = time.NewTicker(granularity)

            case rtcpIncrementSender:
                rs.activeSenders++
            }
        }
    }
    rs.rtcpServiceActive = false
}

// buildRtcpPkt creates an RTCP compound and fills it with a SR or RR packet.
//
// This method loops over the known input streams and fills in receiver reports.
// the method adds a maximum of 31 receiver reports. The SR and/or RRs and the SDES
// of the output stream always fit in the RTCP compund, thus no further checks required.
//
// Other output streams just add their sender reports and SDES info.
//
func (rs *Session) buildRtcpPkt(strOut *SsrcStream, inStreamCnt int) (rc *CtrlPacket) {

    var pktLen, offset int
    if strOut.sender {
        rc, offset = strOut.newCtrlPacket(RtcpSR)
        offset = rc.addHeaderSsrc(offset, strOut.Ssrc())

        var info senderInfo
        info, offset = rc.newSenderInfo()
        strOut.fillSenderInfo(info) // create a sender info block after fixed header and SSRC.
    } else {
        rc, offset = strOut.newCtrlPacket(RtcpRR)
        offset = rc.addHeaderSsrc(offset, strOut.Ssrc())
    }
    pktLen = offset/4 - 1
    // TODO Handle round-robin if we have more then 31 really active input streams (chap 6.4)
    if inStreamCnt >= 31 {
        inStreamCnt = 31
    }
    var rrCnt int
    if inStreamCnt > 0 {
        for _, strIn := range rs.streamsIn {
            if strIn.dataAfterLastReport {
                strIn.dataAfterLastReport = false
                strIn.makeRecvReport(rc)
                pktLen += reportBlockLen / 4 // increment SR/RR to include length of this recv report block
                rrCnt++
                if inStreamCnt--; inStreamCnt <= 0 {
                    break
                }
            }
        }
    }
    rc.SetLength(0, uint16(pktLen)) // length of first RTCP packet in compound: fixed header, 0 or 1 SR, n*RR
    rc.SetCount(0, rrCnt)

    rs.addSdes(strOut, rc)

    return
}

// buildRtcpByePkt builds an RTCP BYE compound.
//
func (rs *Session) buildRtcpByePkt(strOut *SsrcStream, reason string) (rc *CtrlPacket) {
    rc = rs.buildRtcpPkt(strOut, 0)

    headerOffset := rc.InUse()
    strOut.addCtrlHeader(rc, headerOffset, RtcpBye)
    // Here we may add a loop over CSRC (addtional data in ouput steam) and hand over to makeByeData
    offset := strOut.makeByeData(rc, reason)
    rc.SetCount(headerOffset, 1)                                  // currently one BYE SSRC/CSRC per packet
    rc.SetLength(headerOffset, uint16((offset-headerOffset)/4-1)) // length of BYE packet in compound: fixed header plus BYE data
    return
}

// addSenderReport appends a SDES packet into to control packet.
//
func (rs *Session) addSdes(strOut *SsrcStream, rc *CtrlPacket) {
    offsetSdes := rc.InUse()
    if strOut.sdesChunkLen > 0 {
        strOut.addCtrlHeader(rc, offsetSdes, RtcpSdes) // Add a RTCP SDES packet header after the SR/RR packet
        // makeSdesChunk returns position where to append next chunk - for CSRCs that contribute to this, chap 6.5, RFC 3550
        // CSRCs currently not supported, need additional data structures in output stream.
        nextChunk := strOut.makeSdesChunk(rc)
        rc.SetCount(offsetSdes, 1)                                   // currently one SDES chunk per SDES packet
        rc.SetLength(offsetSdes, uint16((nextChunk-offsetSdes)/4-1)) // length of SDES packet in compound: fixed header plus SDES chunk len
    }
}

// addSenderReport appends a sender report into to control packet if the output stream is a sender.
//
// The method just adds the sender report for the output stream. It does not loop over the
// input streams to fill in the sreceiver reports. Only one output stream's sender report
// contains receiver reports of our input streams.
//
func (rs *Session) addSenderReport(strOut *SsrcStream, rc *CtrlPacket) {

    if !strOut.sender {
        return
    }

    headerOffset := rc.InUse()
    if headerOffset+strOut.sdesChunkLen+8 > len(rc.Buffer()) {
        return
    }
    offset := strOut.addCtrlHeader(rc, headerOffset, RtcpSR)
    rc.addHeaderSsrc(offset, strOut.Ssrc())

    var info senderInfo
    info, offset = rc.newSenderInfo()
    strOut.fillSenderInfo(info) // create a sender info block after fixed header and SSRC.

    pktLen := (offset-headerOffset)/4 - 1
    rc.SetLength(headerOffset, uint16(pktLen)) // length of RTCP packet in compound: fixed header, SR, 0*RR
    rc.SetCount(headerOffset, 0)               // zero receiver reports in this SR

    rs.addSdes(strOut, rc)
}

// rtcpSenderCheck is a helper function for OnRecvCtrl and checks if a sender's SSRC.
// 
func (rs *Session) rtcpSenderCheck(rp *CtrlPacket, offset int) (*SsrcStream, uint32, bool) {
    ssrc := rp.Ssrc(offset) // get SSRC from control packet

    rs.streamsMapMutex.Lock()
    str, strIdx, existing := rs.lookupSsrcMap(ssrc)

    // if not found in the input stream then create a new SSRC input stream
    if !existing {
        if len(rs.streamsIn) > rs.MaxNumberInStreams {
            rs.streamsMapMutex.Unlock()
            return nil, MaxNumInStreamReachedCtrl, false
        }
        str = newSsrcStreamIn(&rp.fromAddr, ssrc)
        str.streamStatus = active
        rs.streamsIn[rs.streamInIndex] = str
        rs.streamInIndex++
    } else {
        // Check if an existing stream is active
        if str.streamStatus != active {
            rs.streamsMapMutex.Unlock()
            return nil, WrongStreamStatusCtrl, false
        }
        // Test if RTP packets had been received but this is the first control packet from this source.
        if str.CtrlPort == 0 {
            str.CtrlPort = rp.fromAddr.CtrlPort
        }
    }
    rs.streamsMapMutex.Unlock()

    // Check if sender's SSRC collides or loops 
    if !str.checkSsrcIncomingCtrl(existing, rs, &rp.fromAddr) {
        return nil, StreamCollisionLoopCtrl, false
    }
    // record reception time
    str.statistics.lastRtcpPacketTime = time.Now().UnixNano()
    return str, strIdx, existing
}

// sendDataCtrlEvent is a helper function to OnRecvData and sends one control event to the application 
// if the control event chanel is active.
//
func (rs *Session) sendDataCtrlEvent(code int, ssrc, index uint32) {
    var ctrlEvArr [1]*CtrlEvent
    ctrlEvArr[0] = newCrtlEvent(code, ssrc, index)

    if ctrlEvArr[0] != nil {
        select {
        case rs.ctrlEventChan <- ctrlEvArr[:]: // send control event
        default:
        }
    }
}

// lookupSsrcMap returns a SsrcStream, either a SsrcStreamIn or SsrcStreamOut for a given SSRC, nil and false if none found.
//
func (rs *Session) lookupSsrcMap(ssrc uint32) (str *SsrcStream, idx uint32, exists bool) {
    if str, idx, exists = rs.lookupSsrcMapOut(ssrc); exists {
        return
    }
    if str, idx, exists = rs.lookupSsrcMapIn(ssrc); exists {
        return
    }
    return nil, 0, false
}

// lookupSsrcMapIn returns a SsrcStreamIn for a given SSRC, nil and false if none found.
//
func (rs *Session) lookupSsrcMapIn(ssrc uint32) (*SsrcStream, uint32, bool) {
    for idx, str := range rs.streamsIn {
        if ssrc == str.ssrc {
            return str, idx, true
        }
    }
    return nil, 0, false
}

// lookupSsrcMapOut returns a SsrcStreamOut for a given SSRC, nil and false if none found.
//
func (rs *Session) lookupSsrcMapOut(ssrc uint32) (*SsrcStream, uint32, bool) {
    for idx, str := range rs.streamsOut {
        if ssrc == str.ssrc {
            return str, idx, true
        }
    }
    return nil, 0, false
}

// isOutputSsrc checks if a given SSRC is already used in our output streams.
// Use this functions to detect collisions.
//
func (rs *Session) isOutputSsrc(ssrc uint32) (found bool) {
    var str *SsrcStream
    for _, str = range rs.streamsOut {
        if ssrc == str.ssrc {
            found = true
            break
        }
    }
    return
}

// checkConflictData checks and manages entries of conflicting data addresses.
// If an address/port pair is already recorded just update the time and return
// the entry and true.
//
// If an entry was not found then create an entry, populate it and return entry
// and false.
//
func (rs *Session) checkConflictData(addr *Address) (found bool) {
    var entry *conflictAddr
    tm := time.Now().UnixNano()

    for _, entry = range rs.conflicts {
        if addr.IpAddr.Equal(entry.IpAddr) && addr.DataPort == entry.DataPort {
            found = true
            entry.seenAt = tm
            return
        }
    }
    entry = new(conflictAddr)
    entry.IpAddr = addr.IpAddr
    entry.DataPort = addr.DataPort
    entry.seenAt = tm
    rs.conflicts[rs.conflictIndex] = entry
    rs.conflictIndex++
    found = false
    return
}

// checkConflictData checks and manages entries of conflicting data addresses.
// If an address/port pair is already recorded just update the time and return
// the entry and true.
//
// If an entry was not found then create an entry, populate it and return entry
// and false.
//
func (rs *Session) checkConflictCtrl(addr *Address) (found bool) {
    var entry *conflictAddr
    tm := time.Now().UnixNano()

    for _, entry = range rs.conflicts {
        if addr.IpAddr.Equal(entry.IpAddr) && addr.CtrlPort == entry.CtrlPort {
            found = true
            entry.seenAt = tm
            return
        }
    }
    entry = new(conflictAddr)
    entry.IpAddr = addr.IpAddr
    entry.CtrlPort = addr.CtrlPort
    entry.seenAt = tm
    rs.conflicts[rs.conflictIndex] = entry
    rs.conflictIndex++
    found = false
    return
}

// processSdesChunk checks if the chunk's SSRC is already known and if yes, parse it.
// The method returns the length of the chunk .
//
func (rs *Session) processSdesChunk(chunk sdesChunk, rp *CtrlPacket) (int, uint32, bool) {
    chunkLen, ok := chunk.chunkLen()
    if !ok {
        return 0, 0, false
    }
    strIn, idx, existing := rs.lookupSsrcMapIn(chunk.ssrc())
    if !existing {
        return chunkLen, idx, true
    }
    strIn.parseSdesChunk(chunk)
    return chunkLen, idx, true
}

// replaceStream creates a new output stream, initializes it from the old output stream and replaces the old output stream. 
//
// The old output stream will then become an input streamm - this handling is called if we have a conflict during
// collision, loop detection (see algorithm in chap 8.2, RFC 3550).
//
func (rs *Session) replaceStream(oldOut *SsrcStream) (newOut *SsrcStream) {
    var str *SsrcStream
    var idx uint32
    for idx, str = range rs.streamsOut {
        if oldOut.ssrc == str.ssrc {
            break
        }
    }
    // get new stream and copy over attributes from old stream
    newOut = newSsrcStreamOut(&Address{oldOut.IpAddr, oldOut.DataPort, oldOut.CtrlPort, oldOut.Zone}, 0, 0)

    for itemType, itemTxt := range oldOut.SdesItems {
        newOut.SetSdesItem(itemType, itemTxt)
    }
    newOut.SetPayloadType(oldOut.PayloadType())
    newOut.sender = oldOut.sender

    // Now lock and re-shuffle the streams
    rs.streamsMapMutex.Lock()
    defer rs.streamsMapMutex.Unlock()

    // Don't reuse an existing SSRC
    for _, _, exists := rs.lookupSsrcMap(newOut.Ssrc()); exists; _, _, exists = rs.lookupSsrcMap(newOut.Ssrc()) {
        newOut.newSsrc()
    }
    newOut.streamType = OutputStream
    rs.streamsOut[idx] = newOut // replace the oldOut with a new initialized out, new SSRC, sequence but old address

    // sanity check - this is a panic, something stange happened
    for idx, str = range rs.streamsIn {
        if oldOut.ssrc == str.ssrc {
            panic("Panic: found input stream during collision handling - expected none")
            return
        }
    }
    oldOut.streamType = InputStream
    rs.streamsIn[rs.streamInIndex] = oldOut
    rs.streamInIndex++
    return
}

// The following constants were taken from RFC 3550, chapters 6.3.1 and A.7
const (
    rtcpMinimumTime    = 5.0
    rtcpSenderFraction = 0.25
    rtcpRecvFraction   = 1.0 - rtcpSenderFraction
    compensation       = 2.71828 - 1.5
)

// rtcpInterval helper function computes the next time when to send an RTCP packet.
//
// The algorithm is copied from RFC 2550, A.7 and a little bit adapted to Go. This includes some important comments :-) .
//
func rtcpInterval(members, senders int, rtcpBw, avrgSize float64, weSent, initial bool) (int64, int64) {

    rtcpMinTime := rtcpMinimumTime
    if initial {
        rtcpMinTime /= 2
    }

    /*
     * Dedicate a fraction of the RTCP bandwidth to senders unless
     * the number of senders is large enough that their share is
     * more than that fraction.
     */
    n := members
    if senders <= int((float64(members) * rtcpSenderFraction)) {
        if weSent {
            rtcpBw *= rtcpSenderFraction
            n = senders
        } else {
            rtcpBw *= rtcpRecvFraction
            n -= senders
        }
    }
    /*
     * The effective number of sites times the average packet size is
     * the total number of octets sent when each site sends a report.
     * Dividing this by the effective bandwidth gives the time
     * interval over which those packets must be sent in order to
     * meet the bandwidth target, with a minimum enforced.  In that
     * time interval we send one report so this time is also our
     * average time between reports.
     */
    t := avrgSize * float64(n) / rtcpBw
    if t < rtcpMinTime {
        t = rtcpMinTime
    }
    td := int64(t * 1e9) // determinisitic interval, see chap 6.3.1, 6.3.5
    var randBuf [2]byte
    rand.Read(randBuf[:])
    randNo := uint16(randBuf[0])
    randNo |= uint16(randBuf[1]) << 8
    randFloat := float64(randNo)/65536.0 + 0.5

    t *= randFloat
    t /= compensation
    // return as nanoseconds
    return int64(t * 1e9), td
}

// newCrtlEvent is a little helper function to create and initialize a new control event.
func newCrtlEvent(eventType int, ssrc, idx uint32) (ctrlEv *CtrlEvent) {
    ctrlEv = new(CtrlEvent)
    ctrlEv.EventType = eventType
    ctrlEv.Ssrc = ssrc
    ctrlEv.Index = idx
    return
}

// Number of seconds ellapsed from 1900 to 1970, see RFC 5905
const ntpEpochOffset = 2208988800

// toNtpStamp converts a GO time into the NTP format according to RFC 5905
func toNtpStamp(tm int64) (seconds, fraction uint32) {
    seconds = uint32(tm/1e9 + ntpEpochOffset) // Go uses ns, thus divide by 1e9 to get seconds
    fraction = uint32(((tm % 1e9) << 32) / 1e9)
    return
}

// fromNtp converts a NTP timestamp into GO time 
func fromNtp(seconds, fraction uint32) (tm int64) {
    n := (int64(fraction) * 1e9) >> 32
    tm = (int64(seconds)-ntpEpochOffset)*1e9 + n
    return
}
