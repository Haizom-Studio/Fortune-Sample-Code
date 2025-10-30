package pool

import (
	"eval_miner/config"
	"eval_miner/log"
	"eval_miner/predefine"
	"reflect"
	"time"
)

const (
	POOL_COUNT = iota
	POOL_ID
	POOL_ALL
	POOL_CURRENT
	POOL_MGMT
	SUMMARY
)

type PoolData struct {
	Count   uint
	Pools   []PoolRuntime
	Sum     Summary
	MsgCode int
	ID      uint
}

type PoolArg struct {
	What        int
	CMD         int
	ID          uint
	EnabledOnly bool
	Cfg         config.PoolEntryConfig
	Pools       []config.PoolEntryConfig
}

func (Mgr *PoolManager) Get(arg PoolArg) *PoolData {
	data := PoolData{
		Count: uint(len(Mgr.Pools)),
		Pools: []PoolRuntime{},
	}

	switch arg.What {
	case POOL_COUNT:
	case POOL_ID:
		if len(Mgr.Pools) > int(arg.ID) {
			pool := Mgr.Pools[arg.ID]
			if pool != nil {
				data.Pools[arg.ID] = *pool
			}
		}
	case POOL_ALL:
		for _, v := range Mgr.Pools {
			ok := false
			if !arg.EnabledOnly {
				ok = true
			} else if v.Enabled {
				ok = true
			}

			if ok {
				data.Pools = append(data.Pools, *v)
			}
		}
	case POOL_CURRENT:
		if len(Mgr.Pools) > int(Mgr.CurrentPoolIndex) {
			pool := Mgr.Pools[Mgr.CurrentPoolIndex]
			if pool != nil {
				data.Pools = append(data.Pools, *pool)
			}
		}
	case SUMMARY:
		data.Sum = Sum
	case POOL_MGMT:
		switch arg.CMD {
		case predefine.CMD_REMOVEPOOL:
			data.MsgCode = Mgr.RemovePool(arg.ID)
			pool, err := Mgr.GetPool(Mgr.CurrentPoolIndex)
			if err == nil {
				data.Pools = append(data.Pools, pool)
			}
		case predefine.CMD_ENABLEPOOL:
			data.MsgCode = Mgr.EnablePool(arg.ID)
			if data.MsgCode == predefine.CMD_ENABLEPOOL {
				pool, err := Mgr.GetPool(arg.ID)
				if err == nil {
					data.Pools = append(data.Pools, pool)
				}
			}
		case predefine.CMD_DISABLEPOOL:
			data.MsgCode = Mgr.DisablePool(arg.ID)
			if data.MsgCode == predefine.CMD_DISABLEPOOL {
				pool, err := Mgr.GetPool(arg.ID)
				if err == nil {
					data.Pools = append(data.Pools, pool)
				}
			}
		case predefine.CMD_SWITCHPOOL:
			var pool0 PoolRuntime
			data.MsgCode, pool0 = Mgr.SwitchPool(arg.ID)
			if data.MsgCode == predefine.CMD_SWITCHPOOL {
				log.Infof("switching to pool id %d, %v, URL: %s\n", arg.ID, pool0, pool0.Cfg.URL)
				Done := false
				start_ts := time.Now()
				var pool PoolRuntime = PoolRuntime{}
				var err error
				for !Done {
					pool, err = Mgr.GetPool(Mgr.CurrentPoolIndex)
					if err != nil {
						break
					}
					ts := time.Now()
					// if it's been more than 5 seconds
					if ts.Sub(start_ts) > 5*time.Second {
						Done = true
					}
					// if current pool is Priority 0, the pool we just switched to
					if reflect.DeepEqual(pool0.Cfg, pool.Cfg) {
						Done = true
						log.Infof("Current Pool is our pool\n")
					} else {
						time.Sleep(100 * time.Millisecond)
					}
				}
				if !reflect.DeepEqual(pool0.Cfg, pool.Cfg) {
					log.Infof("Current Pool is not our pool, but is %v, %s\n", pool, pool.Cfg.URL)
				} else {
					data.Pools = append(data.Pools, pool0)
				}
			} else {
				log.Info("no pool to switch to")
			}
		case predefine.CMD_ADDPOOL:
			data.ID, data.MsgCode = Mgr.AddPool(arg.Cfg)

		default:
		}
	default:
	}

	return &data
}
