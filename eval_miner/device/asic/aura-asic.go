package asic

import (
	"container/list"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"sync"
	"time"

	"eval_miner/device/asicio"
	"eval_miner/device/devhdr"
	"eval_miner/log"
)

const (
	ADDR_CHIP_UNIQUE            = 0
	ADDR_CHIP_REVISION          = 1
	ADDR_ASICID                 = 2
	ADDR_BAUD_DIVISOR           = 3
	ADDR_CLK_COUNT              = 4
	ADDR_CLK_COUNT_64B          = 5
	ADDR_HASHCLK_COUNT          = 6
	ADDR_BYTES_RECEIVED         = 7
	ADDR_COM_ERROR              = 10
	ADDR_RSP_ERROR              = 11
	ADDR_DRIVE_STREGTH          = 12
	ADDR_PUD                    = 13
	ADDR_VERSION_BOUND          = 16
	ADDR_VERSION_SHIFT          = 17
	ADDR_SUMMARY                = 18
	ADDR_HIT_CONFIG             = 19
	ADDR_HASH_CONFIG            = 20
	ADDR_STAT_CONFIG            = 21
	ADDR_BIST_THRESHOLD         = 22
	ADDR_BIST                   = 23
	ADDR_PLL_CONFIG             = 24
	ADDR_PLL_FREQ               = 25
	ADDR_PLL_OPTION             = 26
	ADDR_PLL_CAL                = 27
	ADDR_IP_CFG                 = 28
	ADDR_TEMP_CFG               = 29
	ADDR_TEMPERATURE            = 30
	ADDR_DVM_CFG                = 31
	ADDR_VOLTAGE                = 32
	ADDR_DRO_CFG                = 33
	ADDR_DRO                    = 34
	ADDR_THERMAL_TRIP           = 35
	ADDR_MAX_TEMP_SEEN          = 36
	ADDR_SPEED_DELAY            = 39
	ADDR_SPEED_UPPER_BOUND      = 40
	ADDR_SPEED_INCREMENT        = 41
	ADDR_ENGINE_MASK_BASE       = 48
	ADDR_BIST_RESULT_BASE       = 64
	ADDR_ENGINE_RESULT_BASE     = 80
	ADDR_HIT_COUNT_GENERAL      = 96
	ADDR_TRUEHIT_COUNT_GENERAL  = 97
	ADDR_HIT_COUNT_SPECIFIC     = 98
	ADDR_TRUEHIT_COUNT_SPECIFIC = 99
	ADDR_HIT_COUNT_DIFFICULT    = 100
	ADDR_HIT_COUNT_DROPPED      = 101
	ADDR_HIT_COUNT_DROPPED_DIFF = 102
	ADDR_DUTY_CYCLE             = 104
	ADDR_CLOCK_RETARD_BASE      = 106
	ADDR_CONTEXT0_BASE          = 128
	ADDR_SEQUENCE               = 147
	ADDR_CONTEXT1_BASE          = 148
	ADDR_CONTEXT2_BASE          = 164
	ADDR_CONTEXT3_BASE          = 180
	ADDR_HIT0_HEADER_BASE       = 196
	ADDR_HIT0                   = 216
	ADDR_HIT1_HEADER_BASE       = 218
	ADDR_HIT1                   = 238
	ADDR_HIT2                   = 239
)

const (
	CMD_NOTHING   uint8 = 0
	CMD_WRITE     uint8 = 1
	CMD_READ      uint8 = 2 // read a register
	CMD_READWRITE uint8 = 3 // read a register and after reading write it(typically with 0)
	CMD_LOAD0     uint8 = 4 // this will load a copy into chunk1 copy0,1,2,3
	CMD_LOAD1     uint8 = 5 // this will load a copy into chunk1 copy1 only
	CMD_LOAD2     uint8 = 6 // this will load a copy into chunk1 copy2 only
	CMD_LOAD3     uint8 = 7 // this will load a copy into chunk1 copy3 only

	CMD_RETURNHIT uint8 = 0x40 // If bit 6 is set of command word, miner will respond with most recent hit info. Don't use with CMD_BROADCAST
	CMD_BROADCAST uint8 = 0x80 // If bit 7 is set of command word, id will be ignored and all miners will accept the command
)

const (
	CMD_UNIQUE = 0x12345678
	RSP_UNIQUE = 0xdac07654
	HIT_UNIQUE = 0xdac07654

	RSP_LEN_CFG = 16
	RSP_LEN_HIT = 92
	IDLE_BYTES  = 20
)

const (
	tempY = 662.88 // this is default coefficient for the IP. A single point calibration can improve upon this
	tempK = -287.48
)

