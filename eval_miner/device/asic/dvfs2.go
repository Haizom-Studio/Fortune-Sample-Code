package asic

import (
	"math"
	"sync"
	"time"

	"eval_miner/device/devhdr"
	"eval_miner/device/powerstate"
	"eval_miner/device/psu"
	"eval_miner/log"
)

const (
	DVFS_TUNING  = iota
	DVFS_NORMAL  = iota
	DVFS_STANDBY = iota
)

const (
	DVFS_TUNE_INIT        = iota
	DVFS_TUNE_SET_FREQ    = iota
	DVFS_TUNE_STEPPING_UP = iota
	DVFS_TUNE_OPTIMIZE    = iota
	DVFS_TUNE_FINETUNE    = iota
	DVFS_TUNE_DONE        = iota
)

var stateMap = map[int]string{
	DVFS_TUNING:  "TUNING",
	DVFS_NORMAL:  "NORMAL",
	DVFS_STANDBY: "STANDBY",
}

var tuneStateMap = map[int]string{
	DVFS_TUNE_INIT:        "TUNE_INIT",
	DVFS_TUNE_SET_FREQ:    "TUNE_SET_FREQ",
	DVFS_TUNE_STEPPING_UP: "TUNE_STEPPING_UP",
	DVFS_TUNE_FINETUNE:    "TUNE_FINETUNE",
	DVFS_TUNE_DONE:        "TUNE_DONE",
}

const (
	InitTargetTHSFraction = float32(0.98)
)

// settings
const (
	MINER_MODE_ECO          = iota
	MINER_MODE_TURBO        = iota
	MINER_MODE_CUSTOM_THS   = iota
	MINER_MODE_CUSTOM_POWER = iota
)

// internal states
var orgTargetTHS float32
var targetTHS float32    // this is the target TH/s from orgTargetTHS or powerTarget
var curTargetTHS float32 // if curTargetTHS != targetTHS, it starts tuning
var dvfsState, tuneState int
var nBouncingBack int = 0
var targetReducing bool = false

var tune_done_time time.Time

// save hash rate history
var hashRateHist [16]float32
var hashRateHistIdx int = 0

var hashRatesSave [devhdr.MaxHashBoards]float32 // Hash rate calculated per board

// Return true if there is a temperature alarm
func (my *DvfsType) processTemp() bool {
	hotChip = -1
	hotBoard = -1
	max_temp = -40.0
	badTemps := 0

	curBoard = -1
	for i := 0; i < len(my.topology); i++ {
		t := &my.topology[i]
		aa := AsicHandle[t.board+1]
		if aa == nil || aa.deadBoard {
			badTemps++
			continue
		}
		if curBoard == -1 {
			curBoard = t.board
		}
		newTemp := float32(aa.cacheTemps[aa.ChipIdToIndex(uint8(t.id))])
		if newTemp > -300.0 {
			if newTemp > 170.0 {
				t.badTempCtr++
				t.temperature = 0
			} else {
				t.badTempCtr = 0
				t.temperature = newTemp
			}
			avg_temp += t.temperature
			if t.temperature > max_temp {
				hotChip = t.id
				hotBoard = t.board
				max_temp = t.temperature
			}

			if t.badTempCtr == 2 { // Must be 2 bad reads in a row
				log.Errorf("DVFS ALARM: Invalid temperature %.1f on chip %d/%d", t.temperature, t.board+1, t.id)
			}
		} else {
			badTemps++
		}
	}
	if float32(len(my.topology)-badTemps) > 0 {
		avg_temp /= float32(len(my.topology) - badTemps)
	} else {
		avg_temp = 0
	}

	if max_temp >= AsicTempLimit {
		log.Errorf("DVFS ALARM: ASIC temp max is %.1f on chip %d/%d; going to standby mode", max_temp, hotBoard+1, hotChip)
		enterStandbyMode()
		return true
	}

	return false
}

