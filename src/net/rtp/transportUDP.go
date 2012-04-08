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
    "time"
)

// RtpTransportUDP implements the interfaces RtpTransportRecv and RtpTransportWrite for RTP transports.
type TransportUDP struct {
    TransportCommon
    callUpper                   TransportRecv
    toLower                     TransportWrite
    dataConn, ctrlConn          *net.UDPConn
    localAddrRtp, localAddrRtcp *net.UDPAddr
}

// NewRtpTransportUDP creates a new RTP transport for UPD.
//
// addr - The UPD socket's local IP address
// 
// port - The port number of the RTP data port. This must be an even port number.
//        The following odd port number is the control (RTCP) port. 
//
func NewTransportUDP(addr *net.IPAddr, port int) (*TransportUDP, error) {
    tp := new(TransportUDP)
    tp.callUpper = tp
    tp.localAddrRtp = &net.UDPAddr{addr.IP, port}
    tp.localAddrRtcp = &net.UDPAddr{addr.IP, port + 1}
    return tp, nil
}

// ListenOnTransports listens for incoming RTP and RTCP packets addressed
// to this transport. 
//
func (tp *TransportUDP) ListenOnTransports() (err error) {
    tp.dataConn, err = net.ListenUDP(tp.localAddrRtp.Network(), tp.localAddrRtp)
    if err != nil {
        return
    }
    tp.ctrlConn, err = net.ListenUDP(tp.localAddrRtcp.Network(), tp.localAddrRtcp)
    if err != nil {
        tp.dataConn.Close()
        tp.dataConn = nil
        return
    }
    go tp.readDataPacket()
    go tp.readCtrlPacket()
    return nil
}

// *** The following methods implement the rtp.TransportRecv interface.

// SetCallUpper implements the rtp.TransportRecv SetCallUpper method.
func (tp *TransportUDP) SetCallUpper(upper TransportRecv) {
    tp.callUpper = upper
}

// OnRecvRtp implements the rtp.TransportRecv OnRecvRtp method.
//
// TransportUDP does not implement any processing because it is the lowest
// layer and expects an upper layer to receive data.
func (tp *TransportUDP) OnRecvData(rp *DataPacket) bool {
    fmt.Printf("TransportUDP: no registered upper layer RTP packet handler\n")
    return false
}

// OnRecvRtcp implements the rtp.TransportRecv OnRecvRtcp method.
//
// TransportUDP does not implement any processing because it is the lowest
// layer and expects an upper layer to receive data.
func (tp *TransportUDP) OnRecvCtrl(rp *CtrlPacket) bool {
    fmt.Printf("TransportUDP: no registered upper layer RTCP packet handler\n")
    return false
}

// CloseRecv implements the rtp.TransportRecv CloseRecv method.
func (tp *TransportUDP) CloseRecv() {
    //
    // The correct way to do it is to close the UDP connection after setting the
    // stop flags to true. However, until issue 2116 is solved just set the flags
    // and rely on the read timeout in the read packet functions
    //
    tp.dataRecvStop = true
    tp.ctrlRecvStop = true

    //    err := tp.rtpConn.Close()
    //    if err != nil {
    //        fmt.Printf("Close failed: %s\n", err.String())
    //    }
    //    tp.rtcpConn.Close()
}

// setEndChannel receives and set the channel to signal back after network socket was closed and receive loop terminated. 
func (tp *TransportUDP) SetEndChannel(ch TransportEnd) {
    tp.transportEnd = ch
}

// *** The following methods implement the rtp.TransportWrite interface.

// SetToLower implements the rtp.TransportWrite SetToLower method.
//
// Usually TransportUDP is already the lowest layer.
func (tp *TransportUDP) SetToLower(lower TransportWrite) {
    tp.toLower = lower
}

// WriteRtpTo implements the rtp.TransportWrite WriteRtpTo method.
func (tp *TransportUDP) WriteDataTo(rp *DataPacket, addr *Address) (n int, err error) {
    return tp.dataConn.WriteToUDP(rp.buffer[0:rp.inUse], &net.UDPAddr{addr.IpAddr, addr.DataPort})
}

// WriteRtcpTo implements the rtp.TransportWrite WriteRtcpTo method.
func (tp *TransportUDP) WriteCtrlTo(rp *CtrlPacket, addr *Address) (n int, err error) {
    return tp.ctrlConn.WriteToUDP(rp.buffer[0:rp.inUse], &net.UDPAddr{addr.IpAddr, addr.CtrlPort})
}

// CloseWrite implements the rtp.TransportWrite CloseWrite method.
//
// Nothing to do for TransportUDP. The application shall close the receiver (CloseRecv()), this will
// close the local UDP socket.
func (tp *TransportUDP) CloseWrite() {
}

// *** Local functions and methods.

// Here the local RTP and RTCP UDP network receivers. The ListenOnTransports() starts them
// as go functions. The functions just receive data from the network, copy it into
// the packet buffers and forward the packets to the next upper layer via callback
// if callback is not nil

func (tp *TransportUDP) readDataPacket() {
    var buf [defaultBufferSize]byte

    tp.dataRecvStop = false
    for {
        tp.dataConn.SetReadDeadline(time.Now().Add(20 * time.Millisecond)) // 20 ms, re-test and remove after Go issue 2116 is solved
        n, addr, err := tp.dataConn.ReadFromUDP(buf[0:])
        if tp.dataRecvStop {
            break
        }
        if e, ok := err.(net.Error); ok && e.Timeout() {
            continue
        }
        if err != nil {
            break
        }
        rp := newDataPacket()
        rp.fromAddr.IpAddr = addr.IP
        rp.fromAddr.DataPort = addr.Port
        rp.fromAddr.CtrlPort = 0
        rp.inUse = n
        copy(rp.buffer, buf[0:n])

        if tp.callUpper != nil {
            tp.callUpper.OnRecvData(rp)
        }
    }
    tp.dataConn.Close()
    tp.transportEnd <- DataTransportRecvStopped
}

func (tp *TransportUDP) readCtrlPacket() {
    var buf [defaultBufferSize]byte

    tp.ctrlRecvStop = false
    for {
        tp.dataConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) // 100 ms, re-test and remove after Go issue 2116 is solved
        n, addr, err := tp.ctrlConn.ReadFromUDP(buf[0:])
        if tp.ctrlRecvStop {
            break
        }
        if e, ok := err.(net.Error); ok && e.Timeout() {
            continue
        }
        if err != nil {
            break
        }
        rp, _ := newCtrlPacket()
        rp.fromAddr.IpAddr = addr.IP
        rp.fromAddr.CtrlPort = addr.Port
        rp.fromAddr.DataPort = 0
        rp.inUse = n
        copy(rp.buffer, buf[0:n])

        if tp.callUpper != nil {
            tp.callUpper.OnRecvCtrl(rp)
        }
    }
    tp.ctrlConn.Close()
    tp.transportEnd <- CtrlTransportRecvStopped
}
