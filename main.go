// Version 1.7
// Supports Windows, Linux, Mac, and Raspberry Pi, Beagle Bone Black

package main

import (
	"flag"
	"go/build"
	"log"
	"net/http"
	//"path/filepath"
	"net"
	//"os"
	"errors"
	//"fmt"
	"text/template"
)

var (
	version      = "1.75"
	versionFloat = float32(1.74)
	addr         = flag.String("addr", ":8989", "http service address")
	assets       = flag.String("assets", defaultAssetPath(), "path to assets")
	//verbose      = flag.Bool("v", true, "show debug logging")
	verbose = flag.Bool("v", false, "show debug logging")
	//homeTempl *template.Template
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

func main() {

	// setup logging
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	//getList()
	flag.Parse()
	f := flag.Lookup("addr")
	log.Println("Version:" + version)

	ip, err := externalIP()
	if err != nil {
		log.Println(err)
	}

	log.Print("Started server and websocket on " + ip + ":" + f.Value.String())
	//homeTempl = template.Must(template.ParseFiles(filepath.Join(*assets, "home.html")))

	if !*verbose {
		log.Println("You can enter verbose mode to see all logging by starting with the -v command line switch.")
		log.SetOutput(new(NullWriter)) //route all logging to nullwriter
	}

	log.Println("The Serial Port JSON Server is now running.")
	log.Println("If you are using ChiliPeppr, you may go back to it and connect to this server.")

	// launch the hub routine which is the singleton for the websocket server
	go h.run()
	// launch our serial port routine
	go sh.run()
	// launch our dummy data routine
	//go d.run()

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/ws", wsHandler)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal("Error ListenAndServe:", err)
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

func whatismyip() {
	/*
		ifaces, err := net.Interfaces()
		// handle err
		for _, i := range ifaces {
			addrs, err := i.Addrs()
			// handle err
			if err != nil {
				log.Println("got err")
			}
			for _, addr := range addrs {
				switch v := addr.(type) {
				case *net.IPAddr:
					// process IP address
					//log.Println((*net.IPAddr))
				}

			}
		}*/

	/*
		addrs, _ := net.InterfaceAddrs()
		if err != nil {
			os.Stderr.WriteString("Oops: " + err.Error() + "\n")
			os.Exit(1)
		}

		log.Println("The IP Address of this server is:")
		for _, a := range addrs {
			log.Println("In loop")
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					//os.Stdout.WriteString(ipnet.IP.String() + "\n")
					log.Println(ipnet.IP.String())
				}
			}
		}
	*/
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
