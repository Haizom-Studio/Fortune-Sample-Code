package asic

import (
	"math"
	"sort"
	"time"

	ac "eval_miner/device/asiccommon"
	"eval_miner/device/devhdr"
	"eval_miner/device/powerstate"
	"eval_miner/device/psu"
	"eval_miner/log"
)

// TBD:
// Consider high PSU temperature in back-off logic
// Check all for {} loops and make sure they won't hang forever

// Note that t.board is zero-based in this code. When calling the ASIC R/W
// functions, we need to add 1 to access the correct hash board.
func fatal_error() {
	panic("Fatal error encountered!")
}

var (
	MinThsRate  float32 = 30.0
	EcoThsRate  float32 = 90.0   // TBD: Best efficiency
	MaxThsRate  float32 = 185.0  // TBD: Maximum THS rate
	MinTgtPower float32 = 1300.0 // Watts
	MaxTgtPower float32 = 4000.0 // Watts
	maxVolt     float32 = 0.440
)

const (
	low_rate              float32 = 0.97
	high_rate             float32 = low_rate + 0.01
	start_rate            float32 = low_rate - 0.02
	optimize_trigger_rate float32 = 0.70
	minVolt               float32 = 0.170
	hotTempDiff           float32 = 15.0
	finetuneThreshold     float32 = 0.07 // try finetune if targetTHS is within this ratio of current THS
	backOffPctBigStep     float32 = 0.10 // 10% back-off if tempreature or power is too high
	backOffPctSmallStep   float32 = 0.06 // 6% back-off if power is over watermark a little bit
	minPctStep            float32 = 0.015

	noResponseLimit       = 3  // Consecutive no-response reads before declaring a chip dead
	maxChains             = 16 // Arbitrary limit on number of chains we can have
	boardFailureThreshold = 3  // How many times can a board fail before we declare it dead and stop restarting gcminer
)

var hbPresentMask uint8 // HB bitmask - physical HBs, not virtual/chains
var zeroHash uint8      // HB bitmask

var avg_temp float32 = 40.0
var avg_volt [maxChains]float32
var curBoard int = -1
var max_temp float32 = 0
var min_volt [maxChains]float32
var max_volt [maxChains]float32
var hotChip int
var hotBoard int
var maxDeadAsics uint

var deadAsicCtr [maxChains]uint
var zeroHashCtr [maxChains]uint

var dvfsModel string

var (
	tempHighWater float32 = AsicTempLimit - 10

	powerHighWater float32 = psu.MinerPowerMax * 0.99
	powerLowWater  float32 = powerHighWater * 0.95
	backOffPct     float32 = 0.10
	noAsics        bool

	powerHigh    bool
	tripAsserted byte  // HB bitmask
	voltageAlarm uint8 // HB bitmask
)

type TopologyType struct {
	board, row, col, id int
	x, y                int
	tempY, tempK        float32
	bistCoefficient     float32
	temperature         float32
	frequency           float32
	voltage             float32
	hitrate             float32
	voltageGain         float32
	voltageOffset       float32
	badTempCtr          int
	badVoltCtr          int
	noResponseCtr       uint
}

const defaultVoltageGain = 0.00010467773
const defaultVoltageOffset = -0.285892339

type SystemDVFS struct {
}

// NewSystemDVFS creates a new SystemDVFS object, this implements the SystemDVFS interface
// for the ASIC package
func NewSystemDVFS() ac.SystemDVFS {
	return &SystemDVFS{}
}

type TopologyArrayType []TopologyType

func (my *TopologyType) Temp(raw_ip int) float32 {
	raw_ip = raw_ip & 0xffff
	return (float32(raw_ip)-0.5)*my.tempY*(1.0/4096.0) + my.tempK
}

func (my *TopologyType) InverseTemp(temp float32) int { // computes the raw_ip value
	return int((temp-my.tempK)/my.tempY*4096) & 0xffff
}

