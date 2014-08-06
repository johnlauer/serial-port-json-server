package main

import (
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"time"
)

type BufferflowTinyg struct {
	Name         string
	Port         string
	Paused       bool
	StopSending  int
	StartSending int
	sem          chan int // semaphore to wait on until given release
	LatestData   string   // this holds the latest data across multiple serial reads so we can analyze it for qr responses
	BypassMode   bool     // this means don't actually watch for qr responses until we know tinyg is in qr response mode
	//wg           sync.WaitGroup
}

var (
	// the regular expression to find the qr value
	// this regexp will find qr when in json mode or non-json mode on tinyg
	re, _ = regexp.Compile("\"{0,1}qr\"{0,1}:(\\d+)")

	//reWipeToQr, _ = regexp.Compile("(?s)^.*?\"qr\":\\d+")

	// we split the incoming data on newline using this regexp
	// tinyg seems to only send \n but look for \n\r optionally just in case
	reNewline, _ = regexp.Compile("\\n\\r{0,1}")

	// Look for qr's being turned off by user to auto turn-on BypassMode
	/*
		$qv
		[qv]  queue report verbosity      2 [0=off,1=single,2=triple]
		$qv=0
		[qv]  queue report verbosity      0 [0=off,1=single,2=triple]
		{"qv":""}
		{"r":{"qv":0},"f":[1,0,10,5788]}
	*/
	reQrOff, _ = regexp.Compile("{\"qv\":0}|\\[qv\\]\\s+queue report verbosity\\s+0")

	// Look for qr's being turned ON by user to auto turn-off BypassMode
	/*
		$qv
		[qv]  queue report verbosity      3 [0=off,1=single,2=triple]
		{"qv":""}
		{"r":{"qv":3},"f":[1,0,10,5066]}
	*/
	reQrOn, _ = regexp.Compile("{\"qv\":[1-9]}|\\[qv\\]\\s+queue report verbosity\\s+[1-9]")
)

func (b *BufferflowTinyg) Init() {
	b.StartSending = 16
	b.StopSending = 10
	b.sem = make(chan int)

	// start tinyg out in bypass mode because we don't really
	// know if user put tinyg into qr response mode. what we'll
	// do is watch for our first qr response and then assume we're
	// in active mode, i.e. b.BypassMode should then be set to false
	// the reason for this is if we think tinyg is going to send qr
	// responses and we don't get them, we end up holding up all data
	// and essentially break everything. so gotta really watch for this.
	b.BypassMode = true
}

func (b *BufferflowTinyg) BlockUntilReady() bool {
	log.Printf("BlockUntilReady() start\n")
	//log.Printf("buffer:%v\n", b)

	// If we're in BypassMode then just return here so we do no blocking
	if b.BypassMode {
		log.Printf("In BypassMode so won't watch for qr responses.")
		return true
	}

	// We're in active buffer mode. Now we need to see if we've been asked to pause
	// our sending by the OnIncomingData method
	if b.Paused {
		log.Println("It appears we are being asked to pause, so we will wait on b.sem")
		//if b.sem != nil {
		// we want to consume all signals on b.sem so that we actually
		// block here rather than get an immediately queued signal coming in
		// consume all stuff queued
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
		log.Println("Blocking on b.sem until told from OnIncomingData to go")
		unblockType, ok := <-b.sem // will block until told from OnIncomingData to go
		//close(b.sem)
		//b.sem = nil // set semaphore to null cuz don't need it now

		log.Printf("Done blocking cuz got b.sem semaphore release. ok:%v, unblockType:%v\n", ok, unblockType)

		if unblockType == 2 {
			log.Println("This was an unblock of type 2, which means we're being asked to wipe internal buffer. so return false.")
			return false
		}

		/*
			for b.Paused {
				log.Println("We are paused. Yeilding send.")
				time.Sleep(5 * time.Millisecond)
			}
		*/

	} else {
		// still yeild a bit cuz seeing we need to let tinyg
		// have a chance to respond
		seconds := 2 * time.Millisecond
		log.Printf("BlockUntilReady() default yielding on send for TinyG for seconds:%v\n", seconds)
		time.Sleep(seconds)
	}
	log.Printf("BlockUntilReady() end\n")
	return true
}

func (b *BufferflowTinyg) OnIncomingData(data string) {
	log.Printf("OnIncomingData() start. data:%v\n", data)
	// we need to queue up data since it comes in fragmented
	// and wait until we see a newline to analyze if there
	// is a qr value
	b.LatestData += data

	// now split on newline
	arrLines := reNewline.Split(b.LatestData, -1)
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
		//if re.Match([]byte(data)) {
		// see if there is a qr response in our large LatestData buffer
		if re.MatchString(element) {
			// we have a qr value

			// if we've actually seen a qr value that means the user
			// put the tinyg in qr reporting mode, so turn off BypassMode
			// this is essentially a cool/lazy way to turn off BypassMode
			b.BypassMode = false

			//log.Printf("Found a qr value:%v", re)
			res := re.FindStringSubmatch(element)
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
						//if b.sem == nil {
						log.Println("qr is below stopsending threshold, so simply setting b.Paused to true so BlockUntilReady() sees we are paused")
						//b.sem = make(chan int)
						//} else {
						//log.Println("qr is below stopsending, but b.sem seems to already be in action")
						//}
						b.Paused = true
						//b.wg.Add(1)

						//log.Println("Paused sending gcode")
					} else if qr >= b.StartSending {
						//if b.sem != nil {
						b.Paused = false
						log.Println("qr is above startsending, so we will send signal to b.sem to unpause the BlockUntilReady() thread")
						go func() {
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
								//log.Println("Closing b.sem and setting to nil")
								//close(b.sem)
								//b.sem = nil
							}()
							//close(b.sem)
						}()
						//}
						/*
							if b.Paused {
								b.Paused = false
								log.Println("Buffer is paused, but qr shows room, so Sending semaphore to BlockUntilReady to release block")
								b.sem <- 1 // send channel a val to trigger the unblocking in BlockUntilReady()
							}
							log.Println("Started sending gcode again")
						*/
					} else {
						log.Printf("In a middle state where paused is:%v, qr:%v, watching for the buffer planner to go high or low.\n", b.Paused, qr)
					}
				}
			} else {
				log.Printf("Problem extracting qr value in regexp. Didn't get 2 array elements or greater. Huh??? res:%v", res)
			}
		} else if b.BypassMode && reQrOn.MatchString(element) {
			// it looks like user turned on qr reports, so turn off bypass mode
			b.BypassMode = false
			m := BufferMsg{"BypassModeOff", b.Port, element}
			bm, err := json.Marshal(m)
			if err == nil {
				h.broadcastSys <- bm
			}
			log.Printf("User turned on qr reports, so activating buffer control. qr on line:%v\n", element)
		} else if b.BypassMode == false && reQrOff.MatchString(element) {
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

	} // for loop

	// now wipe the LatestData to only have the last line that we did not analyze
	// because we didn't know/think that was a full command yet
	b.LatestData = arrLines[len(arrLines)-1]

	//time.Sleep(3000 * time.Millisecond)
	log.Printf("OnIncomingData() end.\n")
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

	//if b.Paused {
	//	b.Paused = false

	/*
		//if b.sem != nil {
			go func() {
				b.sem <- 2 // send a 2 meaning unblock but cancel subsequent actions
				defer func() {
					log.Println("Closing b.sem from ReleaseLock() and setting to nil")
					//close(b.sem)
					//b.sem = nil
				}()
				//close(b.sem)
			}()
			log.Println("b.sem was not nil, so sent semaphore release")
		}
	*/
	//}
}
