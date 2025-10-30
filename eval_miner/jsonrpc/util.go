package jsonrpc

import (
	"encoding/json"
	log "eval_miner/log"
)

func PrepareJSONResponse(v interface{}) ([]byte, error) {
	jsonResponse, err := json.Marshal(v)
	if err != nil {
		log.Errorf("err %v", err)
		return nil, err
	}
	n := len(jsonResponse)
	if n > 0 {
		if jsonResponse[n-1] != '\n' {
			jsonResponse = append(jsonResponse, '\n')
		}
	}
	return jsonResponse, nil
}
