package main

import (
	//"fmt"
	//"github.com/tarm/goserial"
	//"log"
	"os"
	"os/exec"
	"strings"
	//"encoding/binary"
	//"strconv"
	//"syscall"
	//"fmt"
	//"io"
	"bytes"
	"io/ioutil"
	"log"
	"regexp"
)

func getList() ([]OsSerialPort, os.SyscallError) {
	//return getListViaWmiPnpEntity()
	//return getListViaTtyList()
	return getAllPortsWithManufacturer()
}

func getListViaTtyList() ([]OsSerialPort, os.SyscallError) {
	var err os.SyscallError

	//log.Println("getting serial list on darwin")

	// make buffer of 1000 max serial ports
	// return a slice
	list := make([]OsSerialPort, 1000)

	files, _ := ioutil.ReadDir("/dev/")
	ctr := 0
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "tty") {
			// it is a legitimate serial port
			list[ctr].Name = "/dev/" + f.Name()
			list[ctr].FriendlyName = f.Name()

			// see if we can get a better friendly name
			friendly, ferr := getMetaDataForPort(f.Name())
			if ferr == nil {
				list[ctr].FriendlyName = friendly
			}

			//log.Println("Added serial port to list: ", list[ctr])
			ctr++
		}
		// stop-gap in case going beyond 1000 (which should never happen)
		// i mean, really, who has more than 1000 serial ports?
		if ctr > 999 {
			ctr = 999
		}
		//fmt.Println(f.Name())
		//fmt.Println(f.)
	}
	/*
		list := make([]OsSerialPort, 3)
		list[0].Name = "tty.serial1"
		list[0].FriendlyName = "tty.serial1"
		list[1].Name = "tty.serial2"
		list[1].FriendlyName = "tty.serial2"
		list[2].Name = "tty.Bluetooth-Modem"
		list[2].FriendlyName = "tty.Bluetooth-Modem"
	*/

	return list[0:ctr], err
}

type deviceClass struct {
	BaseClass   int
	Description string
}

func getDeviceClassList() {

}