// Return true if voltage is too high
func (my *DvfsType) processVolt() bool {

	chainCount := devhdr.GetTotalChainCount()
	badVolts := make([]int, chainCount)

	for i := 0; i < int(chainCount); i++ {
		avg_volt[i] = 0
		if AsicHandle[i+1] != nil && AsicHandle[i+1].deadBoard {
			min_volt[i] = 0
		} else {
			min_volt[i] = 400
		}
		max_volt[i] = 0
		badVolts[i] = 0
	}

	for i := 0; i < len(my.topology); i++ {
		t := &my.topology[i]
		aa := AsicHandle[t.board+1]
		if aa == nil {
			continue
		}

		t.voltage = float32(aa.cacheVolts[aa.ChipIdToIndex(uint8(t.id))])
		if t.voltage > maxVolt || t.voltage < minVolt {
			if t.voltage < 0 || t.voltage > 0.800 { // Read failed or non-sane value
				t.voltage = 0
				badVolts[t.board]++
				continue // Bad value read
			}
			t.badVoltCtr++
			if t.badVoltCtr == 3 { // Must see 3 in a row
				log.Errorf("DVFS ALARM: Voltage for chip %d/%d is %.4fV; out of range %.4fV - %.4fV", t.board+1, t.id, t.voltage, minVolt, maxVolt)
				aa.printTracedVoltages()
			}
		} else {
			t.badVoltCtr = 0
		}
		avg_volt[t.board] += t.voltage
		if t.voltage > max_volt[t.board] {
			max_volt[t.board] = t.voltage
		}
		if t.voltage < min_volt[t.board] {
			min_volt[t.board] = t.voltage
		}
	}

	// Record high, low & avg voltage for each HB
	for i := 0; i < int(chainCount); i++ {
		goodReads := float32((len(my.topology) / dd.num_boards) - badVolts[i])
		if goodReads > 0 {
			avg_volt[i] /= goodReads
		} else {
			avg_volt[i] = 0 // All chips are dead
		}
	}

	return false
}

var voltLoopCtr int = 0

func (my *DvfsType) monitorTempVolt() (tempAlarm bool, voltAlarm bool) {
	if voltLoopCtr%2 == 0 {
		for i := 0; i < int(devhdr.GetTotalChainCount()); i++ {
			deadAsicCtr[i] = 0
			aa := AsicHandle[i+1]
			if aa != nil {
				_ = aa.ReadAllTemperature() // ReadHitCounters() reads temps for us now
				_ = aa.ReadAllVoltage()
			}
		}
	}
	voltLoopCtr++

	tempAlarm = my.processTemp()
	voltAlarm = my.processVolt()
	return tempAlarm, voltAlarm
}

// get TruHit & GenHit together
func getHitCounters(board int, chips []uint8) []int64 {
	ret, err := AsicHandle[board+1].ReadRegsPipelined(chips, []uint8{ADDR_TRUEHIT_COUNT_GENERAL, ADDR_HIT_COUNT_GENERAL})
	if err != nil {
		log.Errorf("DVFS ALARM: ReadRegsPipelined board %d returned %v", board+1, err)
	}
	return ret
}

