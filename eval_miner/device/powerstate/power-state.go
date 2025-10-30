package powerstate

import (
	"errors"
	"time"

	"eval_miner/device/devhdr"
	"eval_miner/device/fan"
	"eval_miner/device/psu"
	"eval_miner/log"

	"gobot.io/x/gobot/sysfs"
)

// GPIO1_0: base pin 335
// GPIO0_0: base pin 423
// Need to update arch/arm64/boot/dts/ti/gc-am64-v0.dts to support HASHx_CPU_PSU_DETECT pins! Those are hbPowerOnToGpio below

var (
	alarmShutdown bool = false
)

var hbPowerOnToGpio = map[int]int{
	1: 335,
	2: 346,
	3: 347,
}

var hbPowerIsOn [devhdr.MaxHashBoards]bool
var hbResetIsOn [devhdr.MaxHashBoards]bool

const INTER_BOARD_DELAY = 500 // msec between turning on hash boards

func SystemPowerOff(alarm bool) {
	alarmShutdown = alarm

	log.Error("SystemPowerOff: Powering down system")
	for ii := 1; ii <= int(devhdr.GetTotalChainCount()); ii++ {
		_ = HbPowerOff(ii)
		_ = HbReset(ii)
	}
	if !devhdr.IsPsuDisabled() {
		psu.PsuOff()
	}
}

func SystemUnreset() {
	// Take HB ASICs out of reset
	alarmShutdown = false

	log.Infof("System: Taking hash boards out of reset")
	for ii := 1; ii <= int(devhdr.GetTotalChainCount()); ii++ {
		_ = HbUnreset(ii)
	}
	time.Sleep(time.Second) // Wait a second before trying to access ASICs
}

func IsSysAlarmOff() bool {
	return !alarmShutdown
}

// Returns true if any HB is on. Used to ensure fan speed is sufficient.
func HbPowerIsOn() (bool, error) {

	if !devhdr.GetHashBoardPowerSupport() {
		return true, nil
	}

	for hb := 1; hb <= int(devhdr.GetHashBoardChainCount()); hb++ {

		rstPin := sysfs.NewDigitalPin(hbPowerOnToGpio[hb])
		_ = rstPin.Export()
		_ = rstPin.Direction("in")
		defer func() { // Fix lint error for not checking Unexport return values
			_ = rstPin.Unexport()
		}()

		value, err := rstPin.Read()

		if err != nil {
			return true, err // Assume we may have power to be safe
		}

		if value == 1 {
			return true, nil
		}
	}

	return false, nil // Nothing powered on
}

func HbPowerOn(hb int) error {
	if hb < 1 || hb > int(devhdr.GetHashBoardCount()) { // hb is 1-based, 1-3
		log.Errorf("HbPowerOn: Error - invalid HB parameter %d\n", hb)
		return errors.New("invalid HB ID")
	}

	log.Infof("HbPowerOn board %d: Turning all fans on to 100%%", hb)
	for ii := 0; ii < fan.NUM_FANS; ii++ {
		_, _ = fan.SetSpeed(ii, 100, false)
		time.Sleep(time.Second) // Try not to overload power supply current
	}

	// Make sure there's a 500 msec delay between turning on hash boards
	time.Sleep(INTER_BOARD_DELAY * time.Millisecond)

	pwrPin := sysfs.NewDigitalPin(devhdr.GetHashBoardPowerSysfsValue(uint(hb)))
	_ = pwrPin.Export()
	_ = pwrPin.Direction("out")
	_ = pwrPin.Write(1)
	_ = pwrPin.Unexport()

	hbPowerIsOn[hb-1] = true

	return nil
}

