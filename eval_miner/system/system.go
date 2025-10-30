package system

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"eval_miner/device/devhdr"
	"eval_miner/log"
)

const (
	ControlBoard       string = "cb"
	HashBoard          string = "hb"
	TILevelSensorMfgID uint16 = 0x5449
)

// HashBoardInfo hash board's inventory information stored in hb eeprom
type HashBoardInfo struct {
	BoardName         string
	SerialNumber      string
	PartNumber        string
	BoardRevision     string
	ManufactureInfo   string
	HashBoardAsicInfo string
}

// ControlBoardInfo control board and chassis inventory information stored in cb eeprom
type ControlBoardInfo struct {
	HashBoardInfo
	MacAddress          string
	ChassisSerialNumber string
	ChassisModelNumber  string
	ChassisModelVersion string
	MinerOperatingMode  string
}

// SystemInformation miners unique identifiers about the chassis, control board and hash boards.
type SystemInformation struct {
	HashBoardCount   int
	ControlBoardInfo ControlBoardInfo
	HashBoardInfo    []HashBoardInfo
}

// Cached value of system information
var cachedSysinfo *SystemInformation
var sysInfo SystemInformation
var cfg *ControlBoardInfo

// getI2cEepromConfig returns eeprom i2cConfig
func getEepromInventory(brd string) *ControlBoardInfo {
	var devInfo ControlBoardInfo
	devInfo.BoardName = brd
	devInfo.SerialNumber = "000000001"
	devInfo.PartNumber = "000000001"
	devInfo.ManufactureInfo = "AURA"
	devInfo.BoardRevision = "1.0"
	if brd == ControlBoard {
		devInfo.ChassisSerialNumber = "000000001"
		devInfo.ChassisModelNumber = "AT1500"
		devInfo.ChassisModelVersion = "1.0"
		devInfo.MinerOperatingMode = ""
	} else if strings.Contains(brd, HashBoard) {
		devInfo.HashBoardAsicInfo = "AURA-ASIC"
	}
	log.Debugf(" brd:%v devInfo: %v\n", brd, devInfo)
	return &devInfo
}

// GetSystemInfo returns various miners identifiers.
// for e.x. returns model, serial, part, manufacturing and board revision information
func GetSystemInfo() (*SystemInformation, error) {
	// read the chassisConfiguration
	if ok := devhdr.ReadChassisConfiguration(); ok != nil {
		log.Errorf("Failed to read chassis configuration, %v", ok)
		os.Exit(-1)
	}

	// send the copy of cached system information
	if cachedSysinfo != nil {
		sysInfo = *cachedSysinfo
		return &sysInfo, nil
	}

	sysInfo.HashBoardCount = devhdr.MaxHashBoards
	// Open CB eeprom device
	if cfg = getEepromInventory(ControlBoard); cfg == nil {
		log.Errorf("Failed to get eeprom config for %v", ControlBoard)
		return nil, fmt.Errorf("failed to get eeprom config for %v", ControlBoard)
	}
	sysInfo.ControlBoardInfo = *cfg

	for idx := 1; idx <= sysInfo.HashBoardCount; idx++ {
		hbInfo := HashBoardInfo{BoardName: fmt.Sprintf("%s%d", HashBoard, idx)}
		hbInfo.SerialNumber = ""
		hbInfo.PartNumber = ""
		hbInfo.BoardRevision = ""
		hbInfo.ManufactureInfo = ""
		//}
		sysInfo.HashBoardInfo = append(sysInfo.HashBoardInfo, hbInfo)
	}
	log.Debugf("sysInfo: %+v", sysInfo)
	cachedSysinfo = new(SystemInformation)
	*cachedSysinfo = sysInfo
	if !reflect.DeepEqual(*cachedSysinfo, sysInfo) {
		log.Infof("deepCopy of sysInfo failed: src: %+v dst: %+v", sysInfo, *cachedSysinfo)
		cachedSysinfo = nil
	}
	return &sysInfo, nil
}
