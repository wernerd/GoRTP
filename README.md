# RTP/RTCP stack for Go

This Go package implements a RTP/RTCP stack for Go. The package is a
sub-package of the standard Go _net_ package and uses standard _net_ package
functions.

## How to build

The _rtp_ sources use the GOPATH directory structure. To build, test, and run
the software just add the main goRTP directory to GOPATH. For further
information about this structure run `go help gopath` and follow the
instructions. The _rtp_ package is below the package _net_ to make clear that
_rtp_ is a network related package.

To build the package just run `go build net/rtp` and then `go install
net/rtp`. To excecute the tests just run `go test net/rtp`. The tests check if
the code works with the current Go installation on your system. It should
PASS.

A demo program is available and is called _rtpmain_. Use `go build
net/rtpmain` to build it. The command `go install net/rtpmain` installs it in
the `bin` directory of the main directory.

## How to use

This is a pure RTP / RTCP stack and it does not contain any media processing,
for example generating or packing the payload for audio or video codecs.

The directory `src/net/rtpmain` contains an example Go program that performs a
RTP some tests on _localhost_ that shows how to setup a RTP session, an
output stream and how to send and receive RTP data and control events. Parts
of this program are used in the package documentation.

The software should be ready to use for many RTP applications. Standard
point-to-point RTP applications should not pose any problems. RTP multi-cast
using IP multi-cast addresses is not supported. If somebody really requires IP
multi-cast it could be added at the transport level.

RTCP reporting works without support from application. The stack reports RTCP
packets and if the stack created new input streams and an application may
connect to the control channel to receive the RTCP events. Just have a look
into the example program. The RTCP fields in the stream structures are
accessible - however, to use them you may need to have some know-how of the
RTCP definitions and reporting.

## The documentation

After you downloaded the code you may use standard _godoc_ to get a nice
formatted documentation. Just change into the `src` directory, run `godoc
-http=:6060 -path="."`, point your browser at _localhost:6060_, and select
`src` at the top of the page.

I've added some package global documentation and tried to document the
globally visible methods and functions.

Before you start hacking please have a look into the documentation first, in
particular the package documentation (doc.go).

## Some noteable features

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
  or DSL based networks. The MTU is usually about 1500 bytes, GoRTP limits
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
