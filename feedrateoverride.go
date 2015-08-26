package main

import (
	//"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
)

var (
	reFeedrate         = regexp.MustCompile("(?i)F(\\d+\\.{0,1}\\d*)")
	isFroNeedTriggered = false
	currentFeedrate    = -1.0
)

// Here is where we actually apply the feedrate override on a line of gcode
func doFeedRateOverride(str string, feedrateoverride float32) (bool, string) {

	if feedrateoverride == 0.0 {
		log.Println("Feedrate is nil or 0.0 so returning")
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
	log.Println(strArr2)
	if len(strArr2) == 0 {

		log.Println("No match found for feedrateoverride.")

		// see if the user asked for a feedrate override though
		// if they did, we need to inject one because we didn't find one to adjust
		if isFroNeedTriggered {

			log.Printf("We need to inject a feedrate...\n")

			if currentFeedrate == -1.0 {

				// this means we have no idea what the current feedrate is. that means
				// the gcode before us never specified it ever so we are stuck and can't
				// create the override
				log.Println("We have no idea what the current feedrate is, so giving up")
				return false, ""

			} else {

				// if we get here we need to inject an F at the end of the line
				injectFr := currentFeedrate * float64(feedrateoverride)
				log.Printf("We do know the current feedrate: %v, so we will inject: F%v\n", currentFeedrate, injectFr)

				str = str + "F" + FloatToString(injectFr)
				log.Printf("New gcode line: %v\n", str)

				// set to false so next time through we don't inject again
				isFroNeedTriggered = false

				return true, str
			}

		}

		// no match found for feedrate, but also there is no need for an injection
		// so returning
		log.Println("No need for injection of feedrate either cuz user never asked. Returning.")
		return false, ""
	}

	// set to false so next time through we don't override again
	isFroNeedTriggered = false

	indxArr := reFeedrate.FindAllStringSubmatchIndex(str, -1)
	log.Println(indxArr)

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
		log.Println(itemArr)

		fr, err := strconv.ParseFloat(itemArr[1], 32)
		if err != nil {
			log.Println("Error parsing feedrate val", err)
		} else {

			// set this as current feedrate
			if !isAlreadySetCurrentFeedrate {
				currentFeedrate = fr
				isAlreadySetCurrentFeedrate = true
				log.Printf("Just set current feedrate: %v\n", currentFeedrate)
			}

			newFr := fr * fro
			log.Println(newFr)

			// swap out the string for our new string
			// because we are looping in reverse, these indexes are valid
			str = str[:indxArr[i][2]] + FloatToString(newFr) + str[indxArr[i][3]:]
			log.Println(strings.Replace(str, "\n", "\\n", -1))
		}

	}

	return true, str

}

func FloatToString(input_num float64) string {
	// to convert a float number to a string
	return strconv.FormatFloat(input_num, 'f', 3, 64)
}
