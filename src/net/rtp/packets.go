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
    "encoding/binary"
    "encoding/hex"
    "fmt"
)

const (
    defaultBufferSize  = 1200
    freeListLengthRtp  = 10
    freeListLengthRtcp = 5
    rtpHeaderLength    = 12
    rtcpHeaderLength   = 4
    rtcpSsrcLength     = 4
    padToMultipleOf    = 4
)

const (
    markerPtOffset   = 1
    packetTypeOffset = 1
    lengthOffset     = 2
    sequenceOffset   = 2
    timestampOffset  = sequenceOffset + 2
    ssrcOffsetRtp    = timestampOffset + 4
    ssrcOffsetRtcp   = sequenceOffset + 2
)

const (
    version2Bit  = 0x80
    extensionBit = 0x10
    paddingBit   = 0x20
    markerBit    = 0x80
    ccMask       = 0x0f
    ptMask       = 0x7f
    countMask    = 0x1f
)

// For full reference of registered RTP parameters refer to:
// http://www.iana.org/assignments/rtp-parameters

// RTCP packet types
const (
    RtcpSR    = 200 // SR         sender report          [RFC3550]
    RtcpRR    = 201 // RR         receiver report        [RFC3550]
    RtcpSdes  = 202 // SDES       source description     [RFC3550]
    RtcpBye   = 203 // BYE        goodbye                [RFC3550]
    RtcpApp   = 204 // APP        application-defined    [RFC3550]
    RtcpRtpfb = 205 // RTPFB      Generic RTP Feedback   [RFC4585]
    RtcpPsfb  = 206 // PSFB       Payload-specific       [RFC4585]
    RtcpXr    = 207 // XR         extended report        [RFC3611]
)

// RTCP SDES item types
const (
    SdesEnd       = iota // END          end of SDES list                    [RFC3550]
    SdesCname            // CNAME        canonical name                      [RFC3550]
    SdesName             // NAME         user name                           [RFC3550]
    SdesEmail            // EMAIL        user's electronic mail address      [RFC3550]
    SdesPhone            // PHONE        user's phone number                 [RFC3550]
    SdesLoc              // LOC          geographic user location            [RFC3550]
    SdesTool             // TOOL         name of application or tool         [RFC3550]
    SdesNote             // NOTE         notice about the source             [RFC3550]
    SdesPriv             // PRIV         private extensions                  [RFC3550]
    SdesH323Caddr        // H323-CADDR   H.323 callable address              [Kumar]
    sdesMax
)

// Length of fixed report blocks in bytes
const (
    senderInfoLen  = 20
    reportBlockLen = 24
)

// nullArray is what it's names says: a long array filled with zeros.
// used to clear (fill with zeros) arrays/slices inside a buffer by copying.
var nullArray [1200]byte

type RawPacket struct {
    inUse    int
    padTo    int
    isFree   bool
    fromAddr Address
    buffer   []byte
}

// Buffer returns the internal buffer in raw format.
// Usually only other Transports use the buffer in raw format, for example to encrypt
// or decrypt the buffer.
// Always call Buffer() just before the the buffer is actually used because several packet 
// handling functions may re-allocate buffers.
func (raw *RawPacket) Buffer() []byte {
    return raw.buffer
}

// InUse returns the number of valid bytes in the packet buffer.
// Several function modify the inUse variable, for example when copying payload or setting extensions
// in the RTP packet. Thus "buffer[0:inUse]" is the slice inside the buffer that will be sent or
// was received.  
func (rp *RawPacket) InUse() int {
    return rp.inUse
}

// *** RTP specific functions start here ***

// RTP packet type to define RTP specific functions
type DataPacket struct {
    RawPacket
    payloadLength int16
}

var freeListRtp = make(chan *DataPacket, freeListLengthRtp)

func newDataPacket() (rp *DataPacket) {
    // Grab a packet if available; allocate if not.
    select {
    case rp = <-freeListRtp: // Got one; nothing more to do.
    default:
        rp = new(DataPacket) // None free, so allocate a new one.
        rp.buffer = make([]byte, defaultBufferSize)
    }
    rp.buffer[0] = version2Bit // RTP: V = 2, P, X, CC = 0
    rp.inUse = rtpHeaderLength
    rp.isFree = false
    return
}