func (my *TopologyType) Voltage(raw_ip int) float32 {
	raw_ip = raw_ip & 0xffff
	//return (float32(raw_ip) * 0.00010467773) - .285892339  // Deprecated - pre-calibration voltage calculation

	return (float32(raw_ip) * my.voltageGain) + my.voltageOffset
}

func (t *TopologyType) ChangeVoltageParameters(gain float32, offset float32) {

	t.voltageGain *= gain
	t.voltageOffset = (t.voltageOffset + offset) * gain

}

/***
func CreateEvalTopology(chips int) TopologyArrayType {
	var evalBoard int = 2
	topologyEvalBoard := TopologyArrayType{}
	left_count := 0
	right_count := 128
	var r, c int

	for r = 0; r < 1; r++ {
		for c = 0; c < chips; c++ {

			direction := false

			var x, y int

			if true {

				direction = (r & 1) != 0
				y = r

				if direction {
					x = 2 - c
				} else {
					x = c
				}

			}
			t := TopologyType{}
			t.board = evalBoard
			t.row = r
			t.col = c
			t.tempY = 662.88 // this is default coefficient for the IP. A single point calibration can improve upon this
			t.tempK = -287.48
			t.bistCoefficient = 1.0
			t.x = x
			t.y = y
			if !direction {
				t.id = left_count
				left_count++
			} else {
				t.id = right_count
				right_count++
			}
			topologyEvalBoard = append(topologyEvalBoard, t)

		}
	}

	return topologyEvalBoard
}
***/

func CreateTopology(board int) TopologyArrayType {
	topology_1board := TopologyArrayType{}
	left_count := 0
	right_count := 128
	row_count := 44
	var r, c int

	aa := AsicHandle[board+1]
	if aa == nil { // Probably no ASIC detected on this HB, even if it is inserted
		log.Errorf("DVFS: Board %d has no handle; cannot create topology", board+1)
		return nil
	}
	if aa.BoardAsicConfig != nil && aa.BoardAsicConfig.ChipsHi.High == 202 {
		row_count = 50
	}

	for r = 0; r < row_count; r++ {
		for c = 0; c < 3; c++ {

			direction := false

			var x, y int

			if r < 11 {

				direction = (r & 1) != 0
				y = r

				if direction {
					x = 2 - c
				} else {
					x = c
				}

			} else if r < 22 {

				direction = (r & 1) == 0

				y = 21 - r

				if direction {
					x = 3 + 2 - c
				} else {
					x = 3 + c
				}

			} else if r < 33 {

				direction = (r & 1) != 0

				y = r - 22

				if direction {
					x = 6 + 2 - c
				} else {
					x = 6 + c
				}
			} else {

				direction = (r & 1) == 0
				y = 43 - r

				if direction {
					x = 9 + 2 - c
				} else {
					x = 9 + c
				}
			}

			t := TopologyType{}
			t.board = board
			t.row = r
			t.col = c
			t.tempY = 662.88 // this is default coefficient for the IP. A single point calibration can improve upon this
			t.tempK = -287.48
			t.bistCoefficient = 1.0
			t.x = x
			t.y = y
			t.voltageGain = defaultVoltageGain
			t.voltageOffset = defaultVoltageOffset

			if r&0x1 == 0 {
				t.id = left_count
				left_count++
			} else {
				t.id = right_count
				right_count++
			}
			t.frequency = MinFreq
			topology_1board = append(topology_1board, t)

		}
	}

	//log.Infof("DVFS topology: %v", topology_1board)
	return topology_1board
}

type SystemInfoType struct {
	// Voltage in volts
	min_voltage, max_voltage, voltage_step, max_power float32
	// Frequency in Mhz
	refclk, min_frequency, max_frequency               float32
	max_junction_temp, thermal_trip_temp, optimal_temp float32 // Temp in Celsius
	allowable_bad_engines                              int
}

