package devhdr

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"eval_miner/log"
)

const (
	ChassisConfigFile      string = "chassisconfig.json"
	TeraFluxAirCooledAt15x string = "AT1500"
	TeraFluxFamilyAT15x    string = "at1x"
	TeraFluxEvalSystem     string = "EV1500"
)

var ChassisConfigOnce sync.Once
var ChassisCfg = &ChassisConfig{
	Chassis:        "",
	Family:         "",
	Hashboardcount: 3,
	Chaincount:     3,
}
var HashboardInfo map[uint]*Hb
var MinerMaxLimit MaxLimit
var MinerFansEnabled bool = true // Default to true, just in case

type Thermaltrip struct {
	Gpio  string `json:"gpio,omitempty"`
	Pin   int    `json:"pin,omitempty"`
	Value int    `json:"value,omitempty"`
}

type Presence struct {
	Gpio  string `json:"gpio,omitempty"`
	Pin   int    `json:"pin,omitempty"`
	Value int    `json:"value,omitempty"`
}

type Writeprotect struct {
	Gpio  string `json:"gpio,omitempty"`
	Pin   int    `json:"pin,omitempty"`
	Value int    `json:"value,omitempty"`
}

type Rev0 struct {
	Gpio  string `json:"gpio,omitempty"`
	Pin   int    `json:"pin,omitempty"`
	Value int    `json:"value,omitempty"`
}

type Rev1 struct {
	Gpio  string `json:"gpio,omitempty"`
	Pin   int    `json:"pin,omitempty"`
	Value int    `json:"value,omitempty"`
}

type Reset struct {
	Gpio  string `json:"gpio,omitempty"`
	Pin   int    `json:"pin,omitempty"`
	Value int    `json:"value,omitempty"`
}

type Power struct {
	Gpio  string `json:"gpio,omitempty"`
	Pin   int    `json:"pin,omitempty"`
	Value int    `json:"value,omitempty"`
}

type Gpio struct {
	Thermaltrip  Thermaltrip  `json:"thermaltrip,omitempty"`
	Presence     Presence     `json:"presence,omitempty"`
	Writeprotect Writeprotect `json:"writeprotect,omitempty"`
	Rev0         Rev0         `json:"rev0,omitempty"`
	Rev1         Rev1         `json:"rev1,omitempty"`
	Reset        Reset        `json:"reset,omitempty"`
	Power        Power        `json:"power,omitempty"`
}

