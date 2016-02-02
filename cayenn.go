package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"regexp"
	"strings"
)

type Addr struct {
	IP      string
	Port    int
	Network string
}

type DataAnnounce struct {
	Addr     Addr
	Announce string
	Widget   string
	JsonTag  string
	DeviceId string
}

type ClientAnnounceMsg struct {
	Announce   string
	Widget     string
	MyDeviceId string
	JsonTag    string
}

// This is the UDP packet sent back from the server (us)
// to the client saying "hey we got your announce, this is
// who we are and our IP in case you want to create a TCP
// socket conn back to us for reliable conn"
type ServerAnnounceResponseMsg struct {
	Announce     string
	Widget       string
	YourDeviceId string
	ServerIp     string
	JsonTag      string
}

func udpServerRun() {

	/* Lets prepare a address at any address at port 10001*/
	ServerAddr, err := net.ResolveUDPAddr("udp", ":8988")
	if err != nil {
		log.Println("Error: ", err)
		return
	}

	/* Now listen at selected port */
	ServerConn, err := net.ListenUDP("udp", ServerAddr)
	if err != nil {
		log.Println("Error: ", err)
		return
	}
	defer ServerConn.Close()

	log.Println("UDP server running on port 8988 to listen for incoming device announcements.")
	buf := make([]byte, 1024)

	for {
		n, addr, err := ServerConn.ReadFromUDP(buf)

		if err != nil {
			log.Println("Error: ", err)
		} else {
			log.Println("Received ", string(buf[0:n]), " from ", addr)

			m := DataAnnounce{}
			m.Addr.IP = addr.IP.String()
			m.Addr.Network = addr.Network()
			m.Addr.Port = addr.Port

			var am ClientAnnounceMsg
			err := json.Unmarshal([]byte(buf[0:n]), &am)
			if err != nil {
				log.Println("Err unmarshalling UDP inbound message from device. err:", err)
			}
			m.Announce = am.Announce
			m.Widget = am.Widget
			m.JsonTag = am.JsonTag
			m.DeviceId = am.MyDeviceId

			bm, err := json.Marshal(m)
			if err == nil {
				h.broadcastSys <- bm
			}

			// send back our own AnnounceRecv
			var arm ServerAnnounceResponseMsg
			arm.Announce = "i-am-your-server"
			arm.YourDeviceId = am.MyDeviceId
			arm.ServerIp = ServerConn.LocalAddr().String()
			arm.Widget = am.Widget
			arm.JsonTag = am.JsonTag

			sendUdp(arm, m.Addr.IP, ":8988")
			go makeTcpConnBackToDevice(m.Addr.IP)
		}
	}
}

func cayennSendUdp(s string) {
	// we get here if a client sent into spjs the command
	// cayenn-sendudp 192.168.1.12 any-msg-to-end-of-line
	args := strings.SplitN(s, " ", 3)

	// make sure we got 3 args
	if len(args) < 3 {
		spErr("Error parsing cayenn-sendudp. Returning. msg:" + s)
		return
	}

	ip := args[1]
	if len(ip) < 7 {
		spErr("Error parsing IP address for cayenn-sendudp. Returning. msg:" + s)
		return
	}
	msg := args[2]
	log.Println("cayenn-sendudp ip:", ip, "msg:", msg)
	cayennSendUdpMsg(ip, ":8988", msg)
}

func cayennSendUdpMsg(ipaddr string, port string, msg string) {

	// This method sends a message to a specific IP address / port over UDP
	var service = ipaddr + port

	conn, err := net.Dial("udp", service)

	if err != nil {
		log.Println("Could not resolve udp address or connect to it on ", service)
		log.Println(err)
		return
	}
	defer conn.Close()

	log.Println("Connected to udp server at ", service)

	n, err := conn.Write([]byte(msg))
	if err != nil {
		log.Println("error writing data to server", service)
		log.Println(err)
		return
	}

	if n > 0 {
		log.Println("Wrote ", n, " bytes to server at ", service)
	} else {
		log.Println("Wrote 0 bytes to server. Huh?")
	}
}

func sendUdp(sarm ServerAnnounceResponseMsg, ipaddr string, port string) {

	var service = ipaddr + port

	conn, err := net.Dial("udp", service)

	if err != nil {
		log.Println("Could not resolve udp address or connect to it on ", service)
		log.Println(err)
		return
	}
	defer conn.Close()

	log.Println("Connected to udp server at ", service)

	// add our server ip to packet because esp8266 and Lua make it near impossible
	// to determine the ip the udp packet came from, so we'll include it in the payload
	sarm.ServerIp = conn.LocalAddr().String()
	// drop port, cuz we don't care about it. we have known ports
	re := regexp.MustCompile(":\\d+$")
	sarm.ServerIp = re.ReplaceAllString(sarm.ServerIp, "")

	bmsg, err := json.Marshal(sarm)
	if err != nil {
		log.Println("Error marshalling json for sarm:", sarm, "err:", err)
		return
	}

	n, err := conn.Write([]byte(bmsg))
	if err != nil {
		log.Println("error writing data to server", service)
		log.Println(err)
		return
	}

	if n > 0 {
		log.Println("Wrote ", n, " bytes to server at ", service)
	} else {
		log.Println("Wrote 0 bytes to server. Huh?")
	}

}

func makeTcpConnBackToDevice(ipaddr string) {

	var ip = ipaddr + ":8988"
	conn, err := net.Dial("tcp", ip)
	log.Println("Making TCP connection to:", ip)

	if err != nil {
		log.Println("Error trying to make TCP conn. err:", err)
		return
	}
	defer func() {
		log.Println("Closing TCP conn to:", ip)
		conn.Close()
	}()

	n, err := conn.Write([]byte("hello"))
	if err != nil {
		log.Println("Write to server failed:", err.Error())
		return
	}

	log.Println("Wrote n:", n, "bytes to server")

	connbuf := bufio.NewReader(conn)
	for {
		str, err := connbuf.ReadString('\n')
		if len(str) > 0 {
			log.Println("Got msg on TCP client from ip:", ip)
			log.Println(str)
			h.broadcastSys <- []byte(str)
		}

		if err != nil {
			break
		}
	}
}
