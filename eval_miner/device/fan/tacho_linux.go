//go:build linux
// +build linux

package fan

import (
	"os"
	"strconv"
	"time"

	"github.com/warthog618/gpiod"
)

var lines *gpiod.Lines

const fanFileDir = "/tmp/fan/"

func startTacho() {
	var offsets []int
	for _, v := range tacho_data {
		offsets = append(offsets, v.pinoffset)
	}

	lines, _ = gpiod.RequestLines("gpiochip2", offsets,
		gpiod.WithRisingEdge,
		gpiod.WithEventHandler(eventHandler))

	go func() {
		for {
			time.Sleep(time.Millisecond * 500)
			for ii, v := range tacho_data {
				next := (v.cursor + 1) % 8
				v.counter[next] = 0
				v.cursor = next

				// Compute avg speed and write to file
				prev1 := (v.cursor + 7) % 8
				prev2 := (prev1 + 7) % 8
				prev3 := (prev2 + 7) % 8
				// each round has 2 cycles, so RPM = (cycles in 1.5s) * 40 / 2
				avgRpm := (v.counter[prev3] + v.counter[prev2] + v.counter[prev1]) * 20
				fileName := fanFileDir + "speed_" + strconv.Itoa(ii)
				outStr := []byte(strconv.Itoa(avgRpm) + "\n")
				_ = os.WriteFile(fileName, outStr, 0644)
			}

		}
	}()
}

func eventHandler(evt gpiod.LineEvent) {
	index := pin2index[evt.Offset]
	tacho := tacho_data[index]

	if tacho != nil {
		tacho.counter[tacho.cursor]++
	}
}
