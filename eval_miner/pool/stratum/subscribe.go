package stratum

import (
	"eval_miner/jsonrpc"
	log "eval_miner/log"
	"eval_miner/util"
	"eval_miner/version"
)

func (my *Stratum) Subscribe() error {
	var result interface{}
	vcfg := version.GetVersionConfig()
	method := "mining.subscribe"
	params := []string{vcfg.Agent}
	if my.SubscribeID != "" {
		params = []string{vcfg.Agent, my.SubscribeID}
	}

	_, bytes, err := my.Client.Call(method, -1, params, &result)
	my.StatsFunc.BytesFunc(false, bytes)

	return my.handleError(err)
}

/*
	{"id":0,
	"result":[[["mining.set_difficulty","53ed961d-18af-4912-9b2e-6fb68667129d"],["mining.notify","53ed961d-18af-4912-9b2e-6fb68667129d"]],"306504005ba15d",8],
	"error":null}
*/

func (my *Stratum) SubscribeResult(resp jsonrpc.Response) {
	if resp.Result != nil {
		a, ok := resp.Result.([]interface{})
		if ok {
			//a[0] [["mining.set_difficulty","53ed961d-18af-4912-9b2e-6fb68667129d"],["mining.notify","53ed961d-18af-4912-9b2e-6fb68667129d"]]
			//a[1] "306504005ba15d"
			//a[2] 8
			log.Debugf("a[0] %v, a[1] %v, a[2] %v", a[0], a[1], a[2])

			a0, ok2 := a[0].([]interface{})
			if ok2 {
				if len(a0) >= 1 {
					// a0[0] ["mining.set_difficulty","53ed961d-18af-4912-9b2e-6fb68667129d"]
					a00, ok3 := a0[0].([]interface{})
					if ok3 {
						if len(a00) >= 2 {
							my.SubscribeID = util.ToString(a00[1])
						}
					}
				}
			}
			my.ExtraNonce1 = util.ToString(a[1])
			intVal, err := util.ToUint(a[2])
			if err == nil {
				my.ExtraNonce2Size = uint(intVal)
			}
			log.Debugf("SubscribeID %s, ExtraNonce1 %s, ExtraNonce2Size %d",
				my.SubscribeID, my.ExtraNonce1, my.ExtraNonce2Size)
		} else {
			log.Infof("SubscribeResult: %+v", resp)
		}
	}

	if resp.Error != nil {
		log.Debugf("RespError %v", resp.Error)
	}
}
