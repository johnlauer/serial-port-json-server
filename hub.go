package main

import (
	"fmt"
	"log"

	"github.com/kardianos/osext"
	//"os"
	"os/exec"
	//"path"
	//"path/filepath"
	//"runtime"
	//"debug"
	"encoding/json"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

type hub struct {
	// Registered connections.
	connections map[*connection]bool

	// Inbound messages from the connections.
	broadcast chan []byte

	// Inbound messages from the system
	broadcastSys chan []byte

	// Register requests from the connections.
	register chan *connection

	// Unregister requests from connections.
	unregister chan *connection
}

var h = hub{
	// buffered. go with 1000 cuz should never surpass that
	broadcast:    make(chan []byte, 1000),
	broadcastSys: make(chan []byte, 1000),
	// non-buffered
	//broadcast:    make(chan []byte),
	//broadcastSys: make(chan []byte),
	register:    make(chan *connection),
	unregister:  make(chan *connection),
	connections: make(map[*connection]bool),
}

func (h *hub) run() {
	for {
		select {
		case c := <-h.register:
			h.connections[c] = true
			// send supported commands
			c.send <- []byte("{\"Version\" : \"" + version + "\"} ")
			c.send <- []byte("{\"Commands\" : [\"list\", \"open [portName] [baud] [bufferAlgorithm (optional)]\", \"send [portName] [cmd]\", \"sendnobuf [portName] [cmd]\", \"sendjson {P:portName, Data:[{D:cmdStr, Id:idStr}]}\",  \"close [portName]\", \"bufferalgorithms\", \"baudrates\", \"restart\", \"exit\", \"broadcast [anythingToRegurgitate]\", \"hostname\", \"version\", \"program [portName] [core:architecture:name] [path/to/binOrHexFile]\", \"programfromurl [portName] [core:architecture:name] [urlToBinOrHexFile]\", \"execruntime\", \"exec [command] [arg1] [arg2] [...]\"]} ")
			c.send <- []byte("{\"Hostname\" : \"" + *hostname + "\"} ")
		case c := <-h.unregister:
			delete(h.connections, c)
			// put close in func cuz it was creating panics and want
			// to isolate
			func() {
				// this method can panic if websocket gets disconnected
				// from users browser and we see we need to unregister a couple
				// of times, i.e. perhaps from incoming data from serial triggering
				// an unregister. (NOT 100% sure why seeing c.send be closed twice here)
				defer func() {
					if e := recover(); e != nil {
						log.Println("Got panic: ", e)
					}
				}()
				close(c.send)
			}()
		case m := <-h.broadcast:
			//log.Print("Got a broadcast")
			//log.Print(m)
			//log.Print(len(m))
			if len(m) > 0 {
				//log.Print(string(m))
				//log.Print(h.broadcast)
				checkCmd(m)
				//log.Print("-----")

				for c := range h.connections {
					select {
					case c.send <- m:
						//log.Print("did broadcast to ")
						//log.Print(c.ws.RemoteAddr())
						//c.send <- []byte("hello world")
					default:
						delete(h.connections, c)
						close(c.send)
						go c.ws.Close()
					}
				}
			}
		case m := <-h.broadcastSys:
			//log.Printf("Got a system broadcast: %v\n", string(m))
			//log.Print(string(m))
			//log.Print("-----")

			for c := range h.connections {
				select {
				case c.send <- m:
					//log.Print("did broadcast to ")
					//log.Print(c.ws.RemoteAddr())
					//c.send <- []byte("hello world")
				default:
					delete(h.connections, c)
					close(c.send)
					go c.ws.Close()
				}
			}
		}
	}
}

func checkCmd(m []byte) {
	//log.Print("Inside checkCmd")
	s := string(m[:])
	log.Print(s)

	sl := strings.ToLower(s)

	if strings.HasPrefix(sl, "open") {

		// check if user wants to open this port as a secondary port
		// this doesn't mean much other than allowing the UI to show
		// a port as primary and make other ports sort of act less important
		isSecondary := false
		if strings.HasPrefix(s, "open secondary") {
			isSecondary = true
			// swap out the word secondary
			s = strings.Replace(s, "open secondary", "open", 1)
		}

		// remove newline
		args := strings.Split(strings.TrimSpace(s), " ")
		if len(args) < 3 {
			go spErr("You did not specify a port and baud rate in your open cmd")
			return
		}
		if len(args[1]) < 1 {
			go spErr("You did not specify a serial port")
			return
		}

		baudStr := strings.Replace(args[2], "\n", "", -1)
		baud, err := strconv.Atoi(baudStr)
		if err != nil {
			go spErr("Problem converting baud rate " + args[2])
			return
		}
		// pass in buffer type now as string. if user does not
		// ask for a buffer type pass in empty string
		bufferAlgorithm := ""
		if len(args) > 3 {
			// cool. we got a buffer type request
			buftype := strings.Replace(args[3], "\n", "", -1)
			bufferAlgorithm = buftype
		}
		go spHandlerOpen(args[1], baud, bufferAlgorithm, isSecondary)

	} else if strings.HasPrefix(sl, "close") {

		log.Printf("About to split close commands. cmd:\"%v\"", s)
		// remove newline
		args := strings.Split(strings.TrimSpace(s), " ")
		//args := strings.Split(s, " ")
		log.Printf("The split args for close:%v", args)
		if len(args) > 1 {
			go spClose(args[1])
		} else {
			go spErr("You did not specify a port to close")
		}

	} else if strings.HasPrefix(sl, "programkill") {

		// kill the running process (assumes singleton for now)
		go spHandlerProgramKill()

	} else if strings.HasPrefix(sl, "programfromurl") {

		args := strings.Split(s, " ")
		if len(args) == 4 {
			go spProgramFromUrl(args[1], args[2], args[3])
		} else {
			go spErr("You did not specify a port, a board to program and/or a URL")
		}

	} else if strings.HasPrefix(sl, "program") {

		args := strings.Split(s, " ")
		if len(args) > 3 {
			var slice []string = args[3:len(args)]
			go spProgram(args[1], args[2], strings.Join(slice, " "))
		} else {
			go spErr("You did not specify a port, a board to program and/or a filename")
		}

	} else if strings.HasPrefix(sl, "sendjson") {
		// will catch sendjson

		go spWriteJson(s)

	} else if strings.HasPrefix(sl, "send") {
		// will catch send and sendnobuf

		//args := strings.Split(s, "send ")
		go spWrite(s)

	} else if strings.HasPrefix(sl, "list") {
		go spList()
		//go getListViaWmiPnpEntity()
	} else if strings.HasPrefix(sl, "fro") {
		// User is wanting us to tweak the feedrate on-the-fly
		go spFeedRateOverride(s)

	} else if strings.HasPrefix(sl, "bufferalgorithm") {
		go spBufferAlgorithms()
	} else if strings.HasPrefix(sl, "baudrate") {
		go spBaudRates()
	} else if strings.HasPrefix(sl, "broadcast") {
		go broadcast(s)
	} else if strings.HasPrefix(sl, "restart") {
		restart()
	} else if strings.HasPrefix(sl, "exit") {
		exit()
	} else if strings.HasPrefix(sl, "memstats") {
		memoryStats()
	} else if strings.HasPrefix(sl, "gc") {
		garbageCollection()
	} else if strings.HasPrefix(sl, "bufflowdebug") {
		bufflowdebug(sl)
	} else if strings.HasPrefix(sl, "hostname") {
		getHostname()
	} else if strings.HasPrefix(sl, "version") {
		getVersion()
	} else if strings.HasPrefix(sl, "execruntime") {
		execRuntime()
	} else if strings.HasPrefix(sl, "exec") {
		go execRun(s)
	} else if strings.HasPrefix(sl, "cayenn-sendudp") {
		cayennSendUdp(s)
	} else if strings.HasPrefix(sl, "cayenn-sendtcp") {
		cayennSendTcp(s)
	} else if strings.HasPrefix(sl, "usblist") {
		SendUsbList()
		/*
			} else if strings.HasPrefix(sl, "gethost") {
				hostname, err := gpio.Host()
				if err != nil {
					go h.sendErr(err.Error())
				}
				go h.sendMsg("Host", hostname)

			} else if strings.HasPrefix(sl, "getpinmap") {
				pinMap, err := gpio.PinMap()
				if err != nil {
					go h.sendErr(err.Error())
				}
				go h.sendMsg("PinMap", pinMap)
			} else if strings.HasPrefix(sl, "getpinstates") {
				pinStates, err := gpio.PinStates()
				if err != nil {
					go h.sendErr(err.Error())
				}
				go h.sendMsg("PinStates", pinStates)

			} else if strings.HasPrefix(sl, "initpin") {
				// format : setpin pinId dir pullup
				args := strings.Split(s, " ")
				if len(args) < 4 {
					go h.sendErr("You did not specify a pin and a direction [0|1|low|high] and a name")
					return
				}
				if len(args[1]) < 1 {
					go h.sendErr("You did not specify a pin")
					return
				}
				pin := args[1]
				dirStr := args[2]
				name := args[4]
				dir := In
				switch {
				case dirStr == "1" || dirStr == "out" || dirStr == "output":
					dir = Out
				case dirStr == "0" || dirStr == "in" || dirStr == "input":
					dir = In
				case dirStr == "pwm":
					dir = PWM
				}
				pullup := Pull_None
				switch {
				case args[3] == "1" || args[3] == "up":
					pullup = Pull_Up
				case args[3] == "0" || args[3] == "down":
					pullup = Pull_Down
				}
				err := gpio.PinInit(pin, dir, pullup, name)
				if err != nil {
					go h.sendErr(err.Error())
				}
			} else if strings.HasPrefix(sl, "removepin") {
				// format : removepin pinId
				args := strings.Split(s, " ")
				if len(args) < 2 {
					go h.sendErr("You did not specify a pin id")
					return
				}
				err := gpio.PinRemove(args[1])
				if err != nil {
					go h.sendErr(err.Error())
				}
			} else if strings.HasPrefix(sl, "setpin") {
				// format : setpin pinId high/low/1/0
				args := strings.Split(s, " ")
				if len(args) < 3 {
					go h.sendErr("You did not specify a pin and a state [0|1|low|high]")
					return
				}
				if len(args[1]) < 1 {
					go h.sendErr("You did not specify a pin")
					return
				}
				pin := args[1]
				stateStr := args[2]
				state := 0
				switch {
				case stateStr == "1" || stateStr == "high":
					state = 1
				case stateStr == "0" || stateStr == "low":
					state = 0
				default:
					// assume its a pwm value...if it converts to integer in 0-255 range
					s, err := strconv.Atoi(stateStr)
					if err != nil {
						go h.sendErr("Invalid value, must be between 0 and 255 : " + stateStr)
						return
					}
					if s < 0 || s > 255 {
						go h.sendErr("Invalid value, must be between 0 and 255 : " + stateStr)
						return
					}
					state = s
				}

				err := gpio.PinSet(pin, byte(state))
				if err != nil {
					go h.sendErr(err.Error())
				}
		*/
	} else {
		go spErr("Could not understand command.")
	}

	//log.Print("Done with checkCmd")
}

func bufflowdebug(sl string) {
	log.Println("bufflowdebug start")
	if strings.HasPrefix(sl, "bufflowdebug on") {
		*bufFlowDebugType = "on"
	} else if strings.HasPrefix(sl, "bufflowdebug off") {
		*bufFlowDebugType = "off"
	}
	h.broadcastSys <- []byte("{\"BufFlowDebug\" : \"" + *bufFlowDebugType + "\"}")
	log.Println("bufflowdebug end")
}

func memoryStats() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	json, _ := json.Marshal(memStats)
	log.Printf("memStats:%v\n", string(json))
	h.broadcastSys <- json
}