func getHitCountersBoard(board int) [][2]int64 {
	aa := AsicHandle[board+1]
	if aa == nil || aa.deadBoard {
		return nil
	}

	counters := make([][2]int64, len(aa.seqChipIds))

	var toRetry []uint8
	// fetch counters in batches of 22 to reduce the latency
	// 22 chips with 2 counters each need 6ms to transfer on 3Mbaud UART
	chiplen := len(aa.actualChipIds)
	for i := 0; i < chiplen; i += 22 {
		end := i + 22
		if end > chiplen {
			end = chiplen
		}
		cnts := getHitCounters(board, aa.actualChipIds[i:end])

		for ii := 0; ii < end-i; ii++ {
			if (cnts[ii*2] == -1) || (cnts[ii*2+1] == -1) {
				if toRetry == nil {
					toRetry = []uint8{aa.actualChipIds[i+ii]}
				} else {
					toRetry = append(toRetry, aa.actualChipIds[i+ii])
				}
				continue
			}
			idx := aa.ChipIdToIndex(aa.actualChipIds[i+ii])
			counters[idx][0] = cnts[ii*2]
			counters[idx][1] = cnts[ii*2+1]
			aa.ChipArray[idx].NoResponseCtr = 0
		}
	}

	// retry the failed ones only once
	if toRetry != nil {
		log.Debugf("B%d: getHitCountersBoard: retrying chip %v", board+1, toRetry)
		cnts := getHitCounters(board, toRetry)

		failed := 0
		deadAsicCtr[board] = 0
		for ii := 0; ii < len(toRetry); ii++ {
			idx := aa.ChipIdToIndex(toRetry[ii])
			counters[idx][0] = cnts[ii*2]
			counters[idx][1] = cnts[ii*2+1]

			if cnts[ii*2] != -1 || cnts[ii*2+1] != -1 {
				aa.ChipArray[idx].NoResponseCtr = 0
			} else {
				failed++
				aa.ChipArray[idx].NoResponseCtr++
				if aa.ChipArray[idx].NoResponseCtr > noResponseLimit {
					aa.ChipArray[idx].NotResponsive = true
					deadAsicCtr[board]++
				}
			}
		}
		if deadAsicCtr[board] > maxDeadAsics {

			aa := AsicHandle[board+1]
			if aa != nil && !aa.deadBoard {
				log.Errorf("DVFS ALARM: Marking HB%d as dead with %d non-responsive ASICs", board+1, deadAsicCtr[board])
				log.Infof("DVFS: Topology = %v", dd.topology)
				aa.deadBoard = true
				_ = powerstate.HbPowerOff(board + 1) // Turn it off if we're not going to monitor it. Should we restart gcminer here?
				_ = powerstate.HbReset(board + 1)    // Put it in reset to reduce power further
			}
		}

		if failed > 10 {
			log.Errorf("B%d: too many chips failed to read hit counters: %v", board+1, failed)
		}
	}

	return counters
}

func goGetHitCounters(cnt *[][2]int64, board int, wg *sync.WaitGroup) {
	*cnt = getHitCountersBoard(board)
	if wg != nil {
		wg.Done()
	}
}

// polling counters for all boards
func getHitCountersAll() [][][2]int64 {
	chainCount := devhdr.GetTotalChainCount()
	counters := make([][][2]int64, chainCount)

	ts := time.Now()
	//We can use goroutinge to get counters in parallel, but it causes a lot of byte losses on UART due to TI's buggy driver
	//var wg sync.WaitGroup
	for i := 0; i < int(chainCount); i++ {
		//wg.Add(1)
		//go goGetHitCounters(&counters[i], i, &wg)
		goGetHitCounters(&counters[i], i, nil)
	}
	//wg.Wait()
	log.Debugf("DVFS: getHitCountersAll took %v", time.Since(ts))
	return counters
}