const (
	refClk = 25.0
	vcoSel = 2
	div1   = 1
	div2   = 1
)

const (
	asicFaultyThresholdInSeconds      = 10 * time.Second
	asicFaultyThresholdCount          = 10
	asicFalseFaultyThresholdInMinutes = 10 * time.Minute
	asicFalseFaultyThresholdCount     = 3
)

type ChipEntry struct {
	ChipId        uint8
	Frequency     float32
	Performance   float32
	Voltage       float32
	Temperature   float32
	VoltGain      float32
	VoltOffset    float32
	NotFound      bool
	NotResponsive bool
	NoResponseCtr int
}

type AsicJob struct {
	Version    uint32
	Prevblock  [8]uint32
	Merkle     [8]uint32
	Time       uint32
	Difficulty uint32 // this field is used for the hash but not used for the difficulty screen
	Nonce      uint32
}

// faultyAsicTracker to track information about faulty ASICs.
type faultyAsicTracker struct {
	firstOccurrenceTime time.Time
	counts              uint32
	isFaulty            bool
	falseBadCheck       uint32
}

// Let's prepare 2 queues: one for Asic results, the other for command response.
// The go routine reads from UART and parses the frames to the right queue.
// Both queues needs to be thread safe.

type AuraAsic struct {
	AsicIDs            []uint8
	devName            string
	devFile            *os.File
	revision           uint8
	numEngines         uint16
	brdChainId         uint8
	slotId             uint
	singleThread       bool
	CrcErrCfg          uint32
	CrcErrHit          uint32
	baudRate           uint32
	autoReport         bool // this is a configuration, not current status
	disableCD          bool
	maxChipId          int
	chipPtrs           map[uint8]*ChipEntry // Use this to access a chip entry by chipID instead of chip entry offset
	seqChipIds         []uint8              // sorted list of sequential chip IDs to iterate over chipArray in order
	actualChipIds      []uint8              // sorted list of actual chip IDs to iterate over chipArray in order
	ChipArray          []ChipEntry          // array of each working chip info; sort by chipId
	Debugging          bool                 // for unit test only
	initComplete       bool
	deadBoard          bool
	BoardAsicConfig    *devhdr.HashBoardAsicIdConfig
	lastTempReading    time.Time
	lastVoltReading    time.Time
	lastFreqReading    time.Time
	lastBadAsicMarking time.Time
	cacheTemps         []float64
	cacheVolts         []float64
	cacheFreqs         []float64
	ghits              [maxChip]uint32
	thits              [maxChip]uint32
	deltaGhits         [maxChip]uint32
	deltaThits         [maxChip]uint32
	asicIO             *asicio.AuraAsicIO
	verMask            uint32
	jobCount           uint64
	hitStats           [statsRingSize]HitStat // poll hit counters every 5s and store results of 1min
	lastlog            time.Time
	hitStatsCursor     uint // pointer to next slot
	reportCursor       uint // pointer to next slot

	pollingStart    int
	lastPolling     time.Time
	lastCntrReading time.Time

	swReadErrorAsics []uint8
	// faultyAsicsTrack to track faulty ASICs, where the key is the ASIC ID (uint8)
	// and the value is a pointer to a faultyAsicTracker struct.
	faultyAsicsTrack      map[uint8]*faultyAsicTracker
	faultyAsicTrackerLock sync.Mutex
}

var voltTrace [devhdr.MaxHashBoards]*list.List // Trace queue for voltage readings for each hash board
const voltTraceLen = 30                        // Last minte of voltage readings happening every 2 seconds

var AsicHandle [devhdr.MaxHashBoards + 1]*AuraAsic // 1-based array: hash boards 1 - N; 0 is unused

// Convert chipId to index into ChipArray[]
func (aa *AuraAsic) ChipIdToIndex(chipId uint8) int {
	// chipId is greater than max asics  [i.e. greater than 193 for default cards]
	// or is in unused range [i.e. greater than 65 and less than 128]
	if int(chipId) > aa.BoardAsicConfig.ChipsHi.High ||
		(int(chipId) > aa.BoardAsicConfig.ChipsLow.High && int(chipId) < aa.BoardAsicConfig.ChipsHi.Low) {
		return -1 // This will generally cause a program crash as an invalid array index
	}
	if int(chipId) >= aa.BoardAsicConfig.ChipsHi.Low {
		return int(chipId) - aa.BoardAsicConfig.ChipsHi.Low + aa.BoardAsicConfig.ChipsLow.High + 1 // Offset for chips 128 to 193
	}

	return int(chipId) // Offset for chips 0 to 65
}

