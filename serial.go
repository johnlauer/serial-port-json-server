// Supports Windows, Linux, Mac, BeagleBone Black, and Raspberry Pi

package main

import (
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"strings"
)

type writeRequest struct {
	p      *serport
	d      []byte
	buffer bool
}

type qReport struct {
	Cmd  string
	QCnt int
	Type []string
	D    []string
	Port string
}

type serialhub struct {
	// Opened serial ports.
	ports map[*serport]bool

	//open chan *io.ReadWriteCloser
	//write chan *serport, chan []byte
	write chan writeRequest
	//read chan []byte

	// Register requests from the connections.
	register chan *serport

	// Unregister requests from connections.
	unregister chan *serport
}

type SpPortList struct {
	SerialPorts []SpPortItem
}

type SpPortItem struct {
	Name                      string
	Friendly                  string
	IsOpen                    bool
	Baud                      int
	RtsOn                     bool
	DtrOn                     bool
	BufferAlgorithm           string
	AvailableBufferAlgorithms []string
}

var sh = serialhub{
	//write:   	make(chan *serport, chan []byte),
	write:      make(chan writeRequest),
	register:   make(chan *serport),
	unregister: make(chan *serport),
	ports:      make(map[*serport]bool),
}

func (sh *serialhub) run() {

	log.Print("Inside run of serialhub")
	//s := ser.open()
	//ser.s := s
	//ser.write(s, []byte("hello serial data"))
	for {
		select {
		case p := <-sh.register:
			log.Print("Registering a port: ", p.portConf.Name)
			h.broadcastSys <- []byte("{\"Cmd\":\"Open\",\"Desc\":\"Got register/open on port.\",\"Port\":\"" + p.portConf.Name + "\",\"Baud\":" + strconv.Itoa(p.portConf.Baud) + ",\"BufferType\":\"" + p.BufferType + "\"}")
			//log.Print(p.portConf.Name)
			sh.ports[p] = true
		case p := <-sh.unregister:
			log.Print("Unregistering a port: ", p.portConf.Name)
			h.broadcastSys <- []byte("{\"Cmd\":\"Close\",\"Desc\":\"Got unregister/close on port.\",\"Port\":\"" + p.portConf.Name + "\",\"Baud\":" + strconv.Itoa(p.portConf.Baud) + "}")
			delete(sh.ports, p)
			close(p.sendBuffered)
			close(p.sendNoBuf)
		case wr := <-sh.write:
			//log.Print("Got a write to a port")
			//log.Print("Port: ", string(wr.p.portConf.Name))
			//log.Print(wr.p)
			//log.Print("Data is: ")
			//log.Println(wr.d)
			//log.Print("Data:" + string(wr.d))
			//log.Print("-----")
			log.Printf("Got write to port:%v, buffer:%v, data:%v", wr.p.portConf.Name, wr.buffer, string(wr.d))

			dataCmd := string(wr.d)

			// break the data into individual commands for queuing
			// this is important because:
			// 1) we could be sent multiple serial commands at once and the
			//    serial device may want them sent in smaller chunks to give
			//    better feedback. For example, if we're sent G0 X0\nG0 Y10\n we
			//    could happily send that to a CNC controller like a TinyG. However,
			//    on something like TinyG that would chew up 2 buffer planners. So,
			//    to better match what will happen, we break those into 2 commands
			//    so we get a better granularity of getting back qr responses or
			//    other feedback.
			// 2) we need to break apart specific commands potentially that do
			//    not need newlines. For example, on TinyG we need !~% to be different
			//    commands because we need to pivot off of what they mean. ! means pause
			//    the sending. So, we need that command as its own command in order of
			//    how they were sent to us.
			cmds := wr.p.bufferwatcher.BreakApartCommands(dataCmd)
			dataArr := []string{}
			bufTypeArr := []string{}
			for _, cmd := range cmds {

				// push this cmd onto dataArr for reporting
				dataArr = append(dataArr, cmd)

				// do extra check to see if certain command should wipe out
				// the entire internal serial port buffer we're holding in wr.p.sendBuffered
				wipeBuf := wr.p.bufferwatcher.SeeIfSpecificCommandsShouldWipeBuffer(cmd)
				if wipeBuf {
					log.Printf("We got a command that is asking us to wipe the sendBuffered buf. cmd:%v\n", cmd)
					// just wipe out the current channel and create new
					// hopefully garbage collection works here

					// close the channel
					//close(wr.p.sendBuffered)

					// consume all stuff queued
					func() {
						ctr := 0
						/*
							for data := range wr.p.sendBuffered {
								log.Printf("Consuming sendBuffered queue. d:%v\n", string(data))
								ctr++
							}*/

						keepLooping := true
						for keepLooping {
							select {
							case d, ok := <-wr.p.sendBuffered:
								log.Printf("Consuming sendBuffered queue. ok:%v, d:%v\n", ok, string(d))
								ctr++
								// since we just consumed a buffer item, we need to decrement bufcount
								// we are doing this artificially because we artifically threw
								// away what was in the bufer
								wr.p.itemsInBuffer--
								if ok == false {
									keepLooping = false
								}
							default:
								keepLooping = false
								log.Println("Hit default in select clause")
							}
						}
						log.Printf("Done consuming sendBuffered cmds. ctr:%v\n", ctr)
					}()

					// we still will likely have a sendBuffered that is in the BlockUntilRead()
					// that we have to deal with so it doesn't send to the serial port
					// when we release it

					//wr.p.sendBuffered = make(chan []byte, 256*100)
					// this is internally buffered thread to not send to serial port if blocked
					//go wr.p.writerBuffered()
					// send semaphore release if there is one on the BlockingUntilReady()
					wr.p.bufferwatcher.ReleaseLock()

					// let user know we wiped queue
					log.Printf("itemsInBuffer:%v\n", wr.p.itemsInBuffer)
					h.broadcastSys <- []byte("{\"Cmd\":\"WipedQueue\",\"QCnt\":" + strconv.Itoa(wr.p.itemsInBuffer) + ",\"Port\":\"" + wr.p.portConf.Name + "\"}")

				}

				// do extra check to see if any specific commands should pause
				// the buffer. this means we'll trigger a BlockUntilReady() block
				pauseBuf := wr.p.bufferwatcher.SeeIfSpecificCommandsShouldPauseBuffer(cmd)
				if pauseBuf {
					log.Printf("We need to pause our internal buffer.\n")
					wr.p.bufferwatcher.Pause()
				}

				// do extra check to see if any specific commands should unpause
				// the buffer. this means we'll release the BlockUntilReady() block
				unpauseBuf := wr.p.bufferwatcher.SeeIfSpecificCommandsShouldUnpauseBuffer(cmd)
				if unpauseBuf {
					log.Printf("We need to unpause our internal buffer.\n")
					wr.p.bufferwatcher.Unpause()
				}

				// do extra check to see if certain commands for this buffer type
				// should skip the internal serial port buffering
				// for example ! on tinyg and grbl should skip
				typeBuf := "" // set in if stmt below for reporting afterwards

				if wr.buffer {
					bufferSkip := wr.p.bufferwatcher.SeeIfSpecificCommandsShouldSkipBuffer(cmd)
					if bufferSkip {
						log.Printf("Forcing this cmd to skip buffer. cmd:%v\n", cmd)
						//wr.buffer = false
						typeBuf = "NoBuf"
					} else {
						typeBuf = "Buf"
					}
				} else {
					typeBuf = "NoBuf"
				}

				/*
					if wr.buffer {
						//log.Println("Send was normal send, so sending to wr.p.sendBuffered")
						//wr.p.sendBuffered <- []byte(cmd)
						typeBuf = "Buf"
					} else {
						//log.Println("Send was sendnobuf, so sending to wr.p.sendNoBuf")
						//wr.p.sendNoBuf <- []byte(cmd)
						typeBuf = "NoBuf"
					}
				*/
				// increment queue counter for reporting
				wr.p.itemsInBuffer++
				log.Printf("itemsInBuffer:%v\n", wr.p.itemsInBuffer)

				// push the type of this command to bufTypeArr
				bufTypeArr = append(bufTypeArr, typeBuf)

			} // for loop on broken apart commands

			qr := qReport{
				Cmd:  "Queued",
				Type: bufTypeArr,
				D:    cmds,
				QCnt: wr.p.itemsInBuffer,
				Port: wr.p.portConf.Name,
			}
			json, _ := json.Marshal(qr)
			h.broadcastSys <- json

			// now send off the commands to the appropriate channel
			for index, cmdToSendToChannel := range cmds {

				if bufTypeArr[index] == "Buf" {
					log.Println("Send was normal send, so sending to wr.p.sendBuffered")
					wr.p.sendBuffered <- []byte(cmdToSendToChannel)
				} else {
					log.Println("Send was sendnobuf, so sending to wr.p.sendNoBuf")
					wr.p.sendNoBuf <- []byte(cmdToSendToChannel)
				}
			}
		}
	}
}