func (my *SystemInfoType) SysInfoInit() {

	var aa *AuraAsic
	var i uint32
	for i = 1; i <= devhdr.GetTotalChainCount(); i++ {
		if AsicHandle[i] != nil && AsicHandle[i].BoardAsicConfig != nil {
			aa = AsicHandle[i] // Assume all hash board types are the same as the first hb we find
			break
		}
	}

	if aa != nil && aa.BoardAsicConfig != nil && aa.BoardAsicConfig.ChipsHi.High == 202 { // Zareen
		my.min_voltage = 14.5
	} else {
		my.min_voltage = psu.MinerVoutMin
	}
	my.max_voltage = psu.MinerVoutMax
	my.voltage_step = 0.005
	my.max_power = powerHighWater
	my.refclk = 25.0 // MHz
	my.min_frequency = MinFreq
	my.max_frequency = MaxFreq
	my.max_junction_temp = 110.0
	my.thermal_trip_temp = AsicTempLimit
	my.optimal_temp = 55.0
	my.allowable_bad_engines = 12
}

type BatchType struct {
	action      uint8
	board, addr uint16
	id          int // Id=-1 indicates a broadcast command, so use int instead of u16
	data        uint32
}

type BatchArrayType []BatchType

func (batArray BatchArrayType) Add(board uint16, id int, addr uint16, data uint32, action uint8) BatchArrayType {
	var cmd BatchType

	cmd.board = board
	cmd.id = id
	cmd.addr = addr
	cmd.data = data // Could skip this for reads, but it won't hurt
	cmd.action = action

	batArray = append(batArray, cmd)

	return batArray
}

func chipToTopologyIndex(board int, id int) int {
	for i := 0; i < len(dd.topology); i++ {
		t := &dd.topology[i]
		if t.board == board && t.id == id {
			return i
		}
	}
	log.Errorf("DVFS: chipToTopologyIndex did not find board/chain %d chip %d", board, id)
	return -1
}

func (b BatchArrayType) ReadWriteConfig() error {

	var (
		ii       int
		readData uint32
		bcast    bool
		local_id uint8
		err      error
		t        *TopologyType
	)

	// For each cmd entry
	for ii = 0; ii < len(b); ii++ {

		// Don't talk to dead boards - reads will time out and potentially cause API read failures
		if b[ii].id != -1 { // If not broadcast
			t = &dd.topology[chipToTopologyIndex(int(b[ii].board), b[ii].id)]
			if AsicHandle[b[ii].board+1].deadBoard {
				b[ii].data = 0xffffffff
				continue // Go to next operation
			}
		}

		aa := AsicHandle[b[ii].board+1]
		switch b[ii].action {

		case CMD_NOTHING:
			// Do nothing!

		case CMD_READ:
			readData, err = aa.RegRead(uint8(b[ii].id), uint8(b[ii].addr))
			if err != nil {
				//log.Errorf("DVFS: ReadWriteConfig RegRead error bd %d id %d adr %d: %s, data = 0x%08x", b[ii].board+1, b[ii].id, b[ii].addr, err, readData)
				b[ii].data = 0xffffffff
				t.noResponseCtr++
				if t.noResponseCtr == noResponseLimit {
					log.Errorf("DVFS RegRead: Dead chip %d/%d detected", b[ii].board, b[ii].id)
				}
			} else {
				b[ii].data = readData
				if t.noResponseCtr >= noResponseLimit {
					log.Infof("DVFS RegRead: Dead chip %d/%d is back to life after %d missed responses", b[ii].board, b[ii].id, t.noResponseCtr)
				}
				t.noResponseCtr = 0
			}

		case CMD_WRITE:
			if b[ii].id == -1 {
				bcast = true
			} else {
				bcast = false
			}
			if b[ii].id < 0 && b[ii].id != -1 {
				log.Errorf("DVFS: Batch cmd entry #%d is %d (invalid)/n", ii, b[ii].id)
				fatal_error()
			} else {
				local_id = uint8(b[ii].id)
			}

			err = aa.RegWrite(local_id, uint8(b[ii].addr), uint32(b[ii].data), bcast)
			if err != nil {
				log.Errorf("DVFS: ReadWriteConfig RegWrite error bd %d id %d adr %d: %s, data = 0x%08x", b[ii].board+1, b[ii].id, b[ii].addr, err, readData)
			}

		case CMD_READWRITE:
			readData, _ = aa.RegReadWrite(uint8(b[ii].id), uint8(b[ii].addr), uint32(b[ii].data))
			if err != nil {
				//log.Errorf("DVFS: ReadWriteConfig RegReadWrite error bd %d id %d adr %d: %s, data = 0x%08x", b[ii].board+1, b[ii].id, b[ii].addr, err, readData)
				b[ii].data = 0xffffffff
				t.noResponseCtr++
				if t.noResponseCtr == noResponseLimit {
					log.Errorf("DVFS RegReadWrite: Dead chip %d/%d detected", b[ii].board, b[ii].id)
				}
			} else {
				b[ii].data = readData
				if t.noResponseCtr >= noResponseLimit {
					log.Infof("DVFS RegReadWrite: Dead chip %d/%d is back to life after %d missed responses", b[ii].board, b[ii].id, t.noResponseCtr)
				}
				t.noResponseCtr = 0
			}

		default:
			log.Errorf("DVFS: ReadWriteConfig: unknown command %d at entry %d", b[ii].action, ii)
		}
	}

	return err
}