func ChipIndexToId(chipIdx uint8, asicIdCfg *devhdr.HashBoardAsicIdConfig) int {

	if chipIdx >= uint8(GetAsicCounts()) {
		return -1 // Invalid index - will most likely crash caller
	}
	if chipIdx > uint8(asicIdCfg.ChipsLow.High) {
		return int(chipIdx) - (asicIdCfg.ChipsLow.High + 1) + asicIdCfg.ChipsHi.Low
	}

	return int(chipIdx) // chipId = idx
}

// Broadcast-write same value to same register on all ASICs
func (aa *AuraAsic) regWriteAll(addr uint8, data uint32) error {
	return aa.RegWrite(0, addr, data, true)
}

func (aa *AuraAsic) RegWrite(asicId uint8, addr uint8, data uint32, broadcast bool) error {
	if aa.singleThread {
		return aa.asicIO.BlockingWrite(asicId, addr, CMD_WRITE, data, broadcast)
	}
	var targets []uint8
	if !broadcast {
		targets = []uint8{asicId}
	}
	return aa.asicIO.NonBlockingWrite(targets, []uint8{addr}, []int64{int64(data)}, broadcast)
}

func (aa *AuraAsic) regRWAsync(asicId, addr, cmd uint8, data uint32) error {
	if aa.deadBoard {
		return fmt.Errorf("regRWAsync: HB %d non-responsive", aa.brdChainId)
	}
	return aa.asicIO.BlockingWrite(asicId, addr, cmd, data, false)
}

func (aa *AuraAsic) regReadAsync(asicId uint8, addr uint8) error {
	return aa.regRWAsync(asicId, addr, CMD_READ, 0)
}

func (aa *AuraAsic) getRegReadTimeout(times int64) time.Duration {
	base := int64(RSP_LEN_CFG * 10000000 * 5 / aa.baudRate)
	ret := base * (times + 20)
	// at least wait for 1ms as time is not accurate
	if ret < 1000 {
		ret = 1000
	}
	return time.Duration(ret * int64(time.Microsecond))
}

func (aa *AuraAsic) regRW(asicId uint8, addr uint8, data uint32, cmd uint8) (uint32, error) {
	if aa.singleThread {
		return aa.asicIO.BlockingRead(asicId, addr, cmd, data)
	}
	res, retData, ok := aa.asicIO.NonBlockingRead([]uint8{asicId}, []uint8{addr}, []int64{int64(data)}, cmd)
	if ok != nil || res != 0 {
		return 0, ok
	}
	return uint32(retData[0]), nil
}

func (aa *AuraAsic) RegRead(asicId uint8, addr uint8) (uint32, error) {
	return aa.regRW(asicId, addr, 0, CMD_READ)
}

func (aa *AuraAsic) RegReadWrite(asicId uint8, addr uint8, data uint32) (uint32, error) {
	return aa.regRW(asicId, addr, data, CMD_READWRITE)
}

func (aa *AuraAsic) close() {
	// better to restore the baud rate to default
	if aa.baudRate != 115200 {
		_ = aa.setBaudRate(115200)
	}
	AsicHandle[aa.brdChainId] = nil

	// clean up the go routines
	if !aa.singleThread {
		aa.singleThread = true
		aa.asicIO.SetBlockingReadMode(true)
		aa.asicIO.CloseASICIO()
	}
}

func (aa *AuraAsic) enableAutoReporting(enabled bool) {
	var v uint32 = 0x18
	if aa.disableCD {
		v |= 0x2
	}

	if enabled {
		log.Debug("===Enabling autoreporting")
		_ = aa.regWriteAll(ADDR_HIT_CONFIG, v|1)
	} else {
		log.Debug("===Disabling autoreporting")
		_ = aa.regWriteAll(ADDR_HIT_CONFIG, v)
	}
}

func (aa *AuraAsic) clearAllCounters() {
	_ = aa.regWriteAll(ADDR_COM_ERROR, 0)
	_ = aa.regWriteAll(ADDR_RSP_ERROR, 0)
	_ = aa.regWriteAll(ADDR_HIT_COUNT_GENERAL, 0)
	_ = aa.regWriteAll(ADDR_HIT_COUNT_SPECIFIC, 0)
	_ = aa.regWriteAll(ADDR_TRUEHIT_COUNT_GENERAL, 0)
	_ = aa.regWriteAll(ADDR_TRUEHIT_COUNT_SPECIFIC, 0)
	_ = aa.regWriteAll(ADDR_HIT_COUNT_DIFFICULT, 0)
	_ = aa.regWriteAll(ADDR_HIT_COUNT_DROPPED_DIFF, 0)
}

/* Asic initialization:
 * 1. Check if this is the ASIC that we support
 * 2. disable auto reporting
 * 3. set different version rolling range for each chip
 * Note:
 *  the caller should get the number of ASIC on this hash board before calling this function.
 */
