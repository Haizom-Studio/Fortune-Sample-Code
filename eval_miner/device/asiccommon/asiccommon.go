package asiccommon

import (
	"eval_miner/device/chip"
)

const MinThsRate float32 = 30
const MinTgtPower float32 = 1300
const EcoThsRate float32 = 90

type AsicRW interface {
	CheckResults() (msg *chip.Message, err error)
	SendJob(msg *chip.Message) error

	// Register read/writes
	RegWrite(asicId uint8, addr uint8, data uint32, broadcast bool) error
	ReadRegsPipelined(targets []uint8, addrs []uint8) (results []int64, err error)

	// Board level temperature, voltage, and frequency reads
	ReadAllTemperature() []float64
	ReadAllVoltage() []float64
	ReadAllFrequency() []float64
	SetFrequencyAll(freq float32) error
	SetFrequency(asicId int, freq float32) error
	AsicInitComplete()
}

type SystemDVFS interface {
	// DVFS supported functions
	InitialSetup()
	DVFS() bool
}
