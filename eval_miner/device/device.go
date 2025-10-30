package device

import (
	"errors"

	"eval_miner/device/asiccommon"
	"eval_miner/device/chip"
	"eval_miner/job"
	"eval_miner/log"
	"eval_miner/util"
)

const (
	STATUS_ALIVE = iota
	STATUS_SICK
	STATUS_DEAD
	STATUS_NOSTART
	STATUS_INIT
)

func StatusCode(s int) string {
	switch s {
	case STATUS_ALIVE:
		return "Alive"
	case STATUS_SICK:
		return "Sick"
	case STATUS_DEAD:
		return "Dead"
	case STATUS_NOSTART:
		return "NoStart"
	case STATUS_INIT:
		return "Initialising"
	default:
		return "Dead"
	}
}

type PreScanFunc func(my *Device, j *job.Job)
type ScanFunc func(my *Device, j *job.Job) error
type PostScanFunc func(HWCtxID uint, r *job.JobResult, dev *Device) error
type FindJobFunc func(HWCtxID uint) *job.Job
type PollResultFunc func(my *Device, fn FindJobFunc) error

type DetectionFunc func(my *Device) ([]uint8, error)

type Device struct {
	ID                uint
	SlotId            uint32
	ChainId           uint32
	Name              string
	Kernel            string
	Driver            string
	Path              string
	Enabled           bool
	Status            int
	LastMessageTS     float64
	GStats            job.GetworkStats
	HStats            job.HashStats
	GeneralHitStats   job.HashStats
	SStats            job.ShareStats
	DStats            job.DiffStats
	LastJobResult     job.JobResult
	PoolHashRate      *job.PoolStats
	UpSince           float64
	DiffMin           uint64
	DiffMax           uint64
	VersionRollingSim bool
	TimeRollingSim    bool
	PreScan           PreScanFunc
	Scan              ScanFunc
	PollResult        PollResultFunc
	ChipMap           map[uint]*chip.Chip
	ChipIDArray       []uint8
	ChipPerBoard      uint
	HWJobs            HWJob
	DetectBoard       DetectionFunc
	Asic              asiccommon.AsicRW
	SystemDvfs        asiccommon.SystemDVFS
}

func (my *Device) Uptime() float64 {
	return util.UptimeInSec(util.NowInSec(), my.UpSince)
}

var ErrBoardInitFailure = errors.New("ErrBoardInitFailure")

func (my *Device) Init() error {
	my.ChipMap = make(map[uint]*chip.Chip)
	var chipIdArr []uint8
	my.SystemDvfs = GetSystemDVFS()
	if my.DetectBoard != nil && my.Enabled {
		var err error
		chipIdArr, err = my.DetectBoard(my)
		if err != nil {
			log.Errorf("Board %d detection error %v", my.ID, err)
			my.Enabled = false
			my.Status = STATUS_DEAD
			return ErrBoardInitFailure
		}
		my.ChipPerBoard = (uint)(len(chipIdArr))
	} else {
		chipIdArr = make([]uint8, my.ChipPerBoard)
		for i := uint(0); i < my.ChipPerBoard; i++ {
			chipIdArr[i] = uint8(i)
		}
	}

	for i := uint(0); int(i) < len(chipIdArr); i++ {
		id := uint(chipIdArr[i])
		my.ChipMap[id] = &chip.Chip{
			ID:      id,
			Enabled: true,
		}
	}

	// remember chipIdArr for later chip temp readings
	my.ChipIDArray = chipIdArr

	/*
		board 1: 1-63
		board 2: 65 - 127
		board 3: 129 - 191
	*/
	idStart := (my.ID-1)*16 + 1
	idEnd := my.ID*16 - 1
	my.HWJobs.Init(idStart, idEnd)
	my.Enabled = true
	my.Status = STATUS_ALIVE
	log.Infof("Board %d is alive, HWJob ID %v - %v", my.ID, idStart, idEnd)
	return nil
}

