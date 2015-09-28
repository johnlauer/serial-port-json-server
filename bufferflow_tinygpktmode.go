package main

import (
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	//"time"
	//"errors"
	"fmt"
	"runtime/debug"
	"time"
)

type BufferflowTinygPktMode struct {
	Name         string
	Port         string
	Paused       bool
	ManualPaused bool // indicates user hard paused the buffer on their own, i.e. not from flow control
	//StopSending     int
	//StartSending    int
	//PauseOnEachSend time.Duration // Amount of milliseconds to pause on each send to give TinyG time to send us a qr report
	sem        chan int // semaphore to wait on until given release
	LatestData string   // this holds the latest data across multiple serial reads so we can analyze it for qr responses
	//BypassMode      bool          // this means don't actually watch for qr responses until we know tinyg is in qr response mode
	//wg           sync.WaitGroup

	quit           chan int
	parent_serport *serport

	re                    *regexp.Regexp
	reNewLine             *regexp.Regexp
	reQrOff               *regexp.Regexp
	reQrOn                *regexp.Regexp
	reNoResponse          *regexp.Regexp
	reComment             *regexp.Regexp
	reComment2            *regexp.Regexp
	rePutBackInJsonMode   *regexp.Regexp
	reJsonVerbositySetTo0 *regexp.Regexp
	reCrLfSetTo1          *regexp.Regexp
	reRxResponse          *regexp.Regexp
	reFlowChar            *regexp.Regexp

	// slot counter approach
	reSlotDone *regexp.Regexp // the r:null cmd to look for back from tinyg indicating line processed

	//reCmdsWithNoRResponse *regexp.Regexp // since we're using slot approach, we expect an r:{} response, but some commands don't give that so just don't expect it
	//SlotMax               int            // queue into tinyg using slot approach
	//SlotCtr               int            // queue into tinyg using slot approach

	//lock *sync.Mutex // use a lock/unlock instead of sem chan int

	// do buffer size counting approach instead
	//BufferMax int
	rePacketCtr *regexp.Regexp

	PacketCtrMin       int
	PacketCtrMax       int
	PacketCtrAvail     int
	isInReSyncMode     bool
	reSyncCtr          int
	reSyncCtrTriggerAt int

	//BufferSize      int
	//BufferSizeArray []int
	//BufferCmdArray  []string

	// Use the queue that has an integer id instead
	q      *QueueTid
	tidCtr int

	// use thread locking for b.Paused
	lock *sync.Mutex

	// use thread locking for b.ManualPaused
	manualLock *sync.Mutex

	// use more thread locking for b.semLock
	semLock *sync.Mutex

	// for packet ctr mode since we don't use Queue.go as our ctr, we do our own locking
	packetCtrLock *sync.Mutex

	testDropCtr int
}

