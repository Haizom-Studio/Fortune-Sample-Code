package config

import (
	"strings"

	log "eval_miner/log"
)

type PoolEntryConfig struct {
	URL          string
	Proto        string
	HostNPort    string
	Host         string
	Port         string
	XnSub        bool
	User         string
	Pass         string
	NetworkProto string
	Valid        bool
	firstInvalid bool
}

const (
	StratumPrefix   = "stratum+tcp"
	MAX_POOL_NUMBER = 3
)

func (my *PoolEntryConfig) ParseURL(myURL string) {
	// stratum+tcp://stratum.marapool.com:9999#xnsub
	x := strings.Split(myURL, "#")
	my.XnSub = false
	if len(x) > 1 {
		if x[1] == "xnsub" {
			my.XnSub = true
		}
	}
	if !my.XnSub && strings.Contains(myURL, "nicehash") {
		my.XnSub = true // Per NiceHash, always enable XNSUB for nicehash pools. This is parity with Antminer S devices.
	}

	// stratum+tcp://ss.antpool.com:3333
	a := strings.Split(x[0], "://")

	switch len(a) {
	case 1:
		// this means there is no Stratum prefix
		my.Proto = ""
		my.HostNPort = a[0]
	case 2:
		my.Proto = a[0]
		my.HostNPort = a[1]
	default:
		// this means there are more than one "://"
		my.Proto = ""
		my.HostNPort = ""
	}
	//stratum.slushpool.com:3333
	s := strings.Split(my.HostNPort, ":")
	switch len(s) {
	case 1:
		my.Host = s[0]
		my.Port = "80"
	case 2:
		my.Host = s[0]
		my.Port = s[1]
	default:
		my.Host = ""
		my.Port = ""
	}

	// reconstruct HostNPort in case port is default 80
	my.HostNPort = my.Host + ":" + my.Port

	if my.Proto == "" {
		my.Proto = StratumPrefix
	}

	switch my.Proto {
	case "stratum+tcp":
		my.NetworkProto = "tcp"
	default:
		my.NetworkProto = "tcp"
	}

	my.Valid = true
	if my.Proto != StratumPrefix {
		my.Valid = false
	}
	if my.HostNPort == "" || my.User == "" || my.Host == "" || my.Port == "" {
		my.Valid = false
	}
	if !my.Valid && !my.firstInvalid {
		my.firstInvalid = true
		log.Infof("Pool config %s is not valid", my.URL)
	} else if my.Valid {
		my.firstInvalid = false
	}
}

func (my *PoolEntryConfig) Parse() {
	my.ParseURL(my.URL)
}

func (my *PoolEntryConfig) Equal(cfg *PoolEntryConfig) bool {
	if my.URL != cfg.URL {
		return false
	}
	if my.User != cfg.User {
		return false
	}
	if my.Pass != cfg.Pass {
		return false
	}
	return true
}

type MinerConfig struct {
	Pools []PoolEntryConfig
}
