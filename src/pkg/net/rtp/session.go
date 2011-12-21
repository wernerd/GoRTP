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
    "net"
    "sync"
    "time"
)

// Session contols and manages the resources and actions of a RTP session.
//
type Session struct {
    streamsMapMutex sync.Mutex // synchronize activities on stream maps 
    streamsOut      streamOutMap
    streamsIn       streamInMap
    remotes         remoteMap
    conflicts       conflictMap
    activeSenders,
    streamOutIndex,
    streamInIndex,
    remoteIndex,
    conflictIndex uint32
    dataReceiveChan   DataReceiveChan
    ctrlEventChan     CtrlEventChan
    rtcpCtrlChan      rtcpCtrlChan
    transportEnd      TransportEnd
    transportEndUpper TransportEnd
    transportWrite    TransportWrite
    transportRecv     TransportRecv
}

// Remote stores a remote addess in a transport independent way.
//
// The transport implementations construct UDP or TCP addresses and use them to send the data.
type Address struct {
    IpAddr             net.IP
    DataPort, CtrlPort int
}

// Specific control event type that signal that a new input stream was created.
// 
// If the RTP stack receives a data or control packet for a yet unknown input stream
// (SSRC not known) the stack creates a new input stream and signals this action to the application.
const (
    NewStreamData = iota // Input stream creation triggered by a RTP data packet
    NewStreamCtrl        // Input stream creation triggered by a RTP data packet
)

// The RTP stack sends CtrlEvent to the application if it creates a new input stream or receives RTCP packets.
//
// A RTCP compound may contain several RTCP packets. The RTP stack creates a CtrlEvent structure for each RTCP 
// packet (SDES, BYE, etc) or report and stores them in a slice of CtrlEvent pointers and sends 
// this slice to the application after all RTCP packets and reports are processed. The application may now loop
// over the slice and select the events that it may process.
//   
type CtrlEvent struct {
    EventType int // Either a NewStream* or a Rtcp* packet types, e.g. RtcpSR, RtcpRR, RtcpSdes, RtcpBye
    Ssrc,     // the input stream's SSRC
    Index uint32 // and its index
    ByeReason string // Only for the RtcpBye packet: the resaon string if it was available, empty otherwise
}
// Use a channel to signal if the transports are really closed.
type TransportEnd chan int

// Use a channel to send RTP data packets to the upper layer application.
type DataReceiveChan chan *DataPacket

// Use a channel to send RTCP control events to the upper layer application.
type CtrlEventChan chan []*CtrlEvent

const (
    dataTransportRecvStopped = 0x1
    ctrlTransportRecvStopped = 0x2
)

const (
    dataReceiveChanLen = 3
    ctrlEventChanLen   = 3
)

// conflictAddr stores conflicting address detected during loop and collision check
// It also stores the time of the latest conflict-
type conflictAddr struct {
    Address
    seenAt int64
}

type rtpError string

func (s rtpError) Error() string {
    return string(s)
}

