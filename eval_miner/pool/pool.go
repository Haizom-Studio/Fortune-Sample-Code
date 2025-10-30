package pool

import (
	"eval_miner/config"
	"eval_miner/device"
	"eval_miner/job"
	"eval_miner/log"
	"eval_miner/pool/stratum"
	"eval_miner/util"
)

const (
	MAX_POOL_SEQREJECT = 10
)

// Pool Status
const (
	STATUS_DISABLED = iota
	STATUS_ALIVE
	STATUS_REJECTING
	STATUS_DEAD
	STATUS_UNREACHABLE
	STATUS_UNKNOWN
)

func StatusCode(s int) string {
	switch s {
	case STATUS_ALIVE:
		return "Alive"
	case STATUS_DISABLED:
		return "Disabled"
	case STATUS_DEAD:
		return "Dead"
	case STATUS_REJECTING:
		return "Rejecting"
	case STATUS_UNREACHABLE:
		return "Unreachable"
	case STATUS_UNKNOWN:
		return "Unknown"
	default:
		return "Unknown"
	}
}

type PoolRuntime struct {
	/* Pool Management */
	ID              uint
	SeqNo           uint
	Cfg             config.PoolEntryConfig
	Priority        int
	Enabled         bool
	Running         bool
	Rejecting       bool
	Unreachable     bool
	NoJob           bool
	SeqRejected     int
	GStats          job.GetworkStats
	HStats          job.HashStats
	GeneralHitStats job.HashStats
	SStats          job.ShareStats
	DStats          job.DiffStats
	StratumRecv     job.ByteStats
	StratumSent     job.ByteStats
	Recv            job.ByteStats
	Sent            job.ByteStats
	Height          uint64
	Version         uint32
	S               *stratum.Stratum
	DevFunc         device.DevFunc
	UpSince         float64
	bExit           bool
}

func (p *PoolRuntime) Status() string {
	if !p.Enabled {
		return StatusCode(STATUS_DISABLED)
	}

	if !p.Running {
		return StatusCode(STATUS_DEAD)
	}

	if p.Rejecting {
		return StatusCode(STATUS_REJECTING)
	}

	if p.Unreachable {
		return StatusCode(STATUS_UNREACHABLE)
	}

	return StatusCode(STATUS_ALIVE)
}

func (p *PoolRuntime) Uptime() float64 {
	return util.UptimeInSec(util.NowInSec(), p.UpSince)
}

func (p *PoolRuntime) UpdateHashes(HashDone uint64, GeneralHitDone uint64, J *job.Job) {
	ts := util.NowInSec()
	job.UpdateHashes(&p.HStats, HashDone, ts, p.Uptime())
	job.UpdateHashes(&Sum.HStats, HashDone, ts, Sum.Uptime())

	job.UpdateHashes(&p.GeneralHitStats, GeneralHitDone, ts, p.Uptime())
	job.UpdateHashes(&Sum.GeneralHitStats, GeneralHitDone, ts, Sum.Uptime())

	log.Debugf("Pool[%d] HashStats %+v", p.ID, p.HStats)
	log.Debugf("Sum %+v", Sum.HStats)

	if J == nil {
		return
	}

	/* Stale is a pool concept */
	isStale := J.IsStale()
	if isStale {
		job.UpdateStale(&p.HStats, J.DiffValidate)
		job.UpdateStale(&Sum.HStats, J.DiffValidate)
	}
}

func (p *PoolRuntime) UpdateBytes(IsRecv bool, bs job.ByteStats) {

	if IsRecv {
		job.UpdateBytes(&p.StratumRecv, bs)
		job.UpdateBytes(&Sum.StratumRecv, bs)
	} else {
		job.UpdateBytes(&p.StratumSent, bs)
		job.UpdateBytes(&Sum.StratumSent, bs)
	}
	log.Debugf("Pool[%d] StratumRecv %+v", p.ID, p.StratumRecv)
	log.Debugf("Pool[%d] StratumSent %+v", p.ID, p.StratumSent)
	log.Debugf("Sum %+v", Sum.StratumRecv)
	log.Debugf("Sum %+v", Sum.StratumSent)
}

