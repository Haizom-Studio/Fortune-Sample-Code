package device

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"eval_miner/block"
	"eval_miner/device/asic"
	ac "eval_miner/device/asiccommon"
	"eval_miner/device/chip"
	"eval_miner/device/devhdr"
	"eval_miner/job"
	"eval_miner/log"
	"eval_miner/util"
)

const (
	MAX_TRUE_HIT = 64 * 1024
	MAX_GEN_HIT  = 64 * 1024
)

var (
	asicBoard = Device{
		Name:              "ASICBoard",
		Kernel:            "go",
		Path:              "./",
		Driver:            "ASICBoard",
		Enabled:           true,
		Status:            STATUS_ALIVE,
		UpSince:           util.NowInSec(),
		DiffMin:           devhdr.DiffMin,
		DiffMax:           devhdr.DiffMax,
		HStats:            job.HashStats{UpdateTS: util.NowInSec()},
		GeneralHitStats:   job.HashStats{UpdateTS: util.NowInSec()},
		SStats:            job.ShareStats{LastShareUpdateTS: 0.0},
		VersionRollingSim: false,
		TimeRollingSim:    false,
		PreScan:           ASICBoardPreScan,
		Scan:              ASICBoardScan,
		PollResult:        ASICBoardPollResult,
		ChipPerBoard:      0,
		DetectBoard:       ASICBoardDetection,
	}
)

func ASICBoardPreScan(my *Device, j *job.Job) {
	//  Calculate device diff for the job

	if j.DiffTarget < my.DiffMax {
		j.DevDiff = util.MIN(my.DiffMax, util.ClosestPowerOf2(j.DiffTarget))
	} else {
		j.DevDiff = util.MAX(my.DiffMax, util.ClosestPowerOf2(j.DiffTarget))
	}
	j.DevDiff = util.MAX(my.DiffMin, j.DevDiff)
	j.HWDiff = DiffToHWDiff(uint(j.DevDiff))
	j.DevDiff = uint64(HWDiffToDiff(j.HWDiff))

	j.NewVersion = util.BEHexToUint32(j.VersionStratum)
	NewVersionStrLE := util.SwapBytes(j.VersionStratum)

	NTimeLE := util.SwapBytes(j.NTimeStratum)
	j.NTimeDiff = 0

	NBitsLE := util.SwapBytes(j.NBitsStratum)

	var nRand uint64 = uint64(my.ID)
	Nonce2 := util.HexStringFromNumber(int(j.ExtraNonce2Size), nRand)
	j.ExtraNonce2 = Nonce2
	CoinBaseSum2 := block.CalcCoinBaseHash(j.CoinB1Stratum, j.ExtraNonce1, Nonce2, j.CoinB2Stratum)
	log.Debugf("CoinBaseSum2: %x", CoinBaseSum2)

	MRH := block.CalcMerkleRootHash(CoinBaseSum2[:], j.MerkleBranchStratum)
	log.Debugf("MRH %x", MRH)

	// prepare the job arguments for hwminers
	j.BlockHeaderStr = NewVersionStrLE + j.PrevHashLE + hex.EncodeToString(MRH) + NTimeLE + NBitsLE + "00000000"
	log.Debugf("BlockHeader %s", j.BlockHeaderStr)
}

var ErrAsicNotExist = errors.New("ErrAsicNotExist")

func ASICBoardScan(my *Device, j *job.Job) error {
	if my.Asic == nil {
		return ErrAsicNotExist
	}

	// Send Job to the JobQ
	msg := chip.Message{
		Seq:         j.HWCtxID,
		Diff:        j.HWDiff,
		Board:       j.DevID,
		Body:        j.BlockHeaderStr,
		VersionMask: util.BEHexToUint32(j.ServerMask),
	}

	err := my.Asic.SendJob(&msg)
	if err != nil {
		log.Infof("Scan error: %s", err)
	}
	return err
}

