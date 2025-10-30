package main

import (
	"eval_miner/config"
	"eval_miner/device"
	"eval_miner/device/devhdr"
	"eval_miner/log"
	"eval_miner/pool"
	"os"
	"os/signal"

	"eval_miner/system"
	//"gcminer/util"
	//"gcminer/version"
)

var (
	PoolMgr = pool.PoolManager{}

	DevMgr     = device.DeviceManager{}
	MinerCfg   = config.MinerConfig{}
	poolString = "stratum+tcp://btc.f2pool.com:3333"
	userString = "MiningRobot.eval1"
)

func main() {
	sysinfo, ok := system.GetSystemInfo()
	if ok != nil {
		log.Infof("Failed to read system information %v", ok)
		devhdr.SetMinerMaxLimits("default")
		devhdr.SetFansEnabled("default")
	} else {
		devhdr.SetMinerMaxLimits(sysinfo.ControlBoardInfo.ChassisModelNumber)
		devhdr.SetFansEnabled(sysinfo.ControlBoardInfo.ChassisModelNumber)
	}

	devFunc := DevMgr.Init()
	MinerCfg.Pools = make([]config.PoolEntryConfig, 1)
	MinerCfg.Pools[0].URL = poolString
	MinerCfg.Pools[0].User = userString
	MinerCfg.Pools[0].Pass = ""
	MinerCfg.Pools[0].Valid = true
	PoolMgr.Init(devFunc, MinerCfg)
	PoolMgr.Run()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	<-c

	PoolMgr.Fini()
	DevMgr.Fini()

	log.Info("=============== eval_miner stop===============")
	os.Exit(0)
}