// FreePacket returns the packet to the free RTP list.
// A packet marked as free is ignored, thus calling FreePacket multiple times for the same
// packet is possible.
func (rp *DataPacket) FreePacket() {
    if rp.isFree {
        return
    }
    rp.buffer[0] = 0 // invalidate RTP packet
    rp.inUse = 0
    rp.padTo = 0
    rp.fromAddr.DataPort = 0
    rp.fromAddr.IpAddr = nil
    rp.isFree = true

    select {
    case freeListRtp <- rp: // Packet on free list; nothing more to do.
    default: // Free list full, just carry on.
    }
}

// CsrcCount return the number of CSRC values in this packet
func (rp *DataPacket) CsrcCount() uint8 {
    return rp.buffer[0] & ccMask
}

// SetCsrcList takes the CSRC in this list, converts from host to network order and sets into the RTP packet.
// The new CSRC list replaces an existing CSCR list. The list can have a maximum length of 16 CSCR values, 
// if the list contains more values the method leaves the RTP packet untouched.
func (rp *DataPacket) SetCsrcList(csrc []uint32) {

    if len(csrc) > 16 {
        return
    }
    // For this method: content is any data after an existing (or new) CSRC list. This
    // includes RTP extension data and payload.
    offsetOld := int(rp.CsrcCount()*4 + rtpHeaderLength) // offset to old content
    offsetNew := len(csrc)*4 + rtpHeaderLength           // offset to new content

    newInUse := offsetNew + rp.inUse - offsetOld
    if newInUse > cap(rp.buffer) {
        return
    }
    tmpRp := newDataPacket() // get a new packet first
    newBuf := tmpRp.buffer   // and get its buffer

    copy(newBuf, rp.buffer[0:rtpHeaderLength])              // copy fixed header 
    copy(newBuf[offsetNew:], rp.buffer[offsetOld:rp.inUse]) // copy over old content

    for i := 0; i < len(csrc); i++ {
        binary.BigEndian.PutUint32(newBuf[rtpHeaderLength+i*4:], csrc[i]) // CSCR in network order
    }
    tmpRp.buffer = rp.buffer // switch buffers
    rp.buffer = newBuf
    tmpRp.FreePacket() // free temporary RTP packet

    rp.buffer[0] &^= ccMask // clear old length
    rp.buffer[0] |= byte(len(csrc) & ccMask)
    rp.inUse = newInUse
}

// CsrcList returns the list of CSRC values as uint32 slice in host horder
func (rp *DataPacket) CsrcList() (list []uint32) {
    list = make([]uint32, rp.CsrcCount())
    for i := 0; i < len(list); i++ {
        list[i] = binary.BigEndian.Uint32(rp.buffer[rtpHeaderLength+i*4:])
    }
    return
}

// SetExtension takes a byte slice and set it as extension into the RTP packet.
// The byte slice must conform to one of the formats specified in RFC 3550 or RFC 5258, thus
// the length must be a multiple of uint32 (4) and the length field must be in the 3rd and 4th
// byte (uint16) and its value must adhere to RFC 3550 / RFC 5258. 
func (rp *DataPacket) SetExtension(ext []byte) {
    if (len(ext) % 4) != 0 {
        return
    }
    l := 0
    if len(ext) > 0 {
        l = int((binary.BigEndian.Uint16(ext[2:]) + 1) * 4)
    }
    if l != len(ext) {
        return
    }
    // For this method: content is any data after an existing (or new) Extension area. This
    // is the payload.
    offsetExt := int(rp.CsrcCount()*4 + rtpHeaderLength) // offset to Extension
    offsetOld := int(rp.CsrcCount()*4 + rtpHeaderLength) // offset to old content
    if rp.ExtensionBit() {
        offsetOld += rp.ExtensionLength()
    }
    offsetNew := offsetExt + l // offset to new content

    newInUse := rp.inUse + l - (offsetOld - offsetExt)
    if newInUse > cap(rp.buffer) {
        return
    }
    tmpRp := newDataPacket() // get a new packet first
    newBuf := tmpRp.buffer   // and get its buffer

    copy(newBuf, rp.buffer[0:rtpHeaderLength])              // copy fixed header 
    copy(newBuf[offsetExt:], ext)                           // copy new extension
    copy(newBuf[offsetNew:], rp.buffer[offsetOld:rp.inUse]) // copy over old content

    tmpRp.buffer = rp.buffer // switch buffers
    rp.buffer = newBuf
    tmpRp.FreePacket() // free temporary RTP packet
    if l == 0 {
        rp.buffer[0] &^= extensionBit
    } else {
        rp.buffer[0] |= extensionBit
    }
    rp.inUse = newInUse
}

