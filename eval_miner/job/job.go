package job

import (
	"context"

	"eval_miner/block"
	"eval_miner/util"
)

type Job struct {
	/* Pool */
	JobID               string
	PrevHashStratum     string
	PrevHashLE          string
	CoinB1Stratum       string
	CoinB2Stratum       string
	MerkleBranchStratum []string
	VersionStratum      string // Network
	NBitsStratum        string // Network
	NTimeStratum        string // Network
	CleanJobs           bool
	VersionRolling      bool
	ServerMask          string
	VersionBits         string
	ExtraNonce1         string
	ExtraNonce2Size     uint
	PoolID              uint

	/* stats */
	DiffTarget    uint64
	DiffValidate  uint64
	DevDiff       uint64
	HWDiff        uint
	Height        uint64
	NotifyJobTS   float64
	ScanJobTS     float64
	JobResultTS   float64
	GetworkTDiff  float64
	LastGetworkTS float64

	/* Device */
	DevID          uint
	ChipID         uint
	HWCtxID        uint // for device
	Ctx            context.Context
	CtxCancel      context.CancelFunc
	Scanning       bool
	BlockHeaderStr string // to HWMiner
	MerkleTree     string
	NewVersion     uint32 // SimVersionRolling
	NTimeDiff      uint32
	NewNTime       string
	ExtraNonce2    string
}

var (
	ExpiryInSeconds = 60
)

func (j *Job) IsStale() bool {
	elapsed := j.JobResultTS - j.NotifyJobTS

	return elapsed > float64(ExpiryInSeconds)
}

func (j *Job) ClearJobContext() {
	j.Ctx = nil
	j.CtxCancel = nil
}

func (j *Job) BlockHeight() uint64 {
	CBHexStr := j.CoinB1Stratum + j.ExtraNonce1 + util.ZeroHexString(int(j.ExtraNonce2Size)) + j.CoinB2Stratum
	Tx := block.TxCoinBase{}
	_, err := Tx.DecodeString(CBHexStr)
	if err == nil {
		// got block height, update pool from DiffFunc
		j.Height = Tx.TxInCoinBase.GetHeight()
	}
	return j.Height
}

func (j *Job) NetworkDifficulty() float64 {
	return block.NBitsToDifficulty(util.BEHexToUint32(j.NBitsStratum))
}
