// this is the adapter between aura-asic & asicboard
package asic

import (
	"encoding/hex"
	"fmt"
	"math/bits"
	"strconv"
	"time"

	"eval_miner/device/asicio"
	"eval_miner/device/chip"
	"eval_miner/device/devhdr"
	"eval_miner/log"
)

type HitStat struct {
	elapse     time.Duration
	totalGhits uint32
	totalThits uint32
	deltaGhits []uint32
	deltaThits []uint32
}

type AsicAdapter struct {
	AsicIDs []uint8
}

const (
	statsRingSize                     = 12
	maxChip             int           = 255 // Chip IDs are not necessarily in order
	maxJobInterval      time.Duration = 300 * time.Second
	cntReadInterval     time.Duration = 5 * time.Second
	asicReadingDuration time.Duration = 2 * time.Second
	minPollingInterval  time.Duration = 10 * time.Millisecond
	MaxFreq             float32       = 2000.0 // MHz
	AsicTempLimit       float32       = 120.0
	BaudRateInit        uint32        = 115200
	BaudRateWorking     uint32        = 3000000 // 3M baud rate
	hitMax              uint32        = 2000    // this is about 3x the max hit count of a single chip in 5s
	AsicIdDefault       uint32        = 132
)

var (
	MinFreq float32 = 200 // MHz
)

type HashBoardAsicIdConfigs struct {
	HbAsicConfigs map[string]devhdr.HashBoardAsicIdConfig
}

// Configure per hash board spec
var NumChips uint32 = devhdr.NumChips

var AdapterHandle [devhdr.MaxHashBoards + 1]*AsicAdapter // 1-based array: hash boards 1 - 3; 0 is unused

// GetAsicCounts return the numbers if ASICs in a hashboard
// At this point ASIC counts are fixed based on chassis type
func GetAsicCounts() uint32 {
	return AsicIdDefault
}

func GetAsicIdConfigs(brdId uint) *devhdr.HashBoardAsicIdConfig {

	asicCounts := GetAsicCounts()
	log.Infof("Asic Counts %v", asicCounts)
	return devhdr.DefaultHbAsicIdConfig
}

func AsicDetect(BrdId, slotId uint, uartName string) (*AuraAsic, error) {
	if uartName == "" {
		uartName = "/dev/ttyS" + strconv.Itoa(int(BrdId))
	}

	asicConfigs := GetAsicIdConfigs(BrdId)
	log.Infof("AsicIDConfigs %v", asicConfigs)

	// start from default baud rate
	var aa *AuraAsic
	var err error
	aa, err = AuraAsicInit(uartName, BrdId, slotId, maxChip, BaudRateInit, false, false, asicConfigs)
	if err == nil {
		log.Infof("Asic detected with boardChain %v", BrdId)
	}

	if err != nil {
		log.Errorf("AuraAsicInit returned error %s; retrying\n", err)
		aa, err = AuraAsicInit(uartName, BrdId, slotId, maxChip, BaudRateWorking, false, false, asicConfigs)
		if err != nil {
			log.Errorf("AuraAsicInit retry failed: error %s", err)
		}
	} else {
		err = aa.setBaudRate(BaudRateWorking)
		if err != nil {
			log.Errorf("setBaudRate returned error %s\n", err)
			return nil, err
		}
	}

	_ = aa.regWriteAll(ADDR_CLOCK_RETARD_BASE, 0)
	// Initialize all ASIC voltage and temperature sensors
	aa.runVoltageAndTemperature()
	aa.SetThermalTrip(AsicTempLimit)
	aa.setDutyCycleExtendAll()
	// Set initial frequency
	_ = aa.SetFrequencyAll(MinFreq)
	//old_average_f = MinFreq

	aa.cacheTemps = make([]float64, len(aa.ChipArray))
	aa.cacheVolts = make([]float64, len(aa.ChipArray))
	aa.cacheFreqs = make([]float64, len(aa.ChipArray))
	for i := 0; i < len(aa.ChipArray); i++ {
		aa.cacheTemps[i] = -1000.0
		aa.cacheVolts[i] = -1.0
		aa.cacheFreqs[i] = -1.0
	}

	aa.AsicIDs = aa.seqChipIds
	aa.lastlog = time.Now()
	aa.lastCntrReading = time.Now().Add(-maxJobInterval)
	// Populate initial voltage & temperature values for this board
	aa.SetThermalTrip(AsicTempLimit)
	aa.ReadAllTemperature()
	aa.ReadAllVoltage()
	return aa, nil
}