// Extension returns the byte slice of the RTP packet extension part, if not extension available it returns nil.
// This is not a copy of the extension part but the slice points into the real RTP packet buffer.
func (rp *DataPacket) Extension() []byte {
    if !rp.ExtensionBit() {
        return nil
    }
    offset := int(rp.CsrcCount()*4 + rtpHeaderLength)
    return rp.buffer[offset : offset+rp.ExtensionLength()]
}

// Ssrc returns the SSRC as uint32 in host order.
func (rp *DataPacket) Ssrc() uint32 {
    return binary.BigEndian.Uint32(rp.buffer[ssrcOffsetRtp:])
}

// SetSsrc converts SSRC from host order into network order and stores it in the RTP packet.
func (rp *DataPacket) SetSsrc(ssrc uint32) {
    binary.BigEndian.PutUint32(rp.buffer[ssrcOffsetRtp:], ssrc)
}

// Timestamp returns the Timestamp as uint32 in host order.
func (rp *DataPacket) Timestamp() uint32 {
    return binary.BigEndian.Uint32(rp.buffer[timestampOffset:])
}

// SetTimestamp converts timestamp from host order into network order and stores it in the RTP packet.
func (rp *DataPacket) SetTimestamp(timestamp uint32) {
    binary.BigEndian.PutUint32(rp.buffer[timestampOffset:], timestamp)
}

// SetMarker set or resets the Marker bit.
// If the parameter m is true the methods sets the Marker bit, resets it otherweise.
func (rp *DataPacket) SetMarker(m bool) {
    if m {
        rp.buffer[markerPtOffset] |= markerBit
    } else {
        rp.buffer[markerPtOffset] &^= markerBit
    }
}

// Marker returns the state of the Marker bit.
// If the Marker bit is set the method return true, otherwise it returns false
func (rp *DataPacket) Marker() bool {
    return (rp.buffer[markerPtOffset] & markerBit) == markerBit
}

// SetPadding set or resets the padding bit.
// If the parameter p is true the methods sets the Padding bit, resets it otherweise.
// If parameter p is true and padTo is zero, then this method sets pads the whole
// RTP packet to a multiple of 4, otherwise the given value is used which must be
// greater than 1.
//
//     NOTE: padding is only done when adding payload to the packet, thus if an application
//           required padding then seeting the payload should be the last step in RTP packet creation
func (rp *DataPacket) SetPadding(p bool, padTo int) {
    if padTo == 0 {
        padTo = padToMultipleOf
    }
    if p {
        rp.buffer[0] |= paddingBit
        rp.padTo = padTo
    } else {
        rp.buffer[0] &^= paddingBit
        rp.padTo = 0
    }
}

// Padding returns the state of the Padding bit.
// If the Padding bit is set the method return true, otherwise it returns false
func (rp *DataPacket) Padding() bool {
    return (rp.buffer[0] & paddingBit) == paddingBit
}

// SetPayloadType sets a new payload type value in the RTP packet header.
func (rp *DataPacket) SetPayloadType(pt byte) {
    rp.buffer[markerPtOffset] &^= ptMask // first: clear old type 
    rp.buffer[markerPtOffset] |= (pt & ptMask)
}

// PayloadType return the payload type value from RTP packet header.
func (rp *DataPacket) PayloadType() byte {
    return rp.buffer[markerPtOffset] & ptMask
}

// SetSequence converts the sequence from host order into network order and stores it in the RTP packet header.
func (rp *DataPacket) SetSequence(seq uint16) {
    binary.BigEndian.PutUint16(rp.buffer[sequenceOffset:], seq)
}

