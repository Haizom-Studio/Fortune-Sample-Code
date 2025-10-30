package psu

import (
	"container/list"
	"errors"
	"math"
	"sync"
	"time"

	"eval_miner/device/devhdr"
	"eval_miner/device/smbus"
	"eval_miner/log"
)

const psuStrLen = 16
const psuBus = 5

const bocoPsuAddr = 0x10
const aaPsuAddr = 0x58
const bocoV2PsuAddr = 0x57
const vendorPsuAddr = 0x68

var (
	MinerVoutMin  float32 = 11.0
	MinerVoutMax  float32 = 15.0
	MinerPowerMax float32
	singleInput   bool
	psuRetries    int = 2
)

var (
	MaraMinerVinMin   float32 = 240.0
	MaraMinerVinMax   float32 = 260.0
	MaraMinerVoutMin  float32 = 18.0
	MaraMinerVoutMax  float32 = 22.0
	MaraMinerPowerMax float32 = 15000.0
	MaraMinerTempMax  float32 = 80.0
)

const PSU_MONITOR_INTERVAL_MS = 1000
const singleInputMaxPower = 3400.0 // Watts

var psuMutex sync.Mutex
var psuAddr = uint8(bocoPsuAddr)

var (
	poweredOn         bool = false
	poweredOnMu       sync.Mutex
	psuAlarm          bool  = false // PSU tripped
	vIn2Alarm         bool  = false // 2nd BOCO input is low or not there
	vOutStatus              = uint8(0)
	vOutAlarm         bool  = false
	iOutStatus              = uint8(0)
	iOutAlarm         bool  = false
	inputStatus             = uint8(0)
	inputAlarm        bool  = false
	temperatureStatus       = uint8(0)
	temperatureAlarm  bool  = false
	fanStatus               = uint8(0)
	fanAlarm          bool  = false
	oldSleepReg       uint8 = 0xff
)

var PreInitDone bool
var PreInitError error

const (
	maraPsuVersion   uint8 = 0x00
	cmdOn            uint8 = 0x01
	cmdClearFaults   uint8 = 0x03
	maraErrorCode    uint8 = 0x05 // Mara only
	cmdWatchDog      uint8 = 0x07
	cmdVoutMode      uint8 = 0x20
	cmdVout          uint8 = 0x21
	maraVoutA        uint8 = 0x22
	maraVoutB        uint8 = 0x23
	cmdFan           uint8 = 0x3b
	cmdVoutStat      uint8 = 0x7a
	cmdIoutStat      uint8 = 0x7b
	cmdInputStat     uint8 = 0x7c
	cmdTempStat      uint8 = 0x7d
	cmdFanStat       uint8 = 0x81
	cmdReadVin       uint8 = 0x88
	cmdReadIin       uint8 = 0x89
	cmdReadVout      uint8 = 0x8b
	cmdReadIout      uint8 = 0x8c
	cmdReadTemp1     uint8 = 0x8d
	cmdReadTemp2     uint8 = 0x8e
	cmdReadTemp3     uint8 = 0x8f
	cmdReadFan1      uint8 = 0x90
	cmdReadFan2      uint8 = 0x91
	maraCmdReadVoutb uint8 = 0x94 // Mara only
	maraCmdReadVaux  uint8 = 0x95 // Mara only
	cmdPowerOut      uint8 = 0x96
	cmdPowerIn       uint8 = 0x97
	cmdMfrId         uint8 = 0x99
	cmdMfrModel      uint8 = 0x9a
	cmdMfrRev        uint8 = 0x9b
	cmdMfrLoc        uint8 = 0x9c
	cmdMfrDate       uint8 = 0x9d
	cmdMfrSerial     uint8 = 0x9e
	cmdVinMin        uint8 = 0xa0
	cmdVinMax        uint8 = 0xa1
	maraFwVer        uint8 = 0xa3 // Mara only
	cmdVoutMin       uint8 = 0xa4
	cmdVoutMax       uint8 = 0xa5
	cmdIoutMax       uint8 = 0xa6
	cmdPoutMax       uint8 = 0xa7
	cmdMaxTemp1      uint8 = 0xc0
	cmdMaxTemp2      uint8 = 0xc1
	cmdMaxTemp3      uint8 = 0xc2
	cmdPriFwRev      uint8 = 0xdb
	cmdSecFwRev      uint8 = 0xdc
	cmdReadVin2      uint8 = 0xe0
	cmdReadIin2      uint8 = 0xe1
	cmdReadVin1      uint8 = 0xe1 // AA only
	cmdPowerIn2      uint8 = 0xe2 // BOCO only
	cmdReadVin2a     uint8 = 0xe2 // AA only
	cmdReadIinA      uint8 = 0xe3 // AA only
	cmdReadIinB      uint8 = 0xe4 // AA only
	cmdReadIin2a     uint8 = 0xe5 // AA only
	cmdMajorFwVer    uint8 = 0xe6 // AA only
	cmdReadTemp4     uint8 = 0xe6 // BOCO only
	cmdCoolingSel    uint8 = 0xe7 // BOCO only
	cmdFanEnable     uint8 = 0xe7 // AA only
	cmdSleep         uint8 = 0xea // BOCO only
)

