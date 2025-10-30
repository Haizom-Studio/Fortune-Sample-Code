package stratum

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"eval_miner/config"
	"eval_miner/device"
	"eval_miner/device/chip"
	"eval_miner/job"
	"eval_miner/jsonrpc"
	"eval_miner/log"
	"eval_miner/util"
)

const (
	STATE_INIT = iota
	STATE_CONFIGURE_SENT
	STATE_CONFIGURE_DONE
	STATE_SUBSCRIBE_SENT
	STATE_SUBSCRIBE_DONE
	STATE_XNSUB_SENT
	STATE_XNSUB_DONE
	STATE_AUTHORIZE_SENT
	STATE_AUTHORIZE_DONE
	STATE_GETJOB
	STATE_DISCONNECT
)

const (
	ConfigureTimeout          time.Duration = 5 * time.Second
	SleepForJob               time.Duration = 100 * time.Millisecond
	WaitForJob                float64       = 90.0
	StatsUpdateTime           float64       = 5.0
	MAX_RETRY                               = 3
	DefaultVersionRollingMask string        = "1fffe000"
	DefaultMinimumDifficulty  uint64        = 512
	DefaultVersionRollingBits uint          = 2
)

const DiffTarget string = "00000000FFFF0000000000000000000000000000000000000000000000000000"

// ExtraNonce1. - Hex-encoded, per-connection unique string which will be used for creating generation transactions later.
// ExtraNonce2_size. - The number of bytes that the miner users for its ExtraNonce2 counter.
type Stratum struct {
	Cfg                  *config.PoolEntryConfig
	Client               *jsonrpc.Client
	Retry                int
	State                uint32
	Difficulty           uint64
	ServerMask           string
	SubscribeID          string
	ExtraNonce1          string
	ExtraNonce2Size      uint
	JobQ                 job.JobQ
	bClearJobQ           bool
	VersionRolling       bool
	ConfigureTS          time.Time
	Shares               job.Share
	StatsFunc            job.StatsFunc
	DevFunc              device.DevFunc
	bExit                bool
	bMinimumDifficulty   bool
	bSubscribeExtraNonce bool
	bXnSub               bool
	bPoolExit            *bool
}

func (my *Stratum) handleError(err error) error {
	if err != nil {
		log.Error(err)
	}
	return err
}

func (my *Stratum) handleMethod(Method string, Params []interface{}) {
	switch Method {
	case "mining.set_version_mask":
		//log.Debugf("params[0] %s", Params[0])
		my.ServerMask = util.ToString(Params[0])
		log.Debugf("New ServerMask %s", my.ServerMask)
		my.VersionRolling = true
	case "mining.set_difficulty":
		intVal, err := ToDiff(Params[0])
		if err == nil {
			my.Difficulty = uint64(intVal)
			log.Infof("mining.set_difficulty: %d", my.Difficulty)
		} else {
			log.Infof("mining.set_difficulty: err %v, current target %d", err, my.Difficulty)
		}
	case "mining.set_extranonce":
		// {"id": null, "method": "mining.set_extranonce", "params": ["08000002", 4]}
		if len(Params) < 2 {
			log.Debug("len(Params) < 2")
			break
		}
		my.ExtraNonce1 = util.ToString(Params[0])
		log.Debugf("New ExtraNonce1 %s", my.ExtraNonce1)
	case "mining.notify":
		my.handleNotifyMethod(Params)
	case "client.reconnect":
		// parse reconnect hostname and port
		my.hanleClientReconnect(Params)
	default:
		log.Infof("unknown method %s", Method)
	}
}

func (my *Stratum) handleResponse(resp jsonrpc.Response) {

	log.Debugf("ID %d", resp.ID)

	if len(resp.Method) > 0 {
		log.Debugf("method %s params %v", resp.Method, resp.Params)
		my.handleMethod(resp.Method, resp.Params)
		return
	}

	switch my.State {
	case STATE_CONFIGURE_SENT:
		my.ConfigureResult(resp)
		my.State = STATE_CONFIGURE_DONE
	case STATE_SUBSCRIBE_SENT:
		my.SubscribeResult(resp)
		my.State = STATE_SUBSCRIBE_DONE
	case STATE_XNSUB_SENT:
		my.XnSubResult(resp)
		my.State = STATE_XNSUB_DONE
	case STATE_AUTHORIZE_SENT:
		my.AuthorizeResult(resp)
		my.State = STATE_GETJOB
	case STATE_GETJOB:
		my.SubmitResult(resp)
	default:
		log.Debugf("unknown response %d %v", my.State, resp)
	}
}