func spList() {

	list, _ := getList()
	n := len(list)
	spl := SpPortList{make([]SpPortItem, n, n)}
	ctr := 0
	for _, item := range list {
		spl.SerialPorts[ctr] = SpPortItem{item.Name, item.FriendlyName, false, 0, false, false, "", availableBufferAlgorithms}

		// figure out if port is open
		//spl.SerialPorts[ctr].IsOpen = false
		myport, isFound := findPortByName(item.Name)

		if isFound {
			// we found our port
			spl.SerialPorts[ctr].IsOpen = true
			spl.SerialPorts[ctr].Baud = myport.portConf.Baud
			spl.SerialPorts[ctr].RtsOn = myport.portConf.RtsOn
			spl.SerialPorts[ctr].DtrOn = myport.portConf.DtrOn
			spl.SerialPorts[ctr].BufferAlgorithm = myport.BufferType
		}
		//ls += "{ \"name\" : \"" + item.Name + "\", \"friendly\" : \"" + item.FriendlyName + "\" },\n"
		ctr++
	}

	ls, err := json.MarshalIndent(spl, "", "\t")
	if err != nil {
		log.Println(err)
		h.broadcastSys <- []byte("Error creating json on port list " +
			err.Error())
	} else {
		//log.Print("Printing out json byte data...")
		//log.Print(ls)
		h.broadcastSys <- ls
	}
}