func (my *DvfsType) getHashRate(base [][][2]int64, period time.Duration) (hitrate float32, hashrates []float32, counters [][][2]int64) {

	var (
		expected_total /*hit_total,*/, true_total, hit_rate float32
	)

	counters = getHitCountersAll()
	boardSpeed := make([]float32, devhdr.GetTotalChainCount())

	for i := 0; i < len(my.topology); i++ {
		t := &my.topology[i]
		aa := AsicHandle[t.board+1]
		if aa != nil && !aa.deadBoard {
			index := aa.ChipIdToIndex(uint8(t.id))
			cnt1 := &counters[t.board][index]
			cnt0 := &base[t.board][index]
			v1 := cnt1[1] - cnt0[1]
			v0 := cnt1[0] - cnt0[0]

			if v0 < 0 || v1 < 0 {
				log.Infof("DVFS: WARNING: ASIC %d/%d hit counters went backwards! %d/%d", t.board+1, t.id, v0, v1)
				continue
			}

			// ignore the chips that gen_hit is 0
			if v1 == 0 {
				t.hitrate = 0
				continue
			}

			// 2 cases need calibration:
			//  1. the general hit counter went wild like > 100000, while true hit counter is correct (happens when voltage is too low)
			//     this makes the chip's hitrate lower than actual.
			//  2. the general hit counter is too small like < 100, while true hit counter is correct (happens when frequency is too high)
			//     this makes the chip's hitrate higher than actual.
			v := int64(t.frequency*0.07885*float32(period)) / int64(time.Second) // 0.07885 = 1M*254*4/3/2^32 (frequency in MHz, and 1 for 2^32 hash)
			if v1 > v*2 || v1 < v/2 {
				// try to calibrate the general hit value by frequency * period
				if v0 > 0 {
					log.Debugf("DVFS WARNING: ASIC %d/%d hit counters out of range! %d/%d(cali %d), freq %.1f",
						t.board+1, t.id, v0, v1, v, t.frequency)
				}

				v1 = v
			}
			if v0 > v1 {
				if float32(v0)/float32(v1) > 1.02 {
					log.Infof("??? Suspicious reading bd %d chip %d freq %.1f hitrate %.3f (%d/%d)",
						t.board+1, t.id, t.frequency, t.hitrate, v0, v1)
				}
				v0 = v1
			}

			expected_total += float32(v1)
			true_total += float32(v0)

			t.hitrate = float32(v0) / float32(v1)
			boardSpeed[t.board] += t.frequency * t.hitrate
		}
	}

	if expected_total > 0 {
		hit_rate = true_total / expected_total
	}

	for i := 0; i < int(devhdr.GetTotalChainCount()); i++ {
		boardSpeed[i] *= float32(254*4) / 3000
		hashRatesSave[i] = boardSpeed[i]
	}

	return hit_rate, boardSpeed, counters
}

var old_average_f float32

func (dd *DvfsType) setFreqAll(average_f float32) {

	freq_step := (average_f - old_average_f)
	log.Infof("DVFS: setFreqAll:  %.1f MHz", average_f)
	curr_freq := old_average_f + freq_step

	for i := 0; i < len(dd.topology); i++ {
		t := &dd.topology[i]
		t.frequency = curr_freq
	}

	batch := BatchArrayType{}
	for stagger := 0; stagger < dd.num_cols; stagger++ {
		for i := stagger; i < len(dd.topology); i += dd.num_cols {
			t := &dd.topology[i]
			batch = batch.Add(uint16(t.board), t.id, ADDR_PLL_FREQ, uint32(t.frequency*dd.pll_multiplier), CMD_WRITE)

			// do we need to set duty cycle every time frequency changes?
			if true { // Treat all ASICS as ECO+
				var setting int
				setting = int((48000 / t.frequency)) - 32
				if setting < 0 {
					setting = 0
				}
				if setting > 64 {
					setting = 64
				}
				batch = batch.Add(uint16(t.board), t.id, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(0<<18), CMD_WRITE)
				batch = batch.Add(uint16(t.board), t.id, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(1<<18), CMD_WRITE)
			}
		}
	}
	_ = batch.ReadWriteConfig()

	old_average_f = average_f
}

func (dd *DvfsType) tuneInit() {
	dvfsState = DVFS_TUNING
	tuneState = DVFS_TUNE_INIT
	curTargetTHS = targetTHS
}

func (dd *DvfsType) tuneDone() {
	dvfsState = DVFS_NORMAL
	tune_done_time = time.Now()
	dd.clearHashRateHistory()
}

// remember that this function is called periodically.
// Call functions directly if it needs to be done immediately.
// otherwise, set the tune state and let mainloop call it later.
var lasthitrate float32
var measurement float32
var last_measurement float32

