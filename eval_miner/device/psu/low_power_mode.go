package psu

import (
	"eval_miner/log"
	"fmt"
	"time"
)

const (
	// Low power mode
	BOCO_LP_MODE = 0x01
	// Normal mode
	BOCO_NORMAL_MODE = 0x00
	// eval system state to stay in low power mode
	REFRESH = 1 * time.Second
	// min duration between two low power mode changes
	LP_CHANGE_DURATION = 5 * time.Second
	// min firmware version that supports low power mode
	LP_MIN_FW_VERSION = 4.1
	// max fan speed supported by low power mode due to power limitation
	LP_MAX_FAN_SPEED = 1000.
	// max temperature of the chassis supported by low power mode due to cooling limitation
	LP_MAX_TEMP = 60.
	// max power output demand (watts?)
	LP_MAX_PWR_OUT = 30.
)

// type byte is an alias for uint8
type byte = uint8

// LowPowerMode implements the LowPowerMode interface
type LowPowerMode struct {
	isLowPower bool
	lastChange time.Time
}

var lowPower *LowPowerMode

func init() {
	lowPower = new(LowPowerMode)
}

// SetSleep sets the PSU in low power mode and turn on the monitor
func SetSleep(on bool) {
	if on {
		if err := lowPower.SetLowPower(); err != nil {
			log.Errorf("Failed to set low power mode: %v", err)
		}
	} else { // Normal power is the safe state.
		if err := lowPower.SetNormalPower(); err != nil {
			log.Errorf("Failed to set normal power mode: %v", err)
		}
	}

	go lowPower.Mon()
}

// IsLowPower returns true if the PSU is in low power mode
func IsLowPower() bool {
	lowPower.isLowPower = getSleepReg()
	return lowPower.isLowPower
}

// Mon monitors the system state and switches to normal power mode if the
// system does not satisfy the low power mode requirements
func (lp *LowPowerMode) Mon() {
	for {

		// gracefuly exit when restoring Normal power
		if !IsLowPower() {
			return
		}

		// gracefuly exit when restoring Normal power
		if !lp.checkSystem() {
			if err := lp.SetNormalPower(); err != nil {
				log.Errorf("Failed to set normal power mode: %v", err)
			}
			return
		}

		time.Sleep(REFRESH)
	}
}

// SetLowPower sets the PSU in low power mode after making sure that the system
// satisfies the low power mode requirements
func (lp *LowPowerMode) SetLowPower() error {

	if IsLowPower() { // already in low power mode
		log.Infof("Already in low power mode")
		return nil
	}

	// check duration to avoid too frequent changes
	if time.Since(lp.lastChange) < LP_CHANGE_DURATION {
		log.Infof("Last changed at %v", lp.lastChange)
		return fmt.Errorf("low power mode change too frequent")
	}

	if !lp.checkSystem() {
		return fmt.Errorf("system does not satisfy low power mode requirements")
	}

	// finally write the low power mode command
	setSleepReg(true)
	lp.isLowPower = true
	lp.lastChange = time.Now()
	log.Infof("PSU is now in low power mode")
	return nil
}

// SetNormalPower sets the PSU in normal power mode
func (lp *LowPowerMode) SetNormalPower() error {

	if !IsLowPower() { // already in normal power mode
		log.Infof("Already in normal power mode")
		return nil
	}

	// Normal power is the safe mode. We should enter it ASAP
	setSleepReg(false)
	lp.isLowPower = false
	lp.lastChange = time.Now()
	log.Infof("PSU is now in normal power mode")
	return nil
}

// checkSystem checks if the system satisfies the low power mode requirements
func (lp *LowPowerMode) checkSystem() bool {
	psud, _ := GetPsuStatus(false)

	// check ok
	ok := true
	ok = ok && psud.FanSpeedSet <= LP_MAX_FAN_SPEED
	ok = ok && psud.Temp1 <= LP_MAX_TEMP
	ok = ok && psud.Temp2 <= LP_MAX_TEMP
	ok = ok && psud.PowerOut <= LP_MAX_PWR_OUT

	if !ok {
		log.Infof("System does not satisfy low power mode requirements")
		log.Infof("FanSpeedSet: %v", psud.FanSpeedSet)
		log.Infof("Temp1: %v", psud.Temp1)
		log.Infof("Temp2: %v", psud.Temp2)
		log.Infof("PowerOut: %v", psud.PowerOut)
	}

	return ok
}