func getAllPortsWithManufacturer() ([]OsSerialPort, os.SyscallError) {
	var err os.SyscallError
	var list []OsSerialPort

	// search /sys folder
	oscmd := exec.Command("find", "/sys/", "-name", "manufacturer", "-print") //, "2>", "/dev/null")
	// Stdout buffer
	cmdOutput := &bytes.Buffer{}
	// Attach buffer to command
	oscmd.Stdout = cmdOutput

	errstart := oscmd.Start()
	if errstart != nil {
		log.Printf("Got error running find cmd. Maybe they don't have it installed? %v:", errstart)
		return nil, err
	}
	//log.Printf("Waiting for command to finish... %v", oscmd)

	errwait := oscmd.Wait()

	if errwait != nil {
		log.Printf("Command finished with error: %v", errwait)
		return nil, err
	}

	//log.Printf("Finished without error. Good stuff. stdout:%v", string(cmdOutput.Bytes()))

	// analyze stdout
	// we should be able to split on newline to each file
	files := strings.Split(string(cmdOutput.Bytes()), "\n")
	if len(files) == 0 {
		return nil, err
	}

	reRemoveManuf, _ := regexp.Compile("/manufacturer$")
	reNewLine, _ := regexp.Compile("\n")

	for _, element := range files {

		if len(element) == 0 {
			continue
		}

		// for each manufacturer file, we need to read the val from the file
		// but more importantly find the tty ports for this directory

		// for example, for the TinyG v9 which creates 2 ports, the cmd:
		// find /sys/devices/platform/bcm2708_usb/usb1/1-1/1-1.3/ -name tty[AU]* -print
		// will result in:
		/*
			/sys/devices/platform/bcm2708_usb/usb1/1-1/1-1.3/1-1.3:1.0/tty/ttyACM0
			/sys/devices/platform/bcm2708_usb/usb1/1-1/1-1.3/1-1.3:1.2/tty/ttyACM1
		*/

		// figure out the directory
		directory := reRemoveManuf.ReplaceAllString(element, "")

		// read the device class so we can remove stuff we don't want like hubs
		deviceClassBytes, errRead4 := ioutil.ReadFile(directory + "/bDeviceClass")
		deviceClass := ""
		if errRead4 != nil {
			// there must be a permission issue
			//log.Printf("Problem reading in serial number text file. Permissions maybe? err:%v", errRead3)
			//return nil, err
		}
		deviceClass = string(deviceClassBytes)
		deviceClass = reNewLine.ReplaceAllString(deviceClass, "")

		if deviceClass == "09" || deviceClass == "9" || deviceClass == "09h" {
			log.Printf("This is a hub, so skipping. %v", directory)
			continue
		}

		// read the manufacturer
		manufBytes, errRead := ioutil.ReadFile(element)
		if errRead != nil {
			// there must be a permission issue
			log.Printf("Problem reading in manufacturer text file. Permissions maybe? err:%v", errRead)
			//return nil, err
			continue
		}
		manuf := string(manufBytes)
		manuf = reNewLine.ReplaceAllString(manuf, "")

		// read the product
		productBytes, errRead2 := ioutil.ReadFile(directory + "/product")
		product := ""
		if errRead2 != nil {
			// there must be a permission issue
			//log.Printf("Problem reading in product text file. Permissions maybe? err:%v", errRead2)
			//return nil, err
		}
		product = string(productBytes)
		product = reNewLine.ReplaceAllString(product, "")

		// read the serial number
		serialNumBytes, errRead3 := ioutil.ReadFile(directory + "/serial")
		serialNum := ""
		if errRead3 != nil {
			// there must be a permission issue
			//log.Printf("Problem reading in serial number text file. Permissions maybe? err:%v", errRead3)
			//return nil, err
		}
		serialNum = string(serialNumBytes)
		serialNum = reNewLine.ReplaceAllString(serialNum, "")

		log.Printf("%v : %v (%v) DevClass:%v", manuf, product, serialNum, deviceClass)

		// search folder that had manufacturer file in it
		log.Printf("The directory we are searching is:%v", directory)

		// -name tty[AU]* -print
		oscmd = exec.Command("find", directory, "-name", "tty[AU]*", "-print")

		// Stdout buffer
		cmdOutput = &bytes.Buffer{}
		// Attach buffer to command
		oscmd.Stdout = cmdOutput

		errstart = oscmd.Start()
		if errstart != nil {
			log.Printf("Got error running find cmd. Maybe they don't have it installed? %v:", errstart)
			return nil, err
		}
		//log.Printf("Waiting for command to finish... %v", oscmd)

		errwait = oscmd.Wait()

		if errwait != nil {
			log.Printf("Command finished with error: %v", errwait)
			return nil, err
		}

		//log.Printf("Finished searching manuf directory without error. Good stuff. stdout:%v", string(cmdOutput.Bytes()))
		//log.Printf(" \n")

		// we should be able to split on newline to each file
		filesTty := strings.Split(string(cmdOutput.Bytes()), "\n")

		for _, fileTty := range filesTty {
			log.Printf("\t%v", fileTty)
		}
	}

	return list, err
}

func getMetaDataForPort(port string) (string, error) {
	// search the folder structure on linux for this port name

	// search /sys folder
	oscmd := exec.Command("find", "/sys/devices", "-name", port, "-print") //, "2>", "/dev/null")

	// Stdout buffer
	cmdOutput := &bytes.Buffer{}
	// Attach buffer to command
	oscmd.Stdout = cmdOutput

	err := oscmd.Start()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Waiting for command to finish... %v", oscmd)

	err = oscmd.Wait()

	if err != nil {
		log.Printf("Command finished with error: %v", err)
	} else {
		log.Printf("Finished without error. Good stuff. stdout:%v", string(cmdOutput.Bytes()))
		// analyze stdin

	}

	return port + "coolio", nil
}

func getMetaDataForPortOld(port string) (string, error) {
	// search the folder structure on linux for this port name

	// search /sys folder
	oscmd := exec.Command("find", "/sys/devices", "-name", port, "-print") //, "2>", "/dev/null")

	// Stdout buffer
	cmdOutput := &bytes.Buffer{}
	// Attach buffer to command
	oscmd.Stdout = cmdOutput

	err := oscmd.Start()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Waiting for command to finish... %v", oscmd)

	err = oscmd.Wait()

	if err != nil {
		log.Printf("Command finished with error: %v", err)
	} else {
		log.Printf("Finished without error. Good stuff. stdout:%v", string(cmdOutput.Bytes()))
		// analyze stdin

	}

	return port + "coolio", nil
}