func AuraAsicInit(devName string, brdChainId, slotId uint, maxChipId int, baudrate uint32,
	noCD bool, debug bool, asicIdCfg *devhdr.HashBoardAsicIdConfig) (*AuraAsic, error) {
	aa := AuraAsic{devName: devName, baudRate: baudrate}
	aa.brdChainId = uint8(brdChainId)
	aa.slotId = slotId

	var err error
	var cmd *exec.Cmd

	aa.maxChipId = maxChipId
	aa.disableCD = noCD
	aa.chipPtrs = make(map[uint8]*ChipEntry)
	voltTrace[brdChainId-1] = list.New()
	if aa.asicIO, err = asicio.NewAsicIOInit(baudrate, devName, uint8(brdChainId), uint8(slotId), asicIdCfg); err != nil {
		return nil, err
	}
	// start the chip with single thread mode and move to multi-thread mode later
	aa.singleThread = true
	aa.asicIO.SetBlockingReadMode(true)
	strBr := fmt.Sprintf("%d", baudrate)
	if runtime.GOOS != "darwin" {
		cmd = exec.Command("stty", "-F", aa.devName, "raw", strBr)
	} else {
		cmd = exec.Command("stty", "-f", aa.devName, "raw", strBr) // For Mac
	}
	_ = cmd.Run()
	// write 100B of 0 first in case chips are out of sync
	_ = aa.asicIO.WriteIdle(100)
	aa.enableAutoReporting(false)
	// Clear response buffer
	aa.asicIO.ClearCfgResp()

	// check chips one by one
	for chipID := 0; chipID <= maxChipId; chipID++ {
		_ = aa.regReadAsync(uint8(chipID), ADDR_CHIP_UNIQUE)
		_ = aa.asicIO.WriteIdle(IDLE_BYTES)
	}

	timeout := aa.getRegReadTimeout(int64(maxChipId))
	start := time.Now()
	var chips []int
	for {
		result, _ := aa.asicIO.CheckCfgResp()
		if result != nil {
			if result.Data == 0x61727541 && result.Address == ADDR_CHIP_UNIQUE && int(result.Id) <= maxChipId {
				found := false
				for _, asic := range aa.swReadErrorAsics {
					if asic == result.Id {
						found = true
					}
				}
				if found {
					log.Errorf("Skipping ASIC id %v", result.Id)
					continue
				}
				chips = append(chips, int(result.Id))
				log.Debugf("bd %d chip %d is working", brdChainId, result.Id)
			}
		}
		if time.Since(start) > timeout {
			break
		}
	}

	if len(chips) == 0 {
		aa.close()
		aa.deadBoard = true
		return nil, fmt.Errorf("no Auradine ASIC is detected")
	}

	// sort chipIDs
	sort.Ints(chips)
	log.Infof("Brd %d: %d chips found: %v", brdChainId, len(chips), chips)

	// Remove duplicates
	chips2 := []uint8{}
	keys := make(map[int]bool)
	for _, entry := range chips {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			chips2 = append(chips2, uint8(entry))
		}
	}
	log.Infof("Brd %d: %d non-dup chips2 found: %v", brdChainId, len(chips2), chips2)

	numChips := GetAsicCounts()
	aa.actualChipIds = chips2
	aa.seqChipIds = make([]uint8, numChips)
	aa.ChipArray = []ChipEntry{}
	jj := 0
	for ii := 0; ii < int(numChips); ii++ {
		tempChip := ChipEntry{}
		tempChip.ChipId = uint8(ChipIndexToId(uint8(ii), asicIdCfg))
		aa.seqChipIds[ii] = tempChip.ChipId
		if jj < len(chips2) && tempChip.ChipId == uint8(chips2[jj]) {
			jj++
		} else {
			tempChip.NotFound = true
		}
		aa.ChipArray = append(aa.ChipArray, tempChip)
		aa.chipPtrs[uint8(ChipIndexToId(uint8(ii), asicIdCfg))] = &aa.ChipArray[ii]
	}
	log.Infof("Brd %d: Actual total of %d chips detected: %v\n", brdChainId, len(aa.actualChipIds), aa.actualChipIds)
	log.Infof("Brd %d: Expected total of %d chips : %v\n", brdChainId, len(aa.seqChipIds), aa.seqChipIds)

	resp, err := aa.RegRead(aa.actualChipIds[0], ADDR_CHIP_REVISION)
	if err != nil {
		aa.devFile.Close()
		aa.revision = uint8(resp & 0xff)
		var if_ver = (resp & 0xff00) >> 8
		if if_ver != 1 {
			aa.close()
			return nil, fmt.Errorf("unsupported chip version %d", if_ver)
		}
		aa.numEngines = uint16(resp >> 16)
	}
	aa.clearAllCounters()
	aa.pllInit() // Only do this once
	aa.BoardAsicConfig = asicIdCfg
	log.Infof("Brd %d: BoardAsicConfig %v", aa.brdChainId, aa.BoardAsicConfig)
	aa.singleThread = false
	aa.asicIO.SetBlockingReadMode(false)
	aa.asicIO.EnableAsyncRW()

	log.Debugf("Setting AsicHandle[%d] to %p", brdChainId, &aa)
	AsicHandle[brdChainId] = &aa
	return &aa, nil
}