type Hb struct {
	Slot     uint   `json:"slot,omitempty"`
	Chain    uint   `json:"chain,omitempty"`
	Board    uint   `json:"board,omitempty"`
	Uartname string `json:"uartname,omitempty"`
	Gpio     Gpio   `json:"gpio,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type MaxLimit struct {
	MaxTHs              float32 `json:"maxths,omitempty"`
	MaxPower            float32 `json:"maxpower,omitempty"`
	MaxAsicsInHashboard uint    `json:"maxasicsinhashboard,omitempty"`
	MaxAsicsInChain     uint    `json:"maxasicsinchain,omitempty"`
	MinPowerSoft        uint    `json:"minpowersoft,omitempty"`
	MaxPowerSoft        uint    `json:"maxpowersoft,omitempty"`
}

// This is the debug struct for the chassisconfig.json file
// NO PRODUCTION CODE SHOULD USE THIS
type Debug struct {
	DisableDvfs        bool  `json:"disabledvfs,omitempty"`
	DisablePsu         bool  `json:"disablepsu,omitempty"`
	IsWrongAsicMapping bool  `json:"wrongasicmapping,omitempty"`
	AsicFrequency      int   `json:"asicfrequency,omitempty"`
	VoltStepFactor     int   `json:"voltstepfactor,omitempty"`
	FreqStepFactor     int   `json:"freqstepfactor,omitempty"`
	FreqDelay          int   `json:"freqdelay,omitempty"`
	JobLog             bool  `json:"joblog,omitempty"`
	UartStressTest     bool  `json:"uartstresstest,omitempty"`
	AsicReadFailures1  []int `json:"asicreadfailures-1,omitempty"`
	AsicReadFailures2  []int `json:"asicreadfailures-2,omitempty"`
	AsicReadFailures3  []int `json:"asicreadfailures-3,omitempty"`
}

type ChassisConfig struct {
	Chassis        string              `json:"chassis,omitempty"`
	Family         string              `json:"family,omitempty"`
	Hashboardcount uint                `json:"hashboardcount,omitempty"`
	Chaincount     uint                `json:"chaincount,omitempty"`
	HbPowerSupport bool                `json:"hbpowersupport,omitempty"`
	FanSupport     map[string]bool     `json:"fansupport,omitempty"`
	Hbs            map[string][]Hb     `json:"hbs,omitempty"`
	MaxLimit       map[string]MaxLimit `json:"maxlimit,omitempty"`
	Debug          Debug               `json:"debug,omitempty"`
}

func ReadChassisConfiguration() error {
	ChassisConfigOnce.Do(func() {
		if err := readChassisConfiguration(); err != nil {
			log.Errorf("Failed to read chassis configuration, %v", err)
			os.Exit(-1)
		}
	})
	return nil
}

func readChassisConfiguration() error {
	// Open the json config file
	jsonFile, err := os.Open(os.Getenv("GC_FACTORY_DIR") + "/" + ChassisConfigFile)
	if err != nil {
		log.Errorf("failed to open chassisConfig %v", err)
		return err
	}
	defer jsonFile.Close()
	byteValue, _ := io.ReadAll(jsonFile)

	var c ChassisConfig
	if err := json.Unmarshal(byteValue, &c); err != nil {
		log.Errorf("failed to unmarshall chassisConfig error %v", err)
		return err
	}
	ChassisCfg = &c
	HashboardInfo = make(map[uint]*Hb)
	for slot := uint(1); slot <= MaxHashBoards; slot++ {
		hbs := c.Hbs[fmt.Sprintf("hb%d", slot)]
		for chain := uint(0); chain < c.Chaincount; chain++ {
			hb := &hbs[chain]
			HashboardInfo[hb.Board] = hb
		}
	}
	for _, v := range HashboardInfo {
		log.Debugf("hb %+v", v)
	}
	log.Debugf("Max Limit %+v", ChassisCfg.MaxLimit)
	log.Debugf("chassisConfig %+v", ChassisCfg)
	return nil
}

// GetUartNameFromIds finds the uartName from chassis config, using
// brd and chain Ids.
func GetUartNameFromIds(brd, chn uint32) string {
	hbs := ChassisCfg.Hbs
	hb := hbs[fmt.Sprintf("hb%d", brd)]
	chain := hb[chn]
	if chain.Chain != uint(chn) {
		log.Errorf("Chain Id didn't match!!! actual %v expected %v", chain.Chain, chn)
		return ""
	}
	return fmt.Sprintf("/dev/%v", chain.Uartname)
}

// GetHashBoardChainCount returns the hashboard  count
func GetHashBoardCount() uint32 {
	return 3
}

// GetHashBoardChainCount return the hashboard's chain count
func GetHashBoardChainCount() uint32 {
	return 1
}

// GetMaxLimit return the MaxLimit
func GetMaxLimit() MaxLimit {
	return MaxLimit{
		MaxTHs:              150,
		MaxPower:            5000,
		MaxAsicsInHashboard: 250,
		MaxAsicsInChain:     250,
		MinPowerSoft:        1000,
		MaxPowerSoft:        5000,
	}
}

// GetTotalChainCount returns the total board  count
func GetTotalChainCount() uint32 {
	return 3
}

// GetHashBoardInfo returns the hashboard information for the given slot
func GetHashBoardInfo(slot uint32) []Hb {
	if slot > GetHashBoardCount() {
		log.Errorf("Invalid board Id %v", slot)
		return nil
	}
	hbs := ChassisCfg.Hbs
	return hbs[fmt.Sprintf("hb%d", slot)]
}

// GetThermalTripSysfsValue returns the sysfs thermal trip gpio for a given
// board
func GetThermalTripSysfsValue(brdChainId uint) int {
	return HashboardInfo[brdChainId].Gpio.Thermaltrip.Value
}

// GetThermalTripPinValue returns the thermal trips GPIO PIN for a given
// board
func GetThermalTripPinValue(brdChainId uint) int {
	return HashboardInfo[brdChainId].Gpio.Thermaltrip.Pin
}

// GetHashBoardResetSysfsValue returns the sysfs gpio reset for a given
// board
func GetHashBoardResetSysfsValue(brdChainId uint) int {
	return HashboardInfo[brdChainId].Gpio.Reset.Value
}

// GetHashBoardPresenceSysfsValue returns the sysfs gpio presence for a given
// board
func GetHashBoardPresenceSysfsValue(brdId uint) int {
	return HashboardInfo[brdId].Gpio.Presence.Value
}

// GetHashBoardWriteProtectSysfsValue returns the sysfs gpio write protect for a given
// board
func GetHashBoardWriteProtectSysfsValue(brdId uint) int {
	return HashboardInfo[brdId].Gpio.Writeprotect.Value
}

// GetHashBoardPowerSysfsValue returns the sysfs gpio write protect for a given
// board
func GetHashBoardPowerSysfsValue(brdId uint) int {
	return HashboardInfo[brdId].Gpio.Power.Value
}

// GetHashBoardPowerSupport return the miner have the ability to support power
// ON/OFF the hashboards through GPIO pins
func GetHashBoardPowerSupport() bool {
	return ChassisCfg.HbPowerSupport
}

// GetMinerFanSupport return whether miner has fans to cool down the
// system
func GetMinerFanSupport() bool {
	return true
}

// GetHashBoardSlotId returns the miners hashboard slotId from hbChainId
func GetHashBoardSlotId(boardChainId uint) uint {
	return boardChainId
}

// GetMaxAsicsInHashboard returns the max asics in a hashboard
func GetMaxAsicsInHashboard() uint {
	return MinerMaxLimit.MaxAsicsInHashboard
}

// GetMaxAsicsInChain returns the max asics in a chain
func GetMaxAsicsInChain() uint {
	return MinerMaxLimit.MaxAsicsInChain
}

// GetChassisModelNumber returns the chassis name
func GetChassisModelNumber() string {
	return TeraFluxEvalSystem
}

// IsDvfsDisabled returns true if the dvfs is disabled
func IsDvfsDisabled() bool {
	return false
}

// IsPsuDisabled returns true if the psu is disabled
func IsPsuDisabled() bool {
	return false
}

// IsJobLogEnabled returns true if the psu is disabled
func IsJobLogEnabled() bool {
	return false
}

// IsUartStressTestEnabled returns true if the uart stress test is enabled
func IsUartStressTestEnabled() bool {
	return false
}

// GetBoardChainIdFromSlotAndChipId return the boardChainId
// here is the formula to find the boardChainId for the given
// slot and chipId
// for e.x. Let hash board have max of 2 chains, each chain has
// 250 asics. Here is how mapping goes
// Slot 1 = [
//
//	{
//	   boardChain=1,
//	   asicRange=0-249
//	},
//
//	{
//	   boardChain=2,
//	   asicRange=250-499
//	},
//
// ]
// Slot 2 = [
//
//	{
//	   boardChain=3,
//	   asicRange=0-249
//	},
//
//	{
//	   boardChain=4,
//	   asicRange=250-499
//	},
//
// ]
// Slot 3 = [
//
//	{
//	   boardChain=5,
//	   asicRange=0-249
//	},
//
//	{
//	   boardChain=6,
//	   asicRange=250-499
//	},
//
// ]
func GetBoardChainIdFromSlotAndChipId(slotId, asicId uint) (boardChainId uint) {
	boardChainId = 0
	for k, v := range HashboardInfo {
		if v.Slot == slotId {
			boardChainId = k
			break
		}
	}
	if asicId >= MinerMaxLimit.MaxAsicsInChain*ChassisCfg.Chaincount {
		log.Errorf("asicId is invalid %v for slot %v maxasicsInchain %v maxasicInHb %v",
			asicId, slotId, MinerMaxLimit.MaxAsicsInChain, MinerMaxLimit.MaxAsicsInHashboard)
		return boardChainId
	}
	return boardChainId + asicId/MinerMaxLimit.MaxAsicsInChain
}

func GetInitialAsicFrequency() float32 {
	return 200.0
}

func GetVoltStepFactor() float32 {
	return 100.0
}

func GetFreqStepFactor() int {
	return 1
}

func GetFreqDelay() int {
	return 0
}

func SetFansEnabled(miner string) {
	if v, ok := ChassisCfg.FanSupport[miner]; ok {
		MinerFansEnabled = v
	} else {
		MinerFansEnabled = true // Default to true
	}
	log.Infof("SetFansEnabled(%s): MinerFansEnabled: %v", miner, MinerFansEnabled)
}

func SetMinerMaxLimits(miner string) {
	MinerMaxLimit = ChassisCfg.MaxLimit[miner]
	switch miner {
	case TeraFluxAirCooledAt15x:
		ChassisCfg.Chassis = miner
	default:
		log.Errorf("SetMinerMaxLimits(): Invalid miner %v", miner)
	}
}

// IsTeraFluxFirstGenerationMiners checks chassis family field to determine
// miners family
func IsTeraFluxFirstGenerationMiners() bool {
	return true
}

// GetAsicsReadFailures returns the list of ASICs that induces read failures
func GetAsicsReadFailures(slotId, chainId uint) []uint8 {
	var res []uint8
	chain := chainId % ChassisCfg.Chaincount
	switch slotId {
	case 1:
		for _, val := range ChassisCfg.Debug.AsicReadFailures1 {
			floor := chain * MinerMaxLimit.MaxAsicsInChain
			if uint(val) >= floor && uint(val) <= (chain+1)*MinerMaxLimit.MaxAsicsInChain {
				res = append(res, uint8(uint(val)-floor))
			}
		}
	case 2:
		for _, val := range ChassisCfg.Debug.AsicReadFailures2 {
			floor := chain * MinerMaxLimit.MaxAsicsInChain
			if uint(val) >= floor && uint(val) <= (chain+1)*MinerMaxLimit.MaxAsicsInChain {
				res = append(res, uint8(uint(val)-floor))
			}
		}
	case 3:
		for _, val := range ChassisCfg.Debug.AsicReadFailures3 {
			floor := chain * MinerMaxLimit.MaxAsicsInChain
			if uint(val) >= floor && uint(val) <= (chain+1)*MinerMaxLimit.MaxAsicsInChain {
				res = append(res, uint8(uint(val)-floor))
			}
		}
	}
	return res
}
