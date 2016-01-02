// +build linux,arm

package main

import (
	"errors"
	"log"
	"strconv"

	"github.com/kidoman/embd"
	_ "github.com/kidoman/embd/host/all"
)

type GPIO struct {
	pinStates       map[string]PinState
	pinStateChanged chan PinState
	pinAdded        chan PinState
	pinRemoved      chan string
}

func (g *GPIO) Init(pinStateChanged chan PinState, pinAdded chan PinState, pinRemoved chan string, states map[string]PinState) error {
	g.pinStateChanged = pinStateChanged
	g.pinRemoved = pinRemoved
	g.pinAdded = pinAdded
	g.pinStates = states

	// if its a raspberry pi initialize pi-blaster too
	host, _, err := embd.DetectHost()
	if err != nil {
		return err
	}
	if host == embd.HostRPi {
		InitBlaster()
	}

	err = embd.InitGPIO()
	if err != nil {
		return err
	}
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
	// close all the pins we have open if any
	for _, pinState := range g.pinStates {
		if pinState.Pin != nil {
			switch pinObj := pinState.Pin.(type) {
			case embd.DigitalPin:
				pinObj.Close()
			case embd.PWMPin:
				pinObj.Close()
			case BlasterPin:
				pinObj.Close()
			}
		}
	}

	// if its a raspberry pi close pi-blaster too
	host, _, err := embd.DetectHost()
	if err != nil {
		return err
	}
	if host == embd.HostRPi {
		CloseBlaster()
	}

	err = embd.CloseGPIO()
	if err != nil {
		return err
	}
	return nil
}
func (g *GPIO) PinMap() ([]PinDef, error) {
	desc, err := embd.DescribeHost()
	if err != nil {
		return nil, err
	}

	// wrap pinmap in a struct to make the json easier to parse on the other end
	embdMap := desc.GPIODriver().PinMap()
	pinMap := make([]PinDef, len(embdMap))
	// convert to PinDef format
	for i := 0; i < len(embdMap); i++ {
		pinDesc := embdMap[i]
		caps := make([]string, 0)

		if pinDesc.Caps&embd.CapDigital != 0 {
			caps = append(caps, "Digital")
		}
		if pinDesc.Caps&embd.CapAnalog != 0 {
			caps = append(caps, "Analog")
		}
		if pinDesc.Caps&embd.CapPWM != 0 {
			caps = append(caps, "PWM")
		}
		if pinDesc.Caps&embd.CapI2C != 0 {
			caps = append(caps, "I2C")
		}
		if pinDesc.Caps&embd.CapUART != 0 {
			caps = append(caps, "UART")
		}
		if pinDesc.Caps&embd.CapSPI != 0 {
			caps = append(caps, "SPI")
		}
		if pinDesc.Caps&embd.CapGPMC != 0 {
			caps = append(caps, "GPMC")
		}
		if pinDesc.Caps&embd.CapLCD != 0 {
			caps = append(caps, "LCD")
		}

		pinMap[i] = PinDef{
			pinDesc.ID,
			pinDesc.Aliases,
			caps,
			pinDesc.DigitalLogical,
			pinDesc.AnalogLogical,
		}
	}
	return pinMap, nil
}
func (g *GPIO) Host() (string, error) {
	host, _, err := embd.DetectHost()
	if err != nil {
		return "", err
	}
	return string(host), nil
}
func (g *GPIO) PinStates() (map[string]PinState, error) {
	return g.pinStates, nil
}
func (g *GPIO) PinInit(pinId string, dir Direction, pullup PullUp, name string) error {
	var pin interface{}
	state := byte(0)

	if dir == PWM {

		host, _, err := embd.DetectHost()
		if err != nil {
			return err
		}
		if host == embd.HostRPi {
			// use pi blaster pin
			log.Println("Creating PWM pin on Pi")

			// get the host descriptor
			desc, err := embd.DescribeHost()
			if err != nil {
				return err
			}
			// get the pinmap
			embdMap := desc.GPIODriver().PinMap()
			// lookup the pinId in the map
			var pinDesc *embd.PinDesc
			for i := range embdMap {
				pd := embdMap[i]

				if pd.ID == pinId {
					pinDesc = pd
					break
				}

				for j := range pd.Aliases {
					if pd.Aliases[j] == pinId {
						pinDesc = pd
						break
					}
				}
			}
			if pinDesc != nil {
				// we found a pin with that name....what is its first Alias?
				pinIdInt, err := strconv.Atoi(pinDesc.Aliases[0])
				if err != nil {
					log.Println("Failed to parse int from alias : ", pinDesc.Aliases[0])
					return err
				}
				p := NewBlasterPin(pinIdInt)
				pin = p
			} else {
				log.Println("Failed to find Pin ", pinId)
				return errors.New("Failed to find pin " + pinId)
			}
		} else {
			// bbb, so use embd since pwm pins work there
			p, err := embd.NewPWMPin(pinId)
			if err != nil {
				log.Println("Failed to create PWM Pin using key ", pinId, " : ", err.Error())
				return err
			}
			pin = p
		}
	} else {
		// add a pin
		p, err := embd.NewDigitalPin(pinId)
		if err != nil {
			return err
		}
		pin = p

		err = p.SetDirection(embd.Direction(dir))
		if err != nil {
			return err
		}

		if pullup == Pull_Up {
			err = p.PullUp()

			// pullup and down not implemented on rpi host so we need to manually set initial states
			// not ideal as a pullup really isn't the same thing but it works for most use cases

			if err != nil {
				log.Println("Failed to set pullup on " + pinId + " setting high state instead : " + err.Error())
				// we failed to set pullup, so lets set initial state high instead
				err = p.Write(1)
				state = 1
				if err != nil {
					return err
				}
			}
		} else if pullup == Pull_Down {
			err = p.PullDown()

			if err != nil {

				log.Println("Failed to set pulldown on " + pinId + " setting low state instead : " + err.Error())

				err = p.Write(0)
				state = 1
				if err != nil {
					return err
				}
			}
		}
	}

	// test to see if we already have a state for this pin
	existingPin, exists := g.pinStates[pinId]
	if exists {
		existingPin.Pin = pin
		existingPin.Name = name
		existingPin.Dir = dir
		existingPin.State = state
		existingPin.Pullup = pullup
		g.pinStates[pinId] = existingPin

		g.pinStateChanged <- existingPin
		g.pinRemoved <- pinId
		g.pinAdded <- g.pinStates[pinId]
	} else {
		g.pinStates[pinId] = PinState{pin, pinId, dir, state, pullup, name}
		g.pinAdded <- g.pinStates[pinId]
	}

	return nil
}
func (g *GPIO) PinSet(pinId string, val byte) error {
	// change pin state
	if pin, ok := g.pinStates[pinId]; ok {
		// we have a value....
		switch pinObj := pin.Pin.(type) {
		case embd.DigitalPin:
			err := pinObj.Write(int(val))
			if err != nil {
				return err
			}
		case embd.PWMPin:
			if err := pinObj.SetAnalog(val); err != nil {
				return err
			}
		case BlasterPin:
			err := pinObj.Write(val)
			if err != nil {
				return err
			}
		}
		pin.State = val
		g.pinStates[pinId] = pin
		// notify channel of new pinstate
		g.pinStateChanged <- pin
	}
	return nil
}
func (g *GPIO) PinRemove(pinId string) error {
	// remove a pin
	if pin, ok := g.pinStates[pinId]; ok {
		var err error
		switch pinObj := pin.Pin.(type) {
		case embd.DigitalPin:
			err = pinObj.Close()
			if err != nil {
				return err
			}
		case embd.PWMPin:
			err = pinObj.Close()
			if err != nil {
				return err
			}
		case BlasterPin:
			err = pinObj.Close()
			if err != nil {
				return err
			}
		}
		delete(g.pinStates, pinId)
		g.pinRemoved <- pinId
	}
	return nil
}