// Sequence returns the sequence number as uint16 in host order.
func (rp *DataPacket) Sequence() uint16 {
    return binary.BigEndian.Uint16(rp.buffer[sequenceOffset:])
}

// ExtensionBit returns true if the Extension bit is set in the header, false otherwise.
func (rp *DataPacket) ExtensionBit() bool {
    return (rp.buffer[0] & extensionBit) == extensionBit
}

// ExtensionLength returns the full length in bytes of RTP packet extension (including the main extension header).  
func (rp *DataPacket) ExtensionLength() (length int) {
    if !rp.ExtensionBit() {
        return 0
    }
    offset := int16(rp.CsrcCount()*4 + rtpHeaderLength) // offset to extension header 32bit word
    offset += 2
    length = int(binary.BigEndian.Uint16(rp.buffer[offset:])) + 1 // +1 for the main extension header word
    length *= 4
    return
}

// Payload returns the byte slice of the payload after removing length of possible padding.
//
// The slice is not a copy of the payload but the slice points into the real RTP packet buffer.
func (rp *DataPacket) Payload() []byte {
    payOffset := int(rp.CsrcCount()*4+rtpHeaderLength) + rp.ExtensionLength()
    pad := 0
    if rp.Padding() {
        pad = int(rp.buffer[rp.inUse-1])
    }
    return rp.buffer[payOffset : rp.inUse-pad]
}

// SetPayload copies the contents of payload byte slice into the RTP packet, and replaces an existing payload.
//
// Only SetPayload honors the Padding bit and pads the RTP packet to a multiple of the value specified
// in SetPadding. SetPayload performs padding only if the payload length is greate zero. A payload of
// zero length removes an existing payload including a possible padding
func (rp *DataPacket) SetPayload(payload []byte) {

    payOffset := int(rp.CsrcCount()*4+rtpHeaderLength) + rp.ExtensionLength()
    payloadLenOld := rp.inUse - payOffset

    pad := 0
    padOffset := 0
    if rp.Padding() {
        // adjust payloadLenOld to honor padding length
        if payloadLenOld > rp.padTo {
            payloadLenOld += int(rp.buffer[rp.inUse])
        }
        // Reduce length of inUse by length of old content, thus remove old content
        rp.inUse -= payloadLenOld
        // Compute new padding length
        pad = (len(payload) + rp.inUse) % rp.padTo
        if pad == 0 {
            pad = rp.padTo
        }
    } else {
        // Reduce length of inUse by length of old content, thus remove old content
        rp.inUse -= payloadLenOld
    }
    if (payOffset + len(payload) + pad) > cap(rp.buffer) {
        return
    }
    rp.inUse += copy(rp.buffer[payOffset:], payload)

    if rp.Padding() && len(payload) > 0 {
        padOffset = payOffset + len(payload)
        for i := 0; i < pad-1; i++ {
            rp.buffer[padOffset] = 0
            padOffset++
        }
        rp.buffer[padOffset] = byte(pad)
        rp.inUse += pad
    }
    return
}

func (rp *DataPacket) IsValid() bool {
    if (rp.buffer[0] & version2Bit) != version2Bit {
        return false
    }
    if PayloadFormatMap[int(rp.PayloadType())] == nil {
        return false
    }
    return true
}

