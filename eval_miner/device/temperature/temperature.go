package temperature

import (
	"eval_miner/device/devhdr"
	"eval_miner/device/i2c"
	"eval_miner/device/powerstate"
	"eval_miner/log"
	"time"
)

type Temperature struct {
	Value  float64
	CutOff float64
}

const (
	ERROR_NOT_INIT = iota
	ERROR_THERMAL_CUTOFF
	ERROR_OVERHEAT
)

const (
	ADDR_TEMP_SENSOR = 0x48
)

const (
	TEMP_MONITOR_INTERVAL_SEC = 5 // Seconds between polling
)

const (
	CB_HIGH_TEMP = 75
	HB_HIGH_TEMP = 100
)

const (
	HB_SENSORS = 5
)

var HbTempDesc = []string{
	"Inlet Middle",   // 0x49
	"Inlet Bottom",   // 0x4a
	"Inlet Top",      // 0x4b
	"Exhaust Middle", // 0x4c
	"Exhaust Top",    // 0x4f
}

func getTempAddrHB() []int {
	return []int{0x49, 0x4a, 0x4b, 0x4c, 0x4f}
}

var (
	cbTempAlarm     = false
	hbTempAlarm     [devhdr.MaxHashBoards]bool
	prevCbTempAlarm = false
	prevHbTempAlarm [devhdr.MaxHashBoards]bool
	hbMask          uint32
	hbTempFailures  [devhdr.MaxHashBoards][HB_SENSORS]int // First index is HB number, second index is sensor number
)

func readTemp(bus int, addr int) (v float64, err error) {
	v = 0
	tmpSensor, err := i2c.NewI2cDev(bus, addr)
	if err != nil {
		return
	}

	var temp [2]byte
	err = tmpSensor.ReadReg(0, temp[:])
	tmpSensor.Close()

	if err != nil {
		return
	}

	return float64(int8(temp[0])) + float64(temp[1])/256, nil
}

func ReadCBTemp() (v float64, err error) {
	return readTemp(i2c.BUS_CTRL_BOARD, ADDR_TEMP_SENSOR)
}

// boardNo starts from 1
func ReadHBTemps(boardNo int) (v []float64) {

	addrs := getTempAddrHB()
	for ii, a := range addrs {
		t, err := readTemp(i2c.BUS_HASH_BOARD_BASE+boardNo-1, a)
		if err != nil {
			hbTempFailures[boardNo-1][ii]++
			if hbTempFailures[boardNo-1][ii] < 2 || hbTempFailures[boardNo-1][ii]%100 == 0 { // Don't spam the log
				log.Errorf("Error reading Hash Board %d temperature sensor 0x%02x: %s\n", boardNo, a, err)
			}
		} else {
			hbTempFailures[boardNo-1][ii] = 0
		}
		v = append(v, t)
	}
	return v

}

func TempTooHigh() bool {

	if cbTempAlarm {
		return true
	}

	for ii := 0; ii < int(devhdr.GetHashBoardCount()); ii++ {
		if hbTempAlarm[ii] {
			return true
		}
	}

	return false
}

func Init() {
	for ii := 0; ii < int(devhdr.GetHashBoardCount()); ii++ {
		present, _ := powerstate.HbIsPresent(ii + 1)
		if present {
			hbMask |= 1 << uint(ii)
		}
	}

	temperatureMonitor()

}

func temperatureMonitor() {

	go func() {
		var temperature float64
		var hbTemps []float64
		var err error

		for { //ever
			prevCbTempAlarm = cbTempAlarm
			for ii := 0; ii < int(devhdr.GetHashBoardCount()); ii++ {
				prevHbTempAlarm[ii] = hbTempAlarm[ii]
			}

			cbTempAlarm = false
			temperature, err = ReadCBTemp()
			if err != nil {
				log.Errorf("Error reading Control Board temperature: %s\n", err)
			}
			if temperature >= CB_HIGH_TEMP {
				cbTempAlarm = true // Need hysteresis check?
				powerstate.SystemPowerOff(true)
				if !prevCbTempAlarm { // Spam control
					log.Errorf("ALARM: Control Board temperature %.2fC is above limit %.2fC\n", temperature, float32(CB_HIGH_TEMP))
				}
			}

			for ii := 1; ii <= int(devhdr.GetHashBoardCount()); ii++ {
				if hbMask&(1<<uint(ii-1)) != 0 { // Only check for installed HBs
					hbTempAlarm[ii-1] = false // Need hysteresis check?
					hbTemps = ReadHBTemps(ii)
					for jj := 1; jj <= HB_SENSORS; jj++ {
						if hbTemps[jj-1] >= HB_HIGH_TEMP {
							powerstate.SystemPowerOff(true)
							hbTempAlarm[ii-1] = true
							if !prevHbTempAlarm[ii-1] {
								log.Errorf("ALARM: Hash Board %d sensor %d temperature %.2fC is above limit %.2fC\n", ii, jj, hbTemps[jj-1], float32(HB_HIGH_TEMP))
							}
						}
					}
				}

			}

			if TempTooHigh() {
				powerstate.SystemPowerOff(true)
			}

			time.Sleep(TEMP_MONITOR_INTERVAL_SEC * time.Second)
		}
	}()

}
