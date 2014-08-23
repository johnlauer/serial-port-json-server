package main

import (
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"time"
	"strings"
)

type BufferflowGrbl struct {
	Name string
	Port string
	Paused bool
	BufferMax int
	BufferSize int
	BufferSizeArray []int
	sem chan int
	LatestData string
	version string
	quit chan int

	reNewLine *regexp.Regexp
	re *regexp.Regexp
	initline *regexp.Regexp
	qry *regexp.Regexp
	rpt *regexp.Regexp
}

func (b *BufferflowGrbl) Init() {
	b.Paused = false //set pause true until initline received indicating grbl has initialized

	log.Println("Initting GRBL buffer flow")
	b.BufferMax = 127 //max buffer size 127 bytes available
	b.BufferSize = 0 //initialize buffer at zero bytes

	//create channels
	b.sem = make(chan int)

	//define regex
	b.reNewLine, _ = regexp.Compile("\\r{0,1}\\n{1,2}")  //\\r{0,1}
	b.re, _ = regexp.Compile("(ok|error)")
	b.initline, _ = regexp.Compile("Grbl") 
	b.qry, _ = regexp.Compile("\\?")
	b.rpt, _ = regexp.Compile("^<")

	//build an interval loop at 250ms to query status
	
	ticker := time.NewTicker(100 * time.Millisecond)
	b.quit = make(chan int)
	go func() {
	    for {
	       select {
	        case <- ticker.C:
	            b.rptQuery()
	        case <- b.quit:
	            ticker.Stop()
	            return
	        }
	    }
	 }()
}

func (b *BufferflowGrbl) BlockUntilReady(cmd string) bool {
	log.Printf("BlockUntilReady() start\n")

	//if b.qry.MatchString(cmd){
	//	return true //return if cmd is a query request, even if buffer is paused
	//}

	//Here we add the length of the new command to the buffer size and append the length
	//to the buffer array.  Check if buffersize > buffermax and if so we pause and await free space before
	//sending the command to grbl.
	b.BufferSize += len(cmd) 
	b.BufferSizeArray = append(b.BufferSizeArray,len(cmd)) 
	
	log.Printf("New line length: " + strconv.Itoa(len(cmd)) + " -- Buffer increased to: " + strconv.Itoa(b.BufferSize))
	log.Println(b.BufferSizeArray)

	if b.BufferSize >= b.BufferMax{
		b.Paused = true
	}

	if b.Paused {
		log.Println("It appears we are being asked to pause, so we will wait on b.sem")
		// We are being asked to pause our sending of commands

		//clean out b.sem
		func() {
			ctr := 0

			keepLooping := true
			for keepLooping {
				select {
				case d, ok := <-b.sem:
					log.Printf("Consuming b.sem queue to clear it before we block. ok:%v, d:%v\n", ok, string(d))
					ctr++
					if ok == false {
						keepLooping = false
					}
				default:
					keepLooping = false
					log.Println("Hit default in select clause")
				}
			}
			log.Printf("Done consuming b.sem queue so we're good to block on it now. ctr:%v\n", ctr)
		}()
		// ok, all b.sem signals are now consumed into la-la land

		log.Println("Blocking on b.sem until told from OnIncomingData to go")
		unblockType, ok := <-b.sem // will block until told from OnIncomingData to go

		log.Printf("Done blocking cuz got b.sem semaphore release. ok:%v, unblockType:%v\n", ok, unblockType)

		// we get an unblockType of 1 for normal unblocks
		// we get an unblockType of 2 when we're being asked to wipe the buffer, i.e. from a % cmd
		if unblockType == 2 {
			log.Println("This was an unblock of type 2, which means we're being asked to wipe internal buffer. so return false.")
			// returning false asks the calling method to wipe the serial send once
			// this function returns
			return false
		}
	} 

	log.Printf("BlockUntilReady() end\n")
	
	return true
}

func (b *BufferflowGrbl) OnIncomingData(data string) {
	log.Printf("OnIncomingData() start. data:%q\n", data)

	b.LatestData += data

	//it was found ok was only received with status responses until the grbl buffer is full.
	//b.LatestData = regexp.MustCompile(">\\r\\nok").ReplaceAllString(b.LatestData, ">") //remove oks from status responses

	arrLines := b.reNewLine.Split(b.LatestData, -1)
	log.Printf("arrLines:%v\n", arrLines)

	if len(arrLines) > 1 {
		// that means we found a newline and have 2 or greater array values
		// so we need to analyze our arrLines[] lines but keep last line
		// for next trip into OnIncomingData
		log.Printf("We have data lines to analyze. numLines:%v\n", len(arrLines))

	} else {
		// we don't have a newline yet, so just exit and move on
		// we don't have to reset b.LatestData because we ended up
		// without any newlines so maybe we will next time into this method
		log.Printf("Did not find newline yet, so nothing to analyze\n")
		return
	}
	
	// if we made it here we have lines to analyze
	// so analyze all of them except the last line
	for index, element := range arrLines[:len(arrLines)-1] {
		log.Printf("Working on element:%v, index:%v", element, index)

		//check for 'ok' or 'error' response indicating a gcode line has been processed
		if b.re.MatchString(element){

			if b.BufferSizeArray != nil{
				b.BufferSize -= b.BufferSizeArray[0]
				
				if len(b.BufferSizeArray) > 1{
					b.BufferSizeArray = b.BufferSizeArray[1:len(b.BufferSizeArray)]
				}else{
					b.BufferSizeArray = nil
				}

				log.Printf("Buffer Decreased: " + strconv.Itoa(b.BufferSize))
			}

			if b.BufferSize < b.BufferMax{
				b.Paused = false
				log.Printf("grbl just completed a line of gcode\n")
				go func() {
					gcodeline := element

					log.Printf("StartSending Semaphore goroutine created for gcodeline:%v\n", gcodeline)
					b.sem <- 1

					defer func() {
						gcodeline := gcodeline
						log.Printf("StartSending Semaphore just got consumed by the BlockUntilReady() thread for the gcodeline:%v\n", gcodeline)
					}()
				}()	
			}
		//check for the grbl init line indicating the arduino is ready to accept commands
		//could also pull version from this string, if we find a need for that later
		} else if b.initline.MatchString(element){
			//grbl init line received, unpause and allow buffered input to send to grbl
			b.Paused = false 
			b.BufferSize = 0
			b.BufferSizeArray = nil

			log.Printf("Grbl buffers cleared - ready for input")
			//should I also clear the system buffers here? not sure how other than sending ctrl+x through spWrite.
			go func(){
				b.sem <- 2 //since grbl was just initialized or reset, clear buffer
			}()
		}


		// handle communication back to client
		m := DataPerLine{b.Port, element + "\n"}
		bm, err := json.Marshal(m)
		if err == nil {
			h.broadcastSys <- bm
		}

	} // for loop

	// now wipe the LatestData to only have the last line that we did not analyze
	// because we didn't know/think that was a full command yet
	b.LatestData = arrLines[len(arrLines)-1]

	//time.Sleep(3000 * time.Millisecond)
	log.Printf("OnIncomingData() end.\n")
}

