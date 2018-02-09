// Version 1.95
// Supports Windows, Linux, Mac, and Raspberry Pi, Beagle Bone Black

package main

import (
	"flag"
	"go/build"
	"log"
	"net/http"
	//"path/filepath"
	"errors"
	"fmt"
	"net"
	"os"
	//"net/http/pprof"
	//"runtime"
	"io"
	"runtime/debug"
	"text/template"
	"time"
)

var (
	version      = "1.95"
	versionFloat = float32(1.95)
	addr         = flag.String("addr", ":8989", "http service address. example :8800 to run on port 8800, example 10.0.0.2:9000 to run on specific IP address and port, example 10.0.0.2 to run on specific IP address")
	//	addr  = flag.String("addr", ":8980", "http service address. example :8800 to run on port 8800, example 10.0.0.2:9000 to run on specific IP address and port, example 10.0.0.2 to run on specific IP address")
	saddr = flag.String("saddr", ":8990", "https service address. example :8801 to run https on port 8801")
	scert = flag.String("scert", "cert.pem", "https certificate file")
	skey  = flag.String("skey", "key.pem", "https key file")
	//assets       = flag.String("assets", defaultAssetPath(), "path to assets")
	//	verbose = flag.Bool("v", true, "show debug logging")
	verbose = flag.Bool("v", false, "show debug logging")
	//homeTempl *template.Template
	isLaunchSelf = flag.Bool("ls", false, "Launch self 5 seconds later. This flag is used when you ask for a restart from a websocket client.")
	isAllowExec  = flag.Bool("allowexec", false, "Allow terminal commands to be executed (default false)")

	// regular expression to sort the serial port list
	// typically this wouldn't be provided, but if the user wants to clean
	// up their list with a regexp so it's cleaner inside their end-user interface
	// such as ChiliPeppr, this can make the massive list that Linux gives back
	// to you be a bit more manageable
	regExpFilter = flag.String("regex", "", "Regular expression to filter serial port list, i.e. -regex usb|acm")

	// allow garbageCollection()
	//isGC = flag.Bool("gc", false, "Is garbage collection on? Off by default.")
	//isGC = flag.Bool("gc", true, "Is garbage collection on? Off by default.")
	gcType = flag.String("gc", "std", "Type of garbage collection. std = Normal garbage collection allowing system to decide (this has been known to cause a stop the world in the middle of a CNC job which can cause lost responses from the CNC controller and thus stalled jobs. use max instead to solve.), off = let memory grow unbounded (you have to send in the gc command manually to garbage collect or you will run out of RAM eventually), max = Force garbage collection on each recv or send on a serial port (this minimizes stop the world events and thus lost serial responses, but increases CPU usage)")

	// whether to do buffer flow debugging
	bufFlowDebugType = flag.String("bufflowdebug", "off", "off = (default) We do not send back any debug JSON, on = We will send back a JSON response with debug info based on the configuration of the buffer flow that the user picked")

	// hostname. allow user to override, otherwise we look it up
	hostname = flag.String("hostname", "unknown-hostname", "Override the hostname we get from the OS")

	// turn off cayenn
	isDisableCayenn = flag.Bool("disablecayenn", false, "Disable loading of Cayenn TCP/UDP server on port 8988")
	//	isLoadCayenn = flag.Bool("allowcayenn", false, "Allow loading of Cayenn TCP/UDP server on port 8988")

	createScript = flag.Bool("createstartupscript", false, "Create an /etc/init.d/serial-port-json-server startup script. Available only on Linux.")

//	createScript = flag.Bool("createstartupscript", true, "Create an /etc/init.d/serial-port-json-server startup script. Available only on Linux.")
)

type NullWriter int

func (NullWriter) Write([]byte) (int, error) { return 0, nil }

func defaultAssetPath() string {
	//p, err := build.Default.Import("gary.burd.info/go-websocket-chat", "", build.FindOnly)
	p, err := build.Default.Import("github.com/johnlauer/serial-port-json-server", "", build.FindOnly)
	if err != nil {
		return "."
	}
	return p.Dir
}

func homeHandler(c http.ResponseWriter, req *http.Request) {
	homeTemplate.Execute(c, req.Host)
}

func launchSelfLater() {
	log.Println("Going to launch myself 5 seconds later.")
	time.Sleep(2 * 1000 * time.Millisecond)
	log.Println("Done waiting 5 secs. Now launching...")
}

