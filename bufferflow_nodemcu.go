package main

import (
	"encoding/json"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

type BufferflowNodeMcu struct {
	Name string
	Port string
	//Output         chan []byte
	Input          chan string
	ticker         *time.Ticker
	IsOpen         bool
	bufferedOutput string
	reNewLine      *regexp.Regexp
	reCmdDone      *regexp.Regexp
	// additional lock for BlockUntilReady vs OnIncomingData method
	inOutLock    *sync.Mutex
	q            *Queue
	sem          chan int // semaphore to wait on until given release
	Paused       bool
	ManualPaused bool
	lock         *sync.Mutex
	manualLock   *sync.Mutex
	BufferMax    int
}

func (b *BufferflowNodeMcu) Init() {
	log.Println("Initting timed buffer flow (output once every 16ms)")
	b.bufferedOutput = ""
	b.IsOpen = true
	b.reNewLine, _ = regexp.Compile("\\r{0,1}\\n")
	b.inOutLock = &sync.Mutex{}

	b.q = NewQueue()
	// when we get a > response we know a line was processed
	b.reCmdDone, _ = regexp.Compile("^(>|stdin:|=)")
	b.sem = make(chan int, 1000)
	b.Paused = false
	b.ManualPaused = false
	b.lock = &sync.Mutex{}
	b.manualLock = &sync.Mutex{}
	b.Input = make(chan string)
	b.BufferMax = 2

	go func() {
		for data := range b.Input {

			//log.Printf("Got to b.Input chan loop. data:%v\n", data)

			// Lock the packet ctr at start and then end
			b.inOutLock.Lock()

			b.bufferedOutput = b.bufferedOutput + data
			arrLines := b.reNewLine.Split(b.bufferedOutput, -1)
			if len(arrLines) > 1 {
				// that means we found a newline and have 2 or greater array values
				// so we need to analyze our arrLines[] lines but keep last line
				// for next trip into OnIncomingData
				//log.Printf("We have data lines to analyze. numLines:%v\n", len(arrLines))

			} else {
				// we don't have a newline yet, so just exit and move on
				// we don't have to reset b.LatestData because we ended up
				// without any newlines so maybe we will next time into this method
				//log.Printf("Did not find newline yet, so nothing to analyze\n")
				b.inOutLock.Unlock()
				continue
			}

			log.Printf("Analyzing incoming data. Start.")

			// if we made it here we have lines to analyze
			// so analyze all of them except the last line
			for _, element := range arrLines[:len(arrLines)-1] {
				//log.Printf("Working on element:%v, index:%v", element, index)
				//log.Printf("Working on element:%v, index:%v", element)
				log.Printf("\t\tData:%v", element)

				// check if there was a reset cuz we need to wipe our buffer if there was
				if len(element) > 4 {
					bTxt := []byte(element)[len(element)-4:]
					bTest := []byte{14, 219, 200, 244}
					//log.Printf("\t\ttesting two arrays\n\tbTxt :%v\n\tbTest:%v\n", bTxt, bTest)
					//reWasItReset := regexp.MustCompile("fffd")
					//if reWasItReset.MatchString(element) {
					if ByteArrayEquals(bTxt, bTest) {
						// it was reset, wipe buffer
						b.q.Delete()
						log.Printf("\t\tLooks like it was reset based on 1st 4 bytes. We should wipe buffer.")
						b.SetPaused(false, 2)
					}
				}

				// see if it just got restarted
				reIsRestart := regexp.MustCompile("(NodeMCU custom build by frightanic.com|NodeMCU .+ build .+ powered by Lua)")
				if reIsRestart.MatchString(element) {
					// it was reset, wipe buffer
					b.q.Delete()
					log.Printf("\t\tLooks like it was reset based on NodeMCU build line. We should wipe buffer.")
					b.SetPaused(false, 2)
				}

				// Peek to see if the message back matches the command we just sent in
				lastCmd, _ := b.q.Peek()
				lastCmd = regexp.MustCompile("\n").ReplaceAllString(lastCmd, "")

				cmdProcessed := false
				log.Printf("\t\tSeeing if peek compare to lastCmd makes sense. lastCmd:\"%v\", element:\"%v\"", lastCmd, element)
				if lastCmd == element {
					// we just got back the last command so that is a good indicator we got processed
					log.Printf("\t\tWe got back the same command that was just sent in. That is a sign we are processed.")
					cmdProcessed = true
				}

				//check for >|stdin:|= response indicating a line has been processed
				if cmdProcessed || b.reCmdDone.MatchString(element) {

					// ok, a line has been processed, the if statement below better
					// be guaranteed to be true, cuz if its not we did something wrong
					if b.q.Len() > 0 {
						//b.BufferSize -= b.BufferSizeArray[0]
						doneCmd, id := b.q.Poll()

						// Send cmd:"Complete" back
						m := DataCmdComplete{"Complete", id, b.Port, b.q.Len(), doneCmd}
						bm, err := json.Marshal(m)
						if err == nil {
							h.broadcastSys <- bm
						}

						log.Printf("\tBuffer decreased to b.q.Len:%v\n", b.q.Len())
					} else {
						log.Printf("\tWe should RARELY get here cuz we should have a command in the queue to dequeue when we get the >|stdin:|= response. If you see this debug stmt this is one of those few instances where NodeMCU sent us a >|stdin:|= not in response to a command we sent.")
					}

					if b.q.Len() < b.BufferMax {

						// if we are paused, tell us to unpause cuz we have clean buffer room now
						if b.GetPaused() {

							// we are paused, but we can't just go unpause ourself, because we may
							// be manually paused. this means we have to do a double-check here
							if b.GetManualPaused() == false {

								// we are not in a manual pause state, that means we can go ahead
								// and unpause ourselves
								b.SetPaused(false, 1) //set paused to false first, then release the hold on the buffer
							} else {
								log.Println("\tWe just got incoming >|stdin:|= so we could unpause, but since manual paused we will ignore until next time a >|stdin:|= comes in to unpause")
							}
						}

					}

				}

				// handle communication back to client
				// for base serial data (this is not the cmd:"Write" or cmd:"Complete")
				m := DataPerLine{b.Port, element + "\n"}
				bm, err := json.Marshal(m)
				if err == nil {
					h.broadcastSys <- bm
				}

			} // for loop

			b.bufferedOutput = arrLines[len(arrLines)-1]

			b.inOutLock.Unlock()
			log.Printf("Done with analyzing incoming data.")

		}
	}()

	/*
		go func() {
			b.ticker = time.NewTicker(16 * time.Millisecond)
			for _ = range b.ticker.C {
				if b.bufferedOutput != "" {
					m := SpPortMessage{b.Port, b.bufferedOutput}
					buf, _ := json.Marshal(m)
					b.Output <- []byte(buf)
					//log.Println(buf)
					b.bufferedOutput = ""
				}
			}
		}()
	*/

}

func IntArrayEquals(a []int, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func ByteArrayEquals(a []byte, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func (b *BufferflowNodeMcu) BlockUntilReady(cmd string, id string) (bool, bool, string) {

	// Lock for this ENTIRE method
	b.inOutLock.Lock()

	log.Printf("BlockUntilReady() Start\n")
	log.Printf("\tid:%v, txt:%v\n", id, strings.Replace(cmd, "\n", "\\n", -1))

	// keep track of whether we need to unlock at end of method or not
	// i.e. we unlock if we have to pause, thus we won't have to doubly unlock at end of method
	isNeedToUnlock := true

	b.q.Push(cmd, id)

	if b.q.Len() >= b.BufferMax {
		b.SetPaused(true, 0) // b.Paused = true
		log.Printf("\tIt looks like the local queue at Len: %v is over the allowed size of BufferMax: %v, so we are going to pause. Then when some incoming responses come in a check will occur to see if there's room to send this command. Pausing...", b.q.Len(), b.BufferMax)
	}

	if b.GetPaused() {
		//log.Println("It appears we are being asked to pause, so we will wait on b.sem")
		// We are being asked to pause our sending of commands

		// clear all b.sem signals so when we block below, we truly block
		b.ClearOutSemaphore()

		// since we need other code to run while we're blocking, we better release the packet ctr lock
		b.inOutLock.Unlock()
		// since we already unlocked this thread, note it so we don't doubly unlock
		isNeedToUnlock = false

		log.Println("\tBlocking on b.sem until told from OnIncomingData to go")
		unblockType, ok := <-b.sem // will block until told from OnIncomingData to go

		log.Printf("\tDone blocking cuz got b.sem semaphore release. ok:%v, unblockType:%v\n", ok, unblockType)

		log.Printf("\tDone blocking cuz got b.sem semaphore release. ok:%v, unblockType:%v\n", ok, unblockType)

		// we get an unblockType of 1 for normal unblocks
		// we get an unblockType of 2 when we're being asked to wipe the buffer, i.e. from a % cmd
		if unblockType == 2 {
			log.Println("\tThis was an unblock of type 2, which means we're being asked to wipe internal buffer. so return false.")
			// returning false asks the calling method to wipe the serial send once
			// this function returns
			return false, false, ""
		}
	}

	log.Printf("BlockUntilReady() end\n")

	time.Sleep(10 * time.Millisecond)

	if isNeedToUnlock {
		b.inOutLock.Unlock()
	}

	//return true, willHandleCompleteResponse, newCmd

	return true, true, ""
}

func (b *BufferflowNodeMcu) OnIncomingData(data string) {
	b.Input <- data
}

// Clean out b.sem so it can truly block
func (b *BufferflowNodeMcu) ClearOutSemaphore() {
	keepLooping := true
	for keepLooping {
		select {
		case _, ok := <-b.sem: // case d, ok :=
			//log.Printf("Consuming b.sem queue to clear it before we block. ok:%v, d:%v\n", ok, string(d))
			//ctr++
			if ok == false {
				keepLooping = false
			}
		default:
			keepLooping = false
			//log.Println("Hit default in select clause")
		}
	}
}

func (b *BufferflowNodeMcu) BreakApartCommands(cmd string) []string {
	return []string{cmd}
}

func (b *BufferflowNodeMcu) Pause() {
	return
}

func (b *BufferflowNodeMcu) Unpause() {
	return
}

func (b *BufferflowNodeMcu) SeeIfSpecificCommandsShouldSkipBuffer(cmd string) bool {
	reRestart := regexp.MustCompile("node.restart\\(\\)")
	if reRestart.MatchString(cmd) {
		return true
	} else {
		return false
	}
}

func (b *BufferflowNodeMcu) SeeIfSpecificCommandsShouldPauseBuffer(cmd string) bool {
	return false
}

func (b *BufferflowNodeMcu) SeeIfSpecificCommandsShouldUnpauseBuffer(cmd string) bool {
	return false
}

func (b *BufferflowNodeMcu) SeeIfSpecificCommandsShouldWipeBuffer(cmd string) bool {
	reRestart := regexp.MustCompile("^\\s*node.restart\\(\\)")
	if reRestart.MatchString(cmd) {
		log.Printf("\t\tWe found a node.restart() and thus we will wipe buffer")
		b.ReleaseLock()
		return true
	} else {
		return false
	}
}

func (b *BufferflowNodeMcu) SeeIfSpecificCommandsReturnNoResponse(cmd string) bool {

	reWhiteSpace := regexp.MustCompile("^\\s*$")
	if reWhiteSpace.MatchString(cmd) {
		log.Println("Found a whitespace only command")
		return true
	} else {
		return false
	}

	//return false
}

func (b *BufferflowNodeMcu) ReleaseLock() {
	log.Println("Wiping NodeMCU buffer")

	b.q.Delete()
	b.SetPaused(false, 2)
}

func (b *BufferflowNodeMcu) IsBufferGloballySendingBackIncomingData() bool {
	return true
}

func (b *BufferflowNodeMcu) Close() {
	if b.IsOpen == false {
		// we are being asked a 2nd time to close when we already have
		// that will cause a panic
		log.Println("We got called a 2nd time to close, but already closed")
		return
	}
	b.IsOpen = false

	//b.ticker.Stop()
	close(b.Input)
}

func (b *BufferflowNodeMcu) RewriteSerialData(cmd string, id string) string {
	return ""
}

//	Gets the paused state of this buffer
//	go-routine safe.
func (b *BufferflowNodeMcu) GetPaused() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.Paused
}

//	Sets the paused state of this buffer
//	go-routine safe.
func (b *BufferflowNodeMcu) SetPaused(isPaused bool, semRelease int) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.Paused = isPaused

	// only release semaphore if we are being told to unpause
	if b.Paused == false {
		// the BlockUntilReady thread should be sitting waiting
		// so when we send this should trigger it
		b.sem <- semRelease
		log.Printf("\tJust sent release to b.sem with val:%v, so we will not block the sending to serial port anymore.", semRelease)

	}
}

func (b *BufferflowNodeMcu) GetManualPaused() bool {
	b.manualLock.Lock()
	defer b.manualLock.Unlock()
	return b.ManualPaused
}

func (b *BufferflowNodeMcu) SetManualPaused(isPaused bool) {
	b.manualLock.Lock()
	defer b.manualLock.Unlock()
	b.ManualPaused = isPaused
}