func getHostname() {
	h.broadcastSys <- []byte("{\"Hostname\" : \"" + *hostname + "\"}")
}

func getVersion() {
	h.broadcastSys <- []byte("{\"Version\" : \"" + version + "\"}")
}

func garbageCollection() {
	log.Printf("Starting garbageCollection()\n")
	h.broadcastSys <- []byte("{\"gc\":\"starting\"}")
	memoryStats()
	debug.SetGCPercent(100)
	debug.FreeOSMemory()
	debug.SetGCPercent(-1)
	log.Printf("Done with garbageCollection()\n")
	h.broadcastSys <- []byte("{\"gc\":\"done\"}")
	memoryStats()
}

func exit() {
	log.Println("Starting new spjs process")
	h.broadcastSys <- []byte("{\"Exiting\" : true}")
	log.Fatal("Exited current spjs cuz asked to")

}

func restart() {
	// relaunch ourself and exit
	// the relaunch works because we pass a cmdline in
	// that has serial-port-json-server only initialize 5 seconds later
	// which gives us time to exit and unbind from serial ports and TCP/IP
	// sockets like :8989
	log.Println("Starting new spjs process")
	h.broadcastSys <- []byte("{\"Restarting\" : true}")

	// figure out current path of executable so we know how to restart
	// this process
	/*
		dir, err2 := filepath.Abs(filepath.Dir(os.Args[0]))
		if err2 != nil {
			//log.Fatal(err2)
			fmt.Printf("Error getting executable file path. err: %v\n", err2)
		}
		fmt.Printf("The path to this exe is: %v\n", dir)

		// alternate approach
		_, filename, _, _ := runtime.Caller(1)
		f, _ := os.Open(path.Join(path.Dir(filename), "serial-port-json-server"))
		fmt.Println(f)
	*/

	// using osext
	exePath, err3 := osext.Executable()
	if err3 != nil {
		fmt.Printf("Error getting exe path using osext lib. err: %v\n", err3)
	}
	fmt.Printf("exePath using osext: %v\n", exePath)

	// figure out garbageCollection flag
	//isGcFlag := "false"

	var cmd *exec.Cmd
	/*if *isGC {
		//isGcFlag = "true"
		cmd = exec.Command(exePath, "-ls", "-addr", *addr, "-regex", *regExpFilter, "-gc")
	} else {
		cmd = exec.Command(exePath, "-ls", "-addr", *addr, "-regex", *regExpFilter)

	}*/
	cmd = exec.Command(exePath, "-ls", "-addr", *addr, "-regex", *regExpFilter, "-gc", *gcType)

	//cmd := exec.Command("./serial-port-json-server", "ls")
	err := cmd.Start()
	if err != nil {
		log.Printf("Got err restarting spjs: %v\n", err)
		h.broadcastSys <- []byte("{\"Error\" : \"" + fmt.Sprintf("%v", err) + "\"}")
	} else {
		h.broadcastSys <- []byte("{\"Restarted\" : true}")
	}
	log.Fatal("Exited current spjs for restart")
	//log.Printf("Waiting for command to finish...")
	//err = cmd.Wait()
	//log.Printf("Command finished with error: %v", err)
}