func (dd *DvfsType) tuneHashRate(hitrate float32) bool {
	// Don't change stepFactor for initial environmental adjustments, since this is done
	// during normal operation and not during warm-up.
	average_f := getAvgFreq(curTargetTHS)

recheck_tune_state:
	switch tuneState {
	case DVFS_TUNE_INIT:
		log.Infof("DVFS: DVFS_TUNE_INIT stage starts with voltage %.3f min_voltage %.3f for targetTHS %.3f", dd.voltage, psu.MinerVoutMin, targetTHS)

		// lower down voltage slowly and then set frequency, to avoid unexpect high chip voltage
		if dd.voltage > psu.MinerVoutMin {
			dd.SetVoltage(dd.voltage - 0.5)
			log.Infof("DVFS: lower down voltage to %.3f", dd.voltage)
		}
		if dd.voltage <= psu.MinerVoutMin {
			tuneState = DVFS_TUNE_SET_FREQ
		}
		log.Infof("DVFS: DVFS_TUNE_INIT stage done with voltage %.3f ", dd.voltage)

	case DVFS_TUNE_SET_FREQ:
		dd.setFreqAll(average_f)

		lasthitrate = 0
		measurement = optimize_trigger_rate
		last_measurement = optimize_trigger_rate
		tuneState = DVFS_TUNE_STEPPING_UP

	case DVFS_TUNE_STEPPING_UP:
		if dd.voltage >= dd.systeminfo.max_voltage { // we hit the supply maximum
			log.Infof("DVFS: Supply maximum has been reached")
			return true
		} else if pctOver50Pct() < optimize_trigger_rate {
			pct := pctOver50Pct()
			var step float32

			lasthitrate = hitrate
			last_measurement = measurement
			measurement = (optimize_trigger_rate - hitrate)

			log.Infof("DVFS: DVFS_TUNE_STEPPING_UP voltage %.3f hitrate %.3f lasthitrate %.3f val [%.3f %.3f]", dd.voltage, hitrate, lasthitrate, measurement, last_measurement)

			// Comment: modify the step factor to speed up the tune of psu voltage
			if pct < 0.1 {
				step = 20
			} else if pct < 0.3 {
				step = 10
			} else {
				step = 4
			}

			dd.SetVoltage(float32(math.Min(float64(dd.voltage+dd.systeminfo.voltage_step*step), float64(dd.systeminfo.max_voltage))))
			log.Infof("DVFS: DVFS_TUNE_STEPPING_UP stepping up voltage by %.0f steps to %.3f", step, dd.voltage)
		} else {
			tuneState = DVFS_TUNE_FINETUNE
			goto recheck_tune_state
		}

	case DVFS_TUNE_OPTIMIZE:
		log.Infof("DVFS: DVFS_TUNE_OPTIMIZE average_f %.3f ", average_f)
		if Tune(average_f) {
			tuneState = DVFS_TUNE_FINETUNE
		}

	case DVFS_TUNE_FINETUNE:
		log.Infof("DVFS: DVFS_TUNE_FINETUNE stage voltage %.3f max_voltage %.3f hitrate %.3f start_rate %.3f low_rate %.3f", dd.voltage, dd.systeminfo.max_voltage, hitrate, start_rate, low_rate)
		reached_max := dd.voltage >= dd.systeminfo.max_voltage

		if hitrate <= start_rate && reached_max {
			log.Infof("DVFS: Supply maximum has been reached")
			return true
		}

		lasthitrate = hitrate
		last_measurement = measurement
		measurement = (low_rate - hitrate)

		log.Infof("DVFS: DVFS_TUNE_FINETUNE STEPPING_UP voltage %.3f hitrate %.3f lasthitrate %.3f val [%.3f %.3f] ", dd.voltage, hitrate, lasthitrate, measurement, last_measurement)

		var step float32
		if hitrate < low_rate {
			step = float32(math.Floor(float64((low_rate - hitrate) * 100)))
		} else {
			return true
		}
		if step < 1 {
			step = 1
		}
		dd.SetVoltage(float32(math.Min(float64(dd.voltage+dd.systeminfo.voltage_step*step), float64(dd.systeminfo.max_voltage))))
		log.Infof("DVFS: DVFS_TUNE_FINETUNE stepping up voltage by %.0f steps to %.3f", step, dd.voltage)
	}

	return false
}

func (dd *DvfsType) getInitTargetTHS() float32 {
	return float32(devhdr.EvalThs)
}

