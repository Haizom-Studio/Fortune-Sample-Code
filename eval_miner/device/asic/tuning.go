package asic

import (
	"eval_miner/log"
	"sort"
	"time"
)

const tuneLoops int = 10
const maxFreqChange float32 = 0.05

func Tune(average_f float32) bool {

	var tsPollHitCounter time.Time
	type rankType struct {
		index     int
		passing   float32
		frequency float32
		id        int
	}
	type rankArrayType []rankType

	for iter := 0; iter < tuneLoops; iter++ {

		log.Infof("DVFS Tune: Iteration %d", iter)
		// Sort ASICs by hitrate
		ranking := rankArrayType{}
		for i := 0; i < len(dd.topology); i++ {
			t := &dd.topology[i]
			rankEnt := rankType{i, t.hitrate, t.frequency, t.id}
			ranking = append(ranking, rankEnt)
		}
		sort.Slice(ranking, func(i, j int) bool {
			if ranking[i].passing < ranking[j].passing {
				return true
			} else if ranking[i].passing > ranking[j].passing {
				return false
			} else {
				return ranking[i].frequency > ranking[j].frequency
			}
		})

		// Based on hitrate of each ASIC, increase or decrease its frequency up to 5% of average frequency
		// Trade frequencies to make system hashrate stay the same
		// Only do outlying 50% of ASICs each iteration
		for i := 0; i < len(ranking)/4; i++ {
			slow := &dd.topology[ranking[i].index]
			fast := &dd.topology[ranking[len(ranking)-1-i].index]

			// The further they are to either end of the list, the more frequency should be adjusted
			step := float32(maxFreqChange) * (float32((len(ranking)/2)-i) / float32(len(ranking)/2))
			f_incr := average_f * step
			log.Infof("DVFS Tune: step %.3f f_incr %.3f average_f %.1f", step, f_incr, average_f)
			fast.frequency += f_incr
			slow.frequency -= f_incr
			log.Infof("DVFS Tune: ASIC %d/%d hitrate: %.2f freq -> %.2f, ASIC %d/%d: hitrate %.2f freq -> %.2f", fast.board, fast.id, fast.hitrate, fast.frequency, slow.board, slow.id, slow.hitrate, slow.frequency)
		}
		batch := BatchArrayType{}
		for stagger := 0; stagger < dd.num_cols; stagger++ {
			for i := stagger; i < len(dd.topology); i += dd.num_cols {
				t := &dd.topology[i]
				if t.frequency < average_f*0.4 { // Limit frequency range
					t.frequency = average_f * 0.4
				} else if t.frequency > dd.systeminfo.max_frequency {
					t.frequency = dd.systeminfo.max_frequency
				}

				batch = batch.Add(uint16(t.board), t.id, ADDR_PLL_FREQ, uint32(t.frequency*dd.pll_multiplier), CMD_WRITE)
				var setting int
				setting = int((48000 / t.frequency)) - 32
				if setting < 0 {
					setting = 0
				}
				if setting > 64 {
					setting = 64
				}
				batch = batch.Add(uint16(t.board), t.id, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(0<<18), CMD_WRITE)
				batch = batch.Add(uint16(t.board), t.id, ADDR_DUTY_CYCLE, uint32(setting)|(1<<17)|(1<<18), CMD_WRITE)
			}
		}
		_ = batch.ReadWriteConfig()

		// Wait for hitrate reading for next iteration
		for i := 0; i < 5; i++ {
			if dd.perSecondCheck(0.50) {
				log.Error("DVFS: Tune perSecondCheck exiting early")
				return true
			}
			Delay(1000) // Delay *after* checking for alarms
		}
		tsPollHitCounter = time.Now()
		hitcounters := getHitCountersAll()
		ts := time.Now()
		_, _, _ = dd.getHashRate(hitcounters, ts.Sub(tsPollHitCounter))
		tsPollHitCounter = ts
	}

	return true

}

func pctOver50Pct() float32 {
	var numOver50 int
	for i := 0; i < len(dd.topology); i++ {
		t := &dd.topology[i]
		if t.hitrate >= 0.50 {
			numOver50++
		}
	}
	pct := float32(numOver50) / float32(len(dd.topology))
	log.Infof("DVFS: pctOver50Pct: %.3f", pct)
	return pct
}
