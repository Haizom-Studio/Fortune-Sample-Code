package util

import (
	"encoding/json"
	"fmt"
	"time"
)

type TimeVal struct {
	Sec  int64
	Usec int64
}

func (t TimeVal) MarshalJSON() ([]byte, error) {
	var SecTotal float64 = float64(t.Sec) + float64(t.Usec)/1000000
	//s := fmt.Sprintf("%f", SecTotal)
	return json.Marshal(SecTotal)
}

var (
	UpSince = time.Now()
)

func UptimeInString() string {
	t := time.Now()
	d := t.Sub(UpSince)

	return d.String()
}

func TimeZone() string {
	t := time.Now()
	zone, _ := t.Zone()
	return zone
}

func NowInString() string {
	t := time.Now()
	return t.Format(time.UnixDate)
}

func NowInSec() float64 {
	return float64(time.Now().UnixMicro()) / 1000000.0
}

func NowInSecToString() string {
	s := fmt.Sprintf("%f", NowInSec())
	return s
}

// t2 is now, t1 is time base
func UptimeInSec(t2 float64, t1 float64) float64 {
	if t2 <= t1 {
		return 0.01
	}
	return t2 - t1
}

func SystemUptimeInSec() float64 {
	return UptimeInSec(NowInSec(), float64(UpSince.UnixMicro())/1000000.0)
}
