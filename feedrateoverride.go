package main

import (
	//"fmt"
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"strings"
)

var (
	reFeedrate         = regexp.MustCompile("(?i)F(\\d+\\.{0,1}\\d*)")
	isFroNeedTriggered = false
	isFroOn            = false
	//fro = 0.0
	currentFeedrate       = -1.0
	lastFeedrateSeen      = -1.0
	portsWithFrOverrideOn = make(map[string]bool)
)

type froRequestJson struct {
	Cmd              string
	Desc             string
	Port             string
	FeedRateOverride float32
	IsOn             bool
}

// This is called from hub.go to actually parse the "fro COM7 1.5" command sent by the user
func spFeedRateOverride(arg string) {

	// we will get a string of "fro COM9 2.4" or "fro /dev/ttyUSB0 0.1"
	log.Printf("Inside spFeedRateOverride arg: %v\n", strings.Replace(arg, "\n", "\\n", -1))
	arg = strings.TrimSpace(arg)
	arg = strings.TrimPrefix(arg, " ")

	args := strings.Split(arg, " ")
	log.Println(args)

	if len(args) != 2 && len(args) != 3 {
		errstr := "Could not parse feedrate override command: " + arg
		log.Println(errstr)
		spErr(errstr)
		return
	}
	portname := strings.Trim(args[1], " ")
	log.Println("The port to write to is:" + portname + "---")

	//log.Println("The data is:" + args[2] + "---")

	// see if we have this port open
	myport, isFound := findPortByName(portname)

	if !isFound {
		// we couldn't find the port, so send err
		//isFroOn = false
		spErr("We could not find the serial port " + portname + " that you were trying to apply the feedrate override to.")
		return
	}

	// see if they are just querying status
	if len(args) == 2 {
		sendStatusOnFeedrateOverride(myport)
		return
	}

	// we found our port, so now parse our multiplier
	fro, err := strconv.ParseFloat(strings.TrimSpace(args[2]), 32)
	if err != nil {
		errstr := "Could not parse feedrate override multiplier value: " + args[2]
		log.Println(errstr)
		spErr(errstr)
		return
	}

	myport.isFeedRateOverrideOn = true

	myport.feedRateOverride = float32(fro)

	var frj froRequestJson
	frj.Cmd = "FeedRateOverride"
	frj.FeedRateOverride = myport.feedRateOverride
	frj.Port = myport.portConf.Name
	frj.Desc = "Successfully set the feedrate override."

	if frj.FeedRateOverride <= 0.0 {
		isFroOn = false
		log.Println("User turned off feedrate override by setting it to 0")
		frj.IsOn = false
	} else {
		isFroOn = true
		frj.IsOn = true
	}

	//ls, err := json.MarshalIndent(frj, "", "\t")
	ls, err := json.Marshal(frj)
	if err != nil {
		log.Println(err)
		h.broadcastSys <- []byte("Error creating json on feedrate override report " +
			err.Error())
	} else {
		//log.Print("Printing out json byte data...")
		//log.Print(ls)
		h.broadcastSys <- ls
	}

	// if we made it this far we truly have a feedrate override in play
	// so set boolean that we need to inject it into the next line
	isFroNeedTriggered = true

}

func sendStatusOnFeedrateOverride(myport *serport) {
	// they just want a status
	var frj froRequestJson
	frj.Cmd = "FeedRateOverride"
	frj.FeedRateOverride = myport.feedRateOverride
	frj.Port = myport.portConf.Name
	frj.Desc = "Providing you status of feed rate override."

	if frj.FeedRateOverride <= 0.0 {
		frj.IsOn = false
	} else {
		frj.IsOn = true
	}

	ls, err := json.Marshal(frj)
	if err != nil {
		log.Println(err)
		h.broadcastSys <- []byte("Error creating json on feedrate override report " +
			err.Error())
	} else {
		//log.Print("Printing out json byte data...")
		//log.Print(ls)
		h.broadcastSys <- ls
	}
	return
}