func (aa *AuraAsic) GetAsicNum() int {
	return len(aa.seqChipIds)
}

func (aa *AuraAsic) setVerRolling(shift uint32, lower uint32, upper uint32) error {
	lower = (lower + 3) / 4 * 4 // round up to 4x
	window := (upper - lower + 1) / uint32(len(aa.actualChipIds))

	if window < 4 {
		window = 1
	}
	window = window / 4 * 4

	for i := 0; i < len(aa.actualChipIds); i++ {
		min := lower + window*uint32(i)
		max := min + window - 1
		_ = aa.RegWrite(aa.actualChipIds[i], ADDR_VERSION_BOUND, max<<16+min, false)
		log.Debugf("set version rolling for chip %d: %d - %d", aa.actualChipIds[i], min, max)
	}
	log.Debugf("set version rolling shift to %d", shift)
	return aa.regWriteAll(ADDR_VERSION_SHIFT, shift)
}

func (aa *AuraAsic) setBaudRate(br uint32) error {
	var cmd *exec.Cmd
	divisor := (25000000*64 + br/2) / br
	err := aa.regWriteAll(ADDR_BAUD_DIVISOR, divisor<<16+divisor)

	if err != nil {
		log.Errorf("setBaudRate error %s\n", err)
		return err
	}

	if !aa.Debugging {
		str := fmt.Sprintf("%d", br)
		if runtime.GOOS != "darwin" {
			cmd = exec.Command("stty", "-F", aa.devName, str)
		} else {
			cmd = exec.Command("stty", "-f", aa.devName, str)
		}
		err := cmd.Run()
		if err != nil {
			return err
		}
	}

	aa.asicIO.SetSetBaudRate(br)
	aa.baudRate = br
	return nil
}

// Convert float temperature value to uint32 for ADDR_TEMPERATURE reg
func temperatureConvert(v float32) uint32 {
	return uint32((v-tempK)/(tempY*(1.0/4096.0)) + 0.5)
}

// Initalize ASIC voltage and temperature monitoring sensors
func (aa *AuraAsic) runVoltageAndTemperature() {
	_ = aa.regWriteAll(ADDR_IP_CFG, 0x1) // ipclk is 6.25Mhz
	_ = aa.regWriteAll(ADDR_TEMP_CFG, 0xd)
}

func (aa *AuraAsic) SetThermalTrip(v float32) {
	if v < 0 || v > AsicTempLimit {
		log.Errorf("SetThermalTrip: Temperature %.2f is above limit %.2f; ignoring", v, AsicTempLimit)
		return
	}
	temp := temperatureConvert(v)
	_ = aa.regWriteAll(ADDR_THERMAL_TRIP, temp)
}

