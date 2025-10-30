package stratum

import (
	"eval_miner/jsonrpc"
	log "eval_miner/log"
)

// {"id": X, "method": "mining.extranonce.subscribe", "params": []}\n
func (my *Stratum) XnSub() error {
	var result interface{}

	method := "mining.extranonce.subscribe"
	params := []string{}

	_, bytes, err := my.Client.Call(method, -1, params, &result)
	my.StatsFunc.BytesFunc(false, bytes)

	return my.handleError(err)
}

/*
	{"id": X, "result": true, "error": null}\n
	{"id": X, "result": false, "error": [20, "Not supported.", null]}\n
*/

func (my *Stratum) XnSubResult(resp jsonrpc.Response) {
	if resp.Error != nil {
		// nothing special
		log.Infof("XnSubResult: %v", resp)
		return
	}

	if resp.Result != nil {
		// nothing special
		log.Debugf("XnSubResult: %v", resp)
	}
}
