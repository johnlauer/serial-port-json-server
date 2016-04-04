package main

import (
	//	"log"
	"encoding/json"
	// "runtime"
)

type UsbItem struct {
	Id               string
	BusNum           string
	DeviceNum        string
	VidPid           string
	Name             string
	MaxPower         string
	IsVideo          bool
	VideoResolutions []VideoResolution
	IsAudio          bool
	SerialNumber     string
	DeviceClass      string
	Vendor           string
	Product          string
	Pid              string
	Vid              string
}

type VideoResolution struct {
	Width  int
	Height int
}

type UsbListCmd struct {
	UsbListStatus string
	UsbList       []UsbItem
}

func GetUsbList() []UsbItem {
	//	log.Println("Running main GetUsbList")

	usbList := []UsbItem{}

	// the call to getUsbList() is now handle by the usb_*.go files
	//if runtime.GOOS == "linux" && runtime.GOARCH == "arm" {
		usbList = getUsbList()
	//}

	return usbList
}

func SendUsbList() {

	finalList := UsbListCmd{}
	finalList.UsbListStatus = "Done"
	finalList.UsbList = GetUsbList()
	mapB, _ := json.Marshal(finalList)
	h.broadcastSys <- mapB

}
