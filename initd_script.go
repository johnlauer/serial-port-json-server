package main

import (
	"io/ioutil"
	"log"
	"os"
	"os/exec"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func createStartupScript() {

	log.Println("Creating startup script")
	//	exeName := os.Args[0]
	exeName, err := os.Executable()
	if err != nil {
		log.Println("Got error trying to find executable name. Err:", err)
	}
	log.Println("exeName", exeName)
	script := `#! /bin/sh
### BEGIN INIT INFO
# Provides:          serial-port-json-server
# Required-Start:    $all
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: Manage my cool stuff
### END INIT INFO

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/bin

. /lib/init/vars.sh
. /lib/lsb/init-functions
# If you need to source some other scripts, do it here

case "$1" in
  start)
    log_begin_msg "Starting Serial Port JSON Server service"
# do something
    ` + exeName + ` &
    log_end_msg $?
    exit 0
    ;;
  stop)
    log_begin_msg "Stopping the Serial Port JSON Server"

    # do something to kill the service or cleanup or nothing
    killall serial-port-json-server
    log_end_msg $?
    exit 0
    ;;
  *)
    echo "Usage: /etc/init.d/serial-port-json-server {start|stop}"
    exit 1
    ;;
esac
`
	log.Println(script)

	d1 := []byte(script)
	err2 := ioutil.WriteFile("/etc/init.d/serial-port-json-server", d1, 0755)
	check(err2)

	// install it
	// sudo update-rc.d serial-port-json-server defaults
	cmd := exec.Command("update-rc.d", "serial-port-json-server", "defaults")
	err3 := cmd.Start()
	if err3 != nil {
		log.Fatal(err3)
	}
	log.Printf("Waiting for command to finish...")
	err4 := cmd.Wait()
	if err4 != nil {
		log.Printf("Command finished with error: %v", err4)
	} else {
		log.Printf("Successfully created your startup script in /etc/init.d")
		log.Printf("You can now run /etc/init.id/serial-port-json-server start and this will run automatically on startup")
	}

}
