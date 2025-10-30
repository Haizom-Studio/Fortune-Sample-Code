package util

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func Int(v int) *int {
	return &v
}

func Uint64(v uint64) *uint64 {
	return &v
}

func Float64(v float64) *float64 {
	return &v
}

func Bool(v bool) *bool {
	return &v
}

func ToString(x interface{}) string {
	if x == nil {
		return ""
	}
	strVal := fmt.Sprintf("%v", x)
	return strVal
}

func ToStringArray(x []interface{}) []string {
	a := make([]string, len(x))
	for i := range a {
		a[i] = ToString(x[i])
	}
	return a
}

func ToUint(x interface{}) (uint, error) {
	strVal := fmt.Sprintf("%v", x)
	intVal, err := strconv.ParseUint(strVal, 10, 64)
	return uint(intVal), err
}

func ToBool(x interface{}) bool {
	return reflect.ValueOf(x).Bool()
}

/*
total = int(len(prev_h)/2/4)
out = ”
for i in range(total):

	out = out + prev_h[i*8+6:i*8+6+2]
	out = out + prev_h[i*8+4:i*8+4+2]
	out = out + prev_h[i*8+2:i*8+2+2]
	out = out + prev_h[i*8+0:i*8+0+2]

return out
*/
func SwapBytes(in string) string {
	in_byte := []byte(in)
	out := make([]byte, len(in_byte))

	// In string, two bytes is one hex digit, uint32 has 8 hex digits 4 bytes.
	n := len(in) / 2 / 4
	i := 0
	for i < n {
		out[i*8+0] = in_byte[i*8+6]
		out[i*8+1] = in_byte[i*8+6+1]
		out[i*8+2] = in_byte[i*8+4]
		out[i*8+3] = in_byte[i*8+4+1]
		out[i*8+4] = in_byte[i*8+2]
		out[i*8+5] = in_byte[i*8+2+1]
		out[i*8+6] = in_byte[i*8+0]
		out[i*8+7] = in_byte[i*8+0+1]
		i++
	}

	return string(out)
}

func BEHexToUint32(in string) uint32 {
	val, err := hex.DecodeString(in)
	if err != nil {
		return 0
	}
	if len(val) == 0 {
		return 0
	}

	x1 := binary.BigEndian.Uint32([]byte(val))

	return x1
}

func MIN(vars ...uint64) uint64 {
	min := vars[0]

	for _, i := range vars {
		if min > i {
			min = i
		}
	}

	return min
}

func MAX(vars ...uint64) uint64 {
	max := vars[0]

	for _, i := range vars {
		if max < i {
			max = i
		}
	}

	return max
}

func HexStringFromNumber(nBytes int, x uint64) string {
	if nBytes <= 0 {
		return "00"
	}

	ndigits := nBytes * 2 // each byte holds two hex digits
	fmtStr := fmt.Sprintf("%%0%dx", ndigits)
	b := fmt.Sprintf(fmtStr, x)

	l := len(b)
	if l == ndigits {
		return b
	}

	if l > ndigits {
		return b[l-ndigits:]
	}

	// l < N
	// this will not happen as Sprintf %0Nx should have added zeros in front
	return ZeroHexString(nBytes)
}

func ZeroHexString(nBytes int) string {
	if nBytes <= 0 {
		return "00"
	}

	ndigits := nBytes * 2
	b := make([]byte, ndigits) // each byte holds two hex digits
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func RandStringBytes(n int) string {
	rand.Seed(time.Now().UnixNano())

	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

func UrlToDomain(rawUrl string) string {
	rawUrl = strings.TrimSpace(rawUrl)
	u, err := url.ParseRequestURI(rawUrl)
	if err != nil || u.Host == "" {
		u, err = url.ParseRequestURI("https://" + rawUrl)
		if err != nil {
			// log.Infof("Could not parse URL: %s, error: %v", rawUrl, err)
			return ""
		}
	}
	if u == nil {
		return ""
	}

	parts := strings.Split(u.Hostname(), ".")
	domain := ""
	if len(parts) >= 2 {
		domain = parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return domain
}

// ClosestPowerOf2 returns the 2's power floor value of an argument(n)
func ClosestPowerOf2(n uint64) uint64 {
	exponent := math.Floor(math.Log2(float64(n)))
	result := uint64(math.Pow(2, exponent))
	return result
}