func (b *BufferflowTinygPktMode) Init() {

	b.Paused = false
	b.ManualPaused = false
	b.lock = &sync.Mutex{}
	b.manualLock = &sync.Mutex{}
	b.semLock = &sync.Mutex{}
	b.packetCtrLock = &sync.Mutex{}

	//b.SetPaused(false, 2)

	/* Slot Approach */
	//b.SlotMax = 4 // at most queue up 2 slots, i.e. 2 gcode commands
	//b.SlotCtr = 0 // 0 indicates no gcode lines have been queued into tinyg
	// the regular expression to turn off the pause
	// this regexp will find the r:null response which indicates
	// a line of gcode was processed and thus we can send the next one
	// {"r":{},"f":[1,0,33,134]}
	// when we see this, decrement the b.SlotCtr
	b.reSlotDone, _ = regexp.Compile("{\"r\":{")
	// when we see the response to an rx query so we know how many chars
	// are sitting in the serial buffer
	b.reRxResponse, _ = regexp.Compile("{\"rx\":")
	b.reFlowChar, _ = regexp.Compile("\u0011|\u0013")

	//b.reCmdsWithNoRResponse, _ = regexp.Compile("[!~%]")
	//log.Printf("Using slot approach for TinyG buffering. slotMax:%v, slotCtr:%v\n", b.SlotMax, b.SlotCtr)

	/* End Slot Approach Items */

	/* Start Buffer Size Approach Items */
	//b.BufferMax = 200 //max buffer size 254 bytes available

	//b.BufferSize = 0  //initialize buffer at zero bytes
	b.q = NewQueueTid()
	b.tidCtr = 0

	//b.lock = sync.Mutex
	/* End Buffer Size Approach */

	// Packet Mode Counters
	b.rePacketCtr, _ = regexp.Compile("\"f\":\\[\\d+,\\d+,(\\d+)")
	b.PacketCtrMin = 10   // 3   // if you are at this many packet mode slots left, stop, don't go below it
	b.PacketCtrAvail = 24 //6 // main variable keeping track of how many Line Mode slots are avaialable in TinyG, this is the default number that TinyG starts with
	b.PacketCtrMax = 24   // how many Line Mode packet counters there are max on the TinyG firmware
	b.isInReSyncMode = false
	b.testDropCtr = 0
	b.reSyncCtr = 0
	b.reSyncCtrTriggerAt = 41 // the number of processed Gcode lines that have us a trigger a resync

	//b.StartSending = 20
	//b.StopSending = 18
	//b.PauseOnEachSend = 0 * time.Millisecond

	// make buffered channel big enough we won't overflow it
	// meaning we get told b.sem on incoming data, so at most this could
	// be the size of 1 character and the TinyG only allows 255, so just
	// go high to make sure it's high enough to never block
	// buffered
	b.sem = make(chan int, 1000)
	// non-buffered
	//b.sem = make(chan int)

	// start tinyg out in bypass mode because we don't really
	// know if user put tinyg into qr response mode. what we'll
	// do is watch for our first qr response and then assume we're
	// in active mode, i.e. b.BypassMode should then be set to false
	// the reason for this is if we think tinyg is going to send qr
	// responses and we don't get them, we end up holding up all data
	// and essentially break everything. so gotta really watch for this.
	//b.BypassMode = true
	// looking like bypassmode isn't very helpful
	//b.BypassMode = false

	// the regular expression to find the qr value
	// this regexp will find qr when in json mode or non-json mode on tinyg
	b.re, _ = regexp.Compile("\"{0,1}qr\"{0,1}:(\\d+)")

	//reWipeToQr, _ = regexp.Compile("(?s)^.*?\"qr\":\\d+")

	// we split the incoming data on newline using this regexp
	// tinyg seems to only send \n but look for \n\r optionally just in case
	b.reNewLine, _ = regexp.Compile("\\r{0,1}\\n")

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

	// this regexp catches !, ~, %, \n, $ by itself, or $$ by itself and indicates
	// no r:{} response will come back so don't expect it
	b.reNoResponse, _ = regexp.Compile("^[!~%\n$?]")

	// if we get a cmd with a $ at the start or a ? at start, append
	// a new command that will put tinyg back in json mode
	b.rePutBackInJsonMode, _ = regexp.Compile("^[$?]")

	// see if they tried to turn off json verbosity, which will break things
	b.reJsonVerbositySetTo0, _ = regexp.Compile("(\\$jv\\=0|\\{\"jv\"\\:0\\})")

	// see if they tried to turn on CRLF, which will break things
	b.reCrLfSetTo1, _ = regexp.Compile("(\\$ec\\=1|\\{\"ec\"\\:1\\})")

	b.reComment, _ = regexp.Compile("\\(.*?\\)")
	b.reComment2, _ = regexp.Compile(";.*")

	//initialize query loop
	//b.rxQueryLoop(b.parent_serport)

	//go spWrite("send " + b.parent_serport.portConf.Name + " {rxm:1}\n")

	go func() {
		time.Sleep(1 * time.Millisecond)
		spWriteJson("sendjson {\"P\":\"" + b.parent_serport.portConf.Name + "\",\"Data\":[{\"D\":\"" + "{\\\"rxm\\\":1}\\n\", \"Id\":\"internalInit0\"}]}")

	}()

	/*
		var wr writeRequest
		wr.p = b.parent_serport
		wr.d = "{rxm:1}\n"
		wr.id = "internalInit1"
		write(wr, "internalInit2")
	*/

	// send init cmd once to put in packet mode
	/*
		var wrj writeRequestJson
		//var wd []writeRequestJsonData
		wrj.Data = make([]writeRequestJsonData, 1)
		wrj.Data[0].D = "{rxm:1}\n"
		wrj.Data[0].Id = "internalInit1"

		//wrj.Data = wd
		wrj.P = b.parent_serport.portConf.Name
		wrj.p = b.parent_serport
		log.Printf("about to write init json: %v", wrj)
		writeJson(wrj)
	*/

	/*var cmd Cmd
	cmd.data = "{rxm:1}\n"
	cmd.id = "internalInit1"
	cmd.willHandleCompleteResponse = true
	b.parent_serport.sendBuffered <- cmd */
}

func (b *BufferflowTinygPktMode) RewriteSerialData(cmd string, id string) string {
	return ""
}