// The RTCP control commands a a simple uint32: the MSB defines the command, the lower
// 3 bytes the value for the command, if a value is necssary for the command.
//
const (
    rtcpCtrlCmdMask       = 0xff000000
    rtcpStopService       = 0x01000000
    rtcpModifyInterval    = 0x02000000 // Modify RTCP timer interval, low 3 bytes contain new tick time in ms
    rtcpModifySsrcTimeout = 0x03000000 // Modify SSRC timeout value, see chapter 6.3.5   
    rtcpIncrementSender   = 0x05000000 // stream processing detected a new/re-activated input stream (RTP data received)   
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

// Global Session functions.

// NewSession creates a new RTP session.
//
// A RTP session requires two transports:
//   tpw - a transport that implements the RtpTransportWrite interface
//   tpr - a transport that implements the RtpTransportRecv interface
//
func NewSession(tpw TransportWrite, tpr TransportRecv) *Session {
    rs := new(Session)
    rs.streamsOut = make(streamOutMap, 2)
    rs.streamsIn = make(streamInMap, 2)
    rs.remotes = make(remoteMap, 2)
    rs.conflicts = make(conflictMap, 2)
    rs.transportWrite = tpw
    rs.transportRecv = tpr
    rs.transportEnd = make(TransportEnd, 2)
    rs.rtcpCtrlChan = make(rtcpCtrlChan, 1)
    tpr.SetCallUpper(rs)
    tpr.setEndChannel(rs.transportEnd)

    return rs
}

// AddRemote adds the address and RTP port number of an additional remote peer.
//
// The port number must be even. The socket with the even port number sends and receives
// RTP packets. The socket with next odd port number sends and receives RTCP packets.
//
//   remote - the RTP address of the remote peer. The RTP data port number must be even.
//
func (rs *Session) AddRemote(remote *Address) (index uint32, err error) {
    if (remote.DataPort & 0x1) == 0x1 {
        return 0, rtpError("RTP port number is not an even number")
    }
    rs.remotes[rs.remoteIndex] = remote
    index = rs.remoteIndex
    rs.remoteIndex++
    return
}

// RemoveRemote removes the address at the specified index.
//
func (rs *Session) RemoveRemote(index uint32) {
    delete(rs.remotes, index)
}

// NewOutputStream creates a new RTP output stream and returns its index.
//
// A RTP session may have several output streams. The first output stream (stream with index 0)
// is the standard output stream. To use other outpout streams the application must us the
// the "*ForStream" methods and specifiy the correct indices of the stream.
//
// The index does not change for the lifetime of the stream and will not be reused during the lifeime of this session.
// (up to 2^64 streams per session :-) )
//
//   own  -       the output stream's own address. Required to detect collisions and loops
//   ssrc -       if not zero theen this is the SSRC of the output stream. If zero then 
//                the method generates a random SSRC according to RFC 3550.
//   sequenceNo - if not zero then this is the starting sequence number of the output stream. 
//                If zero then the method generates a random starting sequence number according 
//                to RFC 3550
//
func (rs *Session) NewSsrcStreamOut(own *Address, ssrc uint32, sequenceNo uint16) (index uint32) {
    str := newSsrcStreamOut(own, ssrc, sequenceNo)
    str.streamStatus = active

    // Synchronize - may be called from several Go applicatio functions in parallel
    rs.streamsMapMutex.Lock()
    defer rs.streamsMapMutex.Unlock()

    // Don't reuse an existing SSRC
    for _, _, exists := rs.lookupSsrcMap(str.Ssrc()); exists; _, _, exists = rs.lookupSsrcMap(str.Ssrc()) {
        str.newSsrc()
    }
    rs.streamsOut[rs.streamOutIndex] = str
    index = rs.streamOutIndex
    rs.streamOutIndex++
    return
}

// StartSession activates the transport, sends a first RTCP packet to introduce the output streams.
//
// An application must have created an output stream that the session can use to send RTCP data. This
// is true even if the application is in "listening" mode only. An application must send receiver
// reports to it's remote peers.
//
func (rs *Session) StartSession() (err error) {
    err = rs.ListenOnTransports() // activate the transports
    go rs.rtcpService()
    return
}

// CloseSession closes the complete RTP session immediately.
//
// The methods stops the RTCP service, sends a BYE to all remaining active output streams, and 
// closes the receiver transports,
//
func (rs *Session) CloseSession() {
    rs.rtcpCtrlChan <- rtcpStopService
    for idx := range rs.streamsOut {
        rs.SsrcStreamCloseForIndex(idx)
    }
    rs.CloseRecv() // de-activate the transports
    return
}

// NewDataPacket creates a new RTP packet suitable for use with the standard output stream.
//
// This method returns an initialized RTP packet that contains the correct SSRC, sequence
// number, the updated timestamp, and payload type if payload type was set in the stream.
//
// stamp is the next higher RTP timestamp for this packet. The application computes this
// based on the payload's frequency. For example PCMU with a 8000Hz frequency sends 160
// values every 20m - thus the timestamp must adavance by 160 for every sequential packet.
//
func (rs *Session) NewDataPacket(stamp uint32) *DataPacket {
    str := rs.streamsOut[0]
    return str.NewDataPacket(stamp)
}

// NewDataPacketForStream creates a new RTP packet suitable for use with the specified output stream.
//
// This method returns an initialized RTP packet that contains the correct SSRC, sequence
// number, and payload type if payload type was set in the stream. 
//
func (rs *Session) NewDataPacketForStream(streamIndex uint32, stamp uint32) *DataPacket {
    str := rs.streamsOut[streamIndex]
    return str.NewDataPacket(stamp)
}

// CreateDataReceivedChan creates the data received channel and returns it to the caller.
//
// An application shall listen on this channel to get received RTP data packets.
// If the channel is full then the RTP receiver discards the data packets.
//
func (rs *Session) CreateDataReceiveChan() DataReceiveChan {
    rs.dataReceiveChan = make(DataReceiveChan, dataReceiveChanLen)
    return rs.dataReceiveChan
}

// RemoveDataReceivedChan deletes the data received channel.
//
// The receiver discards all received packets.
//
func (rs *Session) RemoveDataReceiveChan() {
    rs.dataReceiveChan = nil
}

// CreateCtrlEventChan creates the control event channel and returns it to the caller.
//
// An application shall listen on this channel to get control events.
// If the channel is full then the RTCP receiver does not send control events.
//
func (rs *Session) CreateCtrlEventChan() CtrlEventChan {
    rs.ctrlEventChan = make(CtrlEventChan, ctrlEventChanLen)
    return rs.ctrlEventChan
}

// RemoveCtrlEventChan deletes the control event channel.
//
func (rs *Session) RemoveCtrlEventChan() {
    rs.ctrlEventChan = nil
}

// SsrcStreamOut gets the standard output stream.
//
func (rs *Session) SsrcStreamOut() *SsrcStream {
    return rs.streamsOut[0]
}

// SsrcStreamOut gets the output stream at streamIndex.
//
func (rs *Session) SsrcStreamOutForIndex(streamIndex uint32) *SsrcStream {
    return rs.streamsOut[streamIndex]
}

// SsrcStreamIn gets the standard input stream.
//
func (rs *Session) SsrcStreamIn() *SsrcStream {
    return rs.streamsIn[0]
}

// SsrcStreamInForIndex Get the input stream with index.
//
func (rs *Session) SsrcStreamInForIndex(streamIndex uint32) *SsrcStream {
    return rs.streamsIn[streamIndex]
}

// SsrcStreamClose sends a RTCP BYE to the standard output stream (index 0).
//
// The method does not close the stream immediately but marks it as 'is closing'.
// In this state the stream stops its activities, does not send any new data or
// control packets. Eventually it will be in the state "is closed" and its resources
// are returned to the system. An application must not re-use a session.
// 
func (rs *Session) SsrcStreamClose() {
    rs.SsrcStreamOutForIndex(0)
}

// SsrcStreamCloseForIndex sends a RTCP BYE to the stream at index index.
//
// See description for SsrcStreamClose above.
//
func (rs *Session) SsrcStreamCloseForIndex(streamIndex uint32) {
    str := rs.streamsOut[streamIndex]
    rc := rs.buildRtcpByePkt(str, "Go RTP says good-bye")
    rs.WriteCtrl(rc)

    str.streamStatus = isClosing
}

/*
 *** The following methods implement the rtp.RtpTransportRecv interface.
 */

// SetCallUpper implements the rtp.RtpTransportRecv SetCallUpper method.
//
func (rs *Session) SetCallUpper(upper TransportRecv) {
}

// ListenOnTransports implements the rtp.RtpTransportRecv ListenOnTransports method.
//
// The session just forwards this to the appropriate transport receiver.
//
func (rs *Session) ListenOnTransports() (err error) {
    return rs.transportRecv.ListenOnTransports()
}

func (rs *Session) OnRecvData(rp *DataPacket) bool {

    if !rp.IsValid() {
        rp.FreePacket()
        return false
    }

    ssrc := rp.Ssrc()

    tm := time.Now().UnixNano()

    rs.streamsMapMutex.Lock()
    str, _, existing := rs.lookupSsrcMap(ssrc)

    // if not found in the input stream then create a new SSRC input stream
    if !existing {
        str = newSsrcStreamIn(&rp.fromAddr, ssrc)
        rs.streamsIn[rs.streamInIndex] = str
        rs.streamInIndex++
        str.streamStatus = active
        str.statistics.initialDataTime = tm // First packet arrival time.

        var ctrlEvArr [1]*CtrlEvent
        ctrlEvArr[0] = newCrtlEvent(NewStreamData, ssrc, rs.streamInIndex-1)
        select {
        case rs.ctrlEventChan <- ctrlEvArr[:]: // send control event
        default:
        }
    } else {
        // Check if an existing stream is of type input stream and is active
        if str.streamStatus != active {
            rp.FreePacket()
            rs.streamsMapMutex.Unlock()
            return false
        }
        // Test if RTCP packets had been received but this is the first data packet from this source.
        if str.DataPort == 0 {
            str.DataPort = rp.fromAddr.DataPort
        }
    }
    rs.streamsMapMutex.Unlock()

    // Before forwarding packet to next upper layer (application) for further processing:
    // 1) check for collisions and loops. If the packet cannot be assigned to a source, it will be rejected.
    // 2) check the source is a sufficiently well known source
    // TODO: also check CSRC identifiers.
    if !str.checkSsrcIncomingData(existing, rs, rp) || !str.recordReceptionData(rp, rs, tm) {
        // must be discarded due to collision or loop or invalid source
        rp.FreePacket()
        return false
    }

    select {
    case rs.dataReceiveChan <- rp: // forwarded packet, that's all folks
    default:
        rp.FreePacket() // either channel full or not created - free packet
    }
    return true
}

func (rs *Session) OnRecvCtrl(rp *CtrlPacket) bool {

    if pktType := rp.Type(0); pktType != RtcpSR && pktType != RtcpRR {
        rp.FreePacket()
        return false
    }
    ssrc := rp.Ssrc(0) // get SSRC from control packet

    rs.streamsMapMutex.Lock()
    str, strIdx, existing := rs.lookupSsrcMap(ssrc)

    // if not found in the input stream then create a new SSRC input stream
    if !existing {
        str = newSsrcStreamIn(&rp.fromAddr, ssrc)
        str.streamStatus = active
        rs.streamsIn[rs.streamInIndex] = str
        rs.streamInIndex++
    } else {
        // Check if an existing stream is of type input stream and is active
        if str.streamStatus != active {
            rp.FreePacket()
            rs.streamsMapMutex.Unlock()
            return false
        }
        // Test if RTP packets had been received but this is the first control packet from this source.
        if str.CtrlPort == 0 {
            str.CtrlPort = rp.fromAddr.CtrlPort
        }
    }
    rs.streamsMapMutex.Unlock()

    // Check if sender's SSRC collides or loops 
    if !str.checkSsrcIncomingCtrl(existing, rs, &rp.fromAddr) {
        rp.FreePacket()
        return false
    }
    // record reception time
    str.statistics.lastRtcpPacketTime = time.Now().UnixNano()

    offset := 0
    ctrlEvArr := make([]*CtrlEvent, 0, 10)
    if !existing {
        ctrlEvArr = append(ctrlEvArr, newCrtlEvent(NewStreamCtrl, ssrc, rs.streamInIndex-1))
    }

    for offset < rp.inUse {
        switch rp.Type(offset) {
        case RtcpSR:
            str.statistics.lastRtcpSrTime = str.statistics.lastRtcpPacketTime
            str.readSenderInfo(rp.toSenderInfo(rtcpHeaderLength + offset))

            ctrlEvArr = append(ctrlEvArr, newCrtlEvent(RtcpSR, ssrc, strIdx))

            rrCnt := rp.Count(offset)
            // Offset to first RR block: offset to SR + fixed Header length for SR + length of sender info
            rrOffset := offset + rtcpHeaderLength + senderInfoLen

            for i := 0; i < rrCnt; i++ {
                rr := rp.toRecvReport(rrOffset)
                strOut, idx, exists := rs.lookupSsrcMapOut(rr.ssrc())
                // Process Receive Reports that match own output streams (SSRC).
                if exists {
                    strOut.readRecvReport(rr)
                    ctrlEvArr = append(ctrlEvArr, newCrtlEvent(RtcpRR, rr.ssrc(), idx))
                }
                rrOffset += reportBlockLen
            }
            // Advance to the next packet in the compound.
            offset += int((rp.Length(offset) + 1) * 4)

        case RtcpRR:
            rrCnt := rp.Count(offset)
            // Offset to first RR block: offset to RR + fixed Header length for RR
            rrOffset := offset + rtcpHeaderLength
            for i := 0; i < rrCnt; i++ {
                rr := rp.toRecvReport(rrOffset)
                strOut, idx, exists := rs.lookupSsrcMapOut(rr.ssrc())
                // Process Receive Reports that match own output streams (SSRC)
                if exists {
                    strOut.readRecvReport(rr)
                    ctrlEvArr = append(ctrlEvArr, newCrtlEvent(RtcpRR, rr.ssrc(), idx))
                }
                rrOffset += reportBlockLen
            }

            // Advance to the next packet in the compound.
            offset += int((rp.Length(offset) + 1) * 4)

        case RtcpSdes:
            sdesChunkCnt := rp.Count(offset)
            sdesPktLen := int(rp.Length(offset) * 4) // length excl. header word
            // Offset to first SDES chunk: offset to SDES + Header word for SDES
            sdesChunkOffset := offset + 4
            for i := 0; i < sdesChunkCnt; i++ {
                chunk := rp.toSdesChunk(sdesChunkOffset, sdesPktLen)
                chunkLen, idx := rs.processSdesChunk(chunk, rp)
                ctrlEvArr = append(ctrlEvArr, newCrtlEvent(RtcpSdes, chunk.ssrc(), idx))
                sdesChunkOffset += chunkLen
                sdesPktLen -= chunkLen
            }
            // Advance to the next packet in the compound, is also index after SDES packet
            offset += int((rp.Length(offset) + 1) * 4)

        case RtcpBye:
            // Currently the method suports only one SSRC per BYE packet. To enhance this we need
            // to return an array of SSRC/CSRC values.
            //
            byePktLen := int(rp.Length(offset) * 4)
            byeCnt := rp.Count(offset)
            byePkt := rp.toByeData(offset+4, byePktLen)

            // Send BYE control event only for known input streams.
            if st, idx, ok := rs.lookupSsrcMapIn(byePkt.ssrc(0)); ok {
                ctrlEv := newCrtlEvent(RtcpBye, byePkt.ssrc(0), idx)
                ctrlEv.ByeReason = byePkt.getReason(byeCnt)
                ctrlEvArr = append(ctrlEvArr, ctrlEv)
                st.streamStatus = isClosing
            }
            // Advance to the next packet in the compound.
            offset += int((rp.Length(offset) + 1) * 4)
        case RtcpApp:
            // Advance to the next packet in the compound.
            offset += int((rp.Length(offset) + 1) * 4)
        case RtcpRtpfb:
            // Advance to the next packet in the compound.
            offset += int((rp.Length(offset) + 1) * 4)
        case RtcpPsfb:
            // Advance to the next packet in the compound.
            offset += int((rp.Length(offset) + 1) * 4)
        case RtcpXr:
            // Advance to the next packet in the compound.
            offset += int((rp.Length(offset) + 1) * 4)

        }
    }
    select {
    case rs.ctrlEventChan <- ctrlEvArr: // send control event
    default:
    }
    // TODO: re-compute average size
    rp.FreePacket()
    ctrlEvArr = nil
    return true
}

// CloseRecv implements the rtp.RtpTransportRecv CloseRecv method.
//
// The method call the registered transport's CloseRecv() method and waits for the Stopped
// signal data for RTP and RTCP.
// If a upper layer application has registered a transportEnd channel forward the signal to it.
//
func (rs *Session) CloseRecv() {
    if rs.transportRecv != nil {
        rs.transportRecv.CloseRecv()
        for allClosed := 0; allClosed != (dataTransportRecvStopped | ctrlTransportRecvStopped); {
            allClosed |= <-rs.transportEnd
        }
    }
    if rs.transportEndUpper != nil {
        rs.transportEndUpper <- (dataTransportRecvStopped | ctrlTransportRecvStopped)
    }
}

func (rs *Session) setEndChannel(ch TransportEnd) {
    rs.transportEndUpper = ch
}

// WriteData sends a RTP packet of an active output stream to all known remote destinations.
// This functions updates some statistical values to enable RTCP processing.
//
func (rs *Session) WriteData(rp *DataPacket) (n int, err error) {

    strOut, _, _ := rs.lookupSsrcMapOut(rp.Ssrc())
    if strOut.streamStatus != active {
        return 0, nil
    }
    strOut.SenderPacketCnt++
    strOut.SenderOctectCnt += uint32(len(rp.Payload()))

    strOut.streamMutex.Lock()
    if !strOut.sender && rs.rtcpCtrlChan != nil {
        rs.rtcpCtrlChan <- rtcpIncrementSender
    }
    strOut.statistics.lastPacketTime = time.Now().UnixNano()
    strOut.sender = true
    strOut.streamMutex.Unlock()

    // Check here if SRTP is enabled for the SSRC of the packet - a stream attribute?
    for _, remote := range rs.remotes {
        _, err := rs.transportWrite.WriteDataTo(rp, remote)
        if err != nil {
            return 0, err
        }
    }
    return n, nil
}

// WriteCtrl sends RTCP packet of an active output stream to all known remote destinations.
// Usually normal applications don't use this function, RTCP is handled internally.
//
func (rs *Session) WriteCtrl(rp *CtrlPacket) (n int, err error) {

    // Check here if SRTCP is enabled for the SSRC of the packet - a stream attribute?
    strOut, _, _ := rs.lookupSsrcMapOut(rp.Ssrc(0))
    if strOut.streamStatus != active {
        return 0, nil
    }
    for _, remote := range rs.remotes {
        _, err := rs.transportWrite.WriteCtrlTo(rp, remote)
        if err != nil {
            return 0, err
        }
    }
    return n, nil
}

// ******** Local methods **********

func (rs *Session) rtcpService() {
    for _, str := range rs.streamsOut {
        rc := rs.buildRtcpPkt(str)
        rs.WriteCtrl(rc)
    }

    // TODO: implement interval comutation and replace the fixed times here.
    // see chapter 6.2, 6.3, 6.3.1
    
    rtcpInterval := time.Duration(5e9) // 5 seconds
    ssrcTimeout := 5 * rtcpInterval
    dataTimeout := 2 * rtcpInterval

    ticker := time.NewTicker(rtcpInterval)
    var cmd uint32
    for cmd != rtcpStopService {
        select {
        case <-ticker.C:
            now := time.Now().UnixNano()
            for idx, str := range rs.streamsOut {
                switch str.streamStatus {
                case active:
                    rc := rs.buildRtcpPkt(str)
                    rs.WriteCtrl(rc)

                case isClosing:
                    str.streamStatus = isClosed

                case isClosed:
                    delete(rs.streamsOut, idx)
                    continue // no further processing for this channel
                }
                str.streamMutex.Lock()
                // Manage number of active senders. Every time this stream sends a packet the output stream
                // sender variable is set to 2. Thus if no RTP packets sent for 2 RTCP intervals the number 
                // of active senders is decremented if not already zero. See chapter 6.3.8
                rtpDiff := time.Duration(now - str.statistics.lastPacketTime)
                if rtpDiff > dataTimeout {
                    if rs.activeSenders > 0 {
                        str.sender = false
                        rs.activeSenders--
                    }
                }
                str.streamMutex.Unlock()
            }
            for idx, str := range rs.streamsIn {
                str.streamMutex.Lock()
                // Manage number of active senders. Stream processing sets sender to 2 if it receives a RTP data
                // packet. Thus after 2 RTCP intervals the number of active senders is decremented if not already
                // zero. See chapter 6.3.5
                rtpDiff := time.Duration(now - str.statistics.lastPacketTime)
                if rtpDiff > dataTimeout {
                    if rs.activeSenders > 0 {
                        str.sender = false
                        rs.activeSenders--
                    }
                }
                // SSRC timeout processing: check for inactivity longer than 5*non-random interval time (both RTP/RTCP inactivity)
                // chapter 6.3.5
                rtcpDiff := time.Duration(now - str.statistics.lastRtcpPacketTime)
                if rtpDiff > rtcpDiff {
                    rtpDiff = rtcpDiff
                }
                if rtpDiff > ssrcTimeout {
                    delete(rs.streamsIn, idx)
                }
                str.streamMutex.Unlock()
            }
        case cmd = <-rs.rtcpCtrlChan:
            switch cmd & rtcpCtrlCmdMask {
            case rtcpStopService:
                ticker.Stop()

            case rtcpModifyInterval:
                ticker.Stop()
                rtcpInterval = time.Duration(cmd &^ rtcpCtrlCmdMask)
                ticker = time.NewTicker(rtcpInterval)

            case rtcpModifySsrcTimeout:
                ssrcTimeout = time.Duration(cmd &^ rtcpCtrlCmdMask)

            case rtcpIncrementSender:
                rs.activeSenders++
            }
        }
    }
}

// buildRtcpPkt builds a normal RTCP compound that consists of SR/RR and SDES RTCP packets.
//
func (rs *Session) buildRtcpPkt(strOut *SsrcStream) (rc *CtrlPacket) {

    var pktLen int
    if strOut.sender {
        rc = strOut.NewCtrlPacket(RtcpSR)
        strOut.makeSenderInfo(rc) // create a sender info block after fixed header.
        pktLen = (rtcpHeaderLength+senderInfoLen)/4 - 1
    } else {
        rc = strOut.NewCtrlPacket(RtcpRR)
        pktLen = rtcpHeaderLength/4 - 1
    }
    // Loop over all active input streams, max 31, last added report points to next position for following 
    // RTCP packet (SDES or RR if more that 31 active input streams).
    // TODO Check and handle if we have more then 31 input streams, check for selection of streams acc. to RFC when sending RR
    var rrCnt int
    for _, strIn := range rs.streamsIn {
        if strIn.sender {
            strIn.makeRecvReport(rc)
            pktLen += reportBlockLen / 4 // increment SR to include length of this recv report block
            rrCnt++
        }
    }
    rc.SetLength(0, uint16(pktLen)) // length of first RTCP packet in compound: fixed header, SR, n*RR
    rc.SetCount(0, rrCnt)

    offsetSdes := rc.InUse()
    if strOut.sdesChunkLen > 0 {
        strOut.addCtrlHeader(rc, offsetSdes, RtcpSdes) // Add a RTCP SDES packet header after the SR/RR packet
        // makeSdesChunk returns position where to append next chunk - for CSRCs that contribute to this, chap 6.5, RFC 3550
        // CSRCs currently not supported, need additional data structures in output stream.
        nextChunk := strOut.makeSdesChunk(rc)
        rc.SetCount(offsetSdes, 1)                                   // currently one SDES chunk per SDES packet
        rc.SetLength(offsetSdes, uint16((nextChunk-offsetSdes)/4-1)) // length of SDES packet in compound: fixed header plus SDES chunk len
    }
    return
}

// buildRtcpByePkt build a RTCP BYE compund.
//
func (rs *Session) buildRtcpByePkt(strOut *SsrcStream, reason string) (rc *CtrlPacket) {
    rc = rs.buildRtcpPkt(strOut)
    offset := rc.InUse()
    strOut.addCtrlHeader(rc, offset, RtcpBye)
    // Here we may add a loop over CSRC (addtional data in ouput steam) and hand over to makeByeData
    nextOffset := strOut.makeByeData(rc, reason)
    rc.SetCount(offset, 1)                                // currently one BYE SSRC/CSRC per packet
    rc.SetLength(offset, uint16((nextOffset-offset)/4-1)) // length of BYE packet in compound: fixed header plus BYE data
    return
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

// processSdesChunk check if the chunk's SSRC is already known and if yes, parse it.
// the method returns the length of the chunk .
//
func (rs *Session) processSdesChunk(chunk sdesChunk, rp *CtrlPacket) (int, uint32) {
    chunkLen := chunk.chunkLen()
    strIn, idx, existing := rs.lookupSsrcMapIn(chunk.ssrc())
    if !existing {
        return chunkLen, idx
    }
    strIn.parseSdesChunk(chunk)
    return chunkLen, idx
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
    newOut = newSsrcStreamOut(&Address{oldOut.IpAddr, oldOut.DataPort, oldOut.CtrlPort}, 0, 0)

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
    rs.streamsOut[idx] = newOut // replace the oldOut with a new initialized out, new SSRC, sequence but old address

    // sanity check - this is a panic, something stange happened
    for idx, str = range rs.streamsIn {
        if oldOut.ssrc == str.ssrc {
            panic("Panic: found input stream during collision handling - expected none")
            return
        }
    }
    rs.streamsIn[rs.streamInIndex] = oldOut
    rs.streamInIndex++
    return
}

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