type psuData struct {
	PsuOn             bool
	VoutSet           float32
	VoutSetA          float32 // mara only
	VoutSetB          float32 // mara only
	FanSpeedSet       uint16
	VoutStatus        uint8
	IoutStatus        uint8
	InputStatus       uint8
	TemperatureStatus uint8
	FanStatus         uint8
	Vin               float32
	Iin               float32
	Vin2              float32
	Iin2              float32 // BOCO only
	Vin1              float32 // AA only
	Vin2a             float32 // AA only
	IinA              float32 // AA only
	IinB              float32 // AA only
	Iin2a             float32 // AA only
	Vout              float32
	Voutb             float32 // Mara only
	Vaux              float32 // Mara only
	Iout              float32
	Temp1             float32
	Temp2             float32
	Temp3             float32
	FanSpeed1         float32
	FanSpeed2         float32
	PowerOut          float32
	PowerIn           float32
	PowerIn2          float32 // BOCO only
	MfrId             []byte
	MfrModel          []byte
	MfrRevision       []byte
	MfrLocation       []byte
	MfrDate           []byte
	MfrSerial         []byte
	VinMin            float32
	VinMax            float32
	VoutMin           float32
	VoutMax           float32
	IoutMax           float32
	PoutMax           float32
	MaxTemp1          float32
	MaxTemp2          float32
	MaxTemp3          float32
	PriFwRev          []byte
	SecFwRev          []byte
	AutoUpgradeSet    bool
	MajorFwVer        uint8   // AA
	Temp4             float32 // BOCO
	CoolingSelection  uint8   // BOCO
	LowPowerMode      bool
	ErrorCode         uint16
	SingleInputMode   bool
}

var psuTrace *list.List // Trace queue
const psuTraceLen = 30

var ( // Fixed fields
	MfrId       []byte
	MfrModel    []byte
	MfrRevision []byte
	MfrLocation []byte
	MfrDate     []byte
	MfrSerial   []byte
	VinMin      float32
	VinMax      float32
	VoutMin     float32
	VoutMax     float32
	IoutMax     float32
	PoutMax     float32
	MaxTemp1    float32
	MaxTemp2    float32
	MaxTemp3    float32
	PriFwRev    []byte
	SecFwRev    []byte
	MajorFwVer  uint8 // AA
)

func Linear11(word uint16) float32 {

	var exp int
	temp := word >> 11

	if (temp & 0x10) != 0 { // Negative exponent bitfield
		temp |= 0xffe0
		exp = -int((temp ^ 0xffff) + 1)
	} else {
		exp = int(temp)
	}
	value := word & 0x7ff
	var t float32 = float32(value)
	if value&0x400 != 0 { // Negative value, probably a temperature
		t = float32(0x800-word) * -1
	}
	return t * float32(math.Pow(2, float64(exp)))
}

func Linear16(word uint16) float32 {

	if (psuAddr == aaPsuAddr) || (psuAddr == vendorPsuAddr) {
		return float32(word) / 512.0
	} else {
		return float32(word) / 128.0
	}
}

func ReverseLinear16(v float32) uint16 {

	var vFactor float32
	if (psuAddr == aaPsuAddr) || (psuAddr == vendorPsuAddr) {
		vFactor = 512.0
	} else {
		vFactor = 128.0
	}

	return uint16(v * vFactor)
}

// Write an arbitrary command to the PSU. Be careful, this can brick the PSU!
// Only use this command as the last resort to support unimplemented commands.
func Write(cmd, val uint8) error {
	return psuWriteReg(psuAddr, cmd, val)
}

// Read an arbitrary command from the PSU.
func Read(cmd uint8) (uint8, error) {
	return psuReadReg(psuAddr, cmd)
}

// Read an arbitrary command from the PSU.
func ReadWord(cmd uint8) (uint16, error) {
	v, err := psuReadWord(psuAddr, cmd)
	return v, err
}

// GetVoutRange returns the advertised min/max vout.
func GetVoutRange() (float32, float32) {

	if psuAddr == 0 {
		return MinerVoutMin, MinerVoutMax
	}

	psuDev, err := psuGet()
	if err != nil {
		log.Errorf("PSU ERROR: smbus.Open() for bus %d device 0x%2d returned %s", psuBus, psuAddr, err)
		psuMutex.Unlock()
		return MinerVoutMin, MinerVoutMax
	}
	defer psuFree(psuDev)
	if psuAddr == uint8(vendorPsuAddr) {
		MinerVoutMax = MaraMinerVoutMax
		MinerVoutMin = MaraMinerVoutMin
	}
	log.Infof("PSU INFO: Vout range %.3f - %.3f", MinerVoutMin, MinerVoutMax)
	return MinerVoutMin, MinerVoutMax
}

// Wrappers for PSU commands to allow for retries
func psuWriteReg(addr, reg, v uint8) error {
	var err error
	if psuAddr == 0 {
		return errors.New("no PSU detected")
	}

	psuDev, err := psuGet()
	if err != nil {
		log.Errorf("PSU ERROR: smbus.Open() for bus %d device 0x%2d returned %s", psuBus, psuAddr, err)
		psuMutex.Unlock()
		return errors.New("smbus.Open() failed")
	}
	defer psuFree(psuDev)
	for i := 0; i < psuRetries; i++ {
		err = psuDev.WriteReg(addr, reg, v)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
		log.Debugf("PSU psuWriteReg retry %d", i+1)
	}
	return err
}