func main() {

	// Test USB list
	//	GetUsbList()

	// parse all passed in command line arguments
	flag.Parse()

	// setup logging
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// see if we are supposed to wait 5 seconds
	if *isLaunchSelf {
		launchSelfLater()
	}

	// see if they want to just create startup script
	if *createScript {
		createStartupScript()
		return
	}

	//getList()
	log.Println("Version:" + version)

	// hostname
	hn, _ := os.Hostname()
	if *hostname == "unknown-hostname" {
		*hostname = hn
	}
	log.Println("Hostname:", *hostname)

	// turn off garbage collection
	// this is dangerous, as u could overflow memory
	//if *isGC {
	if *gcType == "std" {
		log.Println("Garbage collection is on using Standard mode, meaning we just let Golang determine when to garbage collect.")
	} else if *gcType == "max" {
		log.Println("Garbage collection is on for MAXIMUM real-time collecting on each send/recv from serial port. Higher CPU, but less stopping of the world to garbage collect since it is being done on a constant basis.")
	} else {
		log.Println("Garbage collection is off. Memory use will grow unbounded. You WILL RUN OUT OF RAM unless you send in the gc command to manually force garbage collection. Lower CPU, but progressive memory footprint.")
		debug.SetGCPercent(-1)
	}

	if *isAllowExec {
		log.Println("Enabling exec commands because you passed in -allowexec")
	}

	ip, err := externalIP()
	if err != nil {
		log.Println(err)
	}

	//homeTempl = template.Must(template.ParseFiles(filepath.Join(*assets, "home.html")))

	// see if they provided a regex filter
	if len(*regExpFilter) > 0 {
		log.Printf("You specified a serial port regular expression filter: %v\n", *regExpFilter)
	}

	//GetDarwinMeta()

	if !*verbose {
		log.Println("You can enter verbose mode to see all logging by starting with the -v command line switch.")
		//		log.SetOutput(new(NullWriter)) //route all logging to nullwriter
	}

	// list serial ports
	portList, _ := GetList()
	metaports, _ := GetMetaList()

	/*if errSys != nil {
		log.Printf("Got system error trying to retrieve serial port list. Err:%v\n", errSys)
		log.Fatal("Exiting")
	}*/

	// serial port list thread
	go func() {
		time.Sleep(1300 * time.Millisecond)
		log.SetOutput(io.Writer(os.Stdout))
		log.Println("Your serial ports:")
		if len(portList) == 0 {
			log.Println("\tThere are no serial ports to list.")
		}
		for _, element := range portList {
			// if we have meta data for this port, use it
			setMetaDataForOsSerialPort(&element, metaports)
			log.Printf("\t%v\n", element)

		}
		if !*verbose {
			//log.Println("You can enter verbose mode to see all logging by starting with the -v command line switch.")
			log.SetOutput(new(NullWriter)) //route all logging to nullwriter
		}
	}()

	// launch the hub routine which is the singleton for the websocket server
	go h.run()
	// launch our serial port routine
	go sh.run()
	// launch our dummy data routine
	//go d.run()

	// Run the UDP & TCP Server that are part of the Cayenn protocol
	// This lets us listen for devices announcing they
	// are alive on our local network, or are sending data from sensors,
	// or acknowledgements to commands we send the device.
	// This is used by Cayenn devices such as ESP8266 devices that
	// can speak to SPJS and allow SPJS to pass through their data back to
	// clients such as ChiliPeppr.
	if *isDisableCayenn == false {
		log.Println("Attempting to load Cayenn TCP/UDP server on port 8988...")
		go udpServerRun()
		go tcpServerRun()
	} else {
		log.Println("Disabled loading of Cayenn TCP/UDP server on port 8988")
	}

	// Setup GPIO server
	// Ignore GPIO for now, but it would be nice to get GPIO going natively
	//gpio.PreInit()
	// when the app exits, clean up our gpio ports
	//defer gpio.CleanupGpio()

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/ws", wsHandler)

	go startHttp(ip)
	go startHttps(ip)

	log.Println("The Serial Port JSON Server is now running.")
	log.Println("If you are using ChiliPeppr, you may go back to it and connect to this server.")

	// turn off logging output unless user wanted verbose mode
	// actually, this is now done after the serial port list thread completes
	if !*verbose {
		//		log.SetOutput(new(NullWriter)) //route all logging to nullwriter
	}

	// wait
	ch := make(chan bool)
	<-ch
}