// Here is where we actually apply the feedrate override on a line of gcode
func doFeedRateOverride(str string, feedrateoverride float32) (bool, string) {

	//	myport, isFound := findPortByName(portname)
	//	if myport == nil || myport.isFeedRateOverrideOn == false {
	//		log.Println("This port has no feed rate override on. So returning...")
	//		return false, ""
	//	}

	//log.Println("Feed Rate Override Start")
	// any way we cut this, we MUST extract the feedrate from every line whether
	// fro is on or not because we need the currentFeedrate the moment the user asks
	// us to turn this on
	strArrFsSeen := reFeedrate.FindAllStringSubmatch(str, -1)
	if len(strArrFsSeen) > 0 {
		// we found some feedrate F values, so let's store it
		log.Printf("\tFRO: F's found:%v", strArrFsSeen)
		justFoundFeedrate := strArrFsSeen[len(strArrFsSeen)-1][1]
		lastFeedrateSeen, _ = strconv.ParseFloat(justFoundFeedrate, 64)
		currentFeedrate = lastFeedrateSeen
		log.Printf("\tFRO: Found an F so storing it for reference. lastFeedrateSeen:%v", lastFeedrateSeen)
	}

	if feedrateoverride == 0.0 && !isFroNeedTriggered {
		//log.Println("\tFRO: Feed Rate override is 0.0 so returning")
		return false, ""
	}

	// Typical line of gcode
	// N15 G2 F800.0 X39.0719 Y-3.7614 I-2.0806 J1.2144
	// Which, if the feedrate override is 2.6 we want to make look like
	// N15 G2 F2080.0 X39.0719 Y-3.7614 I-2.0806 J1.2144

	//str := "N15 G2 f800.0 X39.0719 Y-3.7614 F30 I-2.0806 J1.2144"
	//re := regexp.MustCompile("(?i)F(\\d+\\.{0,1}\\d*)")
	//strArr := re.FindAllString(str, -1)
	//fmt.Println(strArr)
	strArr2 := reFeedrate.FindAllStringSubmatch(str, -1)
	//log.Println(strArr2)
	if len(strArr2) == 0 {

		log.Println("\tFRO: No match found for feedrateoverride.")

		// see if the user asked for a feedrate override though
		// if they did, we need to inject one because we didn't find one to adjust
		if isFroNeedTriggered {

			log.Printf("\tFRO: We need to inject a feedrate...\n")

			if currentFeedrate == -1.0 {

				// this means we have no idea what the current feedrate is. that means
				// the gcode before us never specified it ever so we are stuck and can't
				// create the override
				log.Println("\tFRO: We have no idea what the current feedrate is, so giving up")
				return false, ""

			} else {

				myFro := feedrateoverride
				// since a value of 0 means turn off, we need to make it multiply like a 1, but leave it zero to mean turn off
				if myFro == 0.0 {
					myFro = 1.0
				}

				// if we get here we need to inject an F at the end of the line
				injectFr := currentFeedrate * float64(myFro)
				log.Printf("\tFRO: We do know the current feedrate: %v, so we will inject: F%v\n", currentFeedrate, injectFr)

				str = str + "F" + FloatToString(injectFr)
				log.Printf("\tFRO: New gcode line: %v\n", str)

				// set to false so next time through we don't inject again
				isFroNeedTriggered = false

				return true, str
			}

		}

		// no match found for feedrate, but also there is no need for an injection
		// so returning
		log.Printf("\tFRO: No need for injection of feedrate either cuz user never asked. currentFeedrate:%v. Returning.", currentFeedrate)
		return false, ""
	}

	// set to false so next time through we don't override again
	isFroNeedTriggered = false

	indxArr := reFeedrate.FindAllStringSubmatchIndex(str, -1)
	//log.Println(indxArr)

	fro := float64(feedrateoverride)
	//fro := float64(2.6)
	//fro :=

	// keep track of whether we set the override yet in this method
	// this only matters if there are 2 or more F's in one gcode line
	// which should almost never happen, but just in case, since we iterate
	// in reverse, only use the first time through
	isAlreadySetCurrentFeedrate := false

	// loop in reverse so we can inject the new feedrate string at end and not have
	// our indexes thrown off
	for i := len(strArr2) - 1; i >= 0; i-- {

		itemArr := strArr2[i]
		//log.Println(itemArr)

		fr, err := strconv.ParseFloat(itemArr[1], 32)
		if err != nil {
			log.Println("\tFRO: Error parsing feedrate val", err)
		} else {

			// set this as current feedrate
			if !isAlreadySetCurrentFeedrate {
				currentFeedrate = fr
				isAlreadySetCurrentFeedrate = true
				log.Printf("\tFRO: Just set current feedrate: %v\n", currentFeedrate)
			}

			// only if fro is on should we proceed with the actual swap
			if isFroOn == true {

				newFr := fr * fro
				//log.Println(newFr)

				// swap out the string for our new string
				// because we are looping in reverse, these indexes are valid
				str = str[:indxArr[i][2]] + FloatToString(newFr) + str[indxArr[i][3]:]
				log.Println("\tFRO: " + strings.Replace(str, "\n", "\\n", -1))
			}
		}

	}

	return true, str

}

func FloatToString(input_num float64) string {
	// to convert a float number to a string
	return strconv.FormatFloat(input_num, 'f', 3, 64)
}
