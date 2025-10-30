package job

import (
	"errors"
	log "eval_miner/log"
	"eval_miner/util"
	"sync"
)

type JobQ struct {
	queue   []Job
	mx      sync.Mutex
	Created int
}

func (q *JobQ) Enqueue(j Job) {
	q.mx.Lock()
	defer q.mx.Unlock()
	j.NotifyJobTS = util.NowInSec()
	q.queue = append(q.queue, j) // Enqueue
	q.Created++
}

var ErrEmptyJobQ = errors.New("empty jobQ")

func (q *JobQ) Dequeue() (*Job, error) {
	q.mx.Lock()
	defer q.mx.Unlock()

	var j *Job
	var err error

	if len(q.queue) > 0 {
		log.Debugf("%v", q.queue[0])
		j = &q.queue[0]
		q.queue = q.queue[1:]
		err = nil
	} else {
		j = nil
		err = ErrEmptyJobQ
	}

	return j, err
}

func (q *JobQ) ClearQ() int {
	q.mx.Lock()
	defer q.mx.Unlock()

	n := len(q.queue)

	q.queue = nil
	return n
}

func (q *JobQ) Len() int {
	q.mx.Lock()
	defer q.mx.Unlock()

	n := len(q.queue)

	return n
}