func psuWriteWord(addr, reg uint8, v uint16) error {
	var err error
	if psuAddr == 0 {
		return errors.New("no PSU detected")
	}

	psuDev, err := psuGet()
	if err != nil {
		log.Errorf("PSU ERROR: smbus.Open() for bus %d device 0x%2d returned %s", psuBus, psuAddr, err)
		psuMutex.Unlock()
		return errors.New("smbus.Open() failed")
	}
	defer psuFree(psuDev)
	for i := 0; i < psuRetries; i++ {
		err = psuDev.WriteWord(addr, reg, v)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
		log.Debugf("PSU psuWriteWord retry %d", i+1)
	}
	return err
}

func psuWriteByte(reg uint8) error {
	if psuAddr == 0 {
		return errors.New("no PSU detected")
	}

	psuDev, err := psuGet()
	if err != nil {
		log.Errorf("PSU ERROR: smbus.Open() for bus %d device 0x%2d returned %s", psuBus, psuAddr, err)
		psuMutex.Unlock()
		return errors.New("smbus.Open() failed")
	}
	defer psuFree(psuDev)
	for i := 0; i < psuRetries; i++ {
		_, err = psuDev.WriteByte(reg, 0)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
		log.Debugf("PSU psuWriteByte retry %d", i+1)
	}
	return err
}

func psuReadReg(addr, reg uint8) (uint8, error) {
	var v uint8
	if psuAddr == 0 {
		return 0, errors.New("no PSU detected")
	}

	psuDev, err := psuGet()
	if err != nil {
		log.Errorf("PSU ERROR: smbus.Open() for bus %d device 0x%2d returned %s", psuBus, psuAddr, err)
		psuMutex.Unlock()
		return 0, errors.New("smbus.Open() failed")
	}
	defer psuFree(psuDev)
	for i := 0; i < psuRetries; i++ {
		v, err = psuDev.ReadReg(addr, reg)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
		log.Debugf("PSU psuReadReg retry %d", i+1)
	}
	return v, err
}

func swapByte(in uint16) uint16 {
	return (in&0xff)<<8 | (in&0xff00)>>8
}

func swapByteSlice(b []byte) []byte {
	for i := 0; i < len(b); i += 2 {
		b[i], b[i+1] = b[i+1], b[i]
	}
	return b
}

func psuReadWord(addr, reg uint8) (uint16, error) {
	var v uint16
	if psuAddr == 0 {
		return 0, errors.New("no PSU detected")
	}

	psuDev, err := psuGet()
	if err != nil {
		log.Errorf("PSU ERROR: smbus.Open() for bus %d device 0x%2d returned %s", psuBus, psuAddr, err)
		psuMutex.Unlock()
		return 0, errors.New("smbus.Open() failed")
	}
	defer psuFree(psuDev)
	for i := 0; i < psuRetries; i++ {
		v, err = psuDev.ReadWord(addr, reg)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
		log.Debugf("PSU psuReadWord retry %d", i+1)
	}
	return v, err
}

func psuReadN(addr, reg uint8, n int) ([]byte, error) {
	var buf []byte
	if psuAddr == 0 {
		return nil, errors.New("no PSU detected")
	}

	psuDev, err := psuGet()
	if err != nil {
		log.Errorf("PSU ERROR: smbus.Open() for bus %d device 0x%2d returned %s", psuBus, psuAddr, err)
		psuMutex.Unlock()
		return nil, errors.New("smbus.Open() failed")
	}
	defer psuFree(psuDev)
	for i := 0; i < psuRetries; i++ {
		buf, err = psuDev.ReadN(addr, reg, n)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
		log.Debugf("PSU psuReadN retry %d", i+1)
	}
	return buf, err
}

func psuReadBlockData(addr, reg uint8) ([]byte, error) {
	if psuAddr == 0 {
		return nil, errors.New("no PSU detected")
	}

	psuDev, err := psuGet()
	if err != nil {
		log.Errorf("PSU ERROR: smbus.Open() for bus %d device 0x%2d returned %s", psuBus, psuAddr, err)
		psuMutex.Unlock()
		return nil, errors.New("smbus.Open() failed")
	}
	defer psuFree(psuDev)
	var buf []byte
	for i := 0; i < psuRetries; i++ {
		buf, err = psuDev.ReadBlockData(addr, reg)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
		log.Debugf("PSU psuReadBlock retry %d", i+1)
	}
	return buf, err
}

// PsuOn command (typically mapped to command 0x1)
func PsuOn() {

	poweredOnMu.Lock()
	defer poweredOnMu.Unlock()

	log.Info("Turning on PSU")
	if psuAddr == uint8(bocoV2PsuAddr) {
		ok := psuWriteReg(psuAddr, cmdWatchDog, 0)
		if ok != nil {
			log.Errorf("Error writing to PSU WD command %v", ok)
			return
		}
	}
	err := psuWriteReg(psuAddr, cmdOn, 0x80)
	if err != nil {
		log.Errorf("PSU ERROR: PSU On returned %s", err)
		return
	}

	// cached value guaranteed to be in sync w/ actual PSU state
	poweredOn = true
}

// PsuOff command (typically mapped to command 0x1)
func PsuOff() {

	poweredOnMu.Lock()
	defer poweredOnMu.Unlock()

	err := psuWriteReg(psuAddr, cmdOn, 0x00)

	if err != nil {
		log.Errorf("PSU ERROR: PSU Off returned %s", err)
		return
	}

	// cached value guaranteed to be in sync w/ actual PSU state
	poweredOn = false
}