func (my *Stratum) Stop() {
	my.bExit = true
}

func (my *Stratum) Start() error {
	my.bExit = false
	my.State = STATE_INIT
	return my.Run()
}

var ErrUnreachable = errors.New("Unreachable")
var ErrNoJob = errors.New("NoJob")

func (my *Stratum) Run() error {
	var err error = nil
	tstart := 0.0
	tend := 0.0
	tdiff := 0.0
	// thashstats := 0.0
	jobsDiffTarget := new(big.Int)
	jobsDiffTarget, ok := jobsDiffTarget.SetString(DiffTarget, 16)
	if !ok {
		log.Errorf("failed to convert to valid uint256 %v", ok)
		return fmt.Errorf("failed to convert to valid uint256")
	}

	for {
		if err != nil {
			my.bExit = true
		}

		if my.bPoolExit != nil && *my.bPoolExit {
			my.bExit = true
		}

		if my.Client == nil {
			if my.Retry >= MAX_RETRY {
				err = ErrUnreachable
				my.bExit = true
				break
			}
			time.Sleep(5 * time.Second)
			_, err2 := my.Dial()
			if err2 != nil {
				log.Debugf("Dial Err %v", err2)
			}
			my.Retry++
			continue
		} else {
			my.Retry = 0
		}

		if my.bExit {
			nClear := my.JobQ.ClearQ()
			my.StatsFunc.DiscardFunc(nClear)
			my.Client.Stop()
			my.Client = nil
			break
		}

		/*
			Rx Handler
			Capture the signal before the state machine
		*/
		select {
		case msg := <-my.Client.Rxchan:
			log.Infof("rxchan err %v", msg)
			my.State = STATE_DISCONNECT
		default:
		}

		/* Tx Handler */
		switch my.State {
		case STATE_INIT:
			err = my.Configure()
			my.State = STATE_CONFIGURE_SENT
			my.ConfigureTS = time.Now()
		case STATE_CONFIGURE_SENT:
			dt := time.Since(my.ConfigureTS)
			if dt >= ConfigureTimeout {
				// this means the pool does not support configure method
				my.State = STATE_CONFIGURE_DONE
			}
		case STATE_CONFIGURE_DONE:
			err = my.Subscribe()
			my.State = STATE_SUBSCRIBE_SENT
		case STATE_SUBSCRIBE_DONE:
			if !my.bXnSub {
				my.State = STATE_XNSUB_DONE
			} else {
				err = my.XnSub()
				my.State = STATE_XNSUB_SENT
			}
		case STATE_XNSUB_DONE:
			err = my.Authorize()
			my.State = STATE_AUTHORIZE_SENT
			/*
				First tstart set from authorize in case we do not ever receive a job
				Happened with stratum+tcp://btc.viabtc.io:25
			*/
			tstart = util.NowInSec()
		case STATE_GETJOB:
			/*
				Scan for share map for resubmit and remove stale entries
				Resubmit 1 share at a time considering this should be a low freq event
			*/
			se := my.Shares.Scan4Resubmit()
			if se != nil {
				log.Infof("Share: resubmit for share entry ID: %d job: %+v", se.ID, se.J)
				err = my.Submit(&se.J, se.R, int(se.ID))
			}

			nStale, nRemoteFailure := my.Shares.RemoveStale()
			if nStale > 0 {
				log.Infof("Share: Stale %d, RemoteFailure %d", nStale, nRemoteFailure)
				my.StatsFunc.RemoteFailureFunc(nRemoteFailure)
			}

			/*
				Pull Device Mgr for job results
				Submit new job results
			*/
			for {
				devj, devjr := my.DevFunc.GetResult()
				if devj != nil && devjr != nil {
					target := new(big.Int).Div(jobsDiffTarget, big.NewInt(int64(devj.DiffTarget)))
					ret := devjr.HashVal.Cmp(target)
					if ret <= 0 {
						err = my.Submit(devj, *devjr, -1)
						log.Debugf("hash   %v", devjr.HashVal)
						log.Debugf("target %v", target)
						log.Debugf("nonce %v Job ID %v", devjr.Nonce, devj.JobID)
					}
				} else {
					if devjr != nil {
						// this is a hash update message
						if devjr.HWCtxID == chip.SEQ_HASHRATE_UPDATE {
							my.StatsFunc.HashFunc(devjr.HashCount, devjr.GenHashCount, nil)
						}
					}

					break
				}
			}

			/*
				Get new jobs from the pool
			*/
			J := my.GetJob()
			// Getwork stats will be updated inside updatediffs
			tend = util.NowInSec()
			tdiff = tend - tstart

			if tdiff >= WaitForJob {
				log.Infof("Pool %v idle (not getting jobs) for %v seconds, exiting", my.Client.Conn.RemoteAddr(), tdiff)
				err = ErrNoJob
				my.bExit = true
				break
			}

			if J == nil {
				log.Debugf("Nothing to work on")
				time.Sleep(SleepForJob)
				break
			}

			J.GetworkTDiff = tdiff
			/*
				ScanFunc inside device will assign the device in the job (Job.DevID)
				It is important for Stats Functions to run after Scan
			*/
			nClear, _ := my.DevFunc.AddJob(J)
			/*
				my.StatsFunc.DiscardFunc(nClear)
				Do not update stats here as what is being pushed to HW is not considered discarded.
				And the same pool Job can receive multiple results making stats hard to match.
			*/
			log.Debugf("Jobs cleared from device Scan %d", nClear)

			// Update diff stats and getwork stats
			my.StatsFunc.DiffFunc(J)

			// Getwork stats skipping scan functions
			tstart = util.NowInSec()
		case STATE_DISCONNECT:
			// server closed connection or some tx/rx error
			// proxy set ExtraNonce1 only
			// proxy set a new host:port
			my.bExit = true
		default:
			time.Sleep(SleepForJob)
		}
	}
	return err
}

