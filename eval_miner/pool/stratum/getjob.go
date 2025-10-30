package stratum

import (
	"eval_miner/job"
	log "eval_miner/log"
	"eval_miner/util"
)

/*
	{
		"id":null,
		"method":"mining.notify",
		"params":[
				JobID			"a5238779",
				PrevHash		"628b64fc86b9e25b685f59ff51c710b9647c7829000813a10000000000000000",
				CoinB1			"01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff4a0323020bfabe6d6d771a8102e0c332ae320175834693ce57a55baed434a79f39b28bb96d124ce5dc0100000000000000",
				CoinB2			"798723a52f736c7573682f0000000003a8349e25000000001976a9147c154ed1dc59609e3d26abb2df2ea3d587cd8c4188ac00000000000000002c6a4c2952534b424c4f434b3ab66456838ff33dfe0e57c433efe3a0bb1447083cd169b814b488c927003dd5f70000000000000000266a24aa21a9edba789d707151f0f942e9c081110cd9f0eb2b9ef3ee7ea90898e5c9ae9618886f00000000",
				MerkleBranch	["40c7958d748141f8032205e8a419ac36c3e259b3484de8c3849bc40f8d8afc0e",
								"a4ad6fdeb0bf0b253d676b9468ff4395d79ebe34c136af2d0a9482b7a442a2d7",
								"7785f146a32e19b1adf95aa72c7c524a1e8b172b39c207a9a8476a7585b40933",
								"9fddda0e230195f75e8840d36c985ea742dc9b11b043a9f42c03312a74f3c7e9",
								"9afacd357a980d464a8aa70092b8ecf87bb4d778a457fdaf3a6a022c30b8cc0c",
								"12e330035152a7c9a07e8515f262a830e903ce22cf07f5beea29ad5ed56d98ff",
								"656a0ded98c3754ec328a25e23c73214a0820658fa31c1912bbbc7204e0a123e",
								"356568023883785a5f1658f896408e2c9dcf89b196017bc1a105f864705f715c",
								"0730fd58e9a025189af42622a335eb1855bcce199e2eeae36f99b7ae6bd3e1e1",
								"f4088d42bfb225f8a5294573ee40dde2685416c3a17ce50f5c6c9a17519f3809",
								"c33b59ca9cfe6f3d0afa7f3601141eb96294a900c22c394a1fb278aad6099d24"],
				Version			"20000004",
				NBits			"170a9080",
				NTime			"61fa0f58",
				CleanJobs		true
					]
	}
*/
func (my *Stratum) handleNotifyMethod(Params []interface{}) {
	i := 0
	for i < len(Params) {
		log.Debugf("Params[%d] %v", i, Params[i])
		i++
	}

	if len(Params) < 9 {
		log.Debug("len(Params) < 9")
		return
	}

	a, ok := Params[4].([]interface{})
	if !ok {
		log.Debug("Not able to convert MerkleBranch")
		return
	}

	job := job.Job{
		JobID:               util.ToString(Params[0]),
		PrevHashStratum:     util.ToString(Params[1]),
		CoinB1Stratum:       util.ToString(Params[2]),
		CoinB2Stratum:       util.ToString(Params[3]),
		MerkleBranchStratum: util.ToStringArray(a),
		VersionStratum:      util.ToString(Params[5]),
		NBitsStratum:        util.ToString(Params[6]),
		NTimeStratum:        util.ToString(Params[7]),
		CleanJobs:           util.ToBool(Params[8]),
	}

	/*
		Previous block hash inside "mining.notify" is 8 x 4-Byte-string-BE in LE overall. Each uint32 is BE that needs to be swapped.
		852ab3acf6baeb51e883cc88f49ef03ae17ed8110009a5fb0000000000000000 -> 852ab3ac_f6baeb51_e883cc88_f49ef03a_e17ed811_0009a5fb_00000000_00000000
		00000000_00000000_0009a5fb_e17ed811_f49ef03a_e883cc88_f6baeb51_852ab3ac is 00000000000000000009a5fbe17ed811f49ef03ae883cc88f6baeb51852ab3ac which is block 672486
	*/
	job.PrevHashLE = util.SwapBytes(job.PrevHashStratum)

	if job.CleanJobs || my.bClearJobQ {
		nClear := my.JobQ.ClearQ()
		my.StatsFunc.DiscardFunc(nClear)
		log.Infof("clear %d job from jobQ for new job %s", nClear, job.JobID)
		job.CleanJobs = true
	}

	my.JobQ.Enqueue(job)
	log.Debugf("add job %s", job.JobID)
}

func (my *Stratum) GetJob() *job.Job {
	// get a job from jobQ
	var j *job.Job
	j, err := my.JobQ.Dequeue()
	if err != nil {
		return nil
	}

	log.Debugf("working on job %v", j)

	if my.VersionRolling {
		j.VersionRolling = true
		j.ServerMask = my.ServerMask
	}

	log.Debugf("MerkleTree %s", j.MerkleTree)
	j.DiffTarget = my.Difficulty

	j.ExtraNonce1 = my.ExtraNonce1
	j.ExtraNonce2Size = my.ExtraNonce2Size
	j.BlockHeight()

	return j
}