func getSleepReg() bool {

	// Exceptions, some PSUs do not implement the full feature set
	if psuAddr == uint8(vendorPsuAddr) {
		log.Errorf("PSU ERROR: vendor PSU does not support sleep mode")
		return false
	}

	val, err := psuReadReg(psuAddr, cmdSleep)
	if err != nil {
		log.Errorf("PSU ERROR: PSU Sleep returned %s", err)
	}
	if oldSleepReg != val { // Only log this when the value changes
		oldSleepReg = val
		log.Infof("PSU Sleep = 0x%02x", val)
	}
	return val == 0x1
}

// set sleep bit to enable low power mode
func setSleepReg(on bool) {

	var val uint8

	// Exceptions, some PSUs do not implement the full feature set
	if psuAddr == uint8(vendorPsuAddr) {
		log.Errorf("PSU ERROR: vendor PSU does not support sleep mode")
		return
	}

	if on {
		val = 0x01
	} else {
		val = 0x00
	}
	err := psuWriteReg(psuAddr, cmdSleep, val)
	if err != nil {
		log.Errorf("PSU ERROR: PSU Sleep 0x%02x returned %s", val, err)
	}

}

// ClearFaults sets the PSU fault registers to 0
func ClearFaults() {

	err := psuWriteByte(cmdClearFaults) // Just send command byte, no data
	if err != nil {
		log.Errorf("PSU ERROR: PSU WriteByte returned %s", err)
	}
	if psuAddr != uint8(vendorPsuAddr) {
		err = psuWriteReg(psuAddr, cmdIoutStat, 0x00)
		if err != nil {
			log.Errorf("PSU ERROR: PSU WriteReg returned %s", err)
		}
		err = psuWriteReg(psuAddr, cmdVoutStat, 0x00)
		if err != nil {
			log.Errorf("PSU ERROR: PSU WriteReg returned %s", err)
		}
		err = psuWriteReg(psuAddr, cmdInputStat, 0x00)
		if err != nil {
			log.Errorf("PSU ERROR: PSU WriteReg returned %s", err)
		}
		err = psuWriteReg(psuAddr, cmdTempStat, 0x00)
		if err != nil {
			log.Errorf("PSU ERROR: PSU WriteReg returned %s", err)
		}
		err = psuWriteReg(psuAddr, cmdFanStat, 0x00)
		if err != nil {
			log.Errorf("PSU ERROR: PSU WriteReg returned %s", err)
		}
	}

}

// GetVoltage returns the PSU DC voltage setting.
func GetVoltage() float32 {

	if psuAddr == 0 {
		return -1.0
	}

	tempReg2, err := psuReadWord(psuAddr, cmdVout)
	if err != nil {
		log.Errorf("PSU ERROR: psuReadWord returned %s", err)
		return -1.0
	}

	return Linear16(tempReg2)
}

// SetVoltage sets the PSU DC voltage. Note: actual (measured) voltage might be significantly
// different from this setting, especially when there is no load across the terminals.
func SetVoltage(v float32) error {

	if psuAddr == 0 {
		return errors.New("no PSU detected")
	}

	min, max := MinerVoutMin, MinerVoutMax
	if psuAddr == uint8(vendorPsuAddr) {
		min, max = MaraMinerVoutMin, MaraMinerVoutMax
	}

	if v > max {
		log.Infof("PSU ERROR: SetVoltage() %.3fV out of range, clamp to %.3f", v, max)
		v = max
	}
	if v < min {
		log.Infof("PSU ERROR: SetVoltage() %.3fV out of range, clamp to %.3f", v, min)
		v = min
	}

	tempReg2, err := psuReadWord(psuAddr, cmdVout)
	if err != nil {
		log.Errorf("PSU ERROR: psuReadWord returned %s", err)
	}
	if tempReg2 == uint16(ReverseLinear16(v)) {
		log.Infof("PSU VOUT already set to %.3f", v)
		// Voltage is already set to that value; this is a nop. Don't print a message
		return nil
	}

	log.Infof("Setting PSU VOUT to %.3f", v)
	err = psuWriteWord(psuAddr, cmdVout, uint16(ReverseLinear16(v)))
	if err != nil {
		log.Errorf("PSU ERROR: PSU SetVoltage returned %s", err)
		return err
	}

	time.Sleep(500 * time.Millisecond) // Let PSU VOUT settle

	return nil
}

func GetMaxVout(external bool) float32 {
	if psuAddr == 0 {
		return -1.0
	}

	max := MinerVoutMax
	if psuAddr == uint8(vendorPsuAddr) {
		max = MaraMinerVoutMax
	}
	return max
}

func GetMinVout() float32 {
	if psuAddr == 0 {
		return -1.0
	}

	min := MinerVoutMin
	if psuAddr == uint8(vendorPsuAddr) {
		min = MaraMinerVoutMin
	}
	return min
}

func GetMaxPower() float32 {

	if psuAddr == 0 {
		return -1.0
	}

	return MinerPowerMax
}

