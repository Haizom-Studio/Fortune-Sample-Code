package stratum

import (
	"reflect"

	"eval_miner/jsonrpc"
	log "eval_miner/log"
)

func (my *Stratum) Authorize() error {
	var result interface{}

	method := "mining.authorize"
	params := []string{my.Cfg.User, my.Cfg.Pass}
	_, bytes, err := my.Client.Call(method, -1, params, &result)
	my.StatsFunc.BytesFunc(false, bytes)

	return my.handleError(err)
}

func (my *Stratum) AuthorizeResult(resp jsonrpc.Response) {
	if resp.Result != nil {
		v := reflect.ValueOf(resp.Result)
		val := v.Interface().(bool)
		log.Debugf("v %v, val %v", v, val)
	}

	if resp.Error != nil {
		log.Debugf("RespError %v", resp.Error)
	}
}
