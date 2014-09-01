package main

import (
	"encoding/json"
	"log"
	"regexp"
	//"strconv"
	"strings"
	"time"
	"sync"
)

type BufferflowGrbl struct {
	Name            string
	Port            string
	Paused          bool
	BufferMax       int
	//BufferSize      int
	//BufferSizeArray []int
	//queue for buffer size tracking
	q *Queue

	// use thread locking for b.Paused
	lock *sync.Mutex

	sem chan int
	LatestData string
	LastStatus string
	version string
	quit chan int
	parent_serport *serport

	reNewLine *regexp.Regexp
	re        *regexp.Regexp
	initline  *regexp.Regexp
	qry       *regexp.Regexp
	rpt       *regexp.Regexp
}

func (b *BufferflowGrbl) Init() {
	b.Paused = false //set pause true until initline received indicating grbl has initialized

	log.Println("Initting GRBL buffer flow")
	b.BufferMax = 127 //max buffer size 127 bytes available
	//b.BufferSize = 0  //initialize buffer at zero bytes

	b.lock = &sync.Mutex{}
	b.q = NewQueue()

	//create channels
	b.sem = make(chan int)

	//define regex
	b.reNewLine, _ = regexp.Compile("\\r{0,1}\\n{1,2}") //\\r{0,1}
	b.re, _ = regexp.Compile("^(ok|error)")
	b.initline, _ = regexp.Compile("^Grbl")
	b.qry, _ = regexp.Compile("\\?")
	b.rpt, _ = regexp.Compile("^<")

}

