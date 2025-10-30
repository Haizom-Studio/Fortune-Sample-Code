package stratum

import (
	log "eval_miner/log"
	"eval_miner/util"
	"time"
)

func (my *Stratum) hanleClientReconnect(Params []interface{}) {
	plen := len(Params)
	hostname := ""
	port := ""
	wait := uint(0)
	var err error

	if plen == 0 {
		// reconnect to the same host:port but with ExtraNonce1 set
		my.State = STATE_DISCONNECT
		return
	}
	if plen >= 1 {
		hostname = util.ToString(Params[0])
		/*
			check if the hostname is under the same parent domain
		*/
		domain := util.UrlToDomain(hostname)
		domain0 := util.UrlToDomain(my.Cfg.Host)
		if domain == "" || domain0 == "" || domain != domain0 {
			log.Infof("client.reconnect to %s ignored. Domain not matching %s", hostname, my.Cfg.Host)
			return
		}
	}
	if plen >= 2 {
		port = util.ToString(Params[1])
	} else {
		port = my.Cfg.Port
	}

	url := hostname + ":" + port
	my.Cfg.ParseURL(url)

	if plen >= 3 {
		wait, err = util.ToUint(Params[2])
		if err != nil {
			wait = 0
		}

	}

	log.Infof("client.reconnect to %s://%s w/ wait time %d", my.Cfg.Proto, my.Cfg.HostNPort, wait)

	if wait != 0 {
		time.Sleep(time.Duration(wait) * time.Second)
	}

	my.State = STATE_DISCONNECT
}