// ReadRegsPipelined - read multiple registers in pipeline mode
// chipID = -1 means read all chips
// addrs must be sorted, -1 in results means timeout
func (aa *AuraAsic) ReadRegsPipelined(targets []uint8, addrs []uint8) (results []int64, err error) {
	readStub := func() (results []int64, err error) {
		length := len(addrs)
		if targets == nil {
			length *= len(aa.seqChipIds)
		} else {
			length *= len(targets)
		}
		results = make([]int64, length)
		for i := 0; i < length; i++ {
			results[i] = -1
		}
		return results, nil
	}
	isReadAll := false
	var newTargets []uint8
	faultyAsics := false
	newTargetIndexMap := make(map[int]int)

	if targets == nil {
		isReadAll = true
		targets = aa.seqChipIds
		for idx, target := range targets {
			if aa.isFaultyAsic(target) {
				// Comment: clear the bad asic marking asicFalseFaultyThresholdInMinutes after the last marking time
				if !aa.clearFaultyAsic(target) {
					faultyAsics = true
					continue
				}
			}
			if !aa.isAsicDetected(target) {
				continue
			}
			newTargetIndexMap[len(newTargets)] = idx
			newTargets = append(newTargets, target)
		}

	} else {
		for idx, target := range targets {
			if aa.isFaultyAsic(target) {
				// Comment: clear the bad asic marking asicFalseFaultyThresholdInMinutes after the last marking time
				if !aa.clearFaultyAsic(target) {
					faultyAsics = true
					continue
				}
			}
			newTargetIndexMap[len(newTargets)] = idx
			newTargets = append(newTargets, target)
		}
	}
	if faultyAsics && len(newTargets) == 0 {
		return readStub()
	}

	result, retData, _ := aa.asicIO.NonBlockingRead(newTargets, addrs, nil, CMD_READ)
	// wait on response channel
	asicReadError := result
	// If there is no read error from ASICs return the data
	if asicReadError == 0 && !faultyAsics && !isReadAll {
		return retData, nil
	}
	var res []int64
	if isReadAll {
		res = make([]int64, len(aa.seqChipIds)*len(addrs))
	} else {
		res = make([]int64, len(targets)*len(addrs))
	}

	for i := 0; i < len(res); i++ {
		res[i] = -1
	}
	// Now walk through the result find the faulty asics and reconstruct original response
	// payload
	for asicIdx := 0; asicIdx < len(newTargets); asicIdx++ {
		flag := false
		for regIdx := 0; regIdx < len(addrs); regIdx++ {
			index := asicIdx*len(addrs) + regIdx
			resIdx := newTargetIndexMap[asicIdx]
			res[resIdx*len(addrs)+regIdx] = retData[index]
			if retData[index] == -1 && !flag {
				flag = true
				aa.updateAsicFaultyTracker(newTargets[asicIdx])
			}
		}
	}
	return res, nil
}

func (aa *AuraAsic) ReadAllPipelined(addr uint8) (results []int64, err error) {
	return aa.ReadRegsPipelined(nil, []uint8{addr})
}

func (aa *AuraAsic) ReadAllTemperature() []float64 {
	const tempFaultMask = 0x50000 // These fault bits are inverted; 1 means no fault

	if time.Since(aa.lastTempReading) <= asicReadingDuration {
		return aa.cacheTemps
	}
	_ = aa.RegWrite(0, ADDR_TEMP_CFG, 0xd, true)
	results, err := aa.ReadAllPipelined(ADDR_TEMPERATURE)
	if err != nil {
		log.Errorf("ReadAllTemperature error %s", err)
	}
	if len(results) == 0 {
		return []float64{}
	}

	var temps []float64
	for i := 0; i < len(aa.seqChipIds); i++ {
		v := results[i]
		if v == -1 {
			temps = append(temps, -1000.0)
		} else {
			if v&tempFaultMask != tempFaultMask {
				log.Errorf("RegRead for asic %d returned a temperature fault. Raw value: 0x%08x",
					aa.seqChipIds[i], v)
			}
			temp := (float64(v&0xfff)-0.5)*tempY*(1.0/4096.0) + tempK
			temps = append(temps, temp)
			aa.ChipArray[i].Temperature = float32(temp)
		}
	}
	copy(aa.cacheTemps, temps)
	aa.lastTempReading = time.Now()
	return temps
}

func (aa *AuraAsic) ReadAllVoltage() []float64 {

	if !aa.initComplete {
		log.Errorf("ReadAllVoltage: ASICs not initialized; returning")
		return []float64{}
	}
	if time.Since(aa.lastVoltReading) <= asicReadingDuration {
		return aa.cacheVolts
	}

	_ = aa.RegWrite(0, ADDR_DVM_CFG, 0x2, true)
	delay(10)
	_ = aa.RegWrite(0, ADDR_DVM_CFG, 0x3, true)
	delay(10)
	_ = aa.RegWrite(0, ADDR_DVM_CFG, 0x1, true)
	delay(10)
	_ = aa.RegWrite(0, ADDR_DVM_CFG, 0x9|(5<<12), true) // channel 5
	delay(10)
	_ = aa.RegWrite(0, ADDR_DVM_CFG, 0x5, true)
	delay(10)

	results, err := aa.ReadAllPipelined(ADDR_VOLTAGE)
	if err != nil {
		log.Errorf("ReadAllVoltage error %s", err)
	}

	var volts []float64
	for i := 0; i < len(results); i++ {
		if len(results) != len(aa.ChipArray) {
			log.Errorf("ReadAllVoltage: results len %d does not match len of ChipArray %d", len(results), len(aa.ChipArray))
			return volts
		}
		if results[i] == -1 {
			volts = append(volts, -1)
		} else {
			v := results[i]
			ca := aa.ChipArray[i]
			if ca.VoltGain == 0 { // DVFS did not initialize gain & offset; use default values for diags
				ca.VoltGain = 0.00010467773
				ca.VoltOffset = -0.285892339
			}
			volts = append(volts, (float64(v&0xffff)*float64(ca.VoltGain))+float64(ca.VoltOffset))
			ca.Voltage = (float32(v&0xffff) * ca.VoltGain) + ca.VoltOffset
			//log.Infof("ReadAllVoltage: chip %d val 0x%04x volt %f voltGain %f voltOffset %f", ca.ChipId, v&0xffff, ca.Voltage, ca.VoltGain, ca.VoltOffset)
			//log.Infof("ReadAllVoltage: Voltage Result chip %d: %.3fV", chip.ChipId, aa.ChipArray[i].Voltage)
		}
	}

	copy(aa.cacheVolts, volts)
	aa.lastVoltReading = time.Now()
	voltTrace[aa.brdChainId-1].PushBack(volts)
	if voltTrace[aa.brdChainId-1].Len() > voltTraceLen {
		e := voltTrace[aa.brdChainId-1].Front()
		voltTrace[aa.brdChainId-1].Remove(e)
	}
	return volts
}