func GetInputPower(external bool) float32 { // Reread PSU fixed data if not called inside gcminer process

	if psuAddr == 0 {
		return -1.0
	}

	if external {
		getFixedData()
	}

	// Exceptions, some PSUs do not implement the full feature set

	if psuAddr == bocoPsuAddr { // Is this only for the 7.5 kW PSU? Yes - don't break 5kW BOCOs!

		if len(MfrModel) > 5 && MfrModel[3] == '7' && MfrModel[4] == '5' { // 7.5 kW BOCO PSU
			//if true { // 7.5 kW PSU
			tempRegP, err := psuReadWord(psuAddr, cmdPowerIn)
			if err != nil {
				log.Errorf("PSU ERROR: psuReadWord returned %s", err)
				return -1.0
			}
			tempRegP2, err := psuReadWord(psuAddr, cmdPowerIn2)
			if err != nil {
				log.Errorf("PSU ERROR: psuReadWord returned %s", err)
				return -1.0
			}

			return (Linear11(tempRegP) + Linear11(tempRegP2))

		} else { // BOCO 5 kW PSU
			tempRegP, err := psuReadWord(psuAddr, cmdPowerIn)
			if err != nil {
				log.Errorf("PSU ERROR: psuReadWord returned %s", err)
				return -1.0
			}
			return Linear11(tempRegP)
		}

	} else if psuAddr == vendorPsuAddr { // Mara PSU
		tempRegP, err := psuReadWord(psuAddr, cmdPowerIn)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return -1.0
		}
		return Linear11(tempRegP)
	} else { // AA PSU

		tempReg2, err := psuReadWord(psuAddr, cmdPowerIn)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return -1.0
		}

		return float32(tempReg2)
	}

}

// PsuIsOn returns the PSU power state.
func PsuIsOn() bool {
	return poweredOn
}

// PsuAlarmState returns the PSU alarm state. Not all PSUs implement all alarms.
func PsuAlarmState() bool {
	psuDev, err := psuGet()
	if err != nil {
		log.Errorf("PSU ERROR: smbus.Open() for bus %d device 0x%2d returned %s", psuBus, psuAddr, err)
		psuMutex.Unlock()
		return false
	}
	defer psuFree(psuDev)

	// Exceptions, some PSUs do not implement the full feature set
	if psuAddr == uint8(vendorPsuAddr) {
		code, err := psuDev.ReadWord(psuAddr, maraErrorCode)
		if err != nil {
			log.Errorf("PSU ERROR: psuDev.ReadWord returned %s", err)
			return false
		}

		return code != 0
	}

	return (psuAlarm || vIn2Alarm)

}

func psuGet() (*smbus.Conn, error) {
	psuMutex.Lock()
	conn, err := smbus.Open(psuBus, psuAddr)
	if err != nil {
		psuMutex.Unlock()
		return nil, err
	}
	conn.SetTxPEC(true)
	conn.SetRxPEC(false)
	return conn, nil
}

func psuFree(psud *smbus.Conn) {
	psud.Close()
	psuMutex.Unlock()
}

func pollPsuData() {

	var psud psuData
	var err error

	// avoid collision w/ PsuOn() and PsuOff()
	poweredOnMu.Lock()

	psud, err = GetPsuStatus(false) // Dump info to log on error
	if err != nil {
		log.Errorf("PSU ERROR: GetPsuStatus returned %s; retrying", err)
		psud, err = GetPsuStatus(false)
		if err != nil {
			log.Errorf("PSU ERROR: GetPsuStatus returned %s; retry failed!", err)
			return // TBD: What to do here?
		}
	}
	psuTrace.PushBack(psud)
	if psuTrace.Len() > psuTraceLen {
		e := psuTrace.Front()
		psuTrace.Remove(e)
	}

	// Check power state
	if poweredOn && !psud.PsuOn {
		/* Do a retry */
		tempReg, err := psuReadReg(psuAddr, cmdOn)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadReg cmdOn returned %s", err)
		} else {
			if tempReg == 0x80 {
				psud.PsuOn = true
			} else {
				psud.PsuOn = false
			}
		}
		if poweredOn && !psud.PsuOn {
			log.Errorf("ALARM: PSU powered itself off; restarting gcminer")
			psuAlarm = true
			poweredOn = false
			printTrace()
			panic("PSU powered itself off")
		}
	} else if !poweredOn && psud.PsuOn {
		/* Do a retry to make sure we didn't hit a timing issue */
		tempReg, err := psuReadReg(psuAddr, cmdOn)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadReg cmdOn returned %s", err)
		} else {
			if tempReg == 0x80 {
				psud.PsuOn = true
			} else {
				psud.PsuOn = false
			}
		}
		if !poweredOn && psud.PsuOn {
			log.Infof("PSU powered itself back on")
			psuAlarm = false
			poweredOn = true
			printTrace()
		}
	}

	// release lock after consistency check
	poweredOnMu.Unlock()

	// Read Vout status
	vOutStatus = psud.VoutStatus
	if vOutStatus != 0 && !vOutAlarm {
		log.Errorf("ALARM: PSU VOUT Status = 0x%02x", vOutStatus)
		vOutAlarm = true
		printTrace()
	} else if vOutStatus == 0 {
		vOutAlarm = false
	}

	// Read Iout status
	iOutStatus = psud.IoutStatus
	if iOutStatus != 0 && !iOutAlarm {
		log.Errorf("ALARM: PSU IOUT Status = 0x%02x", iOutStatus)
		iOutAlarm = true
		printTrace()
	} else if iOutStatus == 0 {
		iOutAlarm = false
	}

	// Read Input status
	inputStatus = psud.InputStatus
	if inputStatus != 0 && !inputAlarm {
		if psuAddr == bocoPsuAddr {
			if inputStatus == 0x10 {
				inputStatus = 0
			}
		}
		inputAlarm = true
		log.Errorf("ALARM: PSU Input Status = 0x%02x", inputStatus)
		printTrace()
	} else if inputStatus == 0 {
		inputAlarm = false
	}

	// Read Temperature status
	temperatureStatus = psud.TemperatureStatus
	if temperatureStatus != 0 && !temperatureAlarm {
		log.Errorf("ALARM: PSU Temperature Status = 0x%02x", temperatureStatus)
		temperatureAlarm = true
		printTrace()
	} else if temperatureStatus == 0 {
		temperatureAlarm = false
	}

	// Read Fan status
	fanStatus = psud.FanStatus
	if fanStatus != 0 && !fanAlarm {
		log.Errorf("ALARM: PSU fan Status = 0x%02x", fanStatus)
		printTrace()
	} else if fanStatus == 0 {
		fanAlarm = false
	}

}