type DvfsType struct {
	topology                       TopologyArrayType
	systeminfo                     SystemInfoType
	partitions                     [][]int
	num_boards, num_rows, num_cols int // num_boards is number of boards actually present, not max number of boards
	chains_per_board               int
	voltage                        float32
	pll_multiplier                 float32
	last_optimization_temp         float32
	initial                        bool
	dvfsTuneDone                   chan bool
}

var dd DvfsType

// Generate Dvfs struct with sysinfo, system topology, and partitions
func (dd *DvfsType) CreateDvfs() (err error) {
	//var err error
	var ii, kk int

	dd.num_boards, dd.num_cols, dd.num_rows = 0, 0, 0
	dd.chains_per_board = int(devhdr.GetHashBoardChainCount())
	maxDeadAsics = devhdr.GetMaxAsicsInChain() / 2

	dd.dvfsTuneDone = make(chan bool)
	dd.systeminfo.SysInfoInit()

	ii = devhdr.EvalHashBoardId - 1
	topo := CreateTopology(ii)
	if topo != nil {
		log.Infof("DVFS: HB%d is present", ii+1)
		hbPresentMask |= (1 << ii)
		if AsicHandle[ii+1] != nil {
			dd.num_boards++
			log.Infof("DVFS topology: Adding board %d", ii+1)
			dd.topology = append(dd.topology, topo...)
		} else {
			log.Infof("DVFS topology: No handle for board/chain %d", ii+1)
		}
	} else {
		// Message printed out when board is disabled in chassisconfig.json
		log.Debugf("DVFS topology: Unable to create topology for board %d", ii+1)

	}

	log.Infof("hashboardpresentmask %v, num_boards = %d", hbPresentMask, dd.num_boards)
	// Sort the system chips topology by board, row, and column
	sort.Slice(dd.topology, func(i, j int) bool {
		if dd.topology[i].board < dd.topology[j].board {
			return true
		}
		if dd.topology[i].board > dd.topology[j].board {
			return false
		}
		if dd.topology[i].row < dd.topology[j].row {
			return true
		}
		if dd.topology[i].row > dd.topology[j].row {
			return false
		}
		return dd.topology[i].col < dd.topology[j].col
	})
	log.Debugf("DVFS: System topology: %v\n", dd.topology) // TEMP - debugf

	for ii = 0; ii < len(dd.topology); ii++ {
		t := dd.topology[ii]
		if t.board < 0 {
			fatal_error()
		}
		//dd.num_boards = int(math.Max(float64(dd.num_boards), float64(t.board+1)))
		dd.num_rows = int(math.Max(float64(dd.num_rows), float64(t.row+1)))
		dd.num_cols = int(math.Max(float64(dd.num_cols), float64(t.col+1)))
	}
	log.Infof("DVFS: num_boards = %d", dd.num_boards)

	/*** Comment out for eval board testing ***
	if (dd.num_rows * dd.num_cols * dd.num_boards) != int(len(dd.topology)) {
		fatal_error()
	}
	***/

	// I want to split things into num_col*4 partitions so I can roughly adjust one partition independent of another. A partition should never contain more than 1 row element
	for ii = 0; ii < dd.num_cols*4; ii++ {
		dd.partitions = append(dd.partitions, []int{})
		for kk = 0; kk < len(dd.topology); kk++ {
			t := dd.topology[kk]
			if t.col == int(ii/4) && t.row%4 == int(ii%4) {
				dd.partitions[ii] = append(dd.partitions[ii], int(kk))
			}
		}
	}

	count := 0
	for ii = 0; ii < len(dd.partitions); ii++ {
		count += int(len(dd.partitions[ii]))
	}
	if count != int(len(dd.topology)) {
		fatal_error()
	}

	return err
}