// Print outputs a formatted dump of the RTP packet.
func (rp *DataPacket) Print(label string) {
    fmt.Printf("RTP Packet at: %s\n", label)
    fmt.Printf("  fixed header dump:   %s\n", hex.EncodeToString(rp.buffer[0:rtpHeaderLength]))
    fmt.Printf("    Version:           %d\n", (rp.buffer[0]&0xc0)>>6)
    fmt.Printf("    Padding:           %t\n", rp.Padding())
    fmt.Printf("    Extension:         %t\n", rp.ExtensionBit())
    fmt.Printf("    Contributing SRCs: %d\n", rp.CsrcCount())
    fmt.Printf("    Marker:            %t\n", rp.Marker())
    fmt.Printf("    Payload type:      %d (0x%x)\n", rp.PayloadType(), rp.PayloadType())
    fmt.Printf("    Sequence number:   %d (0x%x)\n", rp.Sequence(), rp.Sequence())
    fmt.Printf("    Timestamp:         %d (0x%x)\n", rp.Timestamp(), rp.Timestamp())
    fmt.Printf("    SSRC:              %d (0x%x)\n", rp.Ssrc(), rp.Ssrc())

    if rp.CsrcCount() > 0 {
        cscr := rp.CsrcList()
        fmt.Printf("  CSRC list:\n")
        for i, v := range cscr {
            fmt.Printf("      %d: %d (0x%x)\n", i, v, v)
        }
    }
    if rp.ExtensionBit() {
        extLen := rp.ExtensionLength()
        fmt.Printf("  Extentsion length: %d\n", extLen)
        offsetExt := rtpHeaderLength + int(rp.CsrcCount()*4)
        fmt.Printf("    extension: %s\n", hex.EncodeToString(rp.buffer[offsetExt:offsetExt+extLen]))
    }
    payOffset := rtpHeaderLength + int(rp.CsrcCount()*4) + rp.ExtensionLength()
    fmt.Printf("  payload: %s\n", hex.EncodeToString(rp.buffer[payOffset:rp.inUse]))
}

// *** RTCP specific funtions start here ***

// RTCP packet type to define RTCP specific functions.
type CtrlPacket struct {
    RawPacket
}

var freeListRtcp = make(chan *CtrlPacket, freeListLengthRtcp)

// newCtrlPacket gets a raw packet, initializes the first fixed RTCP header, advances inUse to point after new fixed header.
func newCtrlPacket() (rp *CtrlPacket, offset int) {

    // Grab a packet if available; allocate if not.
    select {
    case rp = <-freeListRtcp: // Got one; nothing more to do.
    default:
        rp = new(CtrlPacket) // None free, so allocate a new one.
        rp.buffer = make([]byte, defaultBufferSize)
    }
    rp.buffer[0] = version2Bit // RTCP: V = 2, P, RC = 0
    rp.inUse = rtcpHeaderLength
    offset = rtcpHeaderLength
    return
}

// addHeaderCtrl adds a new fixed RTCP header field into the compound, initializes, advances inUse to point after new fixed header.
func (rp *CtrlPacket) addHeaderCtrl(offset int) int {
    rp.buffer[offset] = version2Bit // RTCP: V = 2, P, RC = 0
    rp.inUse += 4
    return rp.inUse
}

// addHeaderSsrc adds a SSRC header into the compound (usually after fixed header field), advances inUse to point after SSRC.
func (rp *CtrlPacket) addHeaderSsrc(offset int, ssrc uint32) int {
    binary.BigEndian.PutUint32(rp.buffer[offset:], ssrc)
    rp.inUse += 4
    return rp.inUse
}

func (rp *CtrlPacket) FreePacket() {
    if rp.isFree {
        return
    }
    rp.buffer[0] = 0 // invalidate RTCP packet
    rp.inUse = 0
    rp.padTo = 0
    rp.fromAddr.CtrlPort = 0
    rp.fromAddr.IpAddr = nil
    rp.isFree = true

    select {
    case freeListRtcp <- rp: // Packet on free list; nothing more to do.
    default: // Free list full, just carry on.
    }
}

// SetSsrc converts SSRC from host order into network order and stores it in the RTCP as packet sender.
func (rp *CtrlPacket) SetSsrc(offset int, ssrc uint32) {
    binary.BigEndian.PutUint32(rp.buffer[offset+ssrcOffsetRtcp:], ssrc)
}

// Ssrc returns the SSRC of the packet sender as uint32 in host order.
func (rp *CtrlPacket) Ssrc(offset int) (ssrc uint32) {
    ssrc = binary.BigEndian.Uint32(rp.buffer[offset+ssrcOffsetRtcp:])
    return
}

// Count returns the counter bits in the word defined by offset.
// Offset points to the first byte of the header word of a RTCP packet.
func (rp *CtrlPacket) Count(offset int) int {
    return int(rp.buffer[offset] & countMask)
}

