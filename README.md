serial-port-json-server
=======================
Version 1.3

A serial port JSON websocket &amp; web server that runs from the command line on 
Windows, Mac, Linux, Raspberry Pi, or Beagle Bone that lets you communicate with your serial 
port from a web application. This enables web apps to be written that can 
communicate with your local serial device such as an Arduino, CNC controller, or 
any device that communicates over the serial port.

The app is written in Go. It has an embedded web server and websocket server.
The server runs on the standard port of localhost:8989. You can connect to
it locally with your browser to interact by visiting http://localhost:8989.
The websocket is technically running at ws://localhost/ws.

Supported commands are:
	list
	open [portName] [baud]
	close [portName]
	send [portName] [cmd]

The app is one executable with everything you need and is available ready-to-go
for every major platform.

If you are a web developer and want to write a web application that connects
to somebody's local or remote serial port server, then you simply need to create a 
websocket connection to the localhost or remote host and you will be directly 
interacting with that user's serial port.

For example, if you wanted to create a Gcode Sender web app to enable people to send
3D print or milling commands from your site, this would be a perfect use case. Or if
you've created an oscilloscope web app that connects to an Arduino, it would be another
great use case. Finally you can write web apps that interact with a user's local hardware.

Thanks go to gary.burd.info for the websocket example in Go. Thanks also go to 
tarm/goserial for the serial port base implementation.

How to Build
---------
1. Install Go (http://golang.org/doc/install)
2. If you're on a Mac, install Xcode from the Apple Store.
   If you're on Windows, Linux, Raspberry Pi, or Beagle Bone you are all set.
3. Get go into your path so you can run "go" from any directory:
	On Linux, Mac, Raspberry Pi, Beagle Bone Black
	export PATH=$PATH:/usr/local/go/bin
	On Windows, use the Environment Variables dialog by right-click My Computer
4. Define your GOPATH variable. This is your personal working folder for all your
Go code. This is important because you will be retrieving several projects
from Github and Go needs to know where to download all the files and where to 
build the directory structure. On my Windows computer I created a folder called
C:\Users\John\go and set GOPATH=C:\Users\John\go
5. Change directory into your GOPATH
6. Type "go get github.com/johnlauer/serial-port-json-server". This will retrieve
this Github project and all dependent projects.
7. Then change direcory into github.com\johnlauer\serial-port-json-server. 
8. Type "go build" when you're inside that directory and it will create a binary 
called serial-port-json-server
9. Run it by typing ./serial-port-json-server


Changes in 1.3
- Added ability for buffer flow plugins. There is a new buffer flow plugin 
  for TinyG that watches the {"qr":NN} response. When it sees the qr value
  go below 12 it pauses its own sending and queues up whatever is still coming
  in on the Websocket. This is fine because we've got plenty of RAM on the 
  websocket server. The {"qr":NN} value is still sent back on the websocket as
  soon as it was before, so the host application should see no real difference
  as to how it worked before. The difference now though is that the serial sending
  knows to check if sending is paused to the serial port and queue. This makes
  sure no buffer overflows ever occur. The reason this was becoming important is
  that the lag time between the qr response and the sending of Gcode was too distant
  and this buffer flow needs resolution around 5ms. Normal latency on the Internet
  is like 20ms to 200ms, so it just wasn't fast enough. If the Javascript hosting
  the websocket was busy processing other events, then this lag time became even 
  worse. So, now the Serial Port JSON Server simply helps out by lots of extra
  buffering. Go ahead and pound it even harder with more serial commands and see 
  it fly.

Changes in 1.2
- Added better error handling
- Removed forcibly adding a newline to the serial data being sent to the port. This
  means apps must send in a newline if the serial port expects it.
- Embedded the home.html file inside the binary so there is no longer a dependency
  on an external file.
- TODO: Closing a port on Beagle Bone seems to hang. Only solution now is to kill
  the process and restart.
- TODO: Mac implementation seems to have trouble on writing data after a while. Mac
  gray screen of death can appear. Mac version uses CGO, so it is in unsafe mode.
  May have to rework Mac serial port to use pure golang code.