func (aa *AuraAsic) SendJob(msg *chip.Message) error {

	bhBytes, _ := hex.DecodeString(msg.Body)
	if len(bhBytes) != 80 {
		return fmt.Errorf("wrong job length %d", len(bhBytes))
	}

	log.Debugf("new asic job %v", *msg)

	if msg.VersionMask != aa.verMask || aa.jobCount%16 == 0 {
		// calculate version rolling parameters
		shift := bits.TrailingZeros32(msg.VersionMask)
		lzs := bits.LeadingZeros32(msg.VersionMask)
		min := 0
		max := 4 * aa.GetAsicNum()

		if shift < 32 {
			max = 1 << (32 - shift - lzs)
		} else {
			shift = 13
		}

		_ = aa.setVerRolling(uint32(shift), uint32(min), uint32(max))
		aa.verMask = msg.VersionMask
	}

	ret := aa.asicIO.AsicLoad(uint8(msg.Diff), uint8(msg.Seq), *(*[80]byte)(bhBytes))
	if aa.jobCount == 0 {
		aa.clearAllCounters()
		aa.clearResults()
		aa.enableAutoReporting(false)
	}
	aa.jobCount++
	aa.lastCntrReading = time.Now()

	return ret
}

func (aa *AuraAsic) checkHitCounters() (p []float32, err error) {

	percents := make([]float32, len(aa.AsicIDs))

	curStats := &aa.hitStats[(aa.hitStatsCursor)%statsRingSize]
	if curStats.deltaGhits == nil {
		curStats.deltaThits = make([]uint32, len(aa.AsicIDs))
		curStats.deltaGhits = make([]uint32, len(aa.AsicIDs))
	} else {
		curStats.totalThits = 0
		curStats.totalGhits = 0
	}

	results, err := aa.ReadAllPipelined(ADDR_TRUEHIT_COUNT_GENERAL)
	if err != nil {
		log.Errorf("ReadRegsPipelined error: %v", err)
	} else {
		returned := false
		nonresponding := []uint8{}
		for i := 0; i < len(results); i++ {
			if results[i] == -1 {
				aa.deltaThits[i] = 0
				curStats.deltaThits[i] = 0
				if !aa.ChipArray[i].NotFound {
					nonresponding = append(nonresponding, aa.AsicIDs[i])
				}
			} else {
				returned = true
				curStats.deltaThits[i] = uint32(results[i]) - aa.thits[i]
				// Sanity check the delta value here
				if curStats.deltaThits[i] > hitMax {
					if i == 0 {
						curStats.deltaThits[i] = 0 // No hits for the bad chip reading
					} else {
						curStats.deltaThits[i] = curStats.deltaThits[i-1] // Just borrow our neighbor's delta
					}
				}
				aa.deltaThits[i] = curStats.deltaThits[i]
				curStats.totalThits += curStats.deltaThits[i]
				aa.thits[i] = uint32(results[i])
			}
		}
		log.Debugf("Brd %d true genhits %v", aa.brdChainId, results)
		if !returned {
			log.Debugf("Brd %d: No true gen hits results returned", aa.brdChainId)
		} else {
			if len(nonresponding) > 0 {
				log.Debugf("Error reading TRUEHIT of bd %d chip %v", aa.brdChainId, nonresponding)
			}
		}
	}

	results, err = aa.ReadAllPipelined(ADDR_HIT_COUNT_GENERAL)
	if err == nil {
		nonresponding := []uint8{}
		for i := 0; i < len(results); i++ {
			if results[i] == -1 {
				curStats.deltaGhits[i] = 0
				aa.deltaGhits[i] = 0
				if !aa.ChipArray[i].NotFound {
					nonresponding = append(nonresponding, aa.AsicIDs[i])
				}
			} else {
				curStats.deltaGhits[i] = uint32(results[i]) - aa.ghits[i]
				if curStats.deltaGhits[i] > hitMax {
					if i == 0 {
						curStats.deltaGhits[i] = 0 // No hits for the bad chip reading
					} else {
						curStats.deltaGhits[i] = curStats.deltaGhits[i-1] // Just borrow our neighbor's delta
					}
				}
				aa.deltaGhits[i] = curStats.deltaGhits[i]
				if curStats.deltaGhits[i] > 0 {
					curStats.totalGhits += curStats.deltaGhits[i]
					percents[i] = float32(curStats.deltaThits[i]) * 100 / float32(curStats.deltaGhits[i])
					aa.ChipArray[i].Performance = percents[i] // Chip hitrate
				}
				aa.ghits[i] = uint32(results[i])
			}
		}
		if len(nonresponding) > 0 {
			log.Debugf("Error reading GENHIT of bd/chain %d chip %v", aa.brdChainId, nonresponding)
		}
	}

	elapse := time.Since(aa.lastlog)
	curStats.elapse = elapse
	aa.lastlog = time.Now()
	aa.hitStatsCursor = (aa.hitStatsCursor + 1) % statsRingSize

	seconds := elapse.Seconds()
	log.Debugf("Brd %d performance in last %.0fs: %.0f/%.0f GH/s", aa.brdChainId, seconds,
		float64(curStats.totalThits)*4.295/seconds, float64(curStats.totalGhits)*4.295/seconds)

	hashRates := make([]float32, len(aa.AsicIDs))
	for i := 0; i < len(results); i++ {
		if results[i] != -1 {
			hashRates[i] = float32(curStats.deltaThits[i]) * 4.295 / float32(seconds)
		}
	}
	log.Debugf("Brd %d hash rates by chip(GH/s) %v", aa.brdChainId, hashRates)
	log.Debugf("Brd %d True/Gen hits percents %.1f%%", aa.brdChainId, percents)
	return percents, err
}

