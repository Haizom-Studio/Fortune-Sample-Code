package pool

import (
	"eval_miner/job"
	"eval_miner/util"
)

type Summary struct {
	GStats            job.GetworkStats
	HStats            job.HashStats
	GeneralHitStats   job.HashStats
	SStats            job.ShareStats
	DStats            job.DiffStats
	StratumRecv       job.ByteStats
	StratumSent       job.ByteStats
	Recv              job.ByteStats
	Sent              job.ByteStats
	PoolHS30m         float64
	PoolHS60m         float64
	PoolHS24h         float64
	Height            uint64
	UpSince           float64
	CurrentBlockTime  float64
	CurrentBlockHash  string
	LP                bool
	NetworkDifficulty float64
}

var (
	Sum Summary = Summary{
		HStats:          job.HashStats{UpdateTS: util.NowInSec()},
		GeneralHitStats: job.HashStats{UpdateTS: util.NowInSec()},
		LP:              false,
		UpSince:         util.NowInSec(),
	}
)

func (my *Summary) Uptime() float64 {
	return util.UptimeInSec(util.NowInSec(), my.UpSince)
}

func (my *Summary) UpdateUtility() {
	job.UpdateUtility(&my.SStats, my.Uptime())
}