func (my *Device) Run() bool {
	if !my.Enabled {
		return false
	}

	hasWork := false

	// remove stale
	my.HWJobs.RemoveStaleJob()

	if my.PollResult != nil {
		err := my.PollResult(my, my.HWJobs.FindJobWithLock)
		if err == ErrNoResultYet {
		} else {
			if err != nil {
				log.Debug(err)
			}
			// PollResult has more work when it polled back a result message
			// Only ErrNoResultYet indicate there isn't a message (stats or job result) pending
			hasWork = true
		}
	}

	// run scan job
	J := my.HWJobs.GetJob()

	if J != nil {
		J.DevID = my.ID

		my.PreScan(my, J)

		err := my.Scan(my, J)
		if err != nil {
		} else {
			hasWork = true
		}
	}
	return hasWork
}

var ErrResultNotExist = errors.New("result not exist")
var ErrJobNotExist = errors.New("job not exist")
var ErrDiffNotOnDeviceTarget = errors.New("ErrDiffNotOnDeviceTarget")
var ErrDuplicateResult = errors.New("ErrDuplicateResult")
var ErrNoResultYet = errors.New("ErrNoResultYet")

func (my *Device) PostScan(HWCtxID uint, r *job.JobResult) error {
	if r == nil {
		return ErrResultNotExist
	}

	r.HWCtxID = HWCtxID
	r.DevID = my.ID

	J := my.HWJobs.FindJobWithLock(r.HWCtxID)
	if J == nil {
		my.HStats.Stale++
		return ErrJobNotExist
	}

	// Validate Job Result
	J.DiffValidate = r.Validate(J)
	log.Infof("Job %s HW ID %d, Nonce %s, DiffTarget %d, DevDiff %d, HWDiff %d, DiffSubmit %d",
		J.JobID, HWCtxID, r.Nonce, J.DiffTarget, J.DevDiff, J.HWDiff, r.DiffSubmit)
	chip, ok2 := my.ChipMap[J.ChipID]
	if r.DiffSubmit < J.DevDiff {
		/* HW errors is limited to device */
		my.HStats.HWErrors++
		if ok2 {
			chip.HStats.HWErrors++
		}
		log.Infof("HWErrors: DiffSubmit %v < DevDiff %v", r.DiffSubmit, J.DevDiff)
		return ErrDiffNotOnDeviceTarget
	}
	my.HStats.HWHits++
	if ok2 {
		chip.HStats.HWHits++
	}

	// duplicate Job Result detected
	if r.IsDuplicate(&my.LastJobResult) {
		my.HStats.HWErrors++
		log.Info("HWErrors: duplicate results")
		if ok2 {
			chip.HStats.HWErrors++
		}
		return ErrDuplicateResult
	}

	my.LastJobResult = *r
	my.HWJobs.AddResult(r)

	return nil
}

func (my *Device) UpdateHashes(HashDone uint64, GenHashDone uint64, tsInSec float64) {
	dev := my
	if !dev.Enabled {
		return
	}
	job.UpdateHashes(&dev.HStats, HashDone, tsInSec, dev.Uptime())
	job.UpdateHashes(&dev.GeneralHitStats, GenHashDone, tsInSec, dev.Uptime())
	log.Debugf("Dev[%d] HashStats %+v", dev.ID, dev.HStats)
	log.Debugf("nJobs %d, nResults %d", dev.HWJobs.nJobTotal, dev.HWJobs.nResultTotal)
}

func (my *Device) UpdateChipHashes(HashDone uint64, GenHashDone uint64, hitRate float32, chipID uint, tsInSec float64) {
	dev := my
	if !dev.Enabled {
		return
	}
	chip, ok2 := dev.ChipMap[chipID]
	if !ok2 {
		return
	}
	chip.HitRate = hitRate
	job.UpdateHashes(&chip.HStats, HashDone, tsInSec, dev.Uptime())
	job.UpdateHashes(&chip.GeneralHitStats, GenHashDone, tsInSec, dev.Uptime())
	log.Debugf("Chip[%d] HashStats %+v GeneralHitStats %+v", chipID, chip.HStats, chip.GeneralHitStats)
}