func parseConfigFiles() {

}

func setModelParams() {
	dvfsModel = devhdr.GetChassisModelNumber()
}

// Call this before spawning DVFS thread and save away init values like voltage
func (s SystemDVFS) InitialSetup() {
	var ii int
	batch := BatchArrayType{}
	var (
		//vcosel      int = 2
		pll_divider = (div1 + 1) * (div2 + 1)
	)
	boardChains := int(devhdr.GetHashBoardChainCount())

	parseConfigFiles()
	setModelParams()

	maxLimit := devhdr.GetMaxLimit()
	MaxThsRate = maxLimit.MaxTHs
	powerHighWater = maxLimit.MaxPower * 0.99
	MinTgtPower = float32(maxLimit.MinPowerSoft)
	MaxTgtPower = float32(maxLimit.MaxPowerSoft)
	powerLowWater = powerHighWater * 0.90

	log.Infof("DVFS: Model: %v. Setting MaxThs to %.1f and power high-water to %.1f",
		dvfsModel, MaxThsRate, powerHighWater)

	err := dd.CreateDvfs() // DVFS struct - initializes systeminfo and topology
	if err != nil {
		log.Errorf("Error %s returned by NewDvfsType()", err)
		fatal_error()
	}
	dd.pll_multiplier = float32(pll_divider) / dd.systeminfo.refclk * (1 << 20) // Don't initialize this before dd is initialized!

	dd.initial = true
	dd.last_optimization_temp = -100.0

	for ii = 0; ii < int(devhdr.GetHashBoardCount()); ii++ {

		if hbPresentMask&(1<<ii) != 0 {
			for j := 0; j < boardChains; j++ {
				if AsicHandle[((ii*boardChains)+j)+1] == nil || AsicHandle[((ii*boardChains)+j)+1].deadBoard {
					continue // Assume this whole board is dead
				}
				t := &dd.topology[0]
				// i want a little delay between turning on the temp sensor and clearing max_temp
				batch = batch.Add(uint16((ii*boardChains)+j), -1, ADDR_PLL_FREQ, uint32(dd.systeminfo.min_frequency*dd.pll_multiplier), CMD_WRITE)
				if true { // Treat all ASICS as ECO+
					var setting int
					setting = int((48000 / dd.systeminfo.min_frequency)) - 32
					if setting < 0 {
						setting = 0
					}
					if setting > 64 {
						setting = 64
					}
					batch = batch.Add(uint16((ii*boardChains)+j), -1, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(0<<18), CMD_WRITE)
					batch = batch.Add(uint16((ii*boardChains)+j), -1, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(1<<18), CMD_WRITE)
				} else {
					batch = batch.Add(uint16((ii*boardChains)+j), -1, ADDR_DUTY_CYCLE, 0, CMD_WRITE)
				}
				batch = batch.Add(uint16((ii*boardChains)+j), -1, ADDR_MAX_TEMP_SEEN, 0, CMD_WRITE) // clear max temp seen
				batch = batch.Add(uint16((ii*boardChains)+j), -1, ADDR_THERMAL_TRIP, uint32(t.InverseTemp(dd.systeminfo.thermal_trip_temp)), CMD_WRITE)
			}
		}
	}

	_ = batch.ReadWriteConfig()
	// Allow other processes to access ASICs now that setup is complete
	for i := 0; i < int(devhdr.GetTotalChainCount()); i++ {
		if AsicHandle[i+1] != nil {
			AsicHandle[i+1].initComplete = true
		}

	}

	dd.SetVoltage(dd.systeminfo.min_voltage)
	Delay(500) // let PS settle to new value
	for ii = 0; ii < int(devhdr.GetHashBoardCount()); ii++ {
		if hbPresentMask&(1<<ii) != 0 {
			EnablePowerSwitch(int(ii))
			Delay(1000) // stagger turning on boards to be nice to the PS
		}
	}
}

