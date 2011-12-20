# RTP/RTCP stack for Go

This Go package implements a RTP/RTCP stack for Go. The package is a
sub-package of the standard Go _net_ package and uses standard _net_ package
functions. 

## How to build

The `rtp` package directory contains a standard Makefile. To build the package
just run `gomake` and then `gomake install`. Use `gotest` to execute the tests
check if the code works with the current Go installation on your system. It
should PASS. On my system I currently use `6g version weekly.2011-12-14
10879`. I usually update Go every two weeks or so and adapt the code if
necessary.

## How to use

This is a pure RTP / RTCP stack and it does not contain any media processing,
for example generating the payload for audio or video codecs.

The directory `src/cmd/rtpmain` contains an example Go program that performs a
RTP full duplex test on _localhost_ that shows how to setup a RTP session, an
output stream and how to send and receive RTP data and control events. Parts
of this program are used in the package documentation.

The software has a good beta status (IMHO) and should be ready to use for
smaller RTP applications. Standard point-to-point RTP applications should not
pose any problems. RTP multi-cast using IP multi-cast addresses is not
supported. If somebody really requires IP multi-cast it could be added at the
transport level.

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

## Some WARNING

Currently an application shall not use more than 31 output or input streams
per RTP session. This restriction is not yet enforced by the software but
overthrowing that limit will cause problems. Later versions will remove that
restriction and will handle more streams in a convenient way.

The RTCP reporting currently uses a fixed timer which is set to 5
seconds. Thus at the beginning of a session and then every 5 seconds the
internal RTCP service computes RCTP packets and send them to the known remote
applications. A variable timing that depends on the available bandwidth _may_
be implemented at a later time. However, given todays bandwidth this is not a
high priority issue. The RTCP reports contain the standard stuff like jitter
information, lost packet counters an alike.

Error handling an reporting will be enhanced during the next time - interface
and method signatures may change to include error handling :-) .