func (aa *AuraAsic) printTracedVoltages() {
	chain := aa.brdChainId - 1
	log.Infof("Traced voltages for board/chain %d:", chain+1)
	len := voltTrace[chain].Len()
	for i := 0; i < len; i++ {
		e := voltTrace[chain].Front()
		voltTrace[chain].Remove(e)
		log.Infof("t-%d: %v", len-i, e.Value)
	}
}

// Convert ADDR_PLL_FREQ reg to frequency in MHz
func regToFreq(reg uint32) float32 {
	return float32(reg) * refClk / float32(((div1 + 1) * (div2 + 1) * (1 << 20)))
}

func (aa *AuraAsic) ReadAllFrequency() []float64 {
	if time.Since(aa.lastFreqReading) <= asicReadingDuration {
		return aa.cacheFreqs
	}
	results, err := aa.ReadAllPipelined(ADDR_PLL_FREQ)
	if err != nil {
		log.Errorf("readAllFrequency error %s", err)
	}
	if len(results) != len(aa.ChipArray) {
		return []float64{}
	}
	var freqs []float64
	for i := 0; i < len(aa.ChipArray); i++ {
		if results[i] < 0 {
			freqs = append(freqs, -1.0)
		} else {
			v := regToFreq(uint32(results[i]))
			freqs = append(freqs, float64(v))
			aa.ChipArray[i].Frequency = float32(v)
		}
	}
	copy(aa.cacheFreqs, freqs)
	aa.lastFreqReading = time.Now()
	return freqs
}

func (aa *AuraAsic) setDutyCycleExtendAll() {
	// Use first detected asicId
	val, _ := aa.RegRead(aa.actualChipIds[0], ADDR_HASH_CONFIG)
	val |= (1 << 9)
	_ = aa.regWriteAll(ADDR_HASH_CONFIG, val)
}

func (aa *AuraAsic) SetFrequencyAll(freq float32) error {
	if freq < MinFreq || freq > MaxFreq {
		log.Errorf("SetFrequencyAll: frequency %.2f is out of valid range ", freq)
		return errors.New("function SetFrequencyAll: invalid frequency")
	}
	_ = aa.SetFrequency(-1, freq)
	for ii := 0; ii < len(aa.ChipArray); ii++ {
		aa.ChipArray[ii].Frequency = freq
	}
	return nil
}

func (aa *AuraAsic) pllInit() {
	tempVal := uint32((200 / refClk) * float32(((div1 + 1) * (div2 + 1) * (1 << 20))))
	_ = aa.RegWrite(0, ADDR_PLL_FREQ, tempVal, true)
	if true { // Treat all ASICs as ECO+ from now on
		var setting int
		setting = int((48000 / tempVal)) - 32
		if setting < 0 {
			setting = 0
		}
		if setting > 64 {
			setting = 64
		}
		_ = aa.RegWrite(0, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(0<<18), true)
		_ = aa.RegWrite(0, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(1<<18), true)
	}
	_ = aa.RegWrite(0, ADDR_PLL_CONFIG, 0x12, true)
	_ = aa.RegWrite(0, ADDR_PLL_CONFIG, 0x1d+(div2<<13)+(div1<<10)+(vcoSel<<8), true)
}

