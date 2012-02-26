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

/*
The GoRTP package rtp implements the RTP/RTCP protocol.

This Go implementation of the RTP/RTCP protocol uses a set of structures to
provide a high degree of flexibility and to adhere to the RFC 3550 notation of
RTP sessions (refer to RFC 3550 chapter 3, RTP session) and its associated
streams.

Figure 1 depicts the high-level layout of the data structure. This
documentation describes the parts in some more detail.


  +---------------------+            transport modules
  |       Session       |     +-------+  +-------+  +-------+
  |                     | use |       |  |       |  |       |
  | - top-level module  +-<---+       +--+       +--+       | receiver
  |   for transports    |     +-------+  +-------+  +-------+
  | - start/stop of     |                +-------+  +-------+
  |   transports        |     use        |       |  |       |
  | - RTCP management   +------------->--+       +--+       | sender
  | - interface to      |                +-------+  +-------+
  |   applications      |
  | - maintains streams |
  |                     |
  |                     |
  |                     |  creates and manages
  |                     +------------------------+
  |                     |                        |
  |                     +--------+               |
  |                     |        |               |
  |                     |    +---+---+       +---+---+  stream data:
  +---------------------+    |       |       |       |   - SSRC
                           +-------+ |     +-------+ |   - sequence no
                           |       |-+     |       |-+   - timestamp
                           |       |       |       |     - statistics
                           +-------+       +-------+
                                    Streams
                           RTP input       RTP output

 Figure 1: Data structure in Go RTP implementation


Figure 1 does not show the RTP / RTCP packet module that implements support
and management functions to handle RTP and RTCP packet data structures.


RTP data packets

The Go RTP stack implementation supports the necessary methods to access and
modify the contents and header fields of a RTP data packet. Most often
application only deal with the payload and timestamp of RTP data
packets. The RTP stack maintains the other fields and keeps track of them. 

The packet module implements a leaky buffer mechanism to reduce the number of
dynamically allocated packets. While not an absolute requirement it is advised
to use FreePacket() to return the packet to the packet queue because it
reduces the number of dynamic memory allocation and garbage collection
overhead. This might be an davantage to long running audio/video applications
which send a lot of data on long lasting RTP sessions.


Transport Modules

The Go RTP implementation uses stackable transport modules. The lowest layer
(nearest to the real network) implements the UDP, TCP or other network
transports. Each transport module must implement the interfaces TransportRecv
and/or TransportWrite if it acts a is a receiver oder sender module. This
separtion of interfaces enables an asymmetric transport stack as shown in
figure 1.

Usually upper layer transport modules filter incoming data or create some
specific output data, for example a ICE transport module would handle all ICE
relevant data and would not pass ICE data packets to the Session module
because the Session module expects RTP (SRTP in a later step) data only. The
Session module would discard all non-RTP or non-RTCP packets.

It is the client application's responsibility to build a transport stack
according to its requirements. The application uses the top level transport
module to initialize the RTP session. Usually an application uses only one
transport module. The following code snippet shows how this works:

  var localPort = 5220
  var local, _ = net.ResolveIPAddr("ip", "127.0.0.1")
  
  var remotePort = 5222
  var remote, _ = net.ResolveIPAddr("ip", "127.0.0.1")
  
  ...
  
  // Create a UDP transport with "local" address and use this for a "local" RTP session
  // The RTP session uses the transport to receive and send RTP packets to the remote peer.
  tpLocal, _ := rtp.NewTransportUDP(local, localPort)
 
  // TransportUDP implements TransportWrite and TransportRecv interfaces thus
  // use it as write and read modules for the Session.
  rsLocal = rtp.NewSession(tpLocal, tpLocal)
  
  ...
  
You may have noticed that the code does not use a standard Go UDP address but
separates the IP address and the port number. This separation makes it easier
to implement several network transport, such as UDP or TCP. The network
transport module creates its own address format. RTP requires two port
numbers, one for the data and the other one for the control connection. The
RFC 3550 specifies that ports with even numbers provide the data connection
(RTP), and the next following port number provides the control connection
(RTCP). Therefore the transport module requires only the port number of the
data connection.

An application may stack several transport modules. To do so an application
first creates the required transport modules. Then it connects them in the
following way:

   transportNet, _ := rtp.NewTransportUDP(local, localPort)
   transportAboveNet, _ := rtp.NewTransportWhatEver(....)

   // Register AboveNet as upper layer transport
   transportNet.SetCallUpper(transportAboveNet)

   // Register transportNet as lower layer
   transportAboveNet.SetToLower(transportNet)

The application then uses transportAboveNet to initialize the Session. If
transportAboveNet only filters or processes data in one direction then only
the relevant registration is required. For example if transportAboveNet
processes incoming data (receiving) then the application would perform the
first registration only and initializes the Session as follows:

  session := rtp.NewSession(transportNet, transportAboveNet)


Session and Streams

After an RTP application created the transports it can create a RTP
session. The RTP session requires a write and a read transport module to
manage the data traffic. The NewSession method just allocates and initializes
all necessary data structure but it does not start any data transfers. The
next steps are:

- allocate and initialize output stream(s)

- add the address(es) of the remote application(s) (peers) to the Session

- set up and connect the application's data processing to the Session's data
channel

- optionally set up and connect the application's control processing to the
Session's control event channel

After the application completed these steps it can start the Session. The next
paragraphs show each step in some more detail.


Allocate and initialize streams

The Session provides methods to creates an access output and input streams. An
application must create and initialize output streams, the RTP stack creates
input streams on-the-fly if it detects RTP or RTCP traffic for yet unknown
streams. Creation of a new ouput stream is simple. The application just calls

  strLocalIdx := rsLocal.NewSsrcStreamOut(&rtp.Address{local.IP, localPort, localPort + 1}, 0, 0)

to create a new ouput stream. The RTP stack requires the address to detect
collisions and loops during RTP data transfers. The other two parameters are
usually zero, only if the application want's to control the RTP SSRC and
starting sequence numbers it would set these parameters. To use the output
stream the application must set the payload type for this stream. Applications
use signaling protocol such as SIP or XMPP to negotiate the payload types. The
Go RTP implementation provides the standard set of predefined RTP payloads -
refer to the payload.go documentation. The example uses payload type 0,
standard PCM uLaw (PCMU):

  rsLocal.SsrcStreamOutForIndex(strLocalIdx).SetPayloadType(0)

An application shall always use the stream's index as returned during stream
creation to get the current stream and to call stream methods. The stream's
index does not change duringe the lifetime of a Session while the stream's
internal data structure and thus it's pointer may change. Such a change may
happen if the RTP stack detects a SSRC collision or loop and must reallocate
streams to solve collisions or loops.

Now the output stream is ready.


 Add addresses of remote application(s)

A Session can hold several remote addresses and it sends RTP and RTCP packets
to all known remote applications. To add a remote address the application may
perform:

  rsLocal.AddRemote(&rtp.Address{remote.IP, remotePort, remotePort + 1})


Connect to the RTP receiver

Before starting the RTP Session the application shall connect its main input
processing function the the Session's data receive channel. The next code
snippet show a very simple method that perfoms just this.

    func receivePacketLocal() {
        // Create and store the data receive channel.
        dataReceiver := rsLocal.CreateDataReceiveChan()
        var cnt int

        for {
            select {
            case rp := <-dataReceiver:
                if (cnt % 50) == 0 {
                    println("Remote receiver got:", cnt, "packets")
                }
                cnt++
                rp.FreePacket()
            case <-stopLocalRecv:
                return
            }
        }
    }

This simple function just counts packets and outputs a message. The second
select case is a local channel to stop the loop. The main RTP application
would do something like

  go receivePacketLocal()

to fire up the receiver function. 


Start the Session and perform RTP processing

After all the preparations it's now time to start the RTP Session. The
application just calls

  rsLocal.StartSession()

and the Session is up and running. This method starts the transports receiver
methods and also starts the interal RTCP service. The RTCP service sends some
RTCP information to all known remote peers to announce the application and its
output streams.

Once the applications started the Session it now may gather some data, for
example voice or video data, get a RTP data packet, fill in the gathered data
and send it to the remote peers. Thus an application would perform the
following steps

  payloadByteSlice := gatherPayload(....)
  rp := rsLocal.NewDataPacket(rtpTimestamp)
  rp.SetPayload(payloadByteSlice)
  rsLocal.WriteData(rp)

  rp.FreePacket()

The NewDataPacket method returns an initialized RTP data packet. The packet
contains the output stream's SSRC, the correct sequence number, and the
updated timestamp. The application must compute and provide the timestamp as
defined in RFC 3550, chapter 5.1. The RTP timestamp depends on the payload
attributes, such as sampling frequency, and on the number of data packets per
second. After the application wrote the RTP data packet it shall free the
packet as it helps to reduce the overhead of dynamic memory allocation.

The example uses PCMU and this payload has a sampling frequency of 8000Hz thus
its produces 8000 sampling values per second or 8 values per millisecond. Very
often an audio application sends a data packet every 20ms (50 packets per
second). Such a data packet contains 160 sampling values and the application
must increase the RTP timestamp by 160 for each packet. If an applications
uses other payload types it has to compute and maintain the correct RTP
timestamps and use them when it creates a new RTP data packet.

Some noteable features

* The current release V1.0.0 computes the RTCP intervals based on the length of
RTCP compound packets and the bandwidth allocated to RTCP. The application may
set the bandwidth, if no set GoRTP makes somes educated guesses.
  
* The application may set the maximum number of output and input streams even
while the RTP session is active. If the application des not set GoRTP sets
the values to 5 and 30 respectively.
  
* GoRTP produces SR and RR reports and the associated SDES for active streams
only, thus it implements the activity check as defined in chapter 6.4

* An appplication may use GoRTP in _simple RTP_ mode. In this mode only RTP
data packets are exchanged between the peers. No RTCP service is active, no
statistic counters, and GoRTP discards RTCP packets it receives.

* GoRTP limits the number of RR to 31 per RTCP report interval. GoRTP does not
add an additional RR packet in case it detects more than 31 active input
streams. This restriction is mainly due to MTU contraints of modern Ethernet
or DSL based networks. The MTU is usually less than 1500 bytes, GoRTP limits
the RTP/RTCP packet size to 1200 bytes. The length of an RR is 24 bytes,
thus 31 RR already require 774 bytes. Adding some data for SR and SDES fills
the rest. 

* An application may register to a control event channel and GoRTP delivers a
nice set of control and error events. The events cover:
  - Creation of a new input stream when receiving an RTP or RTCP packet and
    the SSRC was not known
  - RTCP events to inform about RTCP packets and received reports
  - Error events

* Currently GoRTP supports only SR, RR, SDES, and BYE RTCP packets. Inside 
SDES GoRTP does not support SDES Private and SDES H.323 items.


Further documentation

Beside the documentation of the global methods, functions, variables and constants I
also documented all internally used stuff. Thus if you need more information how it 
works or if you would like to enhance GoRTP please have a look in the sources.

*/
package rtp

/*
Internal documentation that for example describes the implementation goes here.
*/