func (dd *DvfsType) SetVoltage(voltage float32) {

	/*** For ASIC simulator
	for ii := 0; ii < int(devhdr.GetHashBoardCount()); ii++ {
		RegWrite(uint8(ii), 00, 0xff, uint32(voltage)*1000, true)
	}
	***/
	if voltage < dd.systeminfo.min_voltage {
		voltage = dd.systeminfo.min_voltage
	}
	_ = psu.SetVoltage(voltage)
	dd.voltage = voltage
}

func EnablePowerSwitch(board int) {

	_ = powerstate.HbPowerOn(board + 1) // HbPowerOn() is one-based

}

func ReadPower() float32 {
	return psu.GetInputPower(false)
}

func Delay(msec time.Duration) {
	//log.Infof("DVFS: Delay %d msec", int(msec))
	time.Sleep(time.Millisecond * msec)
}

func getAvgFreq(ths float32) float32 {
	average_f := ths * 1000000 / (254.0 * 4 / 3) / float32(len(dd.topology)) / low_rate
	// The average frequency per ASIC that we need to get to system THS target, without exceeding max or min
	average_f = float32(math.Max(float64(average_f), float64(dd.systeminfo.min_frequency)))
	average_f = float32(math.Min(float64(average_f), float64(dd.systeminfo.max_frequency)))
	return average_f

}

func (s SystemDVFS) DVFS() bool {
	var i int
	var chainCount = int(devhdr.GetTotalChainCount())
	time.Sleep(time.Second * 5) // Wait for miner threads to finish initializing

	for i = 1; i <= chainCount; i++ {
		log.Debugf("DVFS: AsicHandle[%d] = %v", i, AsicHandle[i])
		if AsicHandle[i] != nil {
			log.Debugf("DVFS: AsicHandle[%d].BoardAsicConfig = %v", i, AsicHandle[i].BoardAsicConfig)
		}
		if AsicHandle[i] != nil && AsicHandle[i].BoardAsicConfig != nil {
			break
		}
	}
	if i > chainCount {
		log.Errorf("DVFS: No system ASICs detected; aborting")
		noAsics = true
		return true
	}

	Delay(1000)

	dd.dvfsMainloop()

	return false
}

func HashBoardAlarm() bool {

	chainCount := int(devhdr.GetTotalChainCount())
	if tripAsserted != 0 || voltageAlarm != 0 || noAsics {
		return true
	}

	for i := 1; i <= chainCount; i++ {
		if AsicHandle[i] != nil && AsicHandle[i].deadBoard {
			return true
		}
	}

	if dvfsState == DVFS_NORMAL {
		for i := 0; i < chainCount; i++ {
			if AsicHandle[i] != nil && hashRatesSave[i] == 0 {
				if zeroHash&(1<<i) == 0 {
					log.Errorf("DVFS ALARM: Hash board/chain %d has zero hash rate", i+1)
				}
				zeroHash |= (1 << i)
				zeroHashCtr[i]++ // What should we do if this happens 3 or 4 times in a row?
				return true
			} else {
				if zeroHash&(1<<i) != 0 {
					log.Infof("DVFS: Hash board/chain %d no longer has zero hash rate; alarm cleared", i+1)
				}
				zeroHash &= ^(1 << i)
				zeroHashCtr[i] = 0
			}
		}
	}

	return false

}