// Line Mode approach
func (b *BufferflowTinygPktMode) BlockUntilReady(cmd string, id string) (bool, bool, string) {

	// Lock the packet ctr at start and then end
	b.packetCtrLock.Lock()

	log.Printf("BlockUntilReady() Start\n")
	log.Printf("\tid:%v, cmd:%v\n", id, strings.Replace(cmd, "\n", "\\n", -1))

	// if we mangle the gcode
	newCmd := ""

	//defer b.packetCtrLock.Unlock()

	// Only increment if cmd is something we'll get an r:{} response to
	isReturnsNoResponse := b.SeeIfSpecificCommandsReturnNoResponse(cmd)
	if isReturnsNoResponse == false {

		// we are using our new Queue that tracks a "tid" or a transaction ID. This
		// is a new feature of TinyG where we can send it a transaction id and it will
		// send that tid back to us with the r:{} that corresponds to it so we can
		// see if errors ever occur
		tid := b.tidCtr
		b.tidCtr++
		reRemoveInlineComment := regexp.MustCompile("\\(.*\\)")
		//newCmd = b.reNewLine.ReplaceAllString(cmd, "")
		newCmd = strings.TrimSpace(cmd)

		// see if this is trackable or not
		// if it starts with $ or ? or % or ~ or ! or is a newline it is not trackable
		reWillItNotGiveR := regexp.MustCompile("^[$?~!%]")
		reIsItJson := regexp.MustCompile("^{")
		reLastCurly := regexp.MustCompile("}$")
		if len(newCmd) < 1 || reWillItNotGiveR.MatchString(newCmd) {
			log.Printf("\tThis cmd will not give us back an R so track it that way. cmd: %v\n", newCmd)
			tid = -1
			b.tidCtr--
		} else if reIsItJson.MatchString(newCmd) {
			// this is json, inject a tid
			log.Printf("\tThis cmd is JSON so we will add a parameter\n")
			newCmd = reLastCurly.ReplaceAllString(newCmd, "")
			newCmd = fmt.Sprintf("%v, tid:%v}\n", newCmd, tid)
			// for now don't track cuz bugs
			tid = -1
			b.tidCtr--
		} else {
			log.Printf("\tThis cmd is Gcode and we will do an inline comment\n")
			newCmd = reRemoveInlineComment.ReplaceAllString(newCmd, "")
			newCmd = fmt.Sprintf("%v ({tid:%v})\n", newCmd, tid)
		}
		b.q.Push(newCmd, id, tid)
		b.PacketCtrAvail--

		/*
			log.Printf("Going to lock inside BlockUntilReady to up the BufferSize and Arrays\n")
			b.lock.Lock()
			b.BufferSize += len(cmd)
			b.BufferSizeArray = append(b.BufferSizeArray, len(cmd))
			b.BufferCmdArray = append(b.BufferCmdArray, cmd)
			b.lock.Unlock()
			log.Printf("Done locking inside BlockUntilReady to up the BufferSize and Arrays\n")
		*/
	} else {
		// this is sketchy. could we overrun the buffer by not counting !~%\n
		// so to give extra room don't actually allow full serial buffer to
		// be used in b.BufferMax
		//log.Printf("Not incrementing buffer size for cmd:%v\n", cmd)

		// one other idea here is to go ahead and send this but add a {"rx":n} request after
		// it so that we do get a packet mode ctr back, or to shove off a sub-process that
		// asks for one 5 seconds later

	}

	log.Printf("\tNumber of packet mode slots currently available: %v\n", b.PacketCtrAvail)
	log.Printf("\tNumber of lines that are in the TinyG buffer:    %v\n", b.q.Len())

	isNeedToUnlock := true

	// count the amount of outbound lines that will get back an r:{} response
	// and then re-sync after a set amount to reset our Line Mode counter
	/*b.reSyncCtr++
	if b.reSyncCtr >= b.reSyncCtrTriggerAt {

		// we need to do a re-sync
		b.reSyncStart()

		// clear all b.sem signals so when we block below, we truly block
		b.ClearOutSemaphore()

		log.Println("\tBlocking on b.sem for re-sync until told from OnIncomingData to go")

		// since we need other code to run while we're blocking, we better release the packet ctr lock
		b.packetCtrLock.Unlock()

		unblockType, ok := <-b.sem // will block until told from OnIncomingData to go

		log.Printf("\tDone blocking for re-sync cuz got b.sem semaphore release. ok:%v, unblockType:%v\n", ok, unblockType)

		// since we already unlocked this thread, note it so we don't doubly unlock
		isNeedToUnlock = false

	} else */if b.PacketCtrAvail <= b.PacketCtrMin {

		log.Printf("\tThe PacketCtrAvail (%v) is at PacketCtrMin (%v), so we are going to pause.\n", b.PacketCtrAvail, b.PacketCtrMin)

		b.SetPaused(true, 0) // b.Paused = true

		// We are being asked to pause our sending of commands

		// clear all b.sem signals so when we block below, we truly block
		b.ClearOutSemaphore()

		log.Println("\tBlocking on b.sem until told from OnIncomingData to go")

		// since we need other code to run while we're blocking, we better release the packet ctr lock
		b.packetCtrLock.Unlock()

		unblockType, ok := <-b.sem // will block until told from OnIncomingData to go

		log.Printf("\tDone blocking cuz got b.sem semaphore release. ok:%v, unblockType:%v\n", ok, unblockType)

		// we get an unblockType of 1 for normal unblocks
		// we get an unblockType of 2 when we're being asked to wipe the buffer, i.e. from a % cmd
		if unblockType == 2 {
			log.Println("\tThis was an unblock of type 2, which means we're being asked to wipe internal buffer. so return false.")
			// returning false asks the calling method to wipe the serial send once
			// this function returns
			log.Printf("BlockUntilReady(cmd:%v, id:%v) End\n", cmd, id)
			return false, false, ""
		}

		// since we already unlocked this thread, note it so we don't doubly unlock
		isNeedToUnlock = false
	}

	// we will get here when we're done blocking and if we weren't cancelled
	// if this cmd returns no response, we need to generate a fake "Complete"
	// so do it now
	willHandleCompleteResponse := true
	if isReturnsNoResponse == true {
		willHandleCompleteResponse = false
	}

	log.Printf("BlockUntilReady() End\n")

	// we are done with using the packet ctr data, so can unlock
	if isNeedToUnlock {
		b.packetCtrLock.Unlock()
	}

	// let's yeild for 10ms just to give TinyG a chance to send us some damn data back
	//time.Sleep(1 * time.Millisecond)

	return true, willHandleCompleteResponse, newCmd
}

