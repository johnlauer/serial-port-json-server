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
	"path/filepath"
	"regexp"
	"sort"
)

func getMetaList() ([]OsSerialPort, os.SyscallError) {

	//return getListViaTtyList()
	return getAllPortsViaManufacturer()
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
			//friendly, ferr := getMetaDataForPort(f.Name())
			//if ferr == nil {
			//	list[ctr].FriendlyName = friendly
			//}

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
	// TODO: take list from http://www.usb.org/developers/defined_class
	// and create mapping.
}

func getAllPortsViaManufacturer() ([]OsSerialPort, os.SyscallError) {
	var err os.SyscallError
	var list []OsSerialPort

	// LOOK FOR THE WORD MANUFACTURER
	// search /sys folder
	files := findFiles("/sys", "^manufacturer$")

	// LOOK FOR THE WORD PRODUCT
	filesFromProduct := findFiles("/sys", "^product$")

	// append both arrays so we have one (then we'll have to de-dupe)
	files = append(files, filesFromProduct...)

	// Now get directories from each file
	re := regexp.MustCompile("/(manufacturer|product)$")
	var mapfile map[string]int
	mapfile = make(map[string]int)
	for _, element := range files {
		// make this directory be a key so it's unique. increment int so we know
		// for debug how many times this directory appeared
		mapfile[re.ReplaceAllString(element, "")]++
	}

	// sort the directory keys
	mapfilekeys := make([]string, len(mapfile))
	i := 0
	for key, _ := range mapfile {
		mapfilekeys[i] = key
		i++
	}
	sort.Strings(mapfilekeys)
	log.Printf("The list of directories with serial port device data:%v", mapfilekeys)

	//reRemoveManuf, _ := regexp.Compile("/manufacturer$")
	reNewLine, _ := regexp.Compile("\n")

	// loop on unique directories
	for _, directory := range mapfilekeys {

		if len(directory) == 0 {
			continue
		}

		// search folder that had manufacturer file in it
		log.Printf("\tDirectory searching: %v", directory)

		// for each manufacturer or product file, we need to read the val from the file
		// but more importantly find the tty ports for this directory

		// for example, for the TinyG v9 which creates 2 ports, the cmd:
		// find /sys/devices/platform/bcm2708_usb/usb1/1-1/1-1.3/ -name tty[AU]* -print
		// will result in:
		/*
			/sys/devices/platform/bcm2708_usb/usb1/1-1/1-1.3/1-1.3:1.0/tty/ttyACM0
			/sys/devices/platform/bcm2708_usb/usb1/1-1/1-1.3/1-1.3:1.2/tty/ttyACM1
		*/

		// figure out the directory
		//directory := reRemoveManuf.ReplaceAllString(element, "")

		// read the device class so we can remove stuff we don't want like hubs
		deviceClassBytes, errRead4 := ioutil.ReadFile(directory + "/bDeviceClass")
		deviceClass := ""
		if errRead4 != nil {
			// there must be a permission issue or the file doesn't exist
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
		manufBytes, errRead := ioutil.ReadFile(directory + "/manufacturer")
		manuf := ""
		if errRead != nil {
			// the file could possibly just not exist, which is normal
			log.Printf("Problem reading in manufacturer text file. It does not exist or Permissions maybe? err:%v", errRead)
			//return nil, err
			//continue
		}
		manuf = string(manufBytes)
		manuf = reNewLine.ReplaceAllString(manuf, "")

		// read the product
		productBytes, errRead2 := ioutil.ReadFile(directory + "/product")
		product := ""
		if errRead2 != nil {
			// the file could possibly just not exist, which is normal
			//log.Printf("Problem reading in product text file. Permissions maybe? err:%v", errRead2)
			//return nil, err
		}
		product = string(productBytes)
		product = reNewLine.ReplaceAllString(product, "")

		// read the serial number
		serialNumBytes, errRead3 := ioutil.ReadFile(directory + "/serial")
		serialNum := ""
		if errRead3 != nil {
			// the file could possibly just not exist, which is normal
			//log.Printf("Problem reading in serial number text file. Permissions maybe? err:%v", errRead3)
			//return nil, err
		}
		serialNum = string(serialNumBytes)
		serialNum = reNewLine.ReplaceAllString(serialNum, "")

		// read idvendor
		idVendorBytes, _ := ioutil.ReadFile(directory + "/idVendor")
		idVendor := ""
		idVendor = reNewLine.ReplaceAllString(string(idVendorBytes), "")

		// read idProduct
		idProductBytes, _ := ioutil.ReadFile(directory + "/idProduct")
		idProduct := ""
		idProduct = reNewLine.ReplaceAllString(string(idProductBytes), "")

		log.Printf("%v : %v (%v) DevClass:%v", manuf, product, serialNum, deviceClass)

		// -name tty[AU]* -print
		filesTty := findDirs(directory, "^tty(A|U).*")

		// generate a unique list of tty ports below
		//var ttyPorts []string
		var m map[string]int
		m = make(map[string]int)
		for _, fileTty := range filesTty {
			if len(fileTty) == 0 {
				continue
			}
			log.Printf("\t%v", fileTty)
			ttyPort := regexp.MustCompile("^.*/").ReplaceAllString(fileTty, "")
			ttyPort = reNewLine.ReplaceAllString(ttyPort, "")
			m[ttyPort]++
			//ttyPorts = append(ttyPorts, ttyPort)
		}
		log.Printf("\tlist of ports on this. map:%v\n", m)
		log.Printf("\t.")
		//sort.Strings(ttyPorts)

		// create order array of ttyPorts so they're in order when
		// we send back via json. this makes for more human friendly reading
		// cuz anytime you do a hash map you can get out of order
		ttyPorts := []string{}
		for key, _ := range m {
			ttyPorts = append(ttyPorts, key)
		}
		sort.Strings(ttyPorts)

		// we now have a very nice list of ttyports for this device. many are just 1 port
		// however, for some advanced devices there are 2 or more ports associated and
		// we have this data correct now, so build out the final OsSerialPort list
		for _, key := range ttyPorts {
			listitem := OsSerialPort{
				Name:         "/dev/" + key,
				FriendlyName: manuf, // + " " + product,
				SerialNumber: serialNum,
				DeviceClass:  deviceClass,
				Manufacturer: manuf,
				Product:      product,
				IdVendor:     idVendor,
				IdProduct:    idProduct,
			}
			if len(product) > 0 {
				listitem.FriendlyName += " " + product
			}

			listitem.FriendlyName += " (" + key + ")"
			listitem.FriendlyName = friendlyNameCleanup(listitem.FriendlyName)

			// append related tty ports
			for _, keyRelated := range ttyPorts {
				if key == keyRelated {
					continue
				}
				listitem.RelatedNames = append(listitem.RelatedNames, "/dev/"+keyRelated)
			}
			list = append(list, listitem)
		}

	}

	// sort ports by item.Name
	sort.Sort(ByName(list))

	log.Printf("Final port list: %v", list)
	return list, err
}

func findFiles(rootpath string, regexpstr string) []string {

	var matchedFiles []string
	re := regexp.MustCompile(regexpstr)
	numScanned := 0
	filepath.Walk(rootpath, func(path string, fi os.FileInfo, _ error) error {
		numScanned++

		if fi.IsDir() == false && re.MatchString(fi.Name()) == true {
			matchedFiles = append(matchedFiles, path)
		}
		return nil
	})
	log.Printf("Rootpath:%v, Numscanned:%v\nMatchedfiles:\n%v", rootpath, numScanned, strings.Join(matchedFiles, "\n"))
	return matchedFiles
}

func findDirs(rootpath string, regexpstr string) []string {

	var matchedFiles []string
	re := regexp.MustCompile(regexpstr)
	numScanned := 0
	filepath.Walk(rootpath, func(path string, fi os.FileInfo, _ error) error {
		numScanned++

		if fi.IsDir() == true && re.MatchString(fi.Name()) == true {
			matchedFiles = append(matchedFiles, path)
		}
		return nil
	})
	log.Printf("Rootpath:%v, Numscanned:%v\nMatcheddirs:\n%v", rootpath, numScanned, strings.Join(matchedFiles, "\n"))
	return matchedFiles
}

// ByAge implements sort.Interface for []Person based on
// the Age field.
type ByName []OsSerialPort

func (a ByName) Len() int           { return len(a) }
func (a ByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool { return a[i].Name < a[j].Name }

func friendlyNameCleanup(fnin string) (fnout string) {
	// This is an industry intelligence method to just cleanup common names
	// out there so we don't get ugly friendly names back
	fnout = regexp.MustCompile("\\(www.arduino.cc\\)").ReplaceAllString(fnin, "")
	fnout = regexp.MustCompile("Arduino\\s+Arduino").ReplaceAllString(fnout, "Arduino")
	fnout = regexp.MustCompile("\\s+").ReplaceAllString(fnout, " ")       // multi space to single space
	fnout = regexp.MustCompile("^\\s+|\\s+$").ReplaceAllString(fnout, "") // trim
	return fnout
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