// SetCount returns the counter bits in the word defined by offset.
// Offset points to the first byte of the header word of a RTCP packet.
func (rp *CtrlPacket) SetCount(offset, count int) {
    rp.buffer[offset] |= byte(count & countMask)
}

// SetLength converts the length from host order into network order and stores it in the RTCP packet header.
// Offset points to the first byte of the header word of a RTCP packet.
func (rp *CtrlPacket) SetLength(offset int, length uint16) {
    binary.BigEndian.PutUint16(rp.buffer[offset+lengthOffset:], length)
}

// Length returns the length as uint16 in host order.
// Offset points to the first byte of the header word of a RTCP packet.
func (rp *CtrlPacket) Length(offset int) uint16 {
    return binary.BigEndian.Uint16(rp.buffer[offset+lengthOffset:])
}

// Type returns the report type stored in the header word.
// Offset points to the first byte of the header word of a RTCP packet.
func (rp *CtrlPacket) Type(offset int) int {
    return int(rp.buffer[offset+packetTypeOffset])
}

// SetType sets the report type in the header word.
// Offset points to the first byte of the header word of a RTCP packet.
func (rp *CtrlPacket) SetType(offset, packetType int) {
    rp.buffer[offset+packetTypeOffset] = byte(packetType)
}

type senderInfo []byte
type recvReport []byte
type sdesChunk []byte
type byeData []byte

/*
 * Functions to fill/access a sender info structure
 */

// newSenderInfo returns a senderInfo which is positioned at the current inUse offet and advances inUse to point after senderInfo.
func (rp *CtrlPacket) newSenderInfo() (info senderInfo, offset int) {
    info = rp.toSenderInfo(rp.inUse)
    rp.inUse += len(info)
    offset = rp.inUse
    return
}

// toSenderInfo returns the senderInfo byte slice inside the RTCP packet buffer as senderInfo type, used for received RTCP packets.
// Use functions for this type to parse and access the senderInfo data.
func (rp *CtrlPacket) toSenderInfo(offset int) senderInfo {
    return rp.buffer[offset : offset+senderInfoLen]
}

// ntpTimeStamp returns the NTP time stamp as second, fraction as unsigned 32bit in host order.
func (in senderInfo) ntpTimeStamp() (seconds, fraction uint32) {
    seconds = binary.BigEndian.Uint32(in[0:])
    fraction = binary.BigEndian.Uint32(in[4:])
    return
}

// setNtpTimeStamp takes NTP timestamp values in host order and sets it in network order in SR.
func (in senderInfo) setNtpTimeStamp(seconds, fraction uint32) {
    binary.BigEndian.PutUint32(in[0:], seconds)
    binary.BigEndian.PutUint32(in[4:], fraction)
}

// rtpTimeStamp returns the RTP time stamp as 32bit unsigned in host order.
func (in senderInfo) rtpTimeStamp() uint32 {
    return binary.BigEndian.Uint32(in[8:])
}

// setRtpTimeStamp takes a 32 unsigned timestamp in host order and sets it in network order in SR.
func (in senderInfo) setRtpTimeStamp(stamp uint32) {
    binary.BigEndian.PutUint32(in[8:], stamp)
}

// packetCount returns the sender's packet count as 32bit unsigned in host order.
func (in senderInfo) packetCount() uint32 {
    return binary.BigEndian.Uint32(in[12:])
}

// setPacketCount takes a 32 unsigned counter in host order and sets it in network order in SR.
func (in senderInfo) setPacketCount(cnt uint32) {
    binary.BigEndian.PutUint32(in[12:], cnt)
}

// octetCount returns the sender's octet count as 32bit unsigned in host order.
func (in senderInfo) octetCount() uint32 {
    return binary.BigEndian.Uint32(in[16:])
}

// setOctetCount takes a 32 unsigned counter in host order and sets it in network order in SR.
func (in senderInfo) setOctetCount(cnt uint32) {
    binary.BigEndian.PutUint32(in[16:], cnt)
}

/*
 * Functions to fill/access a receiver report structure
 */

