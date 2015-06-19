// Supports Windows, Linux, Mac, and Raspberry Pi

package main

import (
	"encoding/xml"
	"strings"
	//"fmt"
	"github.com/facchinm/go-serial"
	//"io/ioutil"
	"log"
	//"os"
	"regexp"
)

type OsSerialPort struct {
	Name         string
	FriendlyName string
	RelatedNames []string // for some devices there are 2 or more ports, i.e. TinyG v9 has 2 serial ports
	SerialNumber string
	DeviceClass  string
	Manufacturer string
	Product      string
	IdProduct    string
	IdVendor     string
}

func GetList() ([]OsSerialPort, error) {

	//log.Println("Doing GetList()")

	ports, err := serial.GetPortsList()

	arrPorts := []OsSerialPort{}
	for _, element := range ports {
		friendly := strings.Replace(element, "/dev/", "", -1)
		arrPorts = append(arrPorts, OsSerialPort{Name: element, FriendlyName: friendly})
	}

	// see if we should filter the list
	if len(*regExpFilter) > 0 {
		// yes, user asked for a filter
		reFilter := regexp.MustCompile("(?i)" + *regExpFilter)

		newarrPorts := []OsSerialPort{}
		for _, element := range arrPorts {
			// if matches regex, include
			if reFilter.MatchString(element.Name) {
				newarrPorts = append(newarrPorts, element)
			} else if reFilter.MatchString(element.FriendlyName) {
				newarrPorts = append(newarrPorts, element)
			} else {
				log.Printf("serial port did not match. port: %v\n", element)
			}

		}
		arrPorts = newarrPorts
	}

	//log.Printf("Done doing GetList(). arrPorts:%v\n", arrPorts)

	return arrPorts, err
}

func GetMetaList() ([]OsSerialPort, error) {
	metaportlist, err := getMetaList()
	if err.Err != nil {
		return nil, err.Err
	}
	return metaportlist, err.Err
}

func GetFriendlyName(portname string) string {
	log.Println("GetFriendlyName from base class")
	return ""
}

type Dict struct {
	Keys    []string `xml:"key"`
	Arrays  []Dict   `xml:"array"`
	Strings []string `xml:"string"`
	Dicts   []Dict   `xml:"dict"`
}

type Result struct {
	XMLName xml.Name `xml:"plist"`
	//Strings []string `xml:"dict>string"`
	Dict `xml:"dict"`
	//Phone   string
	//Groups  []string `xml:"Group>Value"`
}

/*
func GetDarwinMeta() {
	xmlFile, err := os.Open("out.xml")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer xmlFile.Close()

	XMLdata, _ := ioutil.ReadAll(xmlFile)

	var v Result
	//v := Result{}
	xml.Unmarshal(XMLdata, &v)
	log.Printf("Result:%v", len(v.Dicts[0].Arrays))                //, v.Dict.Dicts[0].Keys) // Dicts[0] .Keys[0])
	log.Printf("Result:%v", v.Dicts[0].Arrays[1].Dicts[0].Keys[0]) // Dicts[0] .Keys[0])
	log.Printf("Result:%v", v.Dicts[0].Keys[0])                    // Dicts[0] .Keys[0])

}
*/
