package pool

import (
	"errors"
	"eval_miner/config"
	"eval_miner/device"
	"eval_miner/job"
	"eval_miner/log"
	"eval_miner/predefine"
	"eval_miner/util"
	"reflect"
	"sync"
	"time"
)

type GetFunc func(arg PoolArg) *PoolData
type SaveConfigFunc func() []config.PoolEntryConfig

type PoolFunc struct {
	Get        GetFunc
	SaveConfig SaveConfigFunc
}

const (
	WaitForPool time.Duration = 100 * time.Millisecond
)

var (
	EmptyPoolFunc = PoolFunc{
		Get: func(arg PoolArg) *PoolData { return nil },
	}
)

type PoolManager struct {
	CurrentPoolIndex uint
	DevFunc          device.DevFunc
	Pools            []*PoolRuntime
	mx               sync.Mutex
	bExit            bool
	SeqNo            uint
}

func (my *PoolManager) Init(devFunc device.DevFunc, cfg config.MinerConfig) PoolFunc {
	my.SeqNo = 1

	my.DevFunc = devFunc

	poolFunc := PoolFunc{
		Get:        my.Get,
		SaveConfig: my.SaveConfig,
	}

	my.ApplyConfig(cfg.Pools)

	my.CurrentPoolIndex = config.MAX_POOL_NUMBER + 1000

	return poolFunc
}

func (my *PoolManager) RemoveAllPools() {
	my.mx.Lock()
	defer my.mx.Unlock()

	my.Pools = []*PoolRuntime{}
}

func (my *PoolManager) ApplyConfig(Pools []config.PoolEntryConfig) {
	my.RemoveAllPools()

	for k, v := range Pools {
		if k >= config.MAX_POOL_NUMBER {
			break
		}

		my.AddPool(v)
	}
}

func (my *PoolManager) SaveConfig() []config.PoolEntryConfig {
	my.mx.Lock()
	defer my.mx.Unlock()

	PoolCfg := []config.PoolEntryConfig{}

	for _, v := range my.Pools {
		entry := v.Cfg
		PoolCfg = append(PoolCfg, entry)
	}
	return PoolCfg
}

func (my *PoolManager) UpdatePools(pools []config.PoolEntryConfig, cmd int) (uint, int) {
	my.mx.Lock()
	hasLocalCfg := false
	if len(my.Pools) > 0 {
		hasLocalCfg = true
	}
	my.mx.Unlock()

	if hasLocalCfg && cmd == predefine.CMD_UPDATEZTPPOOLS {
		log.Debugf("has local config, skip updating ZTP pools")
		return 0, predefine.MSG_ZTP_CANNOT_OVERWRITE_LOCAL
	}

	// remove all current pools
	if len(pools) == 0 {
		log.Infof("Removing all pools")
		my.RemoveAllPools()
		return 0, cmd
	}

	Valid := false
	for k := range pools {
		pools[k].Parse()
		if pools[k].Valid {
			Valid = true
		}
	}

	if !Valid {
		log.Infof("invalid update pools details: %v", pools)
		return 0, predefine.MSG_INVALID_UPDATEPOOLS_DETAIL
	}

	my.mx.Lock()
	hasChange := false
	if len(pools) != len(my.Pools) {
		hasChange = true
	} else {
		for k := range pools {
			log.Debugf("pools[%d] %+v", k, pools[k])
			log.Debugf("my.Pools[%d] %+v", k, my.Pools[k].Cfg)
			if !reflect.DeepEqual(pools[k], my.Pools[k].Cfg) {
				hasChange = true
			}
		}
	}
	my.mx.Unlock()
	var cmdStr string
	if cmd == predefine.CMD_UPDATEPOOLS {
		cmdStr = "updatepools"
	} else {
		cmdStr = "updateztppools"
	}

	if hasChange {
		log.Infof("%s has new changes", cmdStr)
		my.ApplyConfig(pools)
	} else {
		log.Debugf("%s has no changes", cmdStr)
	}

	return 0, cmd
}

func (my *PoolManager) AddPool(cfg config.PoolEntryConfig) (uint, int) {
	my.mx.Lock()
	defer my.mx.Unlock()

	Total := len(my.Pools)
	if Total >= config.MAX_POOL_NUMBER {
		return 0, predefine.MSG_TOO_MANY_POOL
	}

	cfg.Parse()

	if !cfg.Valid {
		return 0, predefine.MSG_INVALID_ADDPOOL_DETAIL
	}

	pool := &PoolRuntime{
		ID:              uint(Total),
		HStats:          job.HashStats{UpdateTS: util.NowInSec()},
		GeneralHitStats: job.HashStats{UpdateTS: util.NowInSec()},
		DevFunc:         my.DevFunc,
		UpSince:         util.NowInSec(),
		Enabled:         true,
		Running:         false,
		Rejecting:       false,
		Cfg:             cfg,
		Priority:        Total,
		SeqNo:           my.SeqNo,
	}
	my.SeqNo++
	my.Pools = append(my.Pools, pool)

	log.Debugf("Added pool %+v, total %d", pool, len(my.Pools))
	return pool.ID, predefine.CMD_ADDPOOL
}

func (my *PoolManager) RemovePool(ID uint) int {
	my.mx.Lock()
	defer my.mx.Unlock()

	Total := len(my.Pools)
	if Total <= 1 {
		return predefine.MSG_REMOVE_LAST_POOL
	}

	if ID >= uint(Total) {
		return predefine.MSG_INVALID_POOL_ID
	}

	if ID == my.CurrentPoolIndex {
		return predefine.MSG_REMOVE_ACTIVE_POOL
	}

	pool := my.Pools[ID]
	pool.Stop()

	my.Pools = append(my.Pools[:ID], my.Pools[ID+1:]...)

	for k := range my.Pools {
		if my.Pools[k].Priority > pool.Priority {
			my.Pools[k].Priority--
		}
		if my.Pools[k].ID > ID {
			my.Pools[k].ID--
		}
	}

	log.Infof("Removed pool %+v, total %d", pool, len(my.Pools))

	return predefine.CMD_REMOVEPOOL
}