func (b *BufferflowGrbl) BlockUntilReady(cmd string, id string) (bool, bool) {
	log.Printf("BlockUntilReady() start\n")

	//if b.qry.MatchString(cmd){
	//	return true //return if cmd is a query request, even if buffer is paused
	//}

	//Here we add the length of the new command to the buffer size and append the length
	//to the buffer array.  Check if buffersize > buffermax and if so we pause and await free space before
	//sending the command to grbl.
	//b.BufferSize += len(cmd)
	//b.BufferSizeArray = append(b.BufferSizeArray, len(cmd))

	b.q.Push(cmd, id)

	log.Printf("New line length: %v, buffer size increased to:%v\n", len(cmd), b.q.LenOfCmds())
	log.Println(b.q)

	if b.q.LenOfCmds() >= b.BufferMax {
		b.SetPaused(true)
		log.Printf("Buffer Full - Will send this command when space is available")
	}

	if b.Paused {
		log.Println("It appears we are being asked to pause, so we will wait on b.sem")
		// We are being asked to pause our sending of commands

		// clear all b.sem signals so when we block below, we truly block
		b.ClearOutSemaphore()

		log.Println("Blocking on b.sem until told from OnIncomingData to go")
		unblockType, ok := <-b.sem // will block until told from OnIncomingData to go

		log.Printf("Done blocking cuz got b.sem semaphore release. ok:%v, unblockType:%v\n", ok, unblockType)

		// we get an unblockType of 1 for normal unblocks
		// we get an unblockType of 2 when we're being asked to wipe the buffer, i.e. from a % cmd
		if unblockType == 2 {
			log.Println("This was an unblock of type 2, which means we're being asked to wipe internal buffer. so return false.")
			// returning false asks the calling method to wipe the serial send once
			// this function returns
			return false, false
		}
	}

	log.Printf("BlockUntilReady(cmd:%v, id:%v) end\n", cmd, id)

	return true, true
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
		if b.re.MatchString(element) {

			if b.q.Len() > 0 {
				doneCmd, id := b.q.Poll()

				// Send cmd:"Complete" back
				m := DataCmdComplete{"Complete", id, b.Port, b.q.LenOfCmds(), doneCmd}
				bm, err := json.Marshal(m)
				if err == nil {
					h.broadcastSys <- bm
				}

				log.Printf("Buffer decreased to itemCnt:%v, lenOfBuf:%v\n", b.q.Len(), b.q.LenOfCmds())
			} else {
				log.Printf("We should NEVER get here cuz we should have a command in the queue to dequeue when we get the r:{} response. If you see this debug stmt this is BAD!!!!")
			}

			if b.q.LenOfCmds() < b.BufferMax {

				log.Printf("Grbl just completed a line of gcode\n")

				// if we are paused, tell us to unpause cuz we have clean buffer room now
				if b.GetPaused() {
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

				// let's set that we are no longer paused
				b.SetPaused(false) //b.Paused = false
			}

		//check for the grbl init line indicating the arduino is ready to accept commands
		//could also pull version from this string, if we find a need for that later
		} else if b.initline.MatchString(element) {
			//grbl init line received, unpause and allow buffered input to send to grbl
			if b.GetPaused() { b.SetPaused(false) }

			//b.q.Delete()

			log.Printf("Grbl buffers cleared - ready for input")
			//should I also clear the system buffers here? not sure how other than sending ctrl+x through spWrite.
			go func() {
				b.sem <- 2 //since grbl was just initialized or reset, clear buffer
			}()

		//Check for report output, compare to last report output, if different return to client to update status; otherwise ignore status.
		} else if b.rpt.MatchString(element){
			if(element == b.LastStatus){
				log.Println("Grbl status has not changed, not reporting to client")
				continue  //skip this element as the cnc position has not changed, and move on to the next element.
			}

			b.LastStatus = element //if we make it here something has changed with the status string and laststatus needs updating
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

// Clean out b.sem so it can truly block
func (b *BufferflowGrbl) ClearOutSemaphore() {
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
	// ok, all b.sem signals are now consumed into la-la land
}

func (b *BufferflowGrbl) BreakApartCommands(cmd string) []string {

	// add newline after !~%
	log.Printf("Command Before Break-Apart: %q\n", cmd)

	cmds := strings.Split(cmd, "\n")
	finalCmds := []string{}
	for _, item := range cmds {

		if item == "?" {
			log.Printf("Query added without newline: %q\n", item)
			finalCmds = append(finalCmds, item) //append query request without newline character
		} else if item != "" {
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
	b.SetPaused(true) //b.Paused = true
	//b.BypassMode = false // turn off bypassmode in case it's on
	log.Println("Paused buffer on next BlockUntilReady() call")
}

func (b *BufferflowGrbl) Unpause() {
	b.SetPaused(false) //b.Paused = false
	
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

		//b.q.Delete() //delete tracking queue, all buffered commands will be wiped.
		
		//log.Println("Buffer variables cleared for new input.")
		return true
	}
	return false
}

func (b *BufferflowGrbl) SeeIfSpecificCommandsReturnNoResponse(cmd string) bool {
	/*
		// remove comments
		cmd = b.reComment.ReplaceAllString(cmd, "")
		cmd = b.reComment2.ReplaceAllString(cmd, "")
		if match := b.reNoResponse.MatchString(cmd); match {
			log.Printf("Found cmd that does not get a response from TinyG. cmd:%v\n", cmd)
			return true
		}
	*/
	return false
}

func (b *BufferflowGrbl) ReleaseLock() {
	log.Println("Lock being released in GRBL buffer")

	b.q.Delete()

	//b.Paused = false  -- should this still be unpausing here?

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
func (b *BufferflowGrbl) rptQueryLoop(p *serport){
	b.parent_serport = p //make note of this port for use in clearing the buffer later, on error.
	ticker := time.NewTicker(250 * time.Millisecond)
	b.quit = make(chan int)
	go func() {
	    for {
	       select {
	        case <- ticker.C:
	            
	            n2, err := p.portIo.Write([]byte("?"))

	            log.Print("Just wrote ", n2, " bytes to serial: ?")

				if err != nil {
					errstr := "Error writing to " + p.portConf.Name + " " + err.Error() + " Closing port."
					log.Print(errstr)
					h.broadcastSys <- []byte(errstr)
					ticker.Stop() //stop query loop if we can't write to the port
					break
				}
	        case <- b.quit:
	            ticker.Stop()
	            return
	        }
	    }
	 }()
}

func (b *BufferflowGrbl) Close() {
	//stop the status query loop when the serial port is closed off.
	log.Println("Stopping the status query loop")
	b.quit <- 1
}

//	Gets the paused state of this buffer
//	go-routine safe.
func (b *BufferflowGrbl) GetPaused() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.Paused
}

//	Sets the paused state of this buffer
//	go-routine safe.
func (b *BufferflowGrbl) SetPaused(isPaused bool) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.Paused = isPaused
}