// newSenderInfo returns a senderInfo which is positioned at the current inUse offet and advances inUse to point after senderInfo.
func (rp *CtrlPacket) newRecvReport() (report recvReport, offset int) {
    report = rp.toRecvReport(rp.inUse)
    rp.inUse += len(report)
    offset = rp.inUse
    return
}

// toRecvReport returns the report blocks byte slices inside the RTCP packet buffer as recvReport type.
// Use functions for this type to parse and access the report blocks data.
func (rp *CtrlPacket) toRecvReport(offset int) recvReport {
    return rp.buffer[offset : offset+reportBlockLen]
}

// ssrc returns the receiver report SSRC as 32bit unsigned in host order.
func (rr recvReport) ssrc() uint32 {
    return binary.BigEndian.Uint32(rr[0:])
}

// setSSrc takes a 32 unsigned SSRC in host order and sets it in network order in RR. 
func (rr recvReport) setSsrc(ssrc uint32) {
    binary.BigEndian.PutUint32(rr[0:], ssrc)
}

// packetsLost returns the receiver report packets lost data as 32bit unsigned in host order.
func (rr recvReport) packetsLost() uint32 {
    lost := binary.BigEndian.Uint32(rr[4:])
    return lost >> 8
}

// setPacketsLost takes a 32 unsigned packet lost number in host order and sets lower 24 bits in network order in RR. 
func (rr recvReport) setPacketsLost(pktLost uint32) {
    fracSave := rr[4]
    pktLost &= 0xffffff
    binary.BigEndian.PutUint32(rr[4:], pktLost)
    rr[4] = fracSave
}

// packetsLostFrac returns the receiver report packets lost fractional data as byte.
func (rr recvReport) packetsLostFrac() byte {
    return rr[4]
}

// setPacketsLostFrac takes the byte packet lost fractional and sets it in RR. 
func (rr recvReport) setPacketsLostFrac(frac byte) {
    rr[4] = frac
}

// highestSeq returns the receiver report highest sequence number as 32bit unsigned in host order.
func (rr recvReport) highestSeq() uint32 {
    return binary.BigEndian.Uint32(rr[8:])
}

// setHighestSeq takes a 32 unsigned sequence number in host order and sets it in network order in RR. 
func (rr recvReport) setHighestSeq(seq uint32) {
    binary.BigEndian.PutUint32(rr[8:], seq)
}

// jitter returns the receiver report jitter as 32bit unsigned in host order.
func (rr recvReport) jitter() uint32 {
    return binary.BigEndian.Uint32(rr[12:])
}

// setJitter takes a 32 unsigned jitter value in host order and sets it in network order in RR. 
func (rr recvReport) setJitter(jitter uint32) {
    binary.BigEndian.PutUint32(rr[12:], jitter)
}

// lsr returns the receiver report LSR as 32bit unsigned in host order.
func (rr recvReport) lsr() uint32 {
    return binary.BigEndian.Uint32(rr[16:])
}

// setLsr takes a 32 unsigned LSR value in host order and sets it in network order in RR. 
func (rr recvReport) setLsr(lsr uint32) {
    binary.BigEndian.PutUint32(rr[16:], lsr)
}

// dlsr returns the receiver report DLSR as 32bit unsigned in host order.
func (rr recvReport) dlsr() uint32 {
    return binary.BigEndian.Uint32(rr[20:])
}

// setDlsr takes a 32 unsigned DLSR value in host order and sets it in network order in RR. 
func (rr recvReport) setDlsr(dlsr uint32) {
    binary.BigEndian.PutUint32(rr[20:], dlsr)
}

/*
 * Functions to fill/access a SDES structure
 */

// newSdesChunk returns a SDES chunk which is positioned at the current inUse offet and advances inUse to point after sdesChunk.
func (rp *CtrlPacket) newSdesChunk(length int) (chunk sdesChunk, offset int) {
    chunk = rp.toSdesChunk(rp.inUse, length)
    rp.inUse += len(chunk)
    offset = rp.inUse
    return
}

// toSdesChunk returns the SDES byte slices inside the RTCP packet buffer as sdesChunktype.
// Use functions for this type to parse and access the report blocks data.
func (rp *CtrlPacket) toSdesChunk(offset, length int) sdesChunk {
    if offset > len(rp.buffer) || offset+length > len(rp.buffer) {
        return nil
    }
    return rp.buffer[offset : offset+length]
}

