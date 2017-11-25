package main

import (
	"io/ioutil"
	"log"
	"os"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func createStartupScript() {

	log.Println("Creating startup script")
	exeName := os.Args[0]
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
    ` + exeName + ` -regex usb|acm &
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
	err := ioutil.WriteFile("/etc/init.d/serial-port-json-server", d1, 0755)
	check(err)
}