func startHttp(ip string) {
	f := flag.Lookup("addr")
	log.Println("Starting http server and websocket on " + ip + "" + f.Value.String())
	if err := http.ListenAndServe(*addr, nil); err != nil {
		fmt.Printf("Error trying to bind to http port: %v, so exiting...\n", err)
		fmt.Printf("This can sometimes mean you are already running SPJS and accidentally trying to run a second time, thus why the port would be in use. Also, check your permissions/credentials to make sure you can bind to IP address ports.")
		log.Fatal("Error ListenAndServe:", err)
	}
}

func startHttps(ip string) {
	// generate self-signed cert for testing or local trusted networks
	// openssl req -x509 -nodes -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365

	f := flag.Lookup("saddr")
	cert, certErr := os.Open(*scert)
	key, keyErr := os.Open(*skey)

	cert.Close()
	key.Close()

	if certErr != nil || keyErr != nil {
		log.Println("Missing tls cert and/or key. Will not start HTTPS server.")
		//fmt.Println("Missing tls cert and/or key. Will not start HTTPS server.")
		return
	}

	log.Println("Starting https server and websocket on " + ip + "" + f.Value.String())
	if err := http.ListenAndServeTLS(*saddr, *scert, *skey, nil); err != nil {
		fmt.Printf("Error trying to bind to https port: %v, so exiting...\n", err)
		log.Fatal("Error ListenAndServeTLS:", err)
	}
}

func externalIP() (string, error) {
	//log.Println("Getting external IP")
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Println("Got err getting external IP addr")
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			//log.Println("Iface down")
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			//log.Println("Loopback")
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			log.Println("Got err on iface.Addrs()")
			return "", err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				//log.Println("Ip was nil or loopback")
				continue
			}
			ip = ip.To4()
			if ip == nil {
				//log.Println("Was not ipv4 addr")
				continue // not an ipv4 address
			}
			//log.Println("IP is ", ip.String())
			return ip.String(), nil
		}
	}
	return "", errors.New("are you connected to the network?")
}

var homeTemplate = template.Must(template.New("home").Parse(homeTemplateHtml))

// If you navigate to this server's homepage, you'll get this HTML
// so you can directly interact with the serial port server
const homeTemplateHtml = `<!DOCTYPE html>
<html>
<head>
<title>Serial Port Example</title>
<script type="text/javascript" src="http://ajax.googleapis.com/ajax/libs/jquery/1.4.2/jquery.min.js"></script>
<script type="text/javascript">
    $(function() {

    var conn;
    var msg = $("#msg");
    var log = $("#log");

    function appendLog(msg) {
        var d = log[0]
        var doScroll = d.scrollTop == d.scrollHeight - d.clientHeight;
        msg.appendTo(log)
        if (doScroll) {
            d.scrollTop = d.scrollHeight - d.clientHeight;
        }
    }

    $("#form").submit(function() {
        if (!conn) {
            return false;
        }
        if (!msg.val()) {
            return false;
        }
        conn.send(msg.val() + "\n");
        msg.val("");
        return false
    });

    if (window["WebSocket"]) {
        conn = new WebSocket("ws://{{$}}/ws");
        conn.onclose = function(evt) {
            appendLog($("<div><b>Connection closed.</b></div>"))
        }
        conn.onmessage = function(evt) {
            appendLog($("<div/>").text(evt.data))
        }
    } else {
        appendLog($("<div><b>Your browser does not support WebSockets.</b></div>"))
    }
    });
</script>
<style type="text/css">
html {
    overflow: hidden;
}

body {
    overflow: hidden;
    padding: 0;
    margin: 0;
    width: 100%;
    height: 100%;
    background: gray;
}

#log {
    background: white;
    margin: 0;
    padding: 0.5em 0.5em 0.5em 0.5em;
    position: absolute;
    top: 0.5em;
    left: 0.5em;
    right: 0.5em;
    bottom: 3em;
    overflow: auto;
}

#form {
    padding: 0 0.5em 0 0.5em;
    margin: 0;
    position: absolute;
    bottom: 1em;
    left: 0px;
    width: 100%;
    overflow: hidden;
}

</style>
</head>
<body>
<div id="log"></div>
<form id="form">
    <input type="submit" value="Send" />
    <input type="text" id="msg" size="64"/>
</form>
</body>
</html>
`