func (p *PoolRuntime) UpdateDiffs(J *job.Job) {
	job.UpdateDiffs(&p.DStats, float64(J.DiffTarget), J.DevDiff, p.Uptime())
	job.UpdateDiffs(&Sum.DStats, float64(J.DiffTarget), J.DevDiff, Sum.Uptime())
	p.DevFunc.UpdateDiffs(J)

	log.Debugf("Pool[%d] DiffStats %+v", p.ID, p.DStats)
	log.Debugf("Sum %+v", Sum.DStats)

	job.UpdateGetwork(&p.GStats, J.GetworkTDiff, J.LastGetworkTS, p.S.JobQ.Created)
	job.UpdateGetwork(&Sum.GStats, J.GetworkTDiff, J.LastGetworkTS, p.S.JobQ.Created)
	log.Debugf("Pool[%d] GetworkStats %+v", p.ID, p.GStats)
	log.Debugf("Sum %+v", Sum.GStats)

	p.Height = J.Height
	Sum.Height = J.Height
	log.Debugf("Current height %d", p.Height)

	p.Version = util.BEHexToUint32(J.VersionStratum)

	Sum.CurrentBlockTime = J.NotifyJobTS
	r := job.JobResult{
		NTime: J.NTimeStratum,
	}
	Sum.CurrentBlockHash = r.BlockHeaderHashBEStr(J)

	Sum.NetworkDifficulty = J.NetworkDifficulty()

}

func (p *PoolRuntime) UpdateUtility() {
	job.UpdateUtility(&p.SStats, p.Uptime())
}

func (p *PoolRuntime) UpdateRemoteFailures(n int) {
	job.UpdateRemoteFailures(&p.SStats, n)
	job.UpdateRemoteFailures(&Sum.SStats, n)
}

func (p *PoolRuntime) UpdateShares(bAccepted bool, J *job.Job) {
	ts := util.NowInSec()
	job.UpdateShares(&p.SStats, bAccepted, J.DiffValidate, ts)
	job.UpdateShares(&Sum.SStats, bAccepted, J.DiffValidate, ts)
	p.UpdateUtility()
	Sum.UpdateUtility()

	if bAccepted {
		p.SStats.LastSharePool = p.ID
		p.Rejecting = false
		p.SeqRejected = 0
	} else {
		p.Rejecting = true
		p.SeqRejected++
	}

	log.Debugf("Pool[%d] ShareStats %+v", p.ID, p.SStats)
	log.Debugf("Sum %+v", Sum.SStats)

	J.PoolID = p.ID
	p.DevFunc.UpdateShares(bAccepted, J, ts)
	p.DevFunc.UpdateUtility(J)
}

func (p *PoolRuntime) UpdateDiscarded(nDiscarded int) {
	p.HStats.Discarded += nDiscarded
}

func (my *PoolRuntime) Stop() {
	if my.S != nil {
		my.S.Stop()
	}
	my.bExit = true
}

func (p *PoolRuntime) Start() {
	p.Enabled = true
	p.Running = false
	p.Rejecting = false
	p.Unreachable = false
	p.SeqRejected = 0
	p.bExit = false
}

func (my *PoolRuntime) Run() {
	var err error
	statsFunc := job.StatsFunc{
		ShareFunc:         my.UpdateShares,
		DiffFunc:          my.UpdateDiffs,
		BytesFunc:         my.UpdateBytes,
		HashFunc:          my.UpdateHashes,
		DiscardFunc:       my.UpdateDiscarded,
		RemoteFailureFunc: my.UpdateRemoteFailures,
	}

	my.Running = true
	for {
		if err != nil {
			my.bExit = true

			switch err {
			// no job is typically considered network level of issues and does not affect pool scheduling.
			case stratum.ErrNoJob:
				my.GStats.GetFailures++
			// Unreachable cases lower the pool's effective priority
			case stratum.ErrUnreachable:
				my.Unreachable = true
			default:
			}
		}

		if my.bExit {
			break
		}

		my.S, err = stratum.NewClient(my.Cfg, my.DevFunc, statsFunc, &my.bExit)
		if err != nil {
			log.Errorf("stratum.NewClient error: %s", err)
		}
		err = my.S.Start()
		if err == nil {
			if my.Cfg.Equal(my.S.Cfg) {
				// client.reconnect method will exit here and err is nil in this case
				// reconnect using the ExtraNonce1
				// or reconnect using new host:port
				ExtraNonce1 := my.S.ExtraNonce1
				SubscribeID := my.S.SubscribeID
				Cfg := *my.S.Cfg // keep the host and port from client.reconnect
				my.S, err = stratum.NewClient(Cfg, my.DevFunc, statsFunc, &my.bExit)
				if err != nil {
					log.Errorf("stratum.NewClient error: %s", err)
				}
				my.S.ExtraNonce1 = ExtraNonce1
				my.S.SubscribeID = SubscribeID
				err = my.S.Start()
			}
		}
		if err != nil {
			log.Errorf("Stratum.Start error: %s", err)
		}
	}
	my.Running = false
}
