package job

import (
	"eval_miner/log"
	"eval_miner/util"
	"sync"
)

type ShareEntry struct {
	ID        uint64
	J         Job
	R         JobResult
	ts        float64
	Submitted bool
	Accepted  bool
	n         int
}

type Share struct {
	mx       sync.Mutex
	shmap    map[uint64]*ShareEntry
	staleTTL float64
}

func (my *Share) Add(ID uint64, j *Job, r JobResult, submitted bool) {
	my.mx.Lock()
	defer my.mx.Unlock()

	entry, ok := my.shmap[ID]
	if ok {
		entry.n++
		log.Infof("ID %d exists, %d'th try", ID, entry.n)
		if submitted {
			entry.Submitted = true
		}
	} else {
		newentry := ShareEntry{
			ID:        ID,
			J:         *j,
			R:         r,
			ts:        util.NowInSec(),
			Submitted: submitted,
			n:         1,
		}
		// save a copy of the original Job in case it is being cleared from a new mining.notify method
		newentry.J.ClearJobContext()

		my.shmap[ID] = &newentry
	}
}

func (my *Share) remove(ID uint64) *Job {
	se, ok := my.shmap[ID]
	if !ok {
		log.Errorf("ID %d not exist in Share map", ID)
		return nil
	}

	delete(my.shmap, ID)
	return &se.J
}

func (my *Share) Remove(ID uint64) *Job {
	my.mx.Lock()
	defer my.mx.Unlock()

	return my.remove(ID)
}

func (my *Share) RemoveStale() (int, int) {
	my.mx.Lock()
	defer my.mx.Unlock()

	ts := util.NowInSec()
	nStale := 0
	nRemoteFailure := 0

	for k, v := range my.shmap {
		if v.ts+my.staleTTL < ts {
			my.remove(k)
			nStale++
			if !v.Submitted {
				nRemoteFailure++
			}
		}
	}
	return nStale, nRemoteFailure
}

func (my *Share) Scan4Resubmit() *ShareEntry {
	my.mx.Lock()
	defer my.mx.Unlock()

	for _, v := range my.shmap {
		if !v.Submitted {
			return v
		}
	}
	return nil
}

func (my *Share) Init() {
	my.shmap = make(map[uint64]*ShareEntry)

	my.staleTTL = 120.0 //2 minutes
}