func (dd *DvfsType) startMinFreq() {
	// Start out at min frequency
	batch := BatchArrayType{}
	for stagger := 0; stagger < dd.num_cols; stagger++ {
		for i := stagger; i < len(dd.topology); i += dd.num_cols {
			t := &dd.topology[i]
			batch = batch.Add(uint16(t.board), t.id, ADDR_PLL_FREQ, uint32(MinFreq*dd.pll_multiplier), CMD_WRITE)
			if true { // Treat all ASICS as ECO+
				var setting int
				setting = int((48000 / MinFreq)) - 32
				if setting < 0 {
					setting = 0
				}
				if setting > 64 {
					setting = 64
				}
				batch = batch.Add(uint16(t.board), t.id, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(0<<18), CMD_WRITE)
				batch = batch.Add(uint16(t.board), t.id, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(1<<18), CMD_WRITE)
			}
		}
	}
	_ = batch.ReadWriteConfig()
}

func enterStandbyMode() {
	dvfsState = DVFS_STANDBY
	log.Info("DVFS: Entering Standby mode")
	Delay(2000) // Give gcminer a chance to park itself
	powerstate.SystemPowerOff(false)
	for i := 0; i < int(devhdr.GetTotalChainCount()); i++ {
		if AsicHandle[i+1] != nil {
			AsicHandle[i+1].initComplete = false
		}
	}

	log.Info("DVFS: Standby wait")
}

func (dd *DvfsType) reduceTargetTHS(bigStep bool, msg string) {
	if nBouncingBack == 0 {
		// first time calling this reduce function, always take full step
		if bigStep {
			backOffPct = backOffPctBigStep
		} else {
			backOffPct = backOffPctSmallStep
		}
		nBouncingBack++
	} else {
		if !targetReducing && backOffPct > minPctStep {
			// it just cranked up the targetTHS in prev round, which means the previous target can reach. so don't back off too much
			backOffPct *= float32(nBouncingBack) / float32(nBouncingBack+1)
			nBouncingBack++
		}
	}
	targetReducing = true

	targetTHS = curTargetTHS * (1 - backOffPct)
	realMin := MinThsRate * float32(dd.num_boards) / float32(devhdr.GetTotalChainCount())
	if targetTHS < realMin {
		targetTHS = realMin
	}
	log.Infof("DVFS: reducing target hashrate %.1f%% to %.2f: "+msg, backOffPct*100, targetTHS)

}

var curPower float32

func checkPower() (slowdown bool) {
	// Lower the target hash rate if power is too high
	slowdown = false
	tmp := ReadPower()
	// TBD: Ignore Boco2 power reading of 65535.0; just use previous value
	if tmp < 20000.0 {
		curPower = tmp
	} else {
		log.Errorf("DVFS Monitor: ReadPower returned %.1fW; ignoring", tmp)
	}

	if curPower > powerHighWater {
		if curTargetTHS > MinThsRate { // Are we still above minimum?
			slowdown = true
		}

		if !powerHigh { // Print message the first time
			log.Errorf("DVFS ALARM: power is %.1f; reducing hash rate", curPower)
			powerHigh = true
		}
	} else if powerHigh && curPower < powerLowWater {
		powerHigh = false // Clear the alarm so we can try going back up
		log.Infof("DVFS Monitor: power is %.1f; below low-water mark", curPower)
	}
	return
}

// called almost every second, return value:
//
//	breakSleep:  true to break the sleep early
func (dd *DvfsType) perSecondCheck(hitrate float32) (breakSleep bool) {
	// Check for hot ASICs
	tempAlarm, voltAlarm := dd.monitorTempVolt()
	if tempAlarm {
		return true
	}

	s2 := checkPower()
	if s2 {
		dd.reduceTargetTHS(hitrate < 0.95, "reach power limit")
		dvfsState = DVFS_TUNING
		tuneState = DVFS_TUNE_INIT
		return true
	}

	if voltAlarm {
		return true
	}

	return false
}

// called about 4-6 seconds depending on the hitrate & frequency
func (dd *DvfsType) perMonitorCycleCheck() {

	dd.monitorTempVolt()
	log.Infof("DVFS: avg_temp %.2f, max_temp %.2f (%d/%d), power %.1f", avg_temp, max_temp, hotBoard+1, hotChip, curPower)
	log.Infof("DVFS: avg_volt 1:%.4f 2:%.4f 3:%.4f min 1:%.4f 2:%.4f 3:%.4f max 1:%.4f 2:%.4f 3:%.4f", avg_volt[0], avg_volt[1], avg_volt[2], min_volt[0], min_volt[1], min_volt[2], max_volt[0], max_volt[1], max_volt[2])
}

