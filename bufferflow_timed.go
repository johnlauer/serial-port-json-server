package main

import (
	"encoding/json"
	"log"
	"time"
)

type BufferflowTimed struct {
	Name           string
	Port           string
	Output         chan []byte
	Input          chan string
	ticker         *time.Ticker
	IsOpen         bool
	bufferedOutput string
}

/*
var (
	bufferedOutput string
)
*/

func (b *BufferflowTimed) Init() {
	log.Println("Initting timed buffer flow (output once every 16ms)")
	b.bufferedOutput = ""
	b.IsOpen = true

	go func() {
		for data := range b.Input {
			b.bufferedOutput = b.bufferedOutput + data

		}
	}()

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

}

func (b *BufferflowTimed) BlockUntilReady(cmd string, id string) (bool, bool, string) {
	//log.Printf("BlockUntilReady() start\n")
	return true, false, ""
}

func (b *BufferflowTimed) OnIncomingData(data string) {
	b.Input <- data
}

// Clean out b.sem so it can truly block
func (b *BufferflowTimed) ClearOutSemaphore() {
}

func (b *BufferflowTimed) BreakApartCommands(cmd string) []string {
	return []string{cmd}
}

func (b *BufferflowTimed) Pause() {
	return
}

func (b *BufferflowTimed) Unpause() {
	return
}

func (b *BufferflowTimed) SeeIfSpecificCommandsShouldSkipBuffer(cmd string) bool {
	return false
}

func (b *BufferflowTimed) SeeIfSpecificCommandsShouldPauseBuffer(cmd string) bool {
	return false
}

func (b *BufferflowTimed) SeeIfSpecificCommandsShouldUnpauseBuffer(cmd string) bool {
	return false
}

func (b *BufferflowTimed) SeeIfSpecificCommandsShouldWipeBuffer(cmd string) bool {
	return false
}

func (b *BufferflowTimed) SeeIfSpecificCommandsReturnNoResponse(cmd string) bool {
	return false
}

func (b *BufferflowTimed) ReleaseLock() {
}

func (b *BufferflowTimed) IsBufferGloballySendingBackIncomingData() bool {
	return true
}

func (b *BufferflowTimed) Close() {
	if b.IsOpen == false {
		// we are being asked a 2nd time to close when we already have
		// that will cause a panic
		log.Println("We got called a 2nd time to close, but already closed")
		return
	}
	b.IsOpen = false

	b.ticker.Stop()
	close(b.Input)
}

func (b *BufferflowTimed) GetManualPaused() bool {
	return false
}

func (b *BufferflowTimed) RewriteSerialData(cmd string, id string) string {
	return ""
}

func (b *BufferflowTimed) SetManualPaused(isPaused bool) {
}
