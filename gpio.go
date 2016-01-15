// +build ignore
//+build !linux,!arm
// Ignore this file for now, but it would be nice to get GPIO going natively

package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
)

var (
	gpio GPIO
)

type GPIO struct {
	pinStates       map[string]PinState
	pinStateChanged chan PinState
	pinAdded        chan PinState
	pinRemoved      chan string
}

type Direction int
type PullUp int

type PinState struct {
	Pin    interface{} `json:"-"`
	PinId  string
	Dir    Direction
	State  byte
	Pullup PullUp
	Name   string
}

type PinDef struct {
	ID             string
	Aliases        []string
	Capabilities   []string
	DigitalLogical int
	AnalogLogical  int
}

const STATE_FILE = "pinstates.json"

const (
	In  Direction = 0
	Out Direction = 1
	PWM Direction = 2

	Pull_None PullUp = 0
	Pull_Up   PullUp = 1
	Pull_Down PullUp = 2
)

type GPIOInterface interface {
	PreInit()
	Init(chan PinState, chan PinState, chan string, map[string]PinState) error
	Close() error
	PinMap() ([]PinDef, error)
	Host() (string, error)
	PinStates() (map[string]PinState, error)
	PinInit(string, Direction, PullUp, string) error
	PinSet(string, byte) error
	PinRemove(string) error
}

func (g *GPIO) CleanupGpio() {
	pinStates, err := gpio.PinStates()
	if err != nil {
		log.Println("Error getting pinstates on cleanup: " + err.Error())
	} else {
		data, err := json.Marshal(pinStates)
		if err != nil {
			log.Println("Error marshalling pin states : " + err.Error())
		}
		ioutil.WriteFile(STATE_FILE, data, 0644)
	}
	gpio.Close()
	os.Exit(1)
}

// I took what Ben had in his main.go file and moved it here
func (g *GPIO) PreInit() {

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	go func() {
		for sig := range c {
			// sig is a ^C, handle it
			log.Printf("captured %v, cleaning up gpio and exiting..", sig)
			gpio.CleanupGpio()
		}
	}()

	stateChanged := make(chan PinState)
	pinRemoved := make(chan string)
	pinAdded := make(chan PinState)
	go func() {
		for {
			// start listening on stateChanged and pinRemoved channels and update hub as appropriate
			select {
			case pinState := <-stateChanged:
				go h.sendMsg("PinState", pinState)
			case pinName := <-pinRemoved:
				go h.sendMsg("PinRemoved", pinName)
			case pinState := <-pinAdded:
				go h.sendMsg("PinAdded", pinState)
			}
		}
	}()

	pinStates := make(map[string]PinState)

	// read existing pin states
	if _, err := os.Stat(STATE_FILE); err == nil {
		log.Println("Reading prexisting pinstate file : " + STATE_FILE)
		dat, err := ioutil.ReadFile(STATE_FILE)
		if err != nil {
			log.Println("Failed to read state file : " + STATE_FILE + " : " + err.Error())
			return
		}
		err = json.Unmarshal(dat, &pinStates)
		if err != nil {
			log.Println("Failed to unmarshal json : " + err.Error())
			return
		}
	}

	gpio.Init(stateChanged, pinAdded, pinRemoved, pinStates)

}

func (g *GPIO) Init(pinStateChanged chan PinState, pinAdded chan PinState, pinRemoved chan string, states map[string]PinState) error {
	g.pinStateChanged = pinStateChanged
	g.pinRemoved = pinRemoved
	g.pinAdded = pinAdded
	g.pinStates = states

	// now init pins
	for key, pinState := range g.pinStates {
		if pinState.Name == "" {
			pinState.Name = pinState.PinId
		}
		g.PinInit(key, pinState.Dir, pinState.Pullup, pinState.Name)
		g.PinSet(key, pinState.State)
	}
	return nil
}

func (g *GPIO) Close() error {
	return nil
}
func (g *GPIO) PinMap() ([]PinDef, error) {
	// return a mock pinmap for this mock interface
	pinmap := []PinDef{
		{
			"P8_07",
			[]string{"66", "GPIO_66", "TIMER4"},
			[]string{"analog", "digital", "pwm"},
			66,
			0,
		}, {
			"P8_08",
			[]string{"67", "GPIO_67", "TIMER7"},
			[]string{"analog", "digital", "pwm"},
			67,
			0,
		}, {
			"P8_09",
			[]string{"69", "GPIO_69", "TIMER5"},
			[]string{"analog", "digital", "pwm"},
			69,
			0,
		}, {
			"P8_10",
			[]string{"68", "GPIO_68", "TIMER6"},
			[]string{"analog", "digital", "pwm"},
			68,
			0,
		}, {
			"P8_11",
			[]string{"45", "GPIO_45"},
			[]string{"analog", "digital", "pwm"},
			45,
			0,
		},
	}
	return pinmap, nil
}
func (g *GPIO) Host() (string, error) {
	return "fake", nil
}
func (g *GPIO) PinStates() (map[string]PinState, error) {
	return g.pinStates, nil
}
func (g *GPIO) PinInit(pinId string, dir Direction, pullup PullUp, name string) error {
	// add a pin

	// look up internal ID (we're going to assume its correct already)

	// make a pinstate object
	pinState := PinState{
		nil,
		pinId,
		dir,
		0,
		pullup,
		name,
	}

	g.pinStates[pinId] = pinState

	g.pinAdded <- pinState
	return nil
}
func (g *GPIO) PinSet(pinId string, val byte) error {
	// change pin state
	if pin, ok := g.pinStates[pinId]; ok {
		// we have a value....
		pin.State = val
		g.pinStates[pinId] = pin
		// notify channel of new pinstate
		g.pinStateChanged <- pin
	}
	return nil
}
func (g *GPIO) PinRemove(pinId string) error {
	// remove a pin
	if _, ok := g.pinStates[pinId]; ok {
		// normally you would close the pin here
		delete(g.pinStates, pinId)
		g.pinRemoved <- pinId
	}
	return nil
}
