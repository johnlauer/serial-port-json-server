package main

import (
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type BufferflowTinyg struct {
	Name            string
	Port            string
	Paused          bool
	StopSending     int
	StartSending    int
	PauseOnEachSend time.Duration // Amount of milliseconds to pause on each send to give TinyG time to send us a qr report
	sem             chan int      // semaphore to wait on until given release
	LatestData      string        // this holds the latest data across multiple serial reads so we can analyze it for qr responses
	BypassMode      bool          // this means don't actually watch for qr responses until we know tinyg is in qr response mode
	//wg           sync.WaitGroup
	re        *regexp.Regexp
	reNewline *regexp.Regexp
	reQrOff   *regexp.Regexp
	reQrOn    *regexp.Regexp

	// slot counter approach
	reSlotDone            *regexp.Regexp // the r:null cmd to look for back from tinyg indicating line processed
	reCmdsWithNoRResponse *regexp.Regexp // since we're using slot approach, we expect an r:{} response, but some commands don't give that so just don't expect it
	SlotMax               int            // queue into tinyg using slot approach
	SlotCtr               int            // queue into tinyg using slot approach
}

type DataPerLine struct {
	P string
	D string
}

func (b *BufferflowTinyg) Init() {

	/* Slot Approach */
	b.SlotMax = 4 // at most queue up 2 slots, i.e. 2 gcode commands
	b.SlotCtr = 0 // 0 indicates no gcode lines have been queued into tinyg
	// the regular expression to turn off the pause
	// this regexp will find the r:null response which indicates
	// a line of gcode was processed and thus we can send the next one
	// {"r":{},"f":[1,0,33,134]}
	// when we see this, decrement the b.SlotCtr
	b.reSlotDone, _ = regexp.Compile("\"r\":{")
	b.reCmdsWithNoRResponse, _ = regexp.Compile("[!~%]")
	log.Printf("Using slot approach for TinyG buffering. slotMax:%v, slotCtr:%v\n", b.SlotMax, b.SlotCtr)

	/* End Slot Approach Items */

	//b.StartSending = 20
	//b.StopSending = 18
	b.PauseOnEachSend = 0 * time.Millisecond
	b.sem = make(chan int)

	// start tinyg out in bypass mode because we don't really
	// know if user put tinyg into qr response mode. what we'll
	// do is watch for our first qr response and then assume we're
	// in active mode, i.e. b.BypassMode should then be set to false
	// the reason for this is if we think tinyg is going to send qr
	// responses and we don't get them, we end up holding up all data
	// and essentially break everything. so gotta really watch for this.
	//b.BypassMode = true
	// looking like bypassmode isn't very helpful
	b.BypassMode = false

	// the regular expression to find the qr value
	// this regexp will find qr when in json mode or non-json mode on tinyg
	b.re, _ = regexp.Compile("\"{0,1}qr\"{0,1}:(\\d+)")

	//reWipeToQr, _ = regexp.Compile("(?s)^.*?\"qr\":\\d+")

	// we split the incoming data on newline using this regexp
	// tinyg seems to only send \n but look for \n\r optionally just in case
	b.reNewline, _ = regexp.Compile("\\r{0,1}\\n")

	// Look for qr's being turned off by user to auto turn-on BypassMode
	/*
		$qv
		[qv]  queue report verbosity      2 [0=off,1=single,2=triple]
		$qv=0
		[qv]  queue report verbosity      0 [0=off,1=single,2=triple]
		{"qv":""}
		{"r":{"qv":0},"f":[1,0,10,5788]}
	*/
	b.reQrOff, _ = regexp.Compile("{\"qv\":0}|\\[qv\\]\\s+queue report verbosity\\s+0")

	// Look for qr's being turned ON by user to auto turn-off BypassMode
	/*
		$qv
		[qv]  queue report verbosity      3 [0=off,1=single,2=triple]
		{"qv":""}
		{"r":{"qv":3},"f":[1,0,10,5066]}
	*/
	b.reQrOn, _ = regexp.Compile("{\"qv\":[1-9]}|\\[qv\\]\\s+queue report verbosity\\s+[1-9]")

}

// Slot counter approach
func (b *BufferflowTinyg) BlockUntilReady(cmd string) bool {
	log.Printf("BlockUntilReady() slot ctr approach start. SlotCtr:%v, b.Paused:%v\n", b.SlotCtr, b.Paused)
	//log.Printf("buffer:%v\n", b)

	// If we're in BypassMode then just return here so we do no blocking
	if b.BypassMode {
		log.Printf("In BypassMode so won't watch for qr responses.")
		return true
	}

	// We're in active buffer mode i.e. not BypassMode.
	// Now we need to see if we've been asked to pause
	// our sending by the OnIncomingData method (or any other method or thread)
	if b.Paused {
		log.Println("It appears we are being asked to pause, so we will wait on b.sem")

		// We are being asked to pause our sending of commands

		// To fully pause, we want to consume all signals on b.sem so that we actually
		// block here rather than get an immediately queued signal coming in.
		// This can be confusing why we have to consume all the b.sem signals first
		// before we can pause. here's why. other parts of the spjs can send to us
		// that we can unblock now. in fact, when the incoming qr reports are analyzed,
		// any qr value above our startsending threshold will fire off a b.sem signal.
		// it's as if the incoming thread is overly telling us "yes, you can send again"
		// so when we then get a b.Paused request, we need to make sure we truly pause.
		// the way to do this is to throw away all the "yes, you can send again" signals
		// so that we block until we see a brand spanking new "yes, you can send again"
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

		// SUPER IMPORTANT
		// This block at <-b.sem is the most significant item in the buffer flow
		// Since we saw we were being asked to pause from b.Paused
		// We need to actually implement the pause here using Go's channels
		// which are super bad-ass. We know the serial writing is its own thread
		// so if we block it here, the rest of the serial port json server will still
		// run. it's only the serial sending that will get blocked. we can then unblock
		// from other parts of the code by just sending b.sem <- 1 from anywhere and
		// it will come back here and unblock. So, we'll most likely unblock from the
		// serial reading thread when it sees that a qr report came in saying there's
		// room in the planner buffer again
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

	} else {
		// still yeild a bit cuz seeing we need to let tinyg
		// have a chance to respond
		// IMPORTANT: this value here seems to massively influence whether
		// we get lost lines of gcode or not when sending to the serial port
		// it seems that each gcode line must yield a bit to give the TinyG
		// a chance to send us qr reports within a window where we didn't pound it
		// too hard. You want this value to not be too high and not be too low
		// i found that 7ms works, but was getting the planner buffer to perhaps a 2
		// i found that 10ms works as well, with planner buffer getting to a 3 at its lowest
		// 15ms seems safe and doesn't seem to starve the planner buffer
		//seconds := 50 * time.Millisecond
		//seconds := b.PauseOnEachSend * time.Millisecond
		log.Printf("BlockUntilReady() default yielding on send for TinyG for seconds:%v\n", b.PauseOnEachSend)
		//time.Sleep(b.PauseOnEachSend)
	}

	// increment slot counter because we are now going to SEND this command
	// to tinyg and need to know how many commands we've sent so we know when
	// to pause sending

	// test for cmds that won't have an r response, and in that case, don't expect it, don't
	// pause on it, and don't increment slot ctr
	if b.reCmdsWithNoRResponse.MatchString(cmd) {
		log.Printf("We have a !~% command that gets no r:{} response back, so not increment slot counter.\n")
		//} else if b.reNewline.MatchString(cmd) {
	} else if cmd == "\n" || cmd == "\r\n" {
		log.Printf("We have a newline command that gets no r:{} response back, so not increment slot counter.\n")
	} else {
		b.SlotCtr++
		log.Printf("Incremented slot counter.\n")
	}
	if b.SlotCtr >= b.SlotMax {
		// pause sending
		b.Paused = true
	}

	log.Printf("BlockUntilReady() end. SlotCtr:%v, b.Paused:%v\n", b.SlotCtr, b.Paused)
	return true
}

// Slot counter approach
func (b *BufferflowTinyg) OnIncomingData(data string) {
	//log.Printf("OnIncomingData() start. data:%v\n", data)
	log.Printf("OnIncomingData() start. SlotCtr:%v, b.Paused:%v\n", b.SlotCtr, b.Paused)
	// we need to queue up data since it comes in fragmented
	// and wait until we see a newline to analyze if there
	// is a qr value
	b.LatestData += data

	// now split on newline
	arrLines := b.reNewline.Split(b.LatestData, -1)
	//log.Printf("arrLines:%v\n", arrLines)
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

		if b.reSlotDone.MatchString(element) {
			// we have an r:null response which means our gcode cmd is
			// done executing

			// decrement slot ctr
			b.SlotCtr--

			// Unpause the BlockUntilReady() cmd because
			// there's a new slot available. It's up to BlockUntilReady()
			// to decide to block again

			b.Paused = false
			log.Printf("we just got a slot completed, so we will send signal to b.sem to unpause the BlockUntilReady() thread. slotCtr:%v, b.paused(should be false):%v\n", b.SlotCtr, b.Paused)
			go func() {
				// this method is pretty key
				// we run this asychronously, i.e. it's own thread, that's why
				// you see go func() because that go keyword launches this
				// function as its own thread. we get to tell the BlockUntilReady()
				// method to stop blocking by sending b.sem <- 1 to it
				// if you think about it, because we can start sending, we
				// need to unblock the sending
				gcodeline := element

				log.Printf("StartSending Semaphore goroutine created for gcodeline:%v\n", gcodeline)
				// this is an unbuffered channel, so we will
				// block here which is why this is a goroutine
				b.sem <- 1
				// when we get here that means a BlockUntilReady()
				// method consumed the signal, meaning we unblocked them
				// which is good because they're allowed to start sending
				// again
				defer func() {
					gcodeline := gcodeline
					log.Printf("StartSending Semaphore just got consumed by the BlockUntilReady() thread for the gcodeline:%v\n", gcodeline)
				}()
			}()

		}

		// if we are handling sending the broadcast back to the client
		// from our buffer flow implementation, rather than letting the base
		// implementation of serial port json server do it, then send that
		// broadcast here. however, make sure you told spjs that you were
		// handling the data instead
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
	log.Printf("OnIncomingData() end. SlotCtr:%v, b.Paused:%v\n", b.SlotCtr, b.Paused)
}

// Renaming to ..QrApproach cuz abandoning this technique
func (b *BufferflowTinyg) BlockUntilReadyQrApproach() bool {
	log.Printf("BlockUntilReady() start\n")
	//log.Printf("buffer:%v\n", b)

	// If we're in BypassMode then just return here so we do no blocking
	if b.BypassMode {
		log.Printf("In BypassMode so won't watch for qr responses.")
		return true
	}

	// during testing, try a super long pause
	/*
		seconds := 3 * time.Second
		log.Printf("BlockUntilReady() default yielding on send for TinyG for seconds:%v\n", seconds)
		time.Sleep(seconds)
	*/

	// We're in active buffer mode i.e. not BypassMode.
	// Now we need to see if we've been asked to pause
	// our sending by the OnIncomingData method (or any other method or thread)
	if b.Paused {
		log.Println("It appears we are being asked to pause, so we will wait on b.sem")

		// We are being asked to pause our sending of commands

		// To fully pause, we want to consume all signals on b.sem so that we actually
		// block here rather than get an immediately queued signal coming in.
		// This can be confusing why we have to consume all the b.sem signals first
		// before we can pause. here's why. other parts of the spjs can send to us
		// that we can unblock now. in fact, when the incoming qr reports are analyzed,
		// any qr value above our startsending threshold will fire off a b.sem signal.
		// it's as if the incoming thread is overly telling us "yes, you can send again"
		// so when we then get a b.Paused request, we need to make sure we truly pause.
		// the way to do this is to throw away all the "yes, you can send again" signals
		// so that we block until we see a brand spanking new "yes, you can send again"
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

		// SUPER IMPORTANT
		// This block at <-b.sem is the most significant item in the buffer flow
		// Since we saw we were being asked to pause from b.Paused
		// We need to actually implement the pause here using Go's channels
		// which are super bad-ass. We know the serial writing is its own thread
		// so if we block it here, the rest of the serial port json server will still
		// run. it's only the serial sending that will get blocked. we can then unblock
		// from other parts of the code by just sending b.sem <- 1 from anywhere and
		// it will come back here and unblock. So, we'll most likely unblock from the
		// serial reading thread when it sees that a qr report came in saying there's
		// room in the planner buffer again
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

	} else {
		// still yeild a bit cuz seeing we need to let tinyg
		// have a chance to respond
		// IMPORTANT: this value here seems to massively influence whether
		// we get lost lines of gcode or not when sending to the serial port
		// it seems that each gcode line must yield a bit to give the TinyG
		// a chance to send us qr reports within a window where we didn't pound it
		// too hard. You want this value to not be too high and not be too low
		// i found that 7ms works, but was getting the planner buffer to perhaps a 2
		// i found that 10ms works as well, with planner buffer getting to a 3 at its lowest
		// 15ms seems safe and doesn't seem to starve the planner buffer
		//seconds := 50 * time.Millisecond
		//seconds := b.PauseOnEachSend * time.Millisecond
		log.Printf("BlockUntilReady() default yielding on send for TinyG for seconds:%v\n", b.PauseOnEachSend)
		time.Sleep(b.PauseOnEachSend)
	}
	log.Printf("BlockUntilReady() end\n")
	return true
}

// Renamed to ..QrApproach because we're abandoning that approach
func (b *BufferflowTinyg) OnIncomingDataQrApproach(data string) {
	//log.Printf("OnIncomingData() start. data:%v\n", data)
	log.Printf("OnIncomingData() start\n", data)
	// we need to queue up data since it comes in fragmented
	// and wait until we see a newline to analyze if there
	// is a qr value
	b.LatestData += data

	// now split on newline
	arrLines := b.reNewline.Split(b.LatestData, -1)
	//log.Printf("arrLines:%v\n", arrLines)
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

		if b.re.MatchString(element) {
			// we have a qr value

			// if we've actually seen a qr value that means the user
			// put the tinyg in qr reporting mode, so turn off BypassMode
			// this is essentially a cool/lazy way to turn off BypassMode
			b.BypassMode = false

			//log.Printf("Found a qr value:%v", re)
			res := b.re.FindStringSubmatch(element)
			if len(res) > 1 {
				qr, err := strconv.Atoi(res[1])
				if err != nil {
					log.Printf("Got error converting qr value. huh? err:%v\n", err)
				} else {
					log.Printf("The qr val is:\"%v\"\n", qr)

					// print warning if we got super low on buffer planner
					if qr < 4 {
						log.Printf("------------\nGot qr less than 4!!!! Bad cuz we stop at 10. qr:%v\n---------\n", qr)
					}

					if qr <= b.StopSending {

						// TinyG is below our planner buffer threshold, better
						// stop sending to it
						log.Println("qr is below stopsending threshold, so simply setting b.Paused to true so BlockUntilReady() sees we are paused")
						b.Paused = true
						//log.Println("Paused sending gcode")

					} else if qr >= b.StartSending {

						// TinyG has room in its buffer, remove the pause and
						// start sending in commands again
						b.Paused = false
						log.Println("qr is above startsending, so we will send signal to b.sem to unpause the BlockUntilReady() thread")
						go func() {
							// this method is pretty key
							// we run this asychronously, i.e. it's own thread, that's why
							// you see go func() because that go keyword launches this
							// function as its own thread. we get to tell the BlockUntilReady()
							// method to stop blocking by sending b.sem <- 1 to it
							// if you think about it, because we can start sending, we
							// need to unblock the sending
							gcodeline := element

							log.Printf("StartSending Semaphore goroutine created for qr gcodeline:%v\n", gcodeline)
							// this is an unbuffered channel, so we will
							// block here which is why this is a goroutine
							b.sem <- 1
							// when we get here that means a BlockUntilReady()
							// method consumed the signal, meaning we unblocked them
							// which is good because they're allowed to start sending
							// again
							defer func() {
								gcodeline := gcodeline
								log.Printf("StartSending Semaphore just got consumed by the BlockUntilReady() thread for the qr gcodeline:%v\n", gcodeline)
							}()
						}()
					} else {
						log.Printf("In a middle state where paused is:%v, qr:%v, watching for the buffer planner to go high or low.\n", b.Paused, qr)
					}
				}
			} else {
				log.Printf("Problem extracting qr value in regexp. Didn't get 2 array elements or greater. Huh??? res:%v", res)
			}
		} else if b.BypassMode && b.reQrOn.MatchString(element) {
			// it looks like user turned on qr reports, so turn off bypass mode
			b.BypassMode = false
			m := BufferMsg{"BypassModeOff", b.Port, element}
			bm, err := json.Marshal(m)
			if err == nil {
				h.broadcastSys <- bm
			}
			log.Printf("User turned on qr reports, so activating buffer control. qr on line:%v\n", element)
		} else if b.BypassMode == false && b.reQrOff.MatchString(element) {
			// it looks like user turned off qr reports, so jump into bypass mode
			b.BypassMode = true
			m := BufferMsg{"BypassModeOn", b.Port, element}
			bm, err := json.Marshal(m)
			if err == nil {
				h.broadcastSys <- bm
			}
			log.Printf("User turned off qr reports, so bypassing buffer control. qr off line:%v\n", element)
			if b.sem != nil {
				b.sem <- 1 // send channel a val to trigger the unblocking in BlockUntilReady()
				close(b.sem)
			}
			log.Println("Sent semaphore unblock in case anything was waiting since user entered bypassmode")
		}

		// if we are handling sending the broadcast back to the client
		// from our buffer flow implementation, rather than letting the base
		// implementation of serial port json server do it, then send that
		// broadcast here. however, make sure you told spjs that you were
		// handling the data instead
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

// break commands into individual commands
// so, for example, break on newlines to separate commands
// or, in the case of ~% break those onto separate commands
func (b *BufferflowTinyg) BreakApartCommands(cmd string) []string {
	// add newline after !~%
	reSingle := regexp.MustCompile("([!~%])")
	cmd = reSingle.ReplaceAllString(cmd, "$1\n")
	cmds := strings.Split(cmd, "\n")
	log.Printf("Len of cmds array after split:%v\n", len(cmds))
	//json, _ := json.Marshal(cmds)
	//log.Printf("cmds after split:%v\n", json)
	finalCmds := []string{}
	if len(cmds) == 1 {
		item := cmds[0]
		// just put cmd back in with newline
		if reSingle.MatchString(item) {
			log.Printf("len1. Added cmd back. Not re-adding newline cuz artificially added one earlier. item:'%v'\n", item)
			finalCmds = append(finalCmds, item)
		} else {
			item = item + "\n"
			log.Printf("len1. Re-adding item to finalCmds with newline:'%v'\n", item)
			finalCmds = append(finalCmds, item)
		}
	} else {
		for index, item := range cmds {
			// since more than 1 cmd, loop thru
			if reSingle.MatchString(item) {
				log.Printf("Added cmd back. Not re-adding newline cuz artificially added one earlier. item:'%v'\n", item)
				finalCmds = append(finalCmds, item)
			} else {
				// should we add back our newline? do this if there are elements after us
				if index < len(cmds)-1 {
					// there are cmds after me, so add newline
					//log.Printf("Re-adding newline to item:%v\n", item)
					s := item + "\n"
					finalCmds = append(finalCmds, s)
					log.Printf("Added cmd back with newline. New cmd item:'%v'\n", s)
				} else {
					log.Printf("Skipping adding cmd back cuz just empty newline. item:'%v'\n", item)
					//log.Printf("Re-adding item to finalCmds without adding newline:%v\n", item)
					//finalCmds = append(finalCmds, item)
				}

			}
		}
	}
	log.Printf("Final array of cmds after BreakApartCommands(). finalCmds:%v\n", finalCmds)
	return finalCmds
}

func (b *BufferflowTinyg) Pause() {
	b.Paused = true
	b.BypassMode = false // turn off bypassmode in case it's on
	log.Println("Paused buffer on next BlockUntilReady() call")
}

func (b *BufferflowTinyg) Unpause() {
	b.Paused = false
	log.Println("Unpause(), so we will send signal of 1 to b.sem to unpause the BlockUntilReady() thread")
	go func() {

		log.Printf("Unpause() Semaphore goroutine created.\n")
		// this is an unbuffered channel, so we will
		// block here which is why this is a goroutine

		// sending a 1 asks BlockUntilReady() to move forward
		b.sem <- 1
		// when we get here that means a BlockUntilReady()
		// method consumed the signal, meaning we unblocked them
		// which is good because they're allowed to start sending
		// again
		defer func() {
			log.Printf("Unpause() Semaphore just got consumed by the BlockUntilReady()\n")
		}()
	}()
	log.Println("Unpaused buffer inside BlockUntilReady() call")
}

func (b *BufferflowTinyg) SeeIfSpecificCommandsShouldSkipBuffer(cmd string) bool {
	// remove comments
	cmd = regexp.MustCompile("\\(.*?\\)").ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(";.*").ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[!~%]", cmd); match {
		log.Printf("Found cmd that should skip buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowTinyg) SeeIfSpecificCommandsShouldPauseBuffer(cmd string) bool {
	// remove comments
	cmd = regexp.MustCompile("\\(.*?\\)").ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(";.*").ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[!]", cmd); match {
		log.Printf("Found cmd that should pause buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowTinyg) SeeIfSpecificCommandsShouldUnpauseBuffer(cmd string) bool {
	// remove comments
	cmd = regexp.MustCompile("\\(.*?\\)").ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(";.*").ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[~%]", cmd); match {
		log.Printf("Found cmd that should unpause buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowTinyg) SeeIfSpecificCommandsShouldWipeBuffer(cmd string) bool {
	// remove comments
	cmd = regexp.MustCompile("\\(.*?\\)").ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(";.*").ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[%]", cmd); match {
		log.Printf("Found cmd that should wipe out and reset buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowTinyg) ReleaseLock() {
	log.Println("Lock being released in TinyG buffer")

	b.Paused = false
	b.SlotCtr = 0
	log.Println("ReleaseLock(), so we will send signal of 2 to b.sem to unpause the BlockUntilReady() thread")
	go func() {

		log.Printf("ReleaseLock() Semaphore goroutine created.\n")
		// this is an unbuffered channel, so we will
		// block here which is why this is a goroutine

		// sending a 2 asks BlockUntilReady() to cancel the send
		b.sem <- 2
		// when we get here that means a BlockUntilReady()
		// method consumed the signal, meaning we unblocked them
		// which is good because they're allowed to start sending
		// again
		defer func() {
			log.Printf("ReleaseLock() Semaphore just got consumed by the BlockUntilReady()\n")
		}()
	}()

}

func (b *BufferflowTinyg) IsBufferGloballySendingBackIncomingData() bool {
	// we want to send back incoming data as per line data
	// rather than having the default spjs implemenation that sends back data
	// as it sees it. the reason is that we were getting packets out of order
	// on the browser on bad internet connections. that will still happen with us
	// sending back per line data, but at least it will allow the browser to parse
	// correct json now.
	// TODO: The right way to solve this is to watch for an acknowledgement
	// from the browser and queue stuff up until the acknowledgement and then
	// send the full blast of ganged up data
	return true
}

func (b *BufferflowTinyg) Close() {
}
