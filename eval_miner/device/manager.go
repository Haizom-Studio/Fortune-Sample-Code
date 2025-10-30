package device

import (
	"errors"
	"math"
	"time"

	"eval_miner/device/asic"
	ac "eval_miner/device/asiccommon"
	"eval_miner/device/chip"
	"eval_miner/device/devhdr"
	"eval_miner/device/fan"
	"eval_miner/job"
	"eval_miner/log"

	//"gcminer/device/led"
	"eval_miner/device/powerstate"
	"eval_miner/device/psu"
	//"gcminer/device/temperature"
	//"gcminer/osutil"
)

type AddJobFunc func(j *job.Job) (int, error)
type GetResultFunc func() (j *job.Job, r *job.JobResult)
type UpdateHashFunc func(HashDone uint64, GeneralHitDone uint64, J *job.Job, tsInSec float64)
type UpdateShareFunc func(bAccepted bool, J *job.Job, tsInSec float64)
type UpdateDiffFunc func(J *job.Job)
type UpdateUtilityFunc func(J *job.Job)

type DevFunc struct {
	AddJob        AddJobFunc
	GetResult     GetResultFunc
	UpdateHashes  UpdateHashFunc
	UpdateShares  UpdateShareFunc
	UpdateDiffs   UpdateDiffFunc
	UpdateUtility UpdateUtilityFunc
}

type DeviceManager struct {
	// BoardChainCount total number of independent chains in a miner
	BoardChainCount uint
	// BoardChainMap maps the board's chain to chainId in a miner
	BoardChainMap map[uint]*Device
	// BoardCount total number of physical boards in a miner
	BoardCount uint
	// BoardCount maps physical boardId to list of board chains in a miner
	BoardMap map[uint32][]*Device
	bExit    bool
	// ASIC interface to communicate with board/system/asics
	SystemDVFS ac.SystemDVFS
}

var (
	EmptyDevFunc = DevFunc{
		AddJob:        func(j *job.Job) (int, error) { return 0, nil },
		GetResult:     func() (j *job.Job, r *job.JobResult) { return nil, nil },
		UpdateHashes:  func(HashDone uint64, GeneralHitDone uint64, J *job.Job, tsInSec float64) {},
		UpdateShares:  func(bAccepted bool, J *job.Job, tsInSec float64) {},
		UpdateDiffs:   func(J *job.Job) {},
		UpdateUtility: func(J *job.Job) {},
		//Get:           func(arg DevArg) *DevData { return nil },
	}
)

var ErrDevNotExist = errors.New("not exist")

func (my *DeviceManager) InitBoard(board *Device) {
	err := board.Init()
	if err != nil {
		log.Infof("board id (%v): %v", board.ID, err)
	}
	my.BoardChainMap[board.ID] = board
	my.BoardMap[board.SlotId] = append(my.BoardMap[board.SlotId], board)
}

func (my *DeviceManager) Init() DevFunc {

	// Initialize system/board interface
	my.SystemDVFS = asic.NewSystemDVFS()
	// Initialize system HW
	psu.SetPsuType()
	powerstate.SystemPowerOff(false) // Make sure system is in a clean state
	time.Sleep(time.Second * 2)
	psu.SetSleep(false)
	time.Sleep(time.Second * 2)
	fan.Init()
	log.Info("Starting fans")
	fan.MaxOn()

	devFunc := DevFunc{
		AddJob:        my.AddJob,
		GetResult:     my.GetResult,
		UpdateShares:  my.UpdateShares,
		UpdateDiffs:   my.UpdateDiffs,
		UpdateUtility: my.UpdateUtility,
	}

	go func() {
		err := my.Run()
		if err != nil {
			log.Errorf("err %v", err)
		}
	}()

	return devFunc
}

func (my *DeviceManager) Fini() {
	my.bExit = true

}

func (my *DeviceManager) Run() error {

	psu.PreInit()
	psu.Init()
	time.Sleep(2 * time.Second) // Give the ASICs some time to power on
	my.BoardChainMap = make(map[uint]*Device)
	my.BoardMap = make(map[uint32][]*Device)

	powerstate.SystemUnreset() // Let DVFS handle hash power; just take ASICs out of reset

	brdIdx := uint32(devhdr.EvalHashBoardId)
	brd := asicBoard
	brd.SlotId = uint32(brdIdx)
	brd.ChainId = 0
	brd.ID = uint(brdIdx)
	brd.PoolHashRate = job.NewPoolStats()
	brd.Enabled = true
	my.InitBoard(&brd)
	my.BoardChainCount = devhdr.MaxHashBoards
	my.BoardCount = devhdr.MaxHashBoards

	log.Info("Calling DVFS InitialSetup")
	my.SystemDVFS.InitialSetup()
	log.Info("DVFS InitialSetup complete")

	log.Info("Starting DVFS")
	go func() {
		// Initialize the DVFS.
		my.SystemDVFS.DVFS()
	}()

	for {
		if my.bExit {
			break
		}

		hasWork := false

		for i := uint(0); i <= my.BoardChainCount; i++ {
			dev, ok := my.BoardChainMap[i]
			if !ok {
				continue
			}
			if !dev.Enabled {
				continue
			}

			hasWork = dev.Run()
		}

		if !hasWork {
			var SleepForJob time.Duration = 40 * time.Millisecond
			time.Sleep(SleepForJob)
		}
	}
	return nil
}

