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

// For full reference of registered RTP parameters and payload types refer to:
// http://www.iana.org/assignments/rtp-parameters

// Registry:
// PT        encoding name   audio/video (A/V)  clock rate (Hz)  channels (audio)  Reference
// --------  --------------  -----------------  ---------------  ----------------  ---------
// 0         PCMU            A                  8000             1                 [RFC3551]
// 1         Reserved    
// 2         Reserved
// 3         GSM             A                  8000             1                 [RFC3551]
// 4         G723            A                  8000             1                 [Kumar][RFC3551]
// 5         DVI4            A                  8000             1                 [RFC3551]
// 6         DVI4            A                  16000            1                 [RFC3551]
// 7         LPC             A                  8000             1                 [RFC3551]
// 8         PCMA            A                  8000             1                 [RFC3551]
// 9         G722            A                  8000             1                 [RFC3551]
// 10        L16             A                  44100            2                 [RFC3551]
// 11        L16             A                  44100            1                 [RFC3551]
// 12        QCELP           A                  8000             1                 [RFC3551]
// 13        CN              A                  8000             1                 [RFC3389]
// 14        MPA             A                  90000                              [RFC3551][RFC2250]
// 15        G728            A                  8000             1                 [RFC3551]
// 16        DVI4            A                  11025            1                 [DiPol]
// 17        DVI4            A                  22050            1                 [DiPol]
// 18        G729            A                  8000             1                 [RFC3551]
// 19        Reserved        A
// 20        Unassigned      A
// 21        Unassigned      A
// 22        Unassigned      A
// 23        Unassigned      A
// 24        Unassigned      V
// 25        CelB            V                  90000                              [RFC2029]
// 26        JPEG            V                  90000                              [RFC2435]
// 27        Unassigned      V
// 28        nv              V                  90000                              [RFC3551]
// 29        Unassigned      V
// 30        Unassigned      V
// 31        H261            V                  90000                              [RFC4587]
// 32        MPV             V                  90000                              [RFC2250]
// 33        MP2T            AV                 90000                              [RFC2250]
// 34        H263            V                  90000                              [Zhu]
// 35-71     Unassigned      ?
// 72-76     Reserved for RTCP conflict avoidance                                  [RFC3551]
// 77-95     Unassigned      ?
// 96-127    dynamic         ?                                                     [RFC3551] 

const (
    Audio = 1
    Video = 2
)

// PayloadFormat holds RTP payload formats.
//
// The global variable PayloadFormatMap holds the well known payload formats 
// (see http://www.iana.org/assignments/rtp-parameters). 
// Applications shall not alter these predefined formats.
//
// If an application needs additional payload formats it must create and populate
// PayloadFormat structures and insert them into PayloadFormatMap before setting
// up the RTP communication. The index (key) into the map must be the payload 
// format number. For dynamic payload formats applications shall use payload
// format numbers between 96 and 127 only.
//
// For example if a dynamic format uses the payload number 98 then the application 
// may perform: 
//
//     PayloadFormatMap[98] = &net.rtp.PayloadFormat{98, net.rtp.Audio, 41000, 2, "CD"}
//
type PayloadFormat struct {
    TypeNumber,
    MediaType,
    ClockRate,
    Channels int
    Name string
}
type payloadMap map[int]*PayloadFormat

var PayloadFormatMap = make(payloadMap, 25)

func init() {
    PayloadFormatMap[0] = &PayloadFormat{0, Audio, 8000, 1, "PCMU"}
    // 1         Reserved    
    // 2         Reserved
    PayloadFormatMap[3] = &PayloadFormat{3, Audio, 8000, 1, "GSM"}
    PayloadFormatMap[4] = &PayloadFormat{4, Audio, 8000, 1, "G723"}
    PayloadFormatMap[5] = &PayloadFormat{5, Audio, 8000, 1, "DVI4"}
    PayloadFormatMap[6] = &PayloadFormat{6, Audio, 16000, 1, "DVI4"}
    PayloadFormatMap[7] = &PayloadFormat{7, Audio, 8000, 1, "LPC"}
    PayloadFormatMap[8] = &PayloadFormat{8, Audio, 8000, 1, "PCMA"}
    PayloadFormatMap[9] = &PayloadFormat{9, Audio, 8000, 1, "G722"}
    PayloadFormatMap[10] = &PayloadFormat{10, Audio, 44100, 2, "L16"}
    PayloadFormatMap[11] = &PayloadFormat{11, Audio, 44100, 1, "L16"}
    PayloadFormatMap[12] = &PayloadFormat{12, Audio, 8000, 1, "QCELP"}
    PayloadFormatMap[13] = &PayloadFormat{13, Audio, 8000, 1, "CN"}
    PayloadFormatMap[14] = &PayloadFormat{14, Audio, 90000, 0, "MPA"}
    PayloadFormatMap[15] = &PayloadFormat{15, Audio, 8000, 1, "G728"}
    PayloadFormatMap[16] = &PayloadFormat{16, Audio, 11025, 1, "DVI4"}
    PayloadFormatMap[17] = &PayloadFormat{17, Audio, 22050, 1, "DVI4"}
    PayloadFormatMap[18] = &PayloadFormat{18, Audio, 8000, 1, "G729"}
    // 19        Reserved        A
    // 20        Unassigned      A
    // 21        Unassigned      A
    // 22        Unassigned      A
    // 23        Unassigned      A
    // 24        Unassigned      V
    PayloadFormatMap[25] = &PayloadFormat{25, Video, 90000, 0, "CelB"}
    PayloadFormatMap[26] = &PayloadFormat{26, Video, 90000, 0, "JPEG"}
    // 27        Unassigned      V
    PayloadFormatMap[28] = &PayloadFormat{28, Video, 90000, 0, "nv"}
    // 29        Unassigned      V
    // 30        Unassigned      V
    PayloadFormatMap[31] = &PayloadFormat{31, Video, 90000, 0, "H261"}
    PayloadFormatMap[32] = &PayloadFormat{32, Video, 90000, 0, "MPV"}
    PayloadFormatMap[33] = &PayloadFormat{33, Audio | Video, 90000, 0, "MP2T"}
    PayloadFormatMap[34] = &PayloadFormat{34, Video, 90000, 0, "H263"}
    // 35-71     Unassigned      ?
    // 72-76     Reserved for RTCP conflict avoidance
    // 77-95     Unassigned      ?
    // 96-127    dynamic         ? 
}
