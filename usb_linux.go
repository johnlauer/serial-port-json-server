package main

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func getUsbList() []UsbItem {
	log.Println("Running real linux arm function")

	usbList := []UsbItem{}

	result, err := execShellCmd("lsusb")
	if err != nil {
		log.Println("err", err)
	}
	//log.Println("result", result)

	// we will get results like
	/*
		Bus 001 Device 005: ID 1e4e:0102 Cubeternet GL-UPC822 UVC WebCam
		Bus 001 Device 004: ID 090c:037c Silicon Motion, Inc. - Taiwan (formerly Feiya Technology Corp.)
		Bus 001 Device 003: ID 0424:ec00 Standard Microsystems Corp. SMSC9512/9514 Fast Ethernet Adapter
		Bus 001 Device 002: ID 0424:9514 Standard Microsystems Corp.
		Bus 001 Device 001: ID 1d6b:0002 Linux Foundation 2.0 root hub
	*/

	// parse per line
	lineArr := strings.Split(result, "\n")
	for _, element := range lineArr {
		log.Println("line:", element)
		re := regexp.MustCompile("Bus (\\d+) Device (\\d+): ID (.*?):(.*?) (.*)")
		matches := re.FindStringSubmatch(element)
		//log.Println(matches)
		if len(matches) > 4 {
			usbitem := UsbItem{}
			usbitem.BusNum = matches[1]
			usbitem.DeviceNum = matches[2]
			usbitem.Vid = matches[3]
			usbitem.Pid = matches[4]
			usbitem.VidPid = usbitem.Vid + ":" + usbitem.Pid
			usbitem.Name = matches[5]
			usbList = append(usbList, usbitem)

		}
	}

	reMaxPower := regexp.MustCompile("MaxPower\\s+(\\S+)")
	reVideo := regexp.MustCompile("Video")
	reAudio := regexp.MustCompile("Audio")
	reVidDescriptor := regexp.MustCompile("VideoStreaming Interface Descriptor:")
	reWidth := regexp.MustCompile("wWidth\\s+(\\S+)")
	reHeight := regexp.MustCompile("wHeight\\s+(\\S+)")
	reDeviceClass := regexp.MustCompile("bDeviceClass\\s+(.+)$")
	reVendor := regexp.MustCompile("idVendor\\s+(.+)$")
	reProduct := regexp.MustCompile("idProduct\\s+(.+)$")
	reHex := regexp.MustCompile("0x\\S+")

	// loop thru all ports now
	//	var usbitem UsbItem
	for index, item := range usbList {
		// get detailed info
		cmd := "lsusb -v -s " + item.BusNum + ":" + item.DeviceNum
		log.Println("cmd:", cmd)
		info, _ := execShellCmd(cmd)
		//log.Println("info", info)

		lines := strings.Split(info, "\n")
		for _, line := range lines {

			// see amperage data
			matches := reMaxPower.FindStringSubmatch(line)
			if len(matches) > 1 {
				log.Println("found MaxPower", matches[1])
				usbList[index].MaxPower = matches[1]
			}

			// see if it is video
			isvid := reVideo.MatchString(line)
			if isvid {
				usbList[index].IsVideo = true
			}

			// see if it has audio (could be standalone audio or video with audio
			isaudio := reAudio.MatchString(line)
			if isaudio {
				usbList[index].IsAudio = true
			}

			// see if we have a video resolution
			isviddescriptor := reVidDescriptor.MatchString(line)
			if isviddescriptor {
				if len(usbList[index].VideoResolutions) == 0 {
					usbList[index].VideoResolutions = []VideoResolution{}
				}

				// we found a descriptor, so a width/height should be coming
				vidRes := VideoResolution{}
				usbList[index].VideoResolutions = append(usbList[index].VideoResolutions, vidRes)
			}

			// if we hit a width/height, just set it to the last item in the VideoResolutions array
			matches = reWidth.FindStringSubmatch(line)
			if len(matches) > 1 {
				i, _ := strconv.Atoi(matches[1])
				usbList[index].VideoResolutions[len(usbList[index].VideoResolutions)-1].Width = i
			}
			matches = reHeight.FindStringSubmatch(line)
			if len(matches) > 1 {
				i, _ := strconv.Atoi(matches[1])
				usbList[index].VideoResolutions[len(usbList[index].VideoResolutions)-1].Height = i
			}

			// get device class
			matches = reDeviceClass.FindStringSubmatch(line)
			if len(matches) > 1 {
				usbList[index].DeviceClass = matches[1]
			}

			// get Product
			matches = reProduct.FindStringSubmatch(line)
			if len(matches) > 1 {
				usbList[index].Product = matches[1]
				usbList[index].Product = reHex.ReplaceAllString(usbList[index].Product, "")
				usbList[index].Product = strings.TrimSpace(usbList[index].Product)
			}

			// get Vendor
			matches = reVendor.FindStringSubmatch(line)
			if len(matches) > 1 {
				usbList[index].Vendor = matches[1]
				usbList[index].Vendor = reHex.ReplaceAllString(usbList[index].Vendor, "")
				usbList[index].Vendor = strings.TrimSpace(usbList[index].Vendor)
			}
		}

		// override the video class
		if usbList[index].IsVideo {
			usbList[index].DeviceClass = "Video"
		}
	}

	bout, _ := json.Marshal(usbList)
	log.Println("Final UsbList:", string(bout))
	return usbList
}

func execShellCmd(line string) (string, error) {
	shell := os.Getenv("SHELL")
	oscmd = exec.Command(shell, "-c", line)
	cmdOutput, err := oscmd.CombinedOutput()
	if err != nil {
		log.Println("err running shell cmd", err)
		return string(cmdOutput), err
	}
	//log.Println("shell success output", string(cmdOutput))
	return string(cmdOutput), nil
}
