package device

import (
	"eval_miner/device/chip"
	"eval_miner/job"
	"eval_miner/log"
	"eval_miner/util"
	"sync"
)

type HWJob struct {
	ID           uint
	IDMin        uint
	IDMax        uint
	Jobs         map[uint]*job.Job
	mx           *sync.Mutex
	Results      []job.JobResult
	staleTTL     float64
	nJobTotal    int
	nResultTotal int
}

func (my *HWJob) AddJob(j job.Job) {
	my.mx.Lock()
	defer my.mx.Unlock()

	j.HWCtxID = my.GetID()
	j.ScanJobTS = util.NowInSec()

	v, ok := my.Jobs[j.HWCtxID]
	if ok {
		log.Errorf("HWjob dup entry %+v", v)
	}

	my.Jobs[j.HWCtxID] = &j
	my.nJobTotal++

	log.Debugf("Add Job %s, HW ID %d", j.JobID, j.HWCtxID)
}

func (my *HWJob) GetJob() *job.Job {
	my.mx.Lock()
	defer my.mx.Unlock()

	for _, v := range my.Jobs {
		if !v.Scanning {
			v.Scanning = true

			log.Infof("Get Job %s, HW ID %d", v.JobID, v.HWCtxID)

			return v
		}
	}

	return nil
}

func (my *HWJob) RemoveStaleJob() int {
	my.mx.Lock()
	defer my.mx.Unlock()

	ts := util.NowInSec()
	nStale := 0

	for k, v := range my.Jobs {
		if v.ScanJobTS+my.staleTTL < ts {
			nStale++
			delete(my.Jobs, k)
			log.Infof("Remove job %s, HW ID %d, Q len %v", v.JobID, k, len(my.Jobs))
		}
	}

	return nStale
}

func (my *HWJob) findJob(HWCtxID uint) *job.Job {
	if HWCtxID == chip.SEQ_HASHRATE_UPDATE {
		return nil
	}
	for _, j := range my.Jobs {
		if j.HWCtxID == HWCtxID {
			return j
		}
	}
	log.Infof("Stale job for HWCtxID %d", HWCtxID)
	return nil
}

func (my *HWJob) FindJobWithLock(HWCtxID uint) *job.Job {
	my.mx.Lock()
	defer my.mx.Unlock()

	return my.findJob(HWCtxID)
}

func (my *HWJob) GetResultNJob() (*job.Job, *job.JobResult) {
	my.mx.Lock()
	defer my.mx.Unlock()

	var r *job.JobResult

	if len(my.Results) == 0 {
		return nil, nil
	}

	r = &my.Results[0]
	my.Results = my.Results[1:]

	j := my.findJob(r.HWCtxID)
	if j == nil {
		// This error log is moved outside together with HWError++
		return nil, r
	}

	j.JobResultTS = util.NowInSec()

	return j, r
}

func (my *HWJob) AddResult(r *job.JobResult) {
	my.mx.Lock()
	defer my.mx.Unlock()

	my.Results = append(my.Results, *r)
	my.nResultTotal++

	log.Debugf("Add Result %d", r.HWCtxID)
}

func (my *HWJob) GetID() uint {
	if my.ID < my.IDMin {
		my.ID = my.IDMin
	}

	if my.ID > my.IDMax {
		my.ID = my.IDMin
	}

	ID := my.ID
	my.ID++

	return ID
}

func (my *HWJob) Init(idmin uint, idmax uint) {
	*my = HWJob{
		IDMin:    idmin,
		IDMax:    idmax,
		ID:       idmin,
		staleTTL: 120.0,
	}

	my.Jobs = make(map[uint]*job.Job)

	my.mx = &sync.Mutex{}
}

const (
	lazyDelete = false
)

func (my *HWJob) ClearAndCancelJobs() int {
	my.mx.Lock()
	defer my.mx.Unlock()

	n := 0

	for k, v := range my.Jobs {
		if v.CtxCancel != nil {
			log.Infof("Cancel job %s, HW ID %d", v.JobID, k)
			v.CtxCancel()
			v.CtxCancel = nil
		}

		if !lazyDelete {
			delete(my.Jobs, k)
			log.Debugf("Clear job %s, HW ID %d, Q len %v", v.JobID, k, len(my.Jobs))
		}
		n++
	}

	return n
}