func (my *DeviceManager) GetResult() (*job.Job, *job.JobResult) {

	for i := uint(0); i <= my.BoardChainCount; i++ {
		dev, ok := my.BoardChainMap[i]
		if !ok {
			continue
		}
		if !dev.Enabled {
			continue
		}
		j, r := dev.HWJobs.GetResultNJob()
		if r == nil {
			continue
		}

		if j == nil {
			// ignore hash update message for Stale jobs
			if r.HWCtxID != chip.SEQ_HASHRATE_UPDATE {
				seq := int(dev.HWJobs.ID)
				log.Infof("Stale (dev=%d): Can't find Job for Result %+v, current Seq %v", dev.ID, r, seq)
				dev.HStats.Stale++
			}
			return nil, r
		}

		log.Debugf("Scan result Job ID %v, HW ID %d, nJobs %d, nResults %d", j.JobID, j.HWCtxID, dev.HWJobs.nJobTotal, dev.HWJobs.nResultTotal)
		return j, r
	}

	return nil, nil
}

func (my *DeviceManager) AddJob(J *job.Job) (int, error) {
	nClear := 0
	for i := uint(0); i <= my.BoardChainCount; i++ {
		dev, ok := my.BoardChainMap[i]
		if !ok {
			continue
		}
		if !dev.Enabled {
			continue
		}
		if J.CleanJobs {
			nClear = dev.HWJobs.ClearAndCancelJobs()

			log.Debugf("Board %d: Scan nClear %d", dev.ID, nClear)
		}

		dev.HWJobs.AddJob(*J)
	}

	return nClear, nil
}

func (my *DeviceManager) UpdateDiffs(J *job.Job) {
	dev, ok := my.BoardChainMap[J.DevID]
	if !ok {
		return
	}
	if !dev.Enabled {
		return
	}
	job.UpdateDiffs(&dev.DStats, float64(J.DiffTarget), J.DevDiff, dev.Uptime())
	log.Debugf("Dev[%d] DiffStats %+v", J.DevID, dev.DStats)
	job.UpdateGetwork(&dev.GStats, J.GetworkTDiff, J.LastGetworkTS, 0)
	log.Debugf("Dev[%d] GetworkStats %+v", J.DevID, dev.GStats)
	log.Debugf("nJobs %d, nResults %d", dev.HWJobs.nJobTotal, dev.HWJobs.nResultTotal)
}

func (my *DeviceManager) UpdateShares(bAccepted bool, J *job.Job, tsInSec float64) {
	dev, ok := my.BoardChainMap[J.DevID]
	if !ok {
		return
	}
	if !dev.Enabled {
		return
	}
	dt := job.DataPoint{
		Timestamp: time.Now(),
		Value:     J.DiffTarget,
	}

	job.UpdateShares(&dev.SStats, bAccepted, J.DiffValidate, tsInSec)
	dev.SStats.LastSharePool = J.PoolID
	dev.PoolHashRate.UpdateHashRate(dt, dev.ID)
	log.Debugf("Dev[%d] ShareStats %+v", J.DevID, dev.SStats)
	log.Debugf("nJobs %d, nResults %d", dev.HWJobs.nJobTotal, dev.HWJobs.nResultTotal)

	chip, ok2 := dev.ChipMap[J.ChipID]
	if !ok2 {
		return
	}
	job.UpdateShares(&chip.SStats, bAccepted, J.DiffValidate, tsInSec)
	chip.SStats.LastSharePool = J.PoolID
}

func (my *DeviceManager) UpdateUtility(J *job.Job) {
	dev, ok := my.BoardChainMap[J.DevID]
	if !ok {
		return
	}
	if !dev.Enabled {
		return
	}
	job.UpdateUtility(&dev.SStats, dev.Uptime())
	log.Debugf("Dev[%d] Utility %f", J.DevID, dev.SStats.Utility)
	log.Debugf("nJobs %d, nResults %d", dev.HWJobs.nJobTotal, dev.HWJobs.nResultTotal)

	chip, ok2 := dev.ChipMap[J.ChipID]
	if !ok2 {
		return
	}
	job.UpdateUtility(&chip.SStats, dev.Uptime())
}

func DiffToHWDiff(x uint) uint {
	y0 := math.Log2(float64(x))
	y := uint(y0)

	y += 32 // adjust for the nonce space of 4G
	return y
}

func HWDiffToDiff(x uint) uint {
	if x < 32 {
		x = 32
	}
	x -= 32
	return uint(math.Pow(2, float64(x)))
}
