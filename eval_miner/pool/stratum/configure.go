package stratum

import (
	"eval_miner/jsonrpc"
	"eval_miner/util"
	"reflect"

	log "eval_miner/log"
)

func (my *Stratum) ConfigureResult(resp jsonrpc.Response) {
	if resp.Result != nil {
		v := reflect.ValueOf(resp.Result)
		if v.Kind() == reflect.Map {
			for _, key := range v.MapKeys() {
				val := v.MapIndex(key)
				log.Debugf("key %s value %s", key, val)
				switch key.String() {
				case "version-rolling":
				case "version-rolling.mask":
					my.ServerMask = util.ToString(val)
					log.Debugf("ServerMask set to %s", my.ServerMask)
					my.VersionRolling = true
				case "minimum-difficulty.value":
					intVal, err := ToDiff(val)
					if err == nil {
						my.Difficulty = uint64(intVal)
						log.Debugf("minimum-difficulty.value: %d", my.Difficulty)
					} else {
						log.Infof("minimum-difficulty.value: err %v, current target %d", err, my.Difficulty)
					}
				case "minimum-difficulty":
				default:
				}
			}
		} else {
			log.Infof("ConfigureResult: %+v", resp)
		}
	}

	if resp.Error != nil {
		log.Debugf("RespError %v", resp.Error)
	}
}

func (my *Stratum) Configure() error {
	var result interface{}

	method := "mining.configure"

	ExtentionCode := []string{"version-rolling"}
	ExtentionParamMap := make(map[string]interface{})
	ExtentionParamMap["version-rolling.mask"] = DefaultVersionRollingMask
	ExtentionParamMap["version-rolling.min-bit-count"] = DefaultVersionRollingBits

	if my.bMinimumDifficulty {
		ExtentionCode = append(ExtentionCode, "minimum-difficulty")
		ExtentionParamMap["minimum-difficulty.value"] = DefaultMinimumDifficulty
	}

	if my.bSubscribeExtraNonce {
		ExtentionCode = append(ExtentionCode, "subscribe-extranonce")
	}

	params := []interface{}{ExtentionCode, ExtentionParamMap}

	_, bytes, err := my.Client.Call(method, -1, params, &result)
	my.StatsFunc.BytesFunc(false, bytes)

	return my.handleError(err)
}
