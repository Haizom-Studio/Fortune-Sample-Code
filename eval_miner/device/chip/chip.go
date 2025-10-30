package chip

import (
	"eval_miner/device/temperature"
	"eval_miner/job"
)

type Chip struct {
	ID              uint
	Enabled         bool
	LastMessageTS   float64
	Temp            temperature.Temperature
	Frequency       float64
	Voltage         float64
	HStats          job.HashStats
	GeneralHitStats job.HashStats
	SStats          job.ShareStats
	LastJobResult   job.JobResult
	UpSince         float64
	HitRate         float32
}
