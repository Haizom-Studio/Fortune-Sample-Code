package fan

// The gpiod callback doesn't provide a callback arg or chip info. And pinoffset is the only one to
// distinguish different inputs. So this framework can not handle inputs on different gpio chips
// with same pin offset.
type tachometer struct {
	pinoffset int

	counter [8]int // count the last 4 seconds, each slot is for 0.5s
	cursor  int    // current slot 0-7
}

var tacho_data map[int]*tachometer = make(map[int]*tachometer)
var pin2index map[int]int = make(map[int]int)

func addTacho(index int, pinoffset int) {
	tacho_data[index] = &tachometer{pinoffset: pinoffset}
	pin2index[pinoffset] = index
}

func GetRPM(index int) int {
	v := tacho_data[index]

	if v == nil {
		return -1
	}

	prev1 := (v.cursor + 7) % 8
	prev2 := (prev1 + 7) % 8
	prev3 := (prev2 + 7) % 8

	// each round has 2 cycles, so RPM = (cycles in 1.5s) * 40 / 2
	return (v.counter[prev3] + v.counter[prev2] + v.counter[prev1]) * 20
}