func HbPowerOff(hb int) error {

	if !devhdr.GetHashBoardPowerSupport() {
		return nil
	}

	if hb < 1 || hb > int(devhdr.GetHashBoardCount()) {
		log.Errorf("HbPowerOff: Error - invalid HB parameter %d\n", hb)
		return errors.New("invalid HB ID")
	}

	time.Sleep(INTER_BOARD_DELAY * time.Millisecond)

	log.Infof("HbPowerOff board %d", hb)
	pwrPin := sysfs.NewDigitalPin(devhdr.GetHashBoardPowerSysfsValue(uint(hb)))
	_ = pwrPin.Export()
	_ = pwrPin.Direction("out")
	_ = pwrPin.Write(0)
	_ = pwrPin.Unexport()

	hbPowerIsOn[hb-1] = false

	for ii := 0; ii < int(devhdr.GetHashBoardCount()); ii++ {
		if hbPowerIsOn[ii] {
			return nil
		}
	}

	return nil
}

func HbReset(hb int) error {
	if hb < 1 || hb > int(devhdr.GetTotalChainCount()) {
		log.Errorf("HbReset: Error - invalid HB parameter %d\n", hb)
		return errors.New("invalid HB ID")
	}

	// From the ASIC spec, we should never assert reset if hash VDD is enabled. There
	// is HW logic in the HB to prevent this, but let's just
	// make extra, extra sure by adding this in SW as well.
	if devhdr.GetHashBoardPowerSupport() && hbPowerIsOn[hb-1] {
		log.Errorf("CANNOT put HB %d in reset; HB power is on\n", hb)
		return errors.New("invalid HB reset operation")
	}

	log.Infof("HbReset board %d", hb)
	rstPin := sysfs.NewDigitalPin(devhdr.GetHashBoardResetSysfsValue(uint(hb)))
	_ = rstPin.Export()
	_ = rstPin.Direction("out")
	_ = rstPin.Write(0) // RESET_L - write 0 to assert reset
	_ = rstPin.Unexport()

	hbResetIsOn[hb-1] = true

	return nil
}

func HbUnreset(hb int) error {
	if hb < 1 || hb > int(devhdr.GetTotalChainCount()) {
		log.Errorf("HbUnreset: Error - invalid HB parameter %d\n", hb)
		return errors.New("invalid HB ID")
	}

	log.Infof("HbUnreset board %d", hb)
	rstPin := sysfs.NewDigitalPin(devhdr.GetHashBoardResetSysfsValue(uint(hb)))
	_ = rstPin.Export()
	_ = rstPin.Direction("out")
	_ = rstPin.Write(1) // RESET_L - write 1 to deassert reset
	_ = rstPin.Unexport()

	hbResetIsOn[hb-1] = false

	return nil
}

func HbIsPresent(hb int) (bool, error) {
	if hb < 1 || hb > int(devhdr.GetTotalChainCount()) {
		log.Errorf("HbIsPresent: Error - invalid HB parameter %d\n", hb)
		return false, errors.New("invalid HB ID")
	}

	rstPin := sysfs.NewDigitalPin(devhdr.GetHashBoardPresenceSysfsValue(uint(hb)))
	_ = rstPin.Export()
	_ = rstPin.Direction("in")
	defer func() { // Fix lint error for not checking Unexport return values
		_ = rstPin.Unexport()
	}()

	value, err := rstPin.Read()

	if err != nil {
		return false, err
	}

	if value == 1 { // This GPIO is asserted low. 1 means HB not present
		return false, nil
	}

	return true, nil
}

func HbThermalTripAsserted(hb int) (bool, error) {

	if hb < 1 || hb > int(devhdr.GetTotalChainCount()) {
		log.Errorf("HbThermalTripAsserted: Error - invalid HB parameter %d\n", hb)
		return false, errors.New("invalid HB ID")
	}
	tripPin := sysfs.NewDigitalPin(devhdr.GetThermalTripSysfsValue(uint(hb)))
	_ = tripPin.Export()
	_ = tripPin.Direction("in")
	defer func() { // Fix lint error for not checking Unexport return values
		_ = tripPin.Unexport()
	}()

	value, err := tripPin.Read()
	if err != nil {
		return false, err
	}

	log.Debugf("ThermalTrip value board %v %v %v", hb, devhdr.GetThermalTripSysfsValue(uint(hb)), value)
	if value == 1 { // This GPIO is asserted low. 1 means no thermal trip
		return false, nil
	}

	return true, nil

}