func (b *BufferflowGrbl) BreakApartCommands(cmd string) []string {

	// add newline after !~%
	log.Printf("Command Before Break-Apart: %q\n", cmd)

	cmds := strings.Split(cmd, "\n")
	finalCmds := []string{}
	for _ , item := range cmds {

		if item == "?"{
			log.Printf("Query added without newline: %q\n",item)
			finalCmds = append(finalCmds, item) //append query request without newline character
		}else if item != ""{
			log.Printf("Re-adding newline to item:%v\n", item)
			s := item + "\n"
			finalCmds = append(finalCmds, s)
			log.Printf("New cmd item:%v\n", s)
		}

	}
	log.Printf("Final array of cmds after BreakApartCommands(). finalCmds:%v\n", finalCmds)

	return finalCmds
	//return []string{cmd} //do not process string
}

func (b *BufferflowGrbl) Pause() {
	b.Paused = true
	log.Println("Paused buffer on next BlockUntilReady() call")
}

func (b *BufferflowGrbl) Unpause() {
	b.Paused = false
	log.Println("Unpause(), so we will send signal of 1 to b.sem to unpause the BlockUntilReady() thread")
	go func() {

		log.Printf("Unpause() Semaphore goroutine created.\n")

		// sending a 1 asks BlockUntilReady() to move forward
		b.sem <- 1

		defer func() {
			log.Printf("Unpause() Semaphore just got consumed by the BlockUntilReady()\n")
		}()
	}()
	log.Println("Unpaused buffer inside BlockUntilReady() call")
}

func (b *BufferflowGrbl) SeeIfSpecificCommandsShouldSkipBuffer(cmd string) bool {
	// remove comments
	cmd = regexp.MustCompile("\\(.*?\\)").ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(";.*").ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[!~\\?]", cmd); match {
		log.Printf("Found cmd that should skip buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowGrbl) SeeIfSpecificCommandsShouldPauseBuffer(cmd string) bool {
	// remove comments
	cmd = regexp.MustCompile("\\(.*?\\)").ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(";.*").ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[!]", cmd); match {
		log.Printf("Found cmd that should pause buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowGrbl) SeeIfSpecificCommandsShouldUnpauseBuffer(cmd string) bool {

	cmd = regexp.MustCompile("\\(.*?\\)").ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(";.*").ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[~]", cmd); match {
		log.Printf("Found cmd that should unpause buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowGrbl) SeeIfSpecificCommandsShouldWipeBuffer(cmd string) bool {

	cmd = regexp.MustCompile("\\(.*?\\)").ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(";.*").ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("(\u0018)", cmd); match {
		log.Printf("Found cmd that should wipe out and reset buffer. cmd:%v\n", cmd)
		b.BufferSize = 0
		b.BufferSizeArray = nil
		log.Println("Buffer variables cleared for new input.")
		return true
	}
	return false
}

func (b *BufferflowGrbl) ReleaseLock() {
	log.Println("Lock being released in GRBL buffer")

	b.Paused = false
	log.Println("ReleaseLock(), so we will send signal of 2 to b.sem to unpause the BlockUntilReady() thread")
	go func() {

		log.Printf("ReleaseLock() Semaphore goroutine created.\n")

		// sending a 2 asks BlockUntilReady() to cancel the send
		b.sem <- 2
		defer func() {
			log.Printf("ReleaseLock() Semaphore just got consumed by the BlockUntilReady()\n")
		}()
	}()
}

func (b *BufferflowGrbl) IsBufferGloballySendingBackIncomingData() bool {
	//telling json server that we are handling client responses
	return true
}

//Use this function to open a connection, write directly to serial port and close connection.
//This is used for sending query requests outside of the normal buffered operations that will pause to wait for room in the grbl buffer
//'?' is asynchronous to the normal buffer load and does not need to be paused when buffer full
func (b *BufferflowGrbl) rptQuery(){
	spWrite("sendnobuf " + b.Port + " ?")
}

func (b *BufferflowGrbl) Close(){
	//stop the status query loop when the serial port is closed off.
	log.Println("Stopping the status query loop")
	b.quit <- 1
}