func printTrace() {
	var i int = 1
	for e := psuTrace.Front(); e != nil; e = e.Next() {
		psud := psuData(e.Value.(psuData))
		log.Infof("PSU Trace: t-%d seconds:", psuTrace.Len()-i)
		log.Infof("PSU: Vout status = 0x%02x", psud.VoutStatus)
		log.Infof("PSU: Iout status = 0x%02x", psud.IoutStatus)
		log.Infof("PSU: Input status = 0x%02x", psud.InputStatus)
		log.Infof("PSU: Temperature status = 0x%02x", psud.TemperatureStatus)
		log.Infof("PSU: Fan status = 0x%02x", psud.FanStatus)
		log.Infof("PSU: Vin = %.3fV", psud.Vin)
		log.Infof("PSU: Iin = %.2fA", psud.Iin)
		log.Infof("PSU: Vout = %.3fV", psud.Vout)
		log.Infof("PSU: Iout = %.2fA", psud.Iout)
		log.Infof("PSU: Temperature 1 = %.2fC", psud.Temp1)
		log.Infof("PSU: Temperature 2 = %.2fC", psud.Temp2)
		log.Infof("PSU: Temperature 3 = %.2fC", psud.Temp3)
		log.Infof("PSU: Fan 1 speed = %.2f RPM", psud.FanSpeed1)
		if psuAddr == bocoPsuAddr {
			log.Infof("PSU: Fan 2 speed = %.2f RPM", psud.FanSpeed2)
		}
		log.Infof("PSU: Power Out = %.2fW", psud.PowerOut)
		log.Infof("PSU: Power In = %.2fW", psud.PowerIn)
		if psuAddr == bocoPsuAddr || psuAddr == bocoV2PsuAddr {
			log.Infof("PSU: Vin2 = %.3fV", psud.Vin2)
			log.Infof("PSU: Iin2 = %.2fA", psud.Iin2)
			log.Infof("PSU: Power In 2 = %.2fW", psud.PowerIn2)
			log.Infof("PSU: Temperature 4 = %.2fC", psud.Temp4)
		} else if psuAddr == aaPsuAddr {
			log.Infof("PSU: VinB = %.3fV", psud.Vin2)
			log.Infof("PSU: Vin1 = %.2fA", psud.Vin1)
			log.Infof("PSU: Vin2 = %.2fW", psud.Vin2a)
			log.Infof("PSU: IinA = %.2fA", psud.IinA)
			log.Infof("PSU: IinB = %.2fW", psud.IinB)
			log.Infof("PSU: Iin2 = %.2fW", psud.Iin2a)
		} else if psuAddr == vendorPsuAddr {
			log.Infof("PSU: Error Code = 0x%04x", psud.ErrorCode)
		}
		i++
	}

	// Clear the trace
	psuTrace = list.New()
}

// GetPsuStatus returns the PSU status.
func GetPsuStatus(print bool) (psud psuData, err error) {
	psud = psuData{}

	return psud, nil
}

func StartPsuMonitor() {
	go func() {

		for {

			pollPsuData()
			time.Sleep(PSU_MONITOR_INTERVAL_MS * time.Millisecond)
		}

	}()
}