// ssrc returns the receiver report SSRC as 32bit unsigned in host order.
func (sdes sdesChunk) ssrc() uint32 {
    return binary.BigEndian.Uint32(sdes[0:])
}

// setSSrc takes a 32 unsigned SSRC in host order and sets it in network order in SDES chunk. 
func (sdes sdesChunk) setSsrc(ssrc uint32) {
    binary.BigEndian.PutUint32(sdes[0:], ssrc)
}

// setItemData takes the item type and the item text and fills it into the chunk.
// The functions returns the offset where to store the next item.
func (sdes sdesChunk) setItemData(itemOffset int, itemType byte, text string) int {
    sdes[itemOffset] = itemType
    sdes[itemOffset+1] = byte(len(text))
    return copy(sdes[itemOffset+2:], text) + 2
}

func (sdes sdesChunk) getItemType(itemOffset int) int {
    return int(sdes[itemOffset])
}

func (sdes sdesChunk) getItemLen(itemOffset int) int {
    return int(sdes[itemOffset+1])
}

func (sdes sdesChunk) getItemText(itemOffset, length int) string {
    if itemOffset+2+length > len(sdes) {
        return ""
    }
    return string(sdes[itemOffset+2 : itemOffset+2+length])
}

func (sc sdesChunk) chunkLen() (int, bool) {

    // length is at least: SSRC plus SdesEnd byte
    if 4+1 > len(sc) {
        return 0, false
    }
    length := 4 // include SSRC field of this chunk    
    itemType := sc[length]
    if itemType == SdesEnd { // Cover case if chunk has zero items
        if 4+4 > len(sc) { // SSRC (4 byte), SdesEnd (1 byte) plus 3 bytes padding
            return 0, false
        }
        return 8, true
    }
    // Loop over valid items and add their overall length to offset.
    for ; itemType != SdesEnd; itemType = sc[length] {
        length += int(sc[length+1]) + 2 // lenght points to next item type field
        if length > len(sc) {
            return 0, false
        }
    }
    return (length + 4) &^ 0x3, true
}

// newByePacket returns a BYE data structure which is positioned at the current inUse offet and advances inUse to point after BYE.
func (rp *CtrlPacket) newByeData(length int) (bye byeData, offset int) {
    bye = rp.toByeData(rp.inUse, length)
    rp.inUse += len(bye)
    offset = rp.inUse
    return
}
// toByePacket returns the BYE byte slices inside the RTCP packet buffer as byePacket type.
// Use functions for this type to parse and access the BYE data.
func (rp *CtrlPacket) toByeData(offset, length int) byeData {
    if offset > len(rp.buffer) || offset+length > len(rp.buffer) {
        return nil
    }
    return rp.buffer[offset : offset+length]
}

// ssrc returns the bye data SSRC at ssrcIdx as 32bit unsigned in host order.
func (bye byeData) ssrc(ssrcIdx int) uint32 {
    if (ssrcIdx+1)*4 > len(bye) {
        return 0
    }
    return binary.BigEndian.Uint32(bye[ssrcIdx*4:])
}

// setSSrc takes a 32 unsigned SSRC in host order and sets it at ssrcIdx in bye data (network order). 
func (bye byeData) setSsrc(ssrcIdx int, ssrc uint32) {
    binary.BigEndian.PutUint32(bye[ssrcIdx*4:], ssrc)
}

// setReason takes reason text and fills it into the bye data after ssrcCnt SSRC/CSRC entries.
// The functions returns the offset where to store the next item.
func (bye byeData) setReason(reason string, ssrcCnt int) {
    bye[ssrcCnt*4] = byte(len(reason))
    copy(bye[ssrcCnt*4+1:], reason)
}

// getReason returns the reason string if it is available
func (bye byeData) getReason(ssrcCnt int) string {
    offset := ssrcCnt * 4
    if offset >= len(bye) {
        return ""
    }
    length := int(bye[offset])
    offset++
    if offset+length > len(bye) {
        return ""
    }
    return string(bye[offset : offset+length])
}