func (dd *DvfsType) clearHashRateHistory() {
	for i := 0; i < len(hashRateHist); i++ {
		hashRateHist[i] = 0
	}
}

func (dd *DvfsType) getFunctionalBoards() int {
	functional := 0
	chainCount := int(devhdr.GetTotalChainCount())
	for i := 1; i <= chainCount; i++ {
		if AsicHandle[i] != nil && !AsicHandle[i].deadBoard {
			functional++
		}
	}
	return functional
}

// return average hash rate of last 5 cycles
func (dd *DvfsType) getAvgHashRate(curHashRate float32) float32 {
	// save the history of prorated total hashrate
	functional := dd.getFunctionalBoards()
	if functional != 0 && functional != dd.num_boards {
		curHashRate *= float32(dd.num_boards) / float32(functional)
	}

	hashRateHist[hashRateHistIdx] = curHashRate
	hashRateHistIdx = (hashRateHistIdx + 1) % len(hashRateHist)

	var nonZeroCount int
	avgHashRate := float32(0.0)
	for i := 1; i <= 5; i++ {
		idx := (hashRateHistIdx + len(hashRateHist) - i) % len(hashRateHist)
		if hashRateHist[idx] > 0 {
			nonZeroCount++
			avgHashRate += hashRateHist[idx]
		}
	}
	return avgHashRate / float32(nonZeroCount)
}

// Just need to change targetTHS in monitorPowerTarget() & monitorHashRate(), and perSecondCheck() will apply the change
// this make powerTarget longer to reach, but it's simpler and more stable.
// return true if targetTHS changed
func (dd *DvfsType) monitorPowerTarget(avgHashRate float32, sinceTuneDone time.Duration) bool {
	if targetTHS >= orgTargetTHS*0.99 {
		// here it only handle the case of setting targetTHS back close to orgTarget.
		// monitorHashRate() will handle the case that real hash rate is off from target because of hitrate drift
		return false
	}

	hashpct := avgHashRate / orgTargetTHS
	pwrpct := curPower / powerHighWater

	// try to tune the hash rate up if there's still room for psu
	if hashpct < 0.95 && pwrpct < 0.95 && dd.voltage < dd.systeminfo.max_voltage-0.03 && max_temp < tempHighWater-10 {
		var suggestedStep float32
		if sinceTuneDone > time.Minute*30 {
			if hashpct < 0.75 && pwrpct < 0.75 && max_temp < tempHighWater-25 {
				// restart from the original target if it's cooled down a lot
				targetTHS = orgTargetTHS
				nBouncingBack = 0
				log.Info("DVFS: reset target hashrate to original")
				return true
			} else if hashpct < 0.8 && pwrpct < 0.8 && max_temp < tempHighWater-20 {
				// try bigger step if it's cooled down.
				suggestedStep = backOffPctBigStep
			} else if hashpct < 0.90 && pwrpct < 0.90 && max_temp < tempHighWater-15 {
				suggestedStep = backOffPctSmallStep
			}
		}

		var ratio float32
		if suggestedStep >= minPctStep {
			nBouncingBack = 1
			backOffPct = suggestedStep
			targetReducing = false
			ratio = 1 + backOffPct
		} else {
			if nBouncingBack >= 5 || backOffPct <= minPctStep {
				// tried many times but still can't reach, stop trying
				// or it's kind of converged already, no need to try further
				return false
			}

			if hashpct < pwrpct {
				ratio = 0.965 / pwrpct
			} else {
				ratio = 0.965 / hashpct
			}

			if nBouncingBack == 0 {
				// should not happen. handle it anyway
				nBouncingBack = 1
			}

			// if it just backed off in prev round, don't go back up too much.
			// otherwise, let's go back the same amount as the last time
			if targetReducing {
				// backOffPct = original_value / nBouncingBack
				backOffPct *= float32(nBouncingBack) / float32(nBouncingBack+1)
				nBouncingBack++
				targetReducing = false
			}

			if ratio > 1+backOffPct {
				ratio = 1 + backOffPct
			}
		}

		// avoid internal target goes beyond user setting
		newTarget := targetTHS * ratio
		if newTarget >= orgTargetTHS {
			newTarget = orgTargetTHS
		}
		if newTarget/targetTHS < 1.01 {
			return false
		}

		log.Infof("DVFS: increasing target hashrate %.1f%% to %.2f", (newTarget/targetTHS-1)*100, newTarget)
		targetTHS = newTarget
		return true
	}
	return false
}