func SetPsuType() int {

	psuAddr = 0

	fd, err := smbus.Open(psuBus, bocoPsuAddr)
	if err != nil {
		log.Errorf("PSU ERROR: Open BOCO PSU fd failed, %s", err)
		return 0
	}

	_, err = fd.ReadReg(bocoPsuAddr, cmdOn)
	if err == nil {
		psuAddr = bocoPsuAddr
		fd.Close()
		log.Infof("PSU: BOCO PSU found")
		return bocoPsuAddr
	}

	fd, err = smbus.Open(psuBus, bocoV2PsuAddr)
	if err != nil {
		log.Errorf("PSU ERROR: Open boco2 Psu fd failed, %s", err)
		return 0
	}

	_, err = fd.ReadReg(bocoV2PsuAddr, cmdOn)
	if err == nil {
		psuAddr = bocoV2PsuAddr
		MinerVoutMax = 22.0
		fd.Close()
		log.Infof("PSU: boco2 PsuAddr found")
		return bocoV2PsuAddr
	}

	fd, err = smbus.Open(psuBus, aaPsuAddr)
	if err != nil {
		log.Errorf("PSU ERROR: Open AA PSU fd failed, %s", err)
		return 0
	}

	_, err = fd.ReadReg(aaPsuAddr, cmdOn)
	if err == nil {
		psuAddr = aaPsuAddr
		fd.Close()
		log.Infof("PSU: AA PSU found")
		return aaPsuAddr
	}

	fd, err = smbus.Open(psuBus, vendorPsuAddr)
	if err != nil {
		log.Errorf("PSU ERROR: Open vendor PSU fd failed, %s", err)
		return 0
	}

	_, err = fd.ReadReg(vendorPsuAddr, cmdOn)
	if err == nil {
		psuAddr = vendorPsuAddr
		fd.Close()
		log.Infof("PSU: vendor PSU found")
		MinerVoutMax = MaraMinerVoutMax
		MinerVoutMin = MaraMinerVoutMin
		return vendorPsuAddr
	}

	log.Error("PSU ERROR: No PSU found!!!")
	fd.Close()
	return 0
}

func getFixedData() {
	var strlen uint8
	var tempReg2 uint16
	var err error

	// Get MfrId
	MfrId = make([]byte, psuStrLen)
	MfrModel = make([]byte, psuStrLen)
	MfrRevision = make([]byte, psuStrLen)
	MfrLocation = make([]byte, psuStrLen)
	MfrDate = make([]byte, psuStrLen)
	MfrSerial = make([]byte, psuStrLen)
	PriFwRev = make([]byte, psuStrLen)
	SecFwRev = make([]byte, psuStrLen)

	if psuAddr == vendorPsuAddr {
		copy(MfrId, "Vendor")
		copy(MfrRevision, "0.0")
		copy(MfrLocation, "USA")
		copy(MfrDate, "2024-01-01")
		copy(MfrSerial, "0")
		copy(SecFwRev, "0.0")

		cmd := cmdMfrModel
		MfrModel, err = psuReadN(psuAddr, cmd, 5)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData (mara Model) returned %s", err)
			return
		}
		cmd = maraFwVer
		PriFwRev, err = psuReadN(psuAddr, cmd, 6)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData (mara FW version) returned %s", err)
			return
		}
		cmd = maraPsuVersion
		dat, err := psuReadReg(psuAddr, cmd)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadReg (mara PSU version) returned %s", err)
			return
		}
		MfrRevision[0] = dat
	} else {

		MfrId, err = psuReadBlockData(psuAddr, cmdMfrId)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData returned %s", err)
			return
		}
		strlen = MfrId[0]
		if strlen > 0 && strlen <= psuStrLen {
			// Byte 0 is strlen; need to remove
			MfrId = MfrId[1 : strlen+1]
		}

		// Get MfrModel
		cmd := cmdMfrModel
		MfrModel, err = psuReadBlockData(psuAddr, cmd)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData returned %s", err)
			return
		}
		strlen = MfrModel[0]
		if strlen > 0 && strlen <= psuStrLen {
			// Byte 0 is strlen; need to remove
			MfrModel = MfrModel[1 : strlen+1]
		}

		// Get MFR_REV
		MfrRevision, err = psuReadBlockData(psuAddr, cmdMfrRev)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData returned %s", err)
			return
		}
		strlen = MfrRevision[0]
		if strlen > 0 && strlen <= psuStrLen {
			// Byte 0 is strlen; need to remove
			MfrRevision = MfrRevision[1 : strlen+1]
		}

		// Get MfrLocation
		MfrLocation, err = psuReadBlockData(psuAddr, cmdMfrLoc)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData returned %s", err)
			return
		}
		strlen = MfrLocation[0]
		if strlen > 0 && strlen <= psuStrLen {
			// Byte 0 is strlen; need to remove
			MfrLocation = MfrLocation[1 : strlen+1]
		}

		// Get MfrDate
		MfrDate, err = psuReadBlockData(psuAddr, cmdMfrDate)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData returned %s", err)
			return
		}
		strlen = MfrDate[0]
		if strlen > 0 && strlen <= psuStrLen {
			// Byte 0 is strlen; need to remove
			MfrDate = MfrDate[1 : strlen+1]
		}

		// Get MFR_SN
		MfrSerial, err = psuReadBlockData(psuAddr, cmdMfrSerial)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData returned %s", err)
			return
		}
		strlen = MfrSerial[0]
		if strlen > 0 && strlen <= psuStrLen {
			// Byte 0 is strlen; need to remove
			MfrSerial = MfrSerial[1 : strlen+1]
		}
	}
	if psuAddr == vendorPsuAddr {
		VinMin = MaraMinerVinMin
		VinMax = MaraMinerVinMax
		VoutMin = MaraMinerVoutMin
		VoutMax = MaraMinerVoutMax
		IoutMax = MaraMinerPowerMax / MaraMinerVoutMin
		PoutMax = MaraMinerPowerMax
		MaxTemp1 = MaraMinerTempMax
		MaxTemp2 = MaraMinerTempMax
		MaxTemp3 = MaraMinerTempMax
		SecFwRev = []byte("0.0")
	} else {
		// Get VinMin
		tempReg2, err = psuReadWord(psuAddr, cmdVinMin)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return
		}
		VinMin = Linear11(tempReg2)

		// Get VinMax
		tempReg2, err = psuReadWord(psuAddr, cmdVinMax)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return
		}
		VinMax = Linear11(tempReg2)

		// Get VoutMin
		tempReg2, err = psuReadWord(psuAddr, cmdVoutMin)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return
		}
		VoutMin = Linear16(tempReg2)

		// Get VoutMax
		tempReg2, err = psuReadWord(psuAddr, cmdVoutMax)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return
		}
		VoutMax = Linear16(tempReg2)

		// Get IoutMax
		tempReg2, err = psuReadWord(psuAddr, cmdIoutMax)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return
		}
		IoutMax = Linear11(tempReg2)

		// Get PoutMax
		tempReg2, err = psuReadWord(psuAddr, cmdPoutMax)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return
		}
		PoutMax = Linear11(tempReg2)

		// Get MaxTemp1
		tempReg2, err = psuReadWord(psuAddr, cmdMaxTemp1)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return
		}
		MaxTemp1 = Linear11(tempReg2)

		// Get MaxTemp2
		tempReg2, err = psuReadWord(psuAddr, cmdMaxTemp2)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return
		}
		MaxTemp2 = Linear11(tempReg2)

		// Get MaxTemp3
		tempReg2, err = psuReadWord(psuAddr, cmdMaxTemp3)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadWord returned %s", err)
			return
		}
		MaxTemp3 = Linear11(tempReg2)

		// Get Primary FW Rev
		cmd := cmdPriFwRev
		PriFwRev, err = psuReadBlockData(psuAddr, cmd)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData returned %s", err)
			return
		}
		strlen = PriFwRev[0]
		if strlen > 0 && strlen <= psuStrLen {
			// Byte 0 is strlen; need to null-terminate
			PriFwRev = PriFwRev[1 : strlen+1]
		}
		if psuAddr == vendorPsuAddr {
			strlen = 6
			PriFwRev = PriFwRev[0:strlen]
		}
		// Byte 0 is strlen; need to null-terminate

		// Get Secondary FW Rev
		cmd = cmdSecFwRev
		SecFwRev, err = psuReadBlockData(psuAddr, cmd)
		if err != nil {
			log.Errorf("PSU ERROR: psuReadBlockData returned %s", err)
			return
		}
		strlen = SecFwRev[0]
		if strlen > 0 && strlen <= psuStrLen {
			// Byte 0 is strlen; need to null-terminate
			SecFwRev = SecFwRev[1 : strlen+1]
		}

		// Get Major FW Version
		if psuAddr == aaPsuAddr {
			MajorFwVer, err = psuReadReg(psuAddr, cmdMajorFwVer)
			if err != nil {
				log.Errorf("PSU ERROR: psuReadReg cmdMajorFwVer returned %s", err)
				return
			}
		}
	}
}

