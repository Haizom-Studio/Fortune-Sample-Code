package fan

import (
	"fmt"
	"os"
	"time"

	"eval_miner/device/devhdr"
	"eval_miner/device/pwm"
	"eval_miner/log"
)

type FanInterface interface {
	// initialize all the pins and start tachometers and monitoring
	Setup()

	// set the percentage of full speed. usually, the min speed is around 15%
	SetSpeed(index int, percent uint32) error

	// get the fan speed in RPM
	GetRPM(index int) int
}

const (
	NUM_FANS                       = 4
	FAN_MONITOR_INTERVAL_SEC       = 5
	I2CEepromDeviceAddress   uint8 = 80
	ControlBoardI2CBus       uint8 = 4
	// We'll need to see if we can stay cool enough with one broken fan when the ASICs come in
	// This is ~14% of full speed (7k RPM)
	fanSpeedMin = 1000
)

var (
	fanAlarm  [NUM_FANS]bool
	pollCount uint
	fanSpeed  [NUM_FANS]uint32
	pwmPins   = make(map[int]*pwm.PWMPin)
)

// Add a PWM controlled fan. Index is the handle for other APIs
func addFan(index int, ctrlChip int, ctrlChannel int, tachoPin int) (err error) {
	pin := pwm.NewPin(ctrlChip, ctrlChannel)

	err = pin.Export()
	if err != nil {
		return
	}

	// control pin needs a fixed PWM frequency of 25kHz
	err = pin.SetPeriod(40000)
	if err != nil {
		return
	}

	// set it to half speed by default
	err = pin.SetDutyCycle(20000)
	if err != nil {
		return
	}

	err = pin.Enable(true)
	if err != nil {
		return
	}

	addTacho(index, tachoPin)
	pwmPins[index] = pin

	return nil
}

func FansOff() { // Should only be called from Standby mode

	for i := 0; i < NUM_FANS; i++ {
		fanSpeed[i] = 0
		pin := pwmPins[i]
		_ = pin.SetDutyCyclePercent(0)

	}
}

func MaxOn() {

	for i := 0; i < NUM_FANS; i++ {
		fanSpeed[i] = 100
		pin := pwmPins[i]
		_ = pin.SetDutyCyclePercent(100)

	}
}

func SetSpeed(index int, percent uint32, warning bool) (error, bool) {

	var resp string
	pin := pwmPins[index]
	if pin == nil {
		return fmt.Errorf("invalid fan index %d", index), false
	}

	if warning && percent < GetSpeed(index) {
		fmt.Printf("WARNING: HB power is on. Do you really want to reduce fan speed? (y/n):")
		fmt.Scanf("%s", &resp)
		if resp != "y" {
			return nil, true
		}
	}

	fanSpeed[index] = percent
	return pin.SetDutyCyclePercent(percent), false
}

func GetSpeed(index int) uint32 {
	pin := pwmPins[index]
	if pin == nil {
		log.Errorf("invalid fan index %d", index)
		return 0
	}

	speed, err := pin.GetDutyCyclePercent()
	if err != nil {
		log.Errorf("GetDutyCyclePercent returned %s\n", err)
		return 0
	}
	return speed
}

func FanAlarmState() bool {

	for ii := 0; ii < NUM_FANS; ii++ {
		if fanAlarm[ii] {
			return true
		}
	}

	return false

}

func startFanMon() {

	go func() {
		for {
			var fanSpeed int
			pollCount++
			for ii := 0; ii < NUM_FANS; ii++ {
				fanSpeed = GetRPM(ii)

				if pollCount > 1 { // Ignore first poll data - always 0
					if fanSpeed < fanSpeedMin {
						if !fanAlarm[ii] { // Spam control - only alert first time
							log.Errorf("ALARM: Fan %d speed %d RPM is below threshold %d RPM poll %d\n", ii+1, fanSpeed, fanSpeedMin, pollCount)
						}
						fanAlarm[ii] = true
					} else {
						if fanAlarm[ii] {
							log.Infof("Fan %d speed %d RPM is back above threshold %d poll %d\n", ii+1, fanSpeed, fanSpeedMin, pollCount)
						}
						fanAlarm[ii] = false
					}
				}
			}

			time.Sleep(FAN_MONITOR_INTERVAL_SEC * time.Second)
		}
	}()

}

var Count = 0

func Init() {

	var fans = []struct {
		ctrlChip    int
		ctrlChannel int
		tachoPin    int
	}{{2, 0, 4}, {3, 0, 5}, {0, 0, 2}, {1, 0, 3}}

	_ = os.Mkdir(devhdr.FanFileDir, os.ModeDir)

	Count = len(fans)

	for i := 0; i < Count; i++ {
		err := addFan(i, fans[i].ctrlChip, fans[i].ctrlChannel, fans[i].tachoPin)
		if err != nil {
			log.Errorf("err init fan %d: %s", i, err)
			continue
		}
	}

	startTacho()
	startFanMon()
}