func ASICBoardPollResult(my *Device, findJobFn FindJobFunc) error {

	if my.Asic == nil {
		return ErrAsicNotExist
	}

	msg, err := my.Asic.CheckResults()

	if err != nil {
		return err
	}

	if msg == nil {
		return ErrNoResultYet
	}

	// this is a hashrate update message
	if msg.Seq == chip.SEQ_HASHRATE_UPDATE {

		HashCount := uint64(0)
		GenHashCount := uint64(0)
		// update chip and board hash rates
		ts := util.NowInSec()

		for i := 0; i < chip.CHIP_MAX; i++ {
			chipTrueHit := msg.TrueHit[i]
			chipHashCount := uint64(0)
			if chipTrueHit > MAX_TRUE_HIT {
				chipHashCount = 1 * 0x100000000
			} else {
				chipHashCount = uint64(chipTrueHit * 0x100000000)
			}
			HashCount += chipHashCount

			chipGenHit := msg.GenHit[i]
			chipGenHashCount := uint64(0)
			if chipGenHit > MAX_TRUE_HIT {
				chipGenHashCount = 1 * 0x100000000
			} else {
				chipGenHashCount = uint64(chipGenHit * 0x100000000)
			}
			GenHashCount += chipGenHashCount

			// per chip update
			my.UpdateChipHashes(chipHashCount, chipGenHashCount, msg.HitRate[i], uint(i), ts)
		}

		if GenHashCount < HashCount {
			GenHashCount = HashCount
		}

		my.UpdateHashes(HashCount, GenHashCount, ts)
		// update pool
		r := job.JobResult{
			HWCtxID:      chip.SEQ_HASHRATE_UPDATE,
			GenHashCount: GenHashCount,
			HashCount:    HashCount,
		}

		my.HWJobs.AddResult(&r)

		return nil
	}

	j := findJobFn(msg.Seq)
	if j == nil {
		my.HStats.Stale++
		return ErrJobNotExist
	}

	log.Debugf("PollResult, Job ID %v, msg: &{Seq:%v Diff:%v Chip:%v Engine:%v Board:%v Body:%v}",
		j.JobID, msg.Seq, msg.Diff, msg.Chip, msg.Engine, msg.Board, msg.Body)

	// BHStr to BH
	bhBytes, _ := hex.DecodeString(msg.Body)
	bh, err := block.Byte2BlockHeader(bhBytes)
	if err != nil {
		log.Error("can't decode bh")
		return err
	}

	log.Debugf("bh hex: %x", bh)

	// gather results
	r := job.JobResult{
		Nonce2: j.ExtraNonce2,
		Nonce:  fmt.Sprintf("%08x", bh.Nonce),
		NTime:  fmt.Sprintf("%08x", bh.Time),
	}

	if j.VersionRolling {
		ServerMaskUint32 := util.BEHexToUint32(j.ServerMask)
		NewVersionUint32 := bh.Version
		NewVersionStr := fmt.Sprintf("%08x", NewVersionUint32)
		NewVersionStrLE := util.SwapBytes(NewVersionStr)
		log.Debugf("j.Version %s, NewVersionStr %s, NewVersionStrLE %s",
			j.VersionStratum, NewVersionStr, NewVersionStrLE)
		VersionBits := NewVersionUint32 & ServerMaskUint32

		j.VersionBits = fmt.Sprintf("%08x", VersionBits)
		log.Debugf("j.VersionBits %s", j.VersionBits)
		r.VersionBits = j.VersionBits
		j.NewVersion = NewVersionUint32
	} else {
		// nothing to do
		// j.NewVersion = util.BEHexToUint32(j.VersionStratum)
		NewVersionStr := fmt.Sprintf("%08x", j.NewVersion)
		log.Debugf("j.Version %s, j.NewVersion %s, j.VersionBits %s",
			j.VersionStratum, NewVersionStr, j.VersionBits)
	}

	// this block is for logging the NTimeDiff in submit log
	{
		OldTime := util.BEHexToUint32(j.NTimeStratum)
		NewTime := bh.Time
		j.NewNTime = fmt.Sprintf("%08x", NewTime)
		j.NTimeDiff = NewTime - OldTime
	}

	j.ChipID = msg.Chip

	err = my.PostScan(j.HWCtxID, &r)
	if err != nil {
		if err == ErrDuplicateResult {
			log.Infof("Duplicate Result: %+v", msg)
		} else {
			log.Debug(err)
		}
	}

	return nil
}

func ASICBoardDetection(my *Device) ([]uint8, error) {
	uartName := "/dev/ttyS" + strconv.Itoa(int(my.SlotId))
	my.Asic = nil
	aa, err := asic.AsicDetect(my.ID, uint(my.SlotId), uartName)
	if err != nil {
		return nil, err
	}
	my.Asic = aa
	return aa.AsicIDs, nil
}

func GetSystemDVFS() ac.SystemDVFS {
	return asic.NewSystemDVFS()
}
