package job

import (
	"time"

	"eval_miner/log"
)

type DataPoint struct {
	Timestamp time.Time
	Value     uint64
}

type MovingWindow struct {
	WindowSize          time.Duration
	OldestTs            time.Time
	Values              []DataPoint
	PrevSum             uint64
	TargetHashRateinGhs uint64
}

type PoolStats struct {
	PoolHashRate15Min  *MovingWindow
	PoolHashRate30Min  *MovingWindow
	PoolHashRate60Min  *MovingWindow
	PoolHashRate120Min *MovingWindow
	PoolHashRate1Day   *MovingWindow
}

func NewMovingWindow(windowSize time.Duration) *MovingWindow {
	return &MovingWindow{
		WindowSize: windowSize,
		Values:     make([]DataPoint, 0),
		OldestTs:   time.Now(),
	}
}

func (w *MovingWindow) Update(point DataPoint, id uint) {
	// Update window size if needed
	for time.Since(w.OldestTs) > w.WindowSize {
		if len(w.Values) >= 1 {
			w.OldestTs = w.Values[0].Timestamp
			w.PrevSum = w.PrevSum - w.Values[0].Value
			w.Values = w.Values[1:]
		}
	}

	// Add new data point and update average
	w.Values = append(w.Values, point)
	// We don't need to recompute the values here
	sum := uint64(0)
	for _, p := range w.Values {
		sum += p.Value
	}
	w.PrevSum = sum
	w.TargetHashRateinGhs = sum * 4295 / (uint64(w.WindowSize.Seconds()) * 1000)
	if w.WindowSize == 2*time.Hour {
		log.Infof("HashRate(%v)-%d HR: %v len: %v", w.WindowSize, id, w.TargetHashRateinGhs, len(w.Values))
	}
}

func NewPoolStats() *PoolStats {
	return &PoolStats{
		PoolHashRate15Min:  NewMovingWindow(15 * time.Minute),
		PoolHashRate30Min:  NewMovingWindow(30 * time.Minute),
		PoolHashRate60Min:  NewMovingWindow(1 * time.Hour),
		PoolHashRate120Min: NewMovingWindow(2 * time.Hour),
		PoolHashRate1Day:   NewMovingWindow(24 * time.Hour),
	}
}

func (p *PoolStats) UpdateHashRate(data DataPoint, id uint) {
}