func (aa *AuraAsic) CheckResults() (msg *chip.Message, err error) {
	if aa.jobCount == 0 {
		return nil, nil
	}
	// keep monitoring true hit count
	if time.Since(aa.lastlog) >= cntReadInterval && time.Since(aa.lastCntrReading) < maxJobInterval {
		_, _ = aa.checkHitCounters()
	}

	var r *asicio.ResponseHitType
	r, err = aa.asicIO.CheckHitResult()
	minInterval := minPollingInterval
	if r == nil && !aa.autoReport && time.Since(aa.lastPolling) > minInterval {
		ts := time.Now()
		n, _ := aa.pollHitResults(false)
		aa.lastPolling = time.Now()
		if n > 0 {
			r, _ = aa.asicIO.CheckHitResult()
			for i := 0; i < 10 && r == nil; i++ {
				time.Sleep(time.Millisecond * 2)
				r, _ = aa.asicIO.CheckHitResult()
			}
			log.Infof("Board %d polling took %v, got %v results. returned %v", aa.brdChainId, aa.lastPolling.Sub(ts), n, r != nil)
		}
	}

	if r != nil {
		chipId := r.Id
		engine := r.Result[76]

		msg = &chip.Message{
			Seq:         uint(r.Sequence),
			Diff:        uint(r.Nbits),
			Chip:        uint(chipId),
			Engine:      uint(engine),
			Board:       uint(aa.brdChainId),
			Body:        hex.EncodeToString(r.Result[:]),
			VersionMask: 0,
		}
	} else if aa.reportCursor != aa.hitStatsCursor {
		// sum up all the unreported stats
		msg = &chip.Message{
			Seq: chip.SEQ_HASHRATE_UPDATE,
		}
		for i := aa.reportCursor; i != aa.hitStatsCursor; i = (i + 1) % statsRingSize {
			stats := &aa.hitStats[i]
			for j := 0; j < len(aa.AsicIDs); j++ {
				asicId := aa.AsicIDs[j]
				msg.GenHit[asicId] += uint(stats.deltaGhits[j])
				msg.TrueHit[asicId] += uint(stats.deltaThits[j])
				msg.HitRate[asicId] = aa.ChipArray[j].Performance
			}
		}
		aa.reportCursor = aa.hitStatsCursor
	}

	return msg, err
}

func (aa *AuraAsic) EnableAutoReporting(enabled bool) {
	aa.autoReport = enabled
}

/*
 * Read hit results from all chips and put them into the result queue,
 * hitResult.Hit_unique is not set in this case
 */
func (aa *AuraAsic) pollHitResults(all bool) (int, error) {
	nTotalHits := 0
	chiplen := len(aa.actualChipIds)
	// read summary register in batches of 44 chips to reduce latency
	var start, end int
	if all {
		start = 0
		end = chiplen
	} else {
		start = aa.pollingStart
		if start >= chiplen {
			start = 0
		}
		end = start + 44
		if end > chiplen {
			end = chiplen
		}
		aa.pollingStart = end
	}

	results, err := aa.ReadRegsPipelined(aa.actualChipIds[start:end], []uint8{ADDR_SUMMARY})
	aa.lastTempReading = time.Now()
	if err != nil {
		log.Errorf("ReadAllSummary error %s", err)
		return 0, err
	}

	for i := start; i < end; i++ {
		ret := results[i-start]
		if ret != -1 {
			nHits := ret & 0xf
			if nHits != 0 {
				log.Debugf("chip %d has %d hit results", aa.actualChipIds[i], nHits)
				for j := 0; j < int(nHits); j++ {
					_ = aa.asicIO.ReqestHitResult(aa.actualChipIds[i])
				}
				nTotalHits += int(nHits)
			}
			// Process and cache temperature value
			v := (ret & 0xffff0000) >> 16
			temp := (float64(v)-0.5)*tempY*(1.0/4096.0) + tempK
			aa.cacheTemps[aa.ChipIdToIndex(aa.actualChipIds[i])] = temp
			aa.ChipArray[aa.ChipIdToIndex(aa.actualChipIds[i])].Temperature = float32(temp)
		} else {
			aa.cacheTemps[aa.ChipIdToIndex(aa.actualChipIds[i])] = -1000.0
			aa.ChipArray[aa.ChipIdToIndex(aa.actualChipIds[i])].Temperature = -1000.0
		}
	}

	return nTotalHits, nil
}

// for debug only
func (aa *AuraAsic) GetAsic() *AuraAsic {
	return aa
}

// AsicInitComplete for debug only
func (aa *AuraAsic) AsicInitComplete() {
	aa.initComplete = true
}
