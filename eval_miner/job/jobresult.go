package job

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"

	"eval_miner/block"
	"eval_miner/log"
	"eval_miner/util"
)

type JobResult struct {
	HWCtxID      uint
	DevID        uint
	Nonce2       string
	Nonce        string
	NTime        string
	VersionBits  string
	DiffSubmit   uint64
	HashCount    uint64
	GenHashCount uint64
	HashVal      *big.Int
}

func (r *JobResult) IsDuplicate(r2 *JobResult) bool {

	return r.Nonce == r2.Nonce && r.Nonce2 == r2.Nonce2 && r.NTime == r2.NTime
}

func (r *JobResult) CalcBlockHeaderHash(j *Job) [32]byte {
	CoinBaseSum2 := block.CalcCoinBaseHash(j.CoinB1Stratum, j.ExtraNonce1, r.Nonce2, j.CoinB2Stratum)
	log.Debugf("CoinBaseSum2: %x", CoinBaseSum2)

	MRH := block.CalcMerkleRootHash(CoinBaseSum2[:], j.MerkleBranchStratum)
	log.Debugf("MRH %x", MRH)

	// validate the block before submit
	// prevH and MRH need to be in little endian
	bh := block.GetBlockHeader(j.NewVersion, j.PrevHashLE, hex.EncodeToString(MRH), util.BEHexToUint32(r.NTime), util.BEHexToUint32(j.NBitsStratum), util.BEHexToUint32(r.Nonce))
	bhBytes := block.BlockHeader2Byte(bh)
	log.Debugf("New BH %x", bhBytes)
	sum := sha256.Sum256(bhBytes)
	sum2 := sha256.Sum256([]byte(sum[:]))

	return sum2
}

func (r *JobResult) BlockHeaderHashBEStr(j *Job) string {
	if r.Nonce == "" {
		r.Nonce = "00000000"
	}
	if r.Nonce2 == "" {
		r.Nonce2 = util.ZeroHexString(int(j.ExtraNonce2Size))
	}

	sum2 := r.CalcBlockHeaderHash(j)
	sum2_swap := block.SwapByteInSHA256([]byte(sum2[:]))
	BEStr := fmt.Sprintf("%x", sum2_swap)
	return BEStr
}

func (r *JobResult) CalcDifficulty(j *Job) uint64 {
	// block header hash in little endian, e.g. f62bfbe47456d97ed86a9f771ffcdd34590aea7dce7506000000000000000000
	sum2 := r.CalcBlockHeaderHash(j)

	// block header hash in big endian, e.g. 0000000000000000000675ce7dea0a5934ddfc1f779f6ad87ed95674e4fb2bf6
	sum2_swap := block.SwapByteInSHA256([]byte(sum2[:]))
	log.Debugf("New BH Hash %x", sum2_swap)
	r.HashVal = new(big.Int)
	r.HashVal = r.HashVal.SetBytes(sum2_swap)
	diff := block.CalcDifficulty(sum2)
	return diff
}

func (r *JobResult) Validate(j *Job) uint64 {
	if r.NTime == "" {
		return 0
	}
	if r.Nonce == "" {
		return 0
	}
	if r.Nonce2 == "" {
		r.Nonce2 = util.ZeroHexString(int(j.ExtraNonce2Size))
	}

	r.DiffSubmit = r.CalcDifficulty(j)
	// log.Infof("Job Target Difficulty %d, Submit Difficulty %d", j.DiffTarget, j.DiffSubmit)
	// ts := time.Unix(int64(util.BEHexToUint32(r.NTime)), 0)
	// log.Infof("Time: %v", ts)

	return r.DiffSubmit
}