func (dd *DvfsType) dvfsMainloop() {
	dd.startMinFreq()
	Delay(2000)

	hitrate := float32(0.0)

	log.Infof("DVFS: DVFS_MAIN starting")

	tsPollHitCounter := time.Now()
	hitcounters := getHitCountersAll()

	targetTHS = dd.getInitTargetTHS()
	orgTargetTHS = targetTHS
	avg_temp = 0
	dd.tuneInit()

	oldState := DVFS_TUNING
	oldTuneState := DVFS_TUNE_INIT

	for {
		// Log state changes
		if dvfsState != oldState {
			log.Infof("DVFS: state changed from %s to %s", stateMap[oldState], stateMap[dvfsState])
			oldState = dvfsState
		} else if dvfsState == DVFS_TUNING && tuneState != oldTuneState {
			log.Infof("DVFS: tune state changed from %s to %s", tuneStateMap[oldTuneState], tuneStateMap[tuneState])
			oldTuneState = tuneState
		}

		asicths := curTargetTHS / float32(len(dd.topology))

		// longer sleep time makes hitrate more accurate
		secSleep := int(1.3 / asicths) // to get 300 hits/chip, minimum for an accurate hitrate
		if hitrate < 0.5 {
			secSleep = 3
			if tuneState == DVFS_TUNE_INIT || tuneState == DVFS_TUNE_SET_FREQ {
				secSleep = 1
			}
		} else {
			if tuneState == DVFS_TUNE_FINETUNE {
				// for stages need more accurate hitrate, add one more second
				secSleep += 1
			}
			// Comment: modify/decrease the minimal secSleep for fastTune
			if secSleep < 2 {
				secSleep = 2
			} else if secSleep > 6 {
				secSleep = 6
			}
		}

		log.Debugf("DVFS: Sleeping %d seconds to check hitrate", secSleep)
		for i := 0; i < secSleep; i++ {
			if dd.perSecondCheck(hitrate) {
				log.Error("DVFS: dvfsMainLoop perSecondCheck exiting early")
				break
			}
			Delay(1000) // Delay *after* checking for alarms
		}

		var totalHashRate float32
		if dvfsState != DVFS_STANDBY {
			// Do hitrate temperature and voltage monitoring
			dd.perMonitorCycleCheck()
			// need to apply frequency change immediately in case temp is too high

			var boardspeeds []float32
			ts := time.Now()
			hitrate, boardspeeds, hitcounters = dd.getHashRate(hitcounters, ts.Sub(tsPollHitCounter))
			tsPollHitCounter = ts

			for i := 0; i < len(boardspeeds); i++ {
				totalHashRate += boardspeeds[i]
			}
			totalHashRate /= 1000
			log.Infof("DVFS: hitrate %.3f, boardspeeds %.0f, total %.2f", hitrate, boardspeeds, totalHashRate)
			log.Debugf("DVFS: DVFS_HASHRATE %.2f hitrate %.3f, total %.2f", totalHashRate, hitrate, boardspeeds)
		}

		switch dvfsState {
		case DVFS_TUNING:
			if dd.tuneHashRate(hitrate) {
				dd.tuneDone()
			}

		case DVFS_NORMAL:
			avgHashRate := dd.getAvgHashRate(totalHashRate)
			elapsed := time.Since(tune_done_time)
			if elapsed > time.Minute*2 {
				if dd.monitorPowerTarget(avgHashRate, elapsed) {
					break
				}
			}

		case DVFS_STANDBY:
			// nothing to do for now
		}
	}
}
