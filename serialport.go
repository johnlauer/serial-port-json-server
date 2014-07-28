package main

import (
	"bytes"
	"encoding/json"
	"github.com/johnlauer/goserial"
	"io"
	"log"
	"strconv"
)

type serport struct {
	// The serial port connection.
	portConf *serial.Config
	portIo   io.ReadWriteCloser

	// Keep track of whether we're being actively closed
	// just so we don't show scary error messages
	isClosing bool

	// Buffered channel of outbound messages.
	send chan []byte

	// Do we have an extra channel/thread to watch our buffer?
	BufferType string
	//bufferwatcher *BufferflowDummypause
	bufferwatcher Bufferflow
}

type SpPortMessage struct {
	P string // the port, i.e. com22
	D string // the data, i.e. G0 X0 Y0
}

func (p *serport) reader() {
	//var buf bytes.Buffer
	for {
		ch := make([]byte, 1024)
		n, err := p.portIo.Read(ch)

		// read can return legitimate bytes as well as an error
		// so process the bytes if n > 0
		if n > 0 {
			//log.Print("Read " + strconv.Itoa(n) + " bytes ch: " + string(ch))
			data := string(ch[:n])
			//log.Print("The data i will convert to json is:")
			//log.Print(data)

			// give the data to our bufferflow so it can do it's work
			// to read/translate the data to see if it wants to block
			// writes to the serialport. each bufferflow type will decide
			// this on its own based on its logic, i.e. tinyg vs grbl vs others
			//p.b.bufferwatcher..OnIncomingData(data)
			p.bufferwatcher.OnIncomingData(data)

			//m := SpPortMessage{"Alice", "Hello"}
			m := SpPortMessage{p.portConf.Name, data}
			//log.Print("The m obj struct is:")
			//log.Print(m)

			b, err := json.MarshalIndent(m, "", "\t")
			if err != nil {
				log.Println(err)
				h.broadcastSys <- []byte("Error creating json on " + p.portConf.Name + " " +
					err.Error() + " The data we were trying to convert is: " + string(ch[:n]))
				break
			}
			//log.Print("Printing out json byte data...")
			//log.Print(string(b))
			h.broadcastSys <- b
			//h.broadcastSys <- []byte("{ \"p\" : \"" + p.portConf.Name + "\", \"d\": \"" + string(ch[:n]) + "\" }\n")
		}

		if p.isClosing {
			strmsg := "Shutting down reader on " + p.portConf.Name
			log.Println(strmsg)
			h.broadcastSys <- []byte(strmsg)
			break
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			// hit end of file
			log.Println("Hit end of file on serial port")
		}
		if err != nil {
			log.Println(err)
			h.broadcastSys <- []byte("Error reading on " + p.portConf.Name + " " +
				err.Error() + " Closing port.")
			break
		}

		// loop thru and look for a newline
		/*
			for i := 0; i < n; i++ {
				// see if we hit a newline
				if ch[i] == '\n' {
					// we are done with the line
					h.broadcastSys <- buf.Bytes()
					buf.Reset()
				} else {
					// append to buffer
					buf.WriteString(string(ch[:n]))
				}
			}*/
		/*
			buf.WriteString(string(ch[:n]))
			log.Print(string(ch[:n]))
			if string(ch[:n]) == "\n" {
				h.broadcastSys <- buf.Bytes()
				buf.Reset()
			}
		*/
	}
	p.portIo.Close()
}

// this method runs as its own thread because it's instantiated
// as a "go" method. so if it blocks inside, it is ok
func (p *serport) writer() {
	// this for loop blocks on p.send until that channel
	// sees something come in
	for data := range p.send {

		// we want to block here if we are being asked
		// to pause. the problem is, how do we unblock
		//bufferBlockUntilReady(p.bufferwatcher)
		p.bufferwatcher.BlockUntilReady()

		n2, err := p.portIo.Write(data)

		// if we get here, we were able to write successfully
		// to the serial port because it blocks until it can write

		h.broadcastSys <- []byte("{\"Cmd\" : \"WriteComplete\", \"Bytes\" : " + strconv.Itoa(n2) + ", \"Desc\" : \"Completed write on port.\", \"Port\" : \"" + p.portConf.Name + "\"}")

		log.Print("Just wrote ", n2, " bytes to serial: ", string(data))
		//log.Print(n2)
		//log.Print(" bytes to serial: ")
		//log.Print(data)
		if err != nil {
			errstr := "Error writing to " + p.portConf.Name + " " + err.Error() + " Closing port."
			log.Print(errstr)
			h.broadcastSys <- []byte(errstr)
			break
		}
	}
	msgstr := "Shutting down writer on " + p.portConf.Name
	log.Println(msgstr)
	h.broadcastSys <- []byte(msgstr)
	p.portIo.Close()
}

func spHandlerOpen(portname string, baud int, buftype string) {

	log.Print("Inside spHandler")

	var out bytes.Buffer

	out.WriteString("Opening serial port ")
	out.WriteString(portname)
	out.WriteString(" at ")
	out.WriteString(strconv.Itoa(baud))
	out.WriteString(" baud")
	log.Print(out.String())

	//h.broadcast <- []byte("Opened a serial port bitches")
	h.broadcastSys <- out.Bytes()

	conf := &serial.Config{Name: portname, Baud: baud, RtsOn: true}
	log.Print("Created config for port")
	log.Print(conf)

	sp, err := serial.OpenPort(conf)
	log.Print("Just tried to open port")
	if err != nil {
		//log.Fatal(err)
		log.Print("Error opening port " + err.Error())
		//h.broadcastSys <- []byte("Error opening port. " + err.Error())
		h.broadcastSys <- []byte("{\"Cmd\" : \"OpenFail\", \"Desc\" : \"Error opening port. " + err.Error() + "\", \"Port\" : \"" + conf.Name + "\", \"Baud\" : " + strconv.Itoa(conf.Baud) + " }")

		return
	}
	log.Print("Opened port successfully")
	//p := &serport{send: make(chan []byte, 256), portConf: conf, portIo: sp}
	p := &serport{send: make(chan []byte, 256*100), portConf: conf, portIo: sp, BufferType: buftype}

	// if user asked for a buffer watcher, i.e. tinyg/grbl then attach here
	if buftype != "" {

		if buftype == "tinyg" {
			bw := &BufferflowTinyg{Name: "no name needed"}
			bw.Init()
			p.bufferwatcher = bw
		}
		//p.bufferwatcher := &bufferflow{buffertype: buftype}
		//p.bufferwatcher.buffertype = buftype

		// could open the buffer thread here, or do it when this
		// port is registered. the buffer thread will watch the writer
		// and the reader. it will look at the content and decide
		// if a pause must occur

	} else {
		// for now, just use a dummy pause type bufferflow object
		// to test artificially a delay on the serial port write
		//p.bufferwatcher.buffertype = "dummypause"
		//p.bufferwatcher.BlockUntilReady()
		bw := &BufferflowDummypause{Name: "blah"}
		p.bufferwatcher = bw
		//p.bufferwatcher.Name = "blah2"

	}

	sh.register <- p
	defer func() { sh.unregister <- p }()
	go p.writer()
	p.reader()
}

func spHandlerClose(p *serport) {
	p.isClosing = true
	// close the port
	p.portIo.Close()
	// unregister myself
	// we already have a deferred unregister in place from when
	// we opened. the only thing holding up that thread is the p.reader()
	// so if we close the reader we should get an exit
	h.broadcastSys <- []byte("Closing serial port " + p.portConf.Name)
}