func (my *PoolManager) EnablePool(ID uint) int {
	my.mx.Lock()
	defer my.mx.Unlock()

	Total := len(my.Pools)

	if ID >= uint(Total) {
		return predefine.MSG_INVALID_POOL_ID
	}

	pool := my.Pools[ID]
	if pool.Enabled {
		return predefine.MSG_ALREADY_ENABLED_POOL
	} else {
		pool.Enabled = true
	}

	return predefine.CMD_ENABLEPOOL
}

func (my *PoolManager) DisablePool(ID uint) int {
	my.mx.Lock()
	defer my.mx.Unlock()

	Total := len(my.Pools)

	if ID >= uint(Total) {
		return predefine.MSG_INVALID_POOL_ID
	}

	pool := my.Pools[ID]
	if pool.Enabled {
		pool.Enabled = false
	} else {
		return predefine.MSG_ALREADY_DISABLED_POOL
	}

	return predefine.CMD_DISABLEPOOL
}

func (my *PoolManager) SwitchPool(ID uint) (int, PoolRuntime) {
	my.mx.Lock()
	defer my.mx.Unlock()

	Total := len(my.Pools)

	if ID >= uint(Total) {
		return predefine.MSG_INVALID_POOL_ID, PoolRuntime{}
	}

	pool := my.Pools[ID]

	/* move new pool to the first and priority to 0 */
	if pool.ID != 0 {
		for k := ID; k > 0; k-- {
			my.Pools[k] = my.Pools[k-1]
			my.Pools[k].Priority++
			my.Pools[k].ID++
		}
		my.Pools[0] = pool
		pool.ID = 0
		pool.Priority = 0
	}

	pool.Enabled = true

	return predefine.CMD_SWITCHPOOL, *pool
}

func (my *PoolManager) findPoolWithPriority(Prio int) *PoolRuntime {
	if Prio < 0 || Prio >= len(my.Pools) || Prio >= config.MAX_POOL_NUMBER {
		return nil
	}

	for _, v := range my.Pools {
		if v.Priority == Prio {
			return v
		}
	}
	return nil
}

func (my *PoolManager) SchedulePool() *PoolRuntime {
	// pool1, Enabled & healthy, pool1 could have lower priority than pool2
	var pool1 *PoolRuntime = nil
	// pool2, Enabled but not healthy
	var pool2 *PoolRuntime = nil

	for prio := 0; prio < config.MAX_POOL_NUMBER; prio++ {
		pool := my.findPoolWithPriority(prio)
		if pool == nil {
			continue
		}
		if pool.Enabled {
			if pool.SeqRejected > MAX_POOL_SEQREJECT || pool.Unreachable {
				if pool2 == nil {
					pool2 = pool
				}
			} else {
				if pool1 == nil {
					pool1 = pool
				}
			}
		}
	}

	if pool1 != nil {
		return pool1
	}
	return pool2
}

var ErrPoolNotExist = errors.New("ErrPoolNotExist")

func (my *PoolManager) GetPool(ID uint) (PoolRuntime, error) {
	my.mx.Lock()
	defer my.mx.Unlock()

	if int(ID) >= len(my.Pools) {
		return PoolRuntime{}, ErrPoolNotExist
	}

	return *my.Pools[ID], nil
}

func (my *PoolManager) Fini() {
	my.bExit = true
}

func (my *PoolManager) Run() {
	my.mx.Lock()

	var currentpool *PoolRuntime = nil
	noPoolLastTime := false
	for {
		pool := my.SchedulePool()
		if pool == nil {
			if !noPoolLastTime { // prevent log spam
				log.Infof("No Pool to run")
			}
			noPoolLastTime = true
			my.mx.Unlock()
			time.Sleep(WaitForPool)
			my.mx.Lock()
			continue
		} else {
			if noPoolLastTime {
				log.Infof("Found Pool to run")
			}
			noPoolLastTime = false
		}

		if !pool.Running {
			pool.Start()
			go func(mypool *PoolRuntime) {
				log.Infof("Starting Pool[%d/%d]: Prio %d, %s://%s Worker ID: %s",
					mypool.ID, mypool.SeqNo, mypool.Priority, mypool.Cfg.Proto, mypool.Cfg.HostNPort, mypool.Cfg.User)
				mypool.Run()
				log.Infof("Stopping Pool[%d/%d]: Prio %d, %s://%s Worker ID: %s",
					mypool.ID, mypool.SeqNo, mypool.Priority, mypool.Cfg.Proto, mypool.Cfg.HostNPort, mypool.Cfg.User)
			}(pool)
		}

		if currentpool != pool {
			if currentpool != nil {
				currentpool.Stop()
				log.Infof("Try stopping Pool[%d/%d]: Prio %d, %s://%s Worker ID: %s",
					currentpool.ID, currentpool.SeqNo, currentpool.Priority, currentpool.Cfg.Proto, currentpool.Cfg.HostNPort, currentpool.Cfg.User)
			}
			currentpool = pool
			my.CurrentPoolIndex = currentpool.ID
		}

		if my.bExit {
			break
		}

		my.mx.Unlock()
		time.Sleep(WaitForPool)
		my.mx.Lock()
	}

	my.mx.Unlock()
}