func spListOld() {
	ls := "{\"serialports\" : [\n"
	list, _ := getList()
	for _, item := range list {
		ls += "{ \"name\" : \"" + item.Name + "\", \"friendly\" : \"" + item.FriendlyName + "\" },\n"
	}
	ls = strings.TrimSuffix(ls, "},\n")
	ls += "}\n"
	ls += "]}\n"
	h.broadcastSys <- []byte(ls)
}

func spErr(err string) {
	log.Println("Sending err back: ", err)
	//h.broadcastSys <- []byte(err)
	h.broadcastSys <- []byte("{\"Error\" : \"" + err + "\"}")
}

func spClose(portname string) {
	// look up the registered port by name
	// then call the close method inside serialport
	// that should cause an unregister channel call back
	// to myself

	myport, isFound := findPortByName(portname)

	if isFound {
		// we found our port
		spHandlerClose(myport)
	} else {
		// we couldn't find the port, so send err
		spErr("We could not find the serial port " + portname + " that you were trying to close.")
	}
}

func spWrite(arg string) {
	// we will get a string of comXX asdf asdf asdf
	log.Println("Inside spWrite arg: " + arg)
	arg = strings.TrimPrefix(arg, " ")
	//log.Println("arg after trim: " + arg)
	args := strings.SplitN(arg, " ", 3)
	if len(args) != 3 {
		errstr := "Could not parse send command: " + arg
		log.Println(errstr)
		spErr(errstr)
		return
	}
	portname := strings.Trim(args[1], " ")
	//log.Println("The port to write to is:" + portname + "---")
	//log.Println("The data is:" + args[2] + "---")

	// see if we have this port open
	myport, isFound := findPortByName(portname)

	if !isFound {
		// we couldn't find the port, so send err
		spErr("We could not find the serial port " + portname + " that you were trying to write to.")
		return
	}

	// we found our port
	// create our write request
	var wr writeRequest
	wr.p = myport

	// see if args[0] is send or sendnobuf
	if args[0] != "sendnobuf" {
		// we were just given a "send" so buffer it
		wr.buffer = true
	} else {
		log.Println("sendnobuf specified so wr.buffer is false")
		wr.buffer = false
	}

	// include newline or not in the write? that is the question.
	// for now lets skip the newline
	//wr.d = []byte(args[2] + "\n")
	wr.d = []byte(args[2])

	// send it to the write channel
	sh.write <- wr

}

func findPortByName(portname string) (*serport, bool) {
	portnamel := strings.ToLower(portname)
	for port := range sh.ports {
		if strings.ToLower(port.portConf.Name) == portnamel {
			// we found our port
			//spHandlerClose(port)
			return port, true
		}
	}
	return nil, false
}

func spBufferAlgorithms() {
	//arr := []string{"default", "tinyg", "dummypause"}
	arr := availableBufferAlgorithms
	json := "{\"BufferAlgorithm\" : ["
	for _, elem := range arr {
		json += "\"" + elem + "\", "
	}
	json = regexp.MustCompile(", $").ReplaceAllString(json, "]}")
	h.broadcastSys <- []byte(json)
}

func spBaudRates() {
	arr := []string{"2400", "4800", "9600", "19200", "38400", "57600", "115200", "230400"}
	json := "{\"BaudRate\" : ["
	for _, elem := range arr {
		json += "" + elem + ", "
	}
	json = regexp.MustCompile(", $").ReplaceAllString(json, "]}")
	h.broadcastSys <- []byte(json)
}
