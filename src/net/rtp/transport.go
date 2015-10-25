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

type TransportRecv interface {
    ListenOnTransports() error
    OnRecvData(rp *DataPacket) bool
    OnRecvCtrl(rp *CtrlPacket) bool
    SetCallUpper(upper TransportRecv)
    CloseRecv()
    SetEndChannel(ch TransportEnd)
}

type TransportWrite interface {
    WriteDataTo(rp *DataPacket, addr *Address) (n int, err error)
    WriteCtrlTo(rp *CtrlPacket, addr *Address) (n int, err error)
    SetToLower(lower TransportWrite)
    CloseWrite()
}

type TransportCommon struct {
    transportEnd TransportEnd
    dataRecvStop,
    ctrlRecvStop,
    dataWriteStop,
    ctrlWriteStop bool
}