func (aa *AuraAsic) SetFrequency(asicId int, freq float32) error {

	var bcast bool

	if freq < MinFreq || freq > MaxFreq {
		log.Errorf("SetFrequency: frequency %.2f is out of valid range ", freq)
		return errors.New("function SetFrequency: invalid frequency")
	}

	if asicId == -1 {
		bcast = true
		asicId = 0
	}

	tempVal := uint32((freq / refClk) * float32(((div1 + 1) * (div2 + 1) * (1 << 20))))
	log.Infof("SetFrequency: chip %d frequency = %.02f, regVal = 0x%08x", asicId, freq, tempVal)

	_ = aa.RegWrite(uint8(asicId), ADDR_PLL_FREQ, tempVal, bcast)
	if true { // Treat all ASICs as ECO+ from now on
		var setting int
		setting = int((48000 / tempVal)) - 32
		if setting < 0 {
			setting = 0
		}
		if setting > 64 {
			setting = 64
		}
		_ = aa.RegWrite(uint8(asicId), ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(0<<18), bcast)
		_ = aa.RegWrite(uint8(asicId), ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(1<<18), bcast)
	}
	//_ = aa.RegWrite(uint8(asicId), ADDR_PLL_CONFIG, 0x12, bcast)
	_ = aa.RegWrite(uint8(asicId), ADDR_PLL_CONFIG, 0x1d+(div2<<13)+(div1<<10)+(vcoSel<<8), bcast)

	if !bcast {
		chipIdx := aa.ChipIdToIndex(uint8(asicId))
		aa.ChipArray[chipIdx].Frequency = freq
		log.Infof("SetFrequency: Frequency for chip %d is set to %.2f", asicId, freq)
	}
	return nil

}

func (aa *AuraAsic) clearResults() {
	_ = aa.regWriteAll(ADDR_HIT2, 0)
	_ = aa.regWriteAll(ADDR_HIT1, 0)
	_ = aa.regWriteAll(ADDR_HIT0, 0)
}

func delay(msec time.Duration) {
	time.Sleep(time.Millisecond * msec)
}

// isFaultyAsic checks if a given ASIC ID is flagged as faulty.
// Returns true if the ASIC is considered faulty, false otherwise.
func (aa *AuraAsic) isFaultyAsic(asicId uint8) bool {
	aa.faultyAsicTrackerLock.Lock()
	defer aa.faultyAsicTrackerLock.Unlock()
	if payload, ok := aa.faultyAsicsTrack[asicId]; ok {
		if payload.isFaulty {
			return true
		}
	}
	return false
}

func (aa *AuraAsic) clearFaultyAsic(asicId uint8) bool {
	aa.faultyAsicTrackerLock.Lock()
	defer aa.faultyAsicTrackerLock.Unlock()
	if payload, ok := aa.faultyAsicsTrack[asicId]; ok {
		if (aa.faultyAsicsTrack[asicId].falseBadCheck < asicFalseFaultyThresholdCount) &&
			(time.Since(aa.lastBadAsicMarking) > asicFalseFaultyThresholdInMinutes) {
			payload.isFaulty = false
			aa.faultyAsicsTrack[asicId].falseBadCheck++
			aa.faultyAsicsTrack[asicId].firstOccurrenceTime = time.Now()
			aa.faultyAsicsTrack[asicId].counts = 1
			aa.faultyAsicsTrack[asicId].isFaulty = false
			return true
		}
	}
	return false
}

// isAsicDetected checks if a given ASIC ID is detected.
func (aa *AuraAsic) isAsicDetected(asicId uint8) bool {
	for _, chipId := range aa.actualChipIds {
		if chipId == asicId {
			return true
		}
	}
	return false
}

// updateAsicFaultyTracker updates the faultyAsicTracker for a given ASIC ID.
// If the ASIC ID is not already being tracked, it initializes a new tracker.
// If the tracker exists, it updates the counts and determines if the ASIC is faulty.
func (aa *AuraAsic) updateAsicFaultyTracker(asicId uint8) {
	aa.faultyAsicTrackerLock.Lock()
	defer aa.faultyAsicTrackerLock.Unlock()

	if _, ok := aa.faultyAsicsTrack[asicId]; !ok {
		aa.faultyAsicsTrack[asicId] = &faultyAsicTracker{firstOccurrenceTime: time.Now(), counts: 1}
		return
	}
	asicTrack := aa.faultyAsicsTrack[asicId]
	if time.Since(asicTrack.firstOccurrenceTime) < asicFaultyThresholdInSeconds {
		asicTrack.counts++
		if asicTrack.counts == asicFaultyThresholdCount {
			asicTrack.isFaulty = true
			log.Errorf("Marking Board-%v ASIC %v", aa.brdChainId, asicId)
			aa.lastBadAsicMarking = time.Now()
		}
		aa.faultyAsicsTrack[asicId] = asicTrack
	} else {
		aa.faultyAsicsTrack[asicId] = &faultyAsicTracker{firstOccurrenceTime: time.Now(), counts: 1}
	}
}