type CmdBroadcast struct {
	Cmd string
	Msg string
}

func broadcast(arg string) {
	// we will get a string of broadcast asdf asdf asdf
	log.Println("Inside broadcast arg: " + arg)
	arg = strings.TrimPrefix(arg, " ")
	//log.Println("arg after trim: " + arg)
	args := strings.SplitN(arg, " ", 2)
	if len(args) != 2 {
		errstr := "Could not parse broadcast command: " + arg
		log.Println(errstr)
		spErr(errstr)
		return
	}
	broadcastcmd := strings.Trim(args[1], " ")
	log.Println("The broadcast cmd is:" + broadcastcmd + "---")

	bcmd := CmdBroadcast{
		Cmd: "Broadcast",
		Msg: broadcastcmd,
	}
	json, _ := json.Marshal(bcmd)
	log.Printf("bcmd:%v\n", string(json))
	h.broadcastSys <- json

}

func (h *hub) sendErr(msg string) {
	msgMap := map[string]string{"error": msg}
	log.Println("Error: " + msg)
	bytes, err := json.Marshal(msgMap)
	if err != nil {
		log.Println("Failed to marshal data!")
		return
	}
	h.broadcastSys <- bytes
}

func (h *hub) sendMsg(name string, msg interface{}) {
	msgMap := make(map[string]interface{})
	msgMap[name] = msg
	msgMap["Type"] = name
	//log.Println("Sent: " + name)
	bytes, err := json.Marshal(msgMap)
	if err != nil {
		log.Println("Failed to marshal data!")
		return
	}
	h.broadcastSys <- bytes
}