// Serial buffer size approach
func (b *BufferflowTinygPktMode) OnIncomingData(data string) {

	//log.Printf("OnIncomingData() start. data:%q\n", data)
	//log.Printf("< %q\n", data)

	// Since OnIncomingData is in the reader thread, lock so the writer
	// thread doesn't get messed up from all the bufferarray counting we're doing
	//b.lock.Lock()
	//defer b.lock.Unlock()

	b.LatestData += data

	//it was found ok was only received with status responses until the grbl buffer is full.
	//b.LatestData = regexp.MustCompile(">\\r\\nok").ReplaceAllString(b.LatestData, ">") //remove oks from status responses

	arrLines := b.reNewLine.Split(b.LatestData, -1)
	//js, _ := json.Marshal(arrLines)
	//log.Printf("cnt:%v, arrLines:%v\n", len(arrLines), string(js))

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
		return
	}

	// if we made it here we have lines to analyze
	// so analyze all of them except the last line

	// Lock the packet ctr at start and then end
	b.packetCtrLock.Lock()
	defer b.packetCtrLock.Unlock()
	log.Printf("OnIncomingData() Start.")

	for _, element := range arrLines[:len(arrLines)-1] {
		//log.Printf("Working on element:%v, index:%v", element, index)
		//log.Printf("Working on element:%v, index:%v", element)
		log.Printf("\t< %v", element)

		// COMMENT THIS SECTION OUT WHEN IN PRODUCTION
		// Random r:{} dropping for test cases
		// We are getting stalled jobs in super random use cases, so what we need to do
		// is test out how this algorithm performs during a re-sync to ask TinyG what our
		// counters are at. So, randomly drop r:{}'s to mimic what takes hours to achieve
		// in a real world test.
		/*if b.reSlotDone.MatchString(element) {
			// should we drop or not? how about we drop every 10th
			b.testDropCtr++
			if b.testDropCtr == 10 {
				// drop the r:{}
				log.Println("\tDropping this line for test purposes: %v", element)
				element = ""
				b.testDropCtr = 0
			}
		}*/
		// END COMMENT THIS SECTION OUT WHEN IN PRODUCTION

		//check for r:{} response indicating a gcode line has been processed
		if b.reSlotDone.MatchString(element) {

			// ok, a line has been processed, the if statement below better
			// be guaranteed to be true, cuz if its not we did something wrong
			if b.q.Len() > 0 {

				doneCmd, id, tidLocal := b.q.Poll()

				// We know what our TID is, but we need to extract it from the response from TinyG
				// to make sure it's the same as what we expect. If it's not lets go into evasive action.
				reTid := regexp.MustCompile("\"tid\":(\\d+)")
				tidRemoteArr := reTid.FindStringSubmatch(element)

				// if we are expecting a tid based on our local tid and
				// and we actually have a tid from the remote tid from Tinyg
				if tidLocal >= 0 && len(tidRemoteArr) > 0 {

					// now make sure it really was an integer
					if tidRemote, errTid := strconv.Atoi(tidRemoteArr[1]); errTid == nil {

						// see if our local and remote tid's match. they should!!! if they don't, problemo
						if tidRemote == tidLocal {
							log.Printf("\tAwesome. Our local and remote tids matched. That means we can simply decrement our Line Mode counter and move on with our lives. tid: %v\n", tidLocal)

							// Line Mode Counter decrement
							// For now we are going back to the idea of decrementing the Line Mode counter
							// on the receipt of a r:{} response and decrementing our local counter. This
							// idea is like Character Counting and relies on the local variable being super
							// accurate. We know an r:{} can occasionally get dropped on the floor and thus
							// this approach needs an occasional stop-the-world re-sync, but on a normal per
							// line basis we decrement on the r:{} to keep our local count up to date
							//if !b.isInReSyncMode {
							b.onGotLineModeCounterFromTinyG(b.PacketCtrAvail + 1)
							//} else {
							//	log.Println("\tWe are in re-sync mode so this incoming r:{} will do nothing for us resetting Line Mode ctr until done with re-sync mode")
							//}

							// Send cmd:"Complete" back
							m := DataCmdComplete{"Complete", id, b.Port, b.q.LenOfCmds(), doneCmd}
							bm, err := json.Marshal(m)
							if err == nil {
								h.broadcastSys <- bm
							}

							log.Printf("\tLine completed. New list len: %v, id: %v, line: %v\n", b.q.Len(), id, strings.Replace(doneCmd, "\n", "\\n", -1))

						} else {

							// uh oh. we have a remote tid and local tid, but they don't match. let's just exit for now.
							log.Printf("\tEVASIVE ACTION: Our remote tid does not match our local tid. remote tid: %v, local tid: %v, local gcode (doneCmd): %v, local id: %v, remote r:{}: %v\n", tidRemote, tidLocal, doneCmd, id, element)

							// it is most likely an r:{} got dropped. it's rare, but it's the main bug we have
							// been struggling with. so, it is most likely that the r:{} we just got is for the next
							// line
							nextLineDoneCmd, nextLineId, nextLineTidLocal := b.q.Poll()
							if nextLineTidLocal == tidRemote {
								log.Printf("\tYup, the next line in our local queue matched, so we did miss an r:{} but we are back on track. remote tid: %v, nextLineTidLocal: %v, nextLineDoneCmd (local gcode): %v, nextLine local id: %v, remote r:{}: %v\n", tidRemote, nextLineTidLocal, nextLineDoneCmd, nextLineId, element)

								b.onGotLineModeCounterFromTinyG(b.PacketCtrAvail + 1)

								// Send cmd:"Complete" back
								m := DataCmdComplete{"Complete", id, b.Port, b.q.LenOfCmds(), doneCmd}
								bm, err := json.Marshal(m)
								if err == nil {
									h.broadcastSys <- bm
								}

								log.Printf("\tEVASIVE ACTION FIXED: Line completed. New list len: %v, id: %v, line: %v\n", b.q.Len(), id, strings.Replace(doneCmd, "\n", "\\n", -1))

							}
						}
					}

				} else {

					if tidLocal < 0 {
						log.Printf("\tWe don't have a local tid which meant this was a non-trackable line, so ignore it. element: %v, doneCmd: %v, id: %v, tid: %v\n", element, doneCmd, id, tidLocal)
					} else {

						// see if this is an init r:{} which i think is unfair that TinyG is sending back because
						// there was no actual request for this response
						reIsInitResponse := regexp.MustCompile("\"msg\":\"SYSTEM READY\"")
						if reIsInitResponse.MatchString(element) {
							// this is init cmd, totally skip it. don't send back a complete either cuz it's not a command
							// we were ever sent
							log.Println("\tGot an init cmd r:{} that we never asked for, so ignoring.")

							// TODO: we need to put the b.q.Poll() back into the queue cuz it's not related
							b.q.Shift(doneCmd, id, tidLocal)

						} else {

							log.Printf("\tEVASIVE ACTION: We do have a local tid but we did not get a tid back from tinyg. That's bad. local tid: %v, local gcode (doneCmd): %v, local id: %v, remote r:{}: %v\n", tidLocal, doneCmd, id, element)
							log.Printf("\twhat this most likely means is that we sent a non-gcode command where we can't specify a tid so we are just going on faith nothing got messed up here")

							b.onGotLineModeCounterFromTinyG(b.PacketCtrAvail + 1)

							// Send cmd:"Complete" back
							m := DataCmdComplete{"Complete", id, b.Port, b.q.LenOfCmds(), doneCmd}
							bm, err := json.Marshal(m)
							if err == nil {
								h.broadcastSys <- bm
							}
						}

						log.Printf("\tEVASIVE ACTION FIXED: Line completed. New list len: %v, id: %v, line: %v\n", b.q.Len(), id, strings.Replace(doneCmd, "\n", "\\n", -1))

					}
				}

			} else {
				log.Printf("\tWe should RARELY get here cuz we should have a command in the queue to dequeue when we get the r:{} response. If you see this debug stmt this is one of those few instances where TinyG sent us a r:{} not in response to a command we sent.")
			}

			// Line Mode Counter
			// We are now ignoring this data and resorting to decrementing when an r:{}
			// is received instead. So the code above us is being used instead.
			/*
				// In this mode we have to look at the footer and parse it to see how our packet ctr is doing
				// A typical line looks like this:
				// {"r":{"ej":1},"f":[3,0,4]}
				pktCtrArr := b.rePacketCtr.FindStringSubmatch(element)
				// if we got back an index 1 val (i.e. the digit) and it's parseable as an integer
				// that means we got back the packet mode counter and can pivot off of it
				if len(pktCtrArr) > 0 {

					// now make sure it really was an integer
					if pktCtr, err5 := strconv.Atoi(pktCtrArr[1]); err5 == nil {

						// what to do when we actually get back a Line Mode counter from TinyG
						onGotLineModeCounterFromTinyG(pktCtr)

					} else {
						log.Printf("\tERROR: We could not parse an integer from the footer packet mode ctr???\n")
					}

				} else {
					log.Printf("\tERROR: We got an r:{} response but could not parse out the footer packet mode counter.\n")
				}
			*/

		}

		// see if we are in re-sync mode
		if b.isInReSyncMode {

			// if regexp.MatchString("{\"r\":\{\"rx\":", element) {
			if b.reRxResponse.MatchString(element) {

				log.Printf("\tWe are in re-sync mode and looks like we just got our rx: response: %v\n", element)
				b.reSyncEnd(element)

			} else {
				log.Println("\tIn re-sync mode, but this line was not an rx: val")
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

	// now wipe the LatestData to only have the last line that we did not analyze
	// because we didn't know/think that was a full command yet
	b.LatestData = arrLines[len(arrLines)-1]

	// we are losing incoming serial data because of garbageCollection()
	// doing a "stop the world" and all this data queues up back on the
	// tinyg and we miss stuff coming in, which gets our serial counter off
	// and then causes stalling, so we're going to attempt to force garbageCollection
	// each time we get data so that we don't have pauses as long as we were having
	if *gcType == "max" {
		debug.FreeOSMemory()
	}

	//time.Sleep(3000 * time.Millisecond)
	log.Printf("OnIncomingData() End.\n")
}

func (b *BufferflowTinygPktMode) reSyncStart() {

	// Ok, this method is a stop-the-world approach to syncing our
	// Line Mode counter with what TinyG says it should be. What we do
	// is pause all outgoing traffic. Send a {rx:n} request and then
	// only when we get this counter back do we reset our b.PacketAvailCtr
	// to that count and we know it's authoritative again
	log.Println("Re-sync: Started")

	b.Pause()
	b.isInReSyncMode = true

	//b.Unpause()
	go func() {
		time.Sleep(1000 * time.Millisecond)
		spWriteJson("sendjson {\"P\":\"" + b.parent_serport.portConf.Name + "\",\"Data\":[{\"D\":\"" + "{\\\"rx\\\":null}\\n\", \"Buf\":\"NoBuf\", \"Id\":\"resync1\"}]}")

	}()
	go func() {
		time.Sleep(1200 * time.Millisecond)
		spWriteJson("sendjson {\"P\":\"" + b.parent_serport.portConf.Name + "\",\"Data\":[{\"D\":\"" + "{\\\"rx\\\":null}\\n\", \"Buf\":\"NoBuf\", \"Id\":\"resync1\"}]}")

	}()
}

func (b *BufferflowTinygPktMode) reSyncEnd(gcodeLine string) {

	// Ok, this method is a stop-the-world approach to syncing our
	// Line Mode counter with what TinyG says it should be. What we do
	// is pause all outgoing traffic. Send a {rx:n} request and then
	// only when we get this counter back do we reset our b.PacketAvailCtr
	// to that count and we know it's authoritative again
	//b.Pause()
	b.isInReSyncMode = false
	b.reSyncCtr = 0

	// parse the incoming line so we can extract the counter
	// SEND > {"rx":null}
	// RECV < {"r":{"rx":30},"f":[3,0,30]}
	reLineModeRxVal := regexp.MustCompile("\"rx\":(\\d+)")
	lineRxCtrArr := reLineModeRxVal.FindStringSubmatch(gcodeLine)

	// if we got back an index 1 val (i.e. the digit) and it's parseable as an integer
	// that means we got back the packet mode counter and can pivot off of it
	if len(lineRxCtrArr) > 0 {

		// now make sure it really was an integer
		if lineCtr, err5 := strconv.Atoi(lineRxCtrArr[1]); err5 == nil {

			// what to do when we actually get back a Line Mode counter from TinyG
			//b.onGotLineModeCounterFromTinyG(lineCtr)

			// we just got back what we think is an authoritative answer
			// let's spit out debug to see how far off we are
			log.Printf("Re-sync got back authoritative answer. What we think we should have as available: %v, what TinyG just told us: %v\n", b.PacketCtrAvail, lineCtr)

			// set our current packet ctr to this val cuz it's authoritative
			b.PacketCtrAvail = lineCtr

		} else {
			log.Printf("\tERROR in Re-Sync: We could not parse an integer from the Line Mode ctr???\n")
		}

	} else {
		log.Printf("\tERROR in Re-Sync: We got an r:{\"rx\":...} response but could not parse out the Line Mode counter.\n")
	}

	//b.Unpause()
	log.Println("Re-sync: End")

}

func (b *BufferflowTinygPktMode) onGotLineModeCounterFromTinyG(pktCtr int) {
	// we got back a packet ctr

	// set our current packet ctr to this val cuz it's authoritative
	b.PacketCtrAvail = pktCtr

	// now see if it's above our minimum. if it is make sure we're not paused
	if b.PacketCtrAvail > b.PacketCtrMin {

		// we can make sure we are not paused
		if b.GetPaused() {
			log.Printf("\tPacketCtrAvail: %v, we are paused so unpausing\n", b.PacketCtrAvail)

			// we are paused, but we can't just go unpause ourself, because we may
			// be manually paused. this means we have to do a double-check here
			// and not just go unpausing ourself just cuz we think there's room in the buffer.
			// this is because we could have just sent a ! to the tinyg. we may still
			// get back some random r:{} after the ! was sent, and that would mean we think
			// we can go sending more data, but really we can't cuz we were HARD Manually paused
			if b.GetManualPaused() == false {

				// we are not in a manual pause state, that means we can go ahead
				// and unpause ourselves
				b.SetPaused(false, 1) //set paused to false first, then release the hold on the buffer
			} else {
				log.Println("\tWe just got incoming r:{} so we could unpause, but since manual paused we will ignore until next time a r:{} comes in to unpause")
			}
		} else {
			log.Printf("\tPacketCtrAvail: %v, not paused and ok to not be, so moving on...\n", b.PacketCtrAvail)
		}

	} else {

		// the packet ctr is less than or equal to our minimum
		if b.PacketCtrAvail == b.PacketCtrMin {
			// TinyG just told us an incoming packet mode ctr and it is at our minimum.
			// We should already be paused from the BlockUntilReady method.
			if b.GetPaused() {
				log.Printf("\tPacketCtrAvail: %v, we are paused and will stay paused\n", b.PacketCtrAvail)
			} else {

				log.Printf("\tPacketCtrAvail: %v, we are NOT paused so WARNING WARNING WARNING\n", b.PacketCtrAvail)
			}
		} else {
			// It is less then our minimum which should never happen
			log.Printf("\tPacketCtrAvail: %v, ERROR we should never be below our allowed minimum. That is BAD!!!\n", b.PacketCtrAvail)
		}
	}
}

// Clean out b.sem so it can truly block
func (b *BufferflowTinygPktMode) ClearOutSemaphore() {
	ctr := 0

	keepLooping := true
	for keepLooping {
		select {
		case _, ok := <-b.sem: // case d, ok :=
			//log.Printf("Consuming b.sem queue to clear it before we block. ok:%v, d:%v\n", ok, string(d))
			ctr++
			if ok == false {
				keepLooping = false
			}
		default:
			keepLooping = false
			//log.Println("Hit default in select clause")
		}
	}
	//log.Printf("Done consuming b.sem queue so we're good to block on it now. ctr:%v\n", ctr)
	// ok, all b.sem signals are now consumed into la-la land

}

// break commands into individual commands
// so, for example, break on newlines to separate commands
// or, in the case of ~% break those onto separate commands
func (b *BufferflowTinygPktMode) BreakApartCommands(cmd string) []string {
	// add newline after !~%
	reSingle := regexp.MustCompile("([!~%])")
	cmd = reSingle.ReplaceAllString(cmd, "$1\n")
	cmds := strings.Split(cmd, "\n")
	//log.Printf("Len of cmds array after split:%v\n", len(cmds))
	//json, _ := json.Marshal(cmds)
	//log.Printf("cmds after split:%v\n", json)
	finalCmds := []string{}
	if len(cmds) == 1 {
		item := cmds[0]
		// just put cmd back in with newline
		if reSingle.MatchString(item) {
			//log.Printf("len1. Added cmd back. Not re-adding newline cuz artificially added one earlier. item:'%v'\n", item)
			finalCmds = append(finalCmds, item)
		} else {
			item = item + "\n"
			//log.Printf("len1. Re-adding item to finalCmds with newline:'%v'\n", item)
			finalCmds = append(finalCmds, item)
		}
	} else {
		for index, item := range cmds {
			// since more than 1 cmd, loop thru
			if reSingle.MatchString(item) {
				//log.Printf("Added cmd back. Not re-adding newline cuz artificially added one earlier. item:'%v'\n", item)
				finalCmds = append(finalCmds, item)
			} else {
				// should we add back our newline? do this if there are elements after us
				if index < len(cmds)-1 {
					// there are cmds after me, so add newline
					//log.Printf("Re-adding newline to item:%v\n", item)
					s := item + "\n"
					finalCmds = append(finalCmds, s)
					//log.Printf("Added cmd back with newline. New cmd item:'%v'\n", s)
				} else {
					//log.Printf("Skipping adding cmd back cuz just empty newline. item:'%v'\n", item)
					//log.Printf("Re-adding item to finalCmds without adding newline:%v\n", item)
					//finalCmds = append(finalCmds, item)
				}

			}
		}
	}

	// loop 1 more time to do some rewriting
	newFinalCmds := []string{}
	for _, item := range finalCmds {
		// remove comments
		//item = b.reComment.ReplaceAllString(item, "")
		//item = b.reComment2.ReplaceAllString(item, "")

		// see if we need to override a cmd to not screw stuff up for us
		// if user sets json verbosity to 0, reset it back
		if match := b.reJsonVerbositySetTo0.MatchString(item); match {
			// they turned off json verbosity, shame on them, override it
			// by setting back
			newFinalCmds = append(newFinalCmds, "{\"jv\":1}\n")
		} else if match := b.reCrLfSetTo1.MatchString(item); match {
			// they turned off json verbosity, shame on them, override it
			// by setting back
			newFinalCmds = append(newFinalCmds, "{\"ec\":0}\n")

		} else {

			// just put the command back into the array without modifying
			newFinalCmds = append(newFinalCmds, item)
		}

		// see if need to put back in json mode
		if match := b.rePutBackInJsonMode.MatchString(item); match {
			// yes, this cmd needs to have us put tinyg back in json mode
			newFinalCmds = append(newFinalCmds, "{\"ej\":\"\"}\n")

			/*
				go func() {
					time.Sleep(1500 * time.Millisecond)
					spWriteJson("sendjson {\"P\":\"" + b.parent_serport.portConf.Name + "\",\"Data\":[{\"D\":\"" + "{\\\"ej\\\":1}\\n\", \"Id\":\"internalInit0\"}]}")

				}()
			*/

		}
	}

	//log.Printf("Final array of cmds after BreakApartCommands(). newFinalCmds:%v\n", newFinalCmds)
	return newFinalCmds
}

func (b *BufferflowTinygPktMode) Pause() {

	// Since we're tweaking b.Paused lock all threads
	//b.lock.Lock()
	//defer b.lock.Unlock()

	b.SetPaused(true, 0) //b.Paused = true
	//b.BypassMode = false // turn off bypassmode in case it's on
	//log.Println("Paused buffer on next BlockUntilReady() call")
	log.Println("Paused buffer")
}

func (b *BufferflowTinygPktMode) Unpause() {

	// Since we're tweaking b.Paused lock all threads
	//b.lock.Lock()
	//defer b.lock.Unlock()

	b.SetPaused(false, 1) //b.Paused = false
	//log.Println("Unpause(), so we will send signal of 1 to b.sem to unpause the BlockUntilReady() thread")

	// do this as go-routine so we don't block on the b.sem <- 1 write
	/*
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
	*/
	log.Println("Unpaused buffer") // inside BlockUntilReady() call")
}

func (b *BufferflowTinygPktMode) SeeIfSpecificCommandsShouldSkipBuffer(cmd string) bool {
	// remove comments
	cmd = b.reComment.ReplaceAllString(cmd, "")
	cmd = b.reComment2.ReplaceAllString(cmd, "")
	// \x18 is Ctrl+X which resets TinyG
	if match, _ := regexp.MatchString("[!~%]", cmd); match {
		log.Printf("Found cmd that should skip buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowTinygPktMode) SeeIfSpecificCommandsShouldPauseBuffer(cmd string) bool {
	// remove comments
	cmd = b.reComment.ReplaceAllString(cmd, "")
	cmd = b.reComment2.ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[!]", cmd); match {
		//log.Printf("Found cmd that should pause buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowTinygPktMode) SeeIfSpecificCommandsShouldUnpauseBuffer(cmd string) bool {
	// remove comments
	cmd = b.reComment.ReplaceAllString(cmd, "")
	cmd = b.reComment2.ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[~%]", cmd); match {
		//log.Printf("Found cmd that should unpause buffer. cmd:%v\n", cmd)
		return true
	}
	return false
}

func (b *BufferflowTinygPktMode) SeeIfSpecificCommandsShouldWipeBuffer(cmd string) bool {
	// remove comments
	cmd = b.reComment.ReplaceAllString(cmd, "")
	cmd = b.reComment2.ReplaceAllString(cmd, "")
	if match, _ := regexp.MatchString("[%]", cmd); match {
		//log.Printf("Found cmd that should wipe out and reset buffer. cmd:%v\n", cmd)

		// Since we're tweaking b.Paused lock all threads
		//b.lock.Lock()
		//defer b.lock.Unlock()

		//b.BufferSize = 0
		//b.BufferSizeArray = nil
		//b.BufferCmdArray = nil
		//b.q.Delete()

		b.onGotLineModeCounterFromTinyG(b.PacketCtrAvail + 1)

		// We need to get a new Line Mode report after a buffer wipe, so automatically
		// ask for a {rx:n} report after the wipe
		go func() {
			time.Sleep(100 * time.Millisecond)
			spWriteJson("sendjson {\"P\":\"" + b.parent_serport.portConf.Name + "\",\"Data\":[{\"D\":\"" + "{\\\"rx\\\":null}\\n\", \"Buf\":\"NoBuf\", \"Id\":\"internalWipe1\"}]}")

		}()

		return true
	}
	return false
}

func (b *BufferflowTinygPktMode) SeeIfSpecificCommandsReturnNoResponse(cmd string) bool {
	// remove comments
	//cmd = b.reComment.ReplaceAllString(cmd, "")
	//cmd = b.reComment2.ReplaceAllString(cmd, "")
	//log.Printf("Checking cmd:%v for no response?", cmd)
	if match := b.reNoResponse.MatchString(cmd); match {
		//log.Printf("Found cmd that does not get a response from TinyG. cmd:%v\n", cmd)
		return true
	}
	return false
}

// This is called if user wiped entire buffer of gcode commands queued up
// which is up to 25,000 of them. So, we need to release the OnBlockUntilReady()
// in a way where the command will not get executed, so send unblockType of 2
func (b *BufferflowTinygPktMode) ReleaseLock() {
	log.Println("Lock being released in TinyG buffer")

	b.q.Delete()
	//b.onGotLineModeCounterFromTinyG(b.PacketCtrAvail + 1)
	b.PacketCtrAvail = b.PacketCtrMax
	b.SetPaused(false, 2)
	/*
		// Since we're tweaking b.Paused lock all threads
		b.lock.Lock()

		b.Paused = false
		b.SlotCtr = 0
		b.BufferSize = 0
		b.BufferSizeArray = nil
		b.BufferCmdArray = nil

		b.lock.Unlock()
	*/
	/*
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
	*/
}

func (b *BufferflowTinygPktMode) IsBufferGloballySendingBackIncomingData() bool {
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

//Use this function to open a connection, write directly to serial port and close connection.
//This is used for sending query requests outside of the normal buffered operations that will pause to wait for room in the grbl buffer
//'?' is asynchronous to the normal buffer load and does not need to be paused when buffer full
func (b *BufferflowTinygPktMode) rxQueryLoop(p *serport) {
	b.parent_serport = p //make note of this port for use in clearing the buffer later, on error.
	ticker := time.NewTicker(5000 * time.Millisecond)
	b.quit = make(chan int)
	go func() {
		for {
			select {
			case <-ticker.C:

				// we'll write a lazy formatted version of json to reduce the amt of chars
				// chewed up since we're doing this outside the scope of the serial buffer counter
				n2, err := p.portIo.Write([]byte("{rx:n}\n"))

				log.Print("Just wrote ", n2, " bytes to serial: {rx:n}")

				if err != nil {
					errstr := "Error writing to " + p.portConf.Name + " " + err.Error() + " Closing port."
					log.Print(errstr)
					h.broadcastSys <- []byte(errstr)
					ticker.Stop() //stop query loop if we can't write to the port
					break
				}
			case <-b.quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func (b *BufferflowTinygPktMode) Close() {
	//stop the rx query loop when the serial port is closed off.
	log.Println("Stopping the RX query loop")
	b.ReleaseLock()
	b.Unpause()
	go func() {
		b.quit <- 1
	}()
}

//	Gets the paused state of this buffer
//	go-routine safe.
func (b *BufferflowTinygPktMode) GetPaused() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.Paused
}

//	Sets the paused state of this buffer
//	go-routine safe.
func (b *BufferflowTinygPktMode) SetPaused(isPaused bool, semRelease int) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.Paused = isPaused

	// only release semaphore if we are being told to unpause
	if b.Paused == false {
		// the BlockUntilReady thread should be sitting waiting
		// so when we send this should trigger it
		b.sem <- semRelease
		log.Println("\tJust sent release to b.sem so we will not block the sending to serial port anymore.")

		// since the first consuming of the semRelease will occur
		// by BlockUntilReady since it's sitting waiting then
		// we're good to go ahead and release the rest here
		// so our queue doesn't fill up
		// that's the theory anyway
		//b.ClearOutSemaphore()
	}
	//go func() {
	//log.Printf("StartSending Semaphore goroutine created for gcodeline:%v\n", gcodeline)
	//b.sem <- semRelease

	/*
		defer func() {
			//log.Printf("StartSending Semaphore just got consumed by the BlockUntilReady() thread for the gcodeline:%v\n", gcodeline)
		}()
	*/
	//}()
}

func (b *BufferflowTinygPktMode) GetManualPaused() bool {
	b.manualLock.Lock()
	defer b.manualLock.Unlock()
	return b.ManualPaused
}

func (b *BufferflowTinygPktMode) SetManualPaused(isPaused bool) {
	b.manualLock.Lock()
	defer b.manualLock.Unlock()
	b.ManualPaused = isPaused
}

func (b *BufferflowTinygPktMode) PacketCtrGet() int {
	b.packetCtrLock.Lock()
	defer b.packetCtrLock.Unlock()
	return b.PacketCtrAvail
}
func (b *BufferflowTinygPktMode) PacketCtrSet(val int) {
	b.packetCtrLock.Lock()
	defer b.packetCtrLock.Unlock()
	b.PacketCtrAvail = val
}
func (b *BufferflowTinygPktMode) PacketCtrDecr() int {
	b.packetCtrLock.Lock()
	defer b.packetCtrLock.Unlock()
	b.PacketCtrAvail--
	return b.PacketCtrAvail
}
func (b *BufferflowTinygPktMode) PacketCtrIncr() int {
	b.packetCtrLock.Lock()
	defer b.packetCtrLock.Unlock()
	b.PacketCtrAvail++
	return b.PacketCtrAvail
}
func (b *BufferflowTinygPktMode) PacketCtrIsTooLow() bool {
	b.packetCtrLock.Lock()
	defer b.packetCtrLock.Unlock()
	if b.PacketCtrAvail <= b.PacketCtrMin {
		return true
	} else {
		return false
	}
}