func IsSingleInput() bool {
	return singleInput
}

func PreInit() {

	rc := SetPsuType()
	if rc == 0 { // No PSU detected
		return
	}

	psuTrace = list.New()

	getFixedData()

	psud, _ := GetPsuStatus(true) // We dump PSU info into the log from DeviceManager when we check Vin2

	if psud.Vin2 < 180.0 && (psuAddr != vendorPsuAddr) {
		singleInput = true
		MinerPowerMax = singleInputMaxPower
		log.Infof("PSU ALARM: Single AC input detected. Max power = %.1fW", MinerPowerMax)
	} else {
		MinerPowerMax = devhdr.GetMaxLimit().MaxPower // Watts; should we use PoutMax from PSU?
		log.Infof("PSU: Max power = %.1fW", MinerPowerMax)
	}
	if psuAddr == vendorPsuAddr {
		MinerPowerMax = MaraMinerPowerMax
	}
	_ = SetVoltage(MinerVoutMin)

	// Sanity-check limit values from PSU; only valid for Boco PSUs
	if psuAddr == bocoPsuAddr || psuAddr == bocoV2PsuAddr {
		if psud.VoutMin < 11.0 || psud.VoutMin > 18.0 {
			log.Errorf("PSU: VoutMin %.3fV is invalid; leaving at default %.3fV", psud.VoutMin, MinerVoutMin)
		}
		if psud.VoutMax < 15.0 || psud.VoutMax > 25.0 {
			log.Errorf("PSU: VoutMax %.3fV is invalid; leaving at default %.3fV", psud.VoutMax, MinerVoutMax)
		} else {
			MinerVoutMax = psud.VoutMax
			log.Infof("PSU: VoutMax from PSU is %.3fV", MinerVoutMax)
		}
		if psud.PoutMax < 4000.0 || psud.PoutMax > 15000.0 {
			log.Errorf("PSU: PoutMax %.1fW is invalid; leaving at default %.1fW", psud.PoutMax, MinerPowerMax)
		} else {
			MinerPowerMax = psud.PoutMax
			log.Infof("PSU: PoutMax from PSU is %.1fW", MinerPowerMax)
		}
	}

	PreInitDone = true
}

func Init() {
	// skip preinit on either done or error
	if !PreInitDone && PreInitError == nil {
		PreInit()
	}
	ClearFaults()
	PsuOn()

	StartPsuMonitor()
	log.Infof("PSU: PSU monitor started")
}