func (my *Stratum) Dial() (*jsonrpc.Client, error) {
	if my.Client != nil {
		my.Client.Stop()
		my.Client = nil
	}

	c, err := jsonrpc.NewClient(my.Cfg.NetworkProto,
		my.Cfg.HostNPort,
		func(resp jsonrpc.Response, bytes job.ByteStats) error {
			my.StatsFunc.BytesFunc(true, bytes)
			my.handleResponse(resp)
			return nil
		})
	my.Client = c

	log.Infof("Dialing %s://%s Worker ID: %s", my.Cfg.Proto, my.Cfg.HostNPort, my.Cfg.User)

	return c, err
}

func NewClient(cfg config.PoolEntryConfig, devFunc device.DevFunc, statsFunc job.StatsFunc, bPoolExit *bool) (*Stratum, error) {

	s := Stratum{
		State:                STATE_INIT,
		ServerMask:           DefaultVersionRollingMask,
		Difficulty:           DefaultMinimumDifficulty,
		Cfg:                  &cfg,
		bClearJobQ:           true, /* BTC.com has no ClearJobs flag set in any of the jobs, resulting in stale jobs */
		StatsFunc:            statsFunc,
		DevFunc:              devFunc,
		bMinimumDifficulty:   false,
		bSubscribeExtraNonce: false,
		bXnSub:               cfg.XnSub,
		bPoolExit:            bPoolExit,
	}

	s.Shares.Init()

	var err error
	s.Client, err = s.Dial()

	return &s, err
}

func ToDiff(x interface{}) (uint, error) {
	strVal := fmt.Sprintf("%v", x)
	// val, err := strconv.ParseUint(strVal, 10, 64)
	val, err := strconv.ParseFloat(strVal, 64)
	return uint(val), err
}
