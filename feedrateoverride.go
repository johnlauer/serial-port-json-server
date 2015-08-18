package main

import (
	//"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
)

var (
	reFeedrate = regexp.MustCompile("(?i)F(\\d+\\.{0,1}\\d*)")
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
		log.Println("No match found for feedrateoverride. Returning.")
		return false, ""
	}

	indxArr := reFeedrate.FindAllStringSubmatchIndex(str, -1)
	log.Println(indxArr)

	fro := float64(feedrateoverride)
	//fro := float64(2.6)
	//fro :=

	// loop in reverse so we can inject the new feedrate string at end and not have
	// our indexes thrown off
	for i := len(strArr2) - 1; i >= 0; i-- {

		itemArr := strArr2[i]
		log.Println(itemArr)

		fr, err := strconv.ParseFloat(itemArr[1], 32)
		if err != nil {
			log.Println("Error parsing feedrate val", err)
		} else {
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
