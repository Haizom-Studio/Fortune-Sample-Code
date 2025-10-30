package stratum

import (
	"eval_miner/job"
	"eval_miner/jsonrpc"
	log "eval_miner/log"
	"eval_miner/util"
)

func (my *Stratum) Submit(j *job.Job, r job.JobResult, seID int) error {
	var result interface{}

	method := "mining.submit"
	//params [user, jobid, Nonce2, ntime, Nonce, version_bits]
	params := []string{my.Cfg.User, j.JobID, r.Nonce2, r.NTime, r.Nonce}
	if my.VersionRolling {
		params = append(params, r.VersionBits)
	}

	ID, bytes, err := my.Client.Call(method, seID, params, &result)
	log.Infof("Submit: ID %v seID %v Job ID %s, HW ID %d, Nonce2 %s, NTime %s, NTimeDiff %d, Nonce %s, VersionBits %s, ExtraNonce1: %s, DiffTarget %d, DiffSubmit %d",
		ID, seID, j.JobID, j.HWCtxID, r.Nonce2, r.NTime, j.NTimeDiff, r.Nonce, r.VersionBits, my.ExtraNonce1, j.DiffTarget, r.DiffSubmit)
	log.Debugf("Job: %+v, Result: %+v", j, r)

	my.StatsFunc.BytesFunc(false, bytes)

	if err == nil {
		my.Shares.Add(ID, j, r, true)
	} else {
		my.Shares.Add(ID, j, r, false)
	}

	return my.handleError(err)
}

func (my *Stratum) SubmitResult(resp jsonrpc.Response) bool {
	j := my.Shares.Remove(resp.ID)

	bAccepted := false
	if resp.Result != nil {
		bAccepted = util.ToBool(resp.Result)
	}
	/*
		If we can't find the job in the share cache, we can't update stats properly.
	*/
	if j != nil {
		my.StatsFunc.ShareFunc(bAccepted, j)
		log.Infof("Submit Result: %v, Resp: %v, Job ID %s, HW ID %v", bAccepted, resp, j.JobID, j.HWCtxID)
		log.Debugf("Submit Result Job: %+v", j)
	} else {
		log.Infof("Submit Result: %v, Resp: %v, Job:<nil>", bAccepted, resp)
	}

	return bAccepted
}
