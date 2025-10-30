package asicio

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"math"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"eval_miner/device/devhdr"
	"eval_miner/log"
)

// asicReadTimeouts ----- if a cmd response does not arrive within this threshold, the cmd will be timeout. This is based on the assumption cmdreader is running.
//			  if for some reason, cmdreader is in sleep/not-running mode, we should compensate that non-activity cmdreader time.
// asicActivityMaxTimeouts ---- if cmdreader is running (not in sleep mode), the time since the last cmdReadTime should be small. If in cmd_timeout go routine,
//			  we are seeing the time since the last cmdReadTime is larger than this threshold, the cmdreader should be in no-activity mode.
//			  Then we should not do the actual timeout, instead of putting the command back to cmd history list and check again next time.
// asicActivityMinTimeouts ---- if cmdreader is active for the past asicActivityMinTimeouts milli-second, we may expect cmd response should arrive soon.
//			  So we should give some more time for cmd to complete.

const (
	asicCommandHistoryPollingPeriod      = 20 * time.Millisecond
	asicActivityMinTimeouts              = 20 * time.Millisecond
	asicActivityMaxTimeouts              = 100 * time.Millisecond
	asicReadTimeouts                     = 200 * time.Millisecond
	asicReadMaxTimeouts                  = 1000 * time.Millisecond
	asicRegisterReadRoundTripCount       = 1000
	asicRegisterReadLoggingTime60Seconds = 300
)

// releaseCommand releases the command by sending a value to its channel
func (aa *AuraAsicIO) releaseCommand(c *asicCmd, val int) {
	// If this command is already processed return
	if c.completed {
		return
	}
	if c.finishTime == (time.Time{}) || c.responseTime == (time.Time{}) {
		c.ch <- val
		return
	}

	if len(aa.elapsedTime) >= asicRegisterReadRoundTripCount {
		aa.elapsedTimeSum -= aa.elapsedTime[0]
		aa.elapsedTime = aa.elapsedTime[1:]
	}
	aa.elapsedTime = append(aa.elapsedTime, c.finishTime.Sub(c.requestTime))
	aa.elapsedTimeSum += c.finishTime.Sub(c.requestTime)
	c.ch <- val
	c.completed = true
}

// cmdTimeoutChecker checks for command timeouts and removes timed-out commands from history
func (aa *AuraAsicIO) cmdTimeoutChecker() {
	var count int
	for {
		time.Sleep(asicCommandHistoryPollingPeriod)
		var pendingCmds []uint64
		var pendingMapCmds []uint64
		count++
		aa.cmdHistoryLock.Lock()
		var newCommandHistory []*asicCmd
		if len(aa.cmdHistory) > 8 {
			log.Errorf("Warning: cmdHistoryLen %d ", len(aa.cmdHistory))
		}
		for idx := len(aa.cmdHistory) - 1; idx >= 0; idx-- {
			cmd := aa.cmdHistory[idx]
			if time.Since(cmd.requestTime) >= asicReadTimeouts {
				// 	Comment: There could be HUGE time difference between two continuous cmdreader iterations. This huge
				//	time difference could cause false cmd timeout:
				//	Case 1: cmd request is submitted before the blackout window (cmdreader was in sleep/no-activity mode)
				//	Case 2: the time since last activity time is longer than the threshold (cmdreader is now in sleep/no-activity mode)
				//	Case 3: no or incomplete cmd response, but cmdreader is actively getting response.
				//	Case 4: bad asic. When bad asic exists, there will be no response from the bad asic even though cmd channel is alive.
				//		Under this scenario, we will timeout the cmd anyway after the threshold asicReadMaxTimeout is hit
				if time.Since(cmd.requestTime) >= asicReadMaxTimeouts {
					cmd.finishTime = time.Now()
					log.Errorf("Timedout: request Time %v response Time %v [%d %d] Blackout Time [ %v %v] cur_time %v last_alive_time %v last read time %v",
						cmd.requestTime, cmd.responseTime, cmd.resps, (len(cmd.targets) * len(cmd.addrs)), aa.cmdBlackoutWindowTime, aa.cmdBlackoutWindowDuration, time.Now(), aa.cmdAliveTime, aa.cmdReadTime)
					aa.removePayloadsFromMapandRelease(cmd, 1)
				} else if cmd.requestTime.Before(aa.cmdBlackoutWindowTime) { // cmd is submitted before the blackout window
					cmd.requestTime = aa.cmdBlackoutWindowTime.Add(aa.cmdBlackoutWindowDuration)
					pendingCmds = append(pendingCmds, cmd.seq)
					newCommandHistory = append(newCommandHistory, cmd)
				} else if time.Since(aa.cmdReadTime) > asicActivityMaxTimeouts { // no cmdreader activity for 100 second
					pendingCmds = append(pendingCmds, cmd.seq)
					newCommandHistory = append(newCommandHistory, cmd)
				} else if time.Since(aa.cmdReadTime) < asicActivityMinTimeouts { // have cmdreader activity within 20 second, therefore we expect cmd response will come soon
					pendingCmds = append(pendingCmds, cmd.seq)
					newCommandHistory = append(newCommandHistory, cmd)
				} else {
					cmd.finishTime = time.Now()
					log.Errorf("Board-%d Timedout elapse %v addrs %v seq: %v resps %v targets %v", aa.brdChainId,
						time.Since(cmd.requestTime).Milliseconds(), cmd.addrs, cmd.seq, cmd.resps, cmd.targets)
					aa.removePayloadsFromMapandRelease(cmd, 1)
				}
			} else {
				pendingCmds = append(pendingCmds, cmd.seq)
				newCommandHistory = append(newCommandHistory, cmd)
			}
		}
		aa.cmdHistory = newCommandHistory
		aa.cmdHistoryLock.Unlock()

		if count >= asicRegisterReadLoggingTime60Seconds && len(aa.elapsedTime) >= asicRegisterReadRoundTripCount {
			count = 0
			aa.cmdRegLock.Lock()
			for k := range aa.cmdReverseRegReadMap {
				pendingMapCmds = append(pendingMapCmds, k.seq)
			}
			aa.cmdRegLock.Unlock()
		}
	}
}

// getCmdRegReadFromPayload returns the oldest cmd pointer for the given payload
func (aa *AuraAsicIO) getCmdRegReadFromPayload(asicReg asicReadPayload) *asicCmd {
	aa.cmdRegLock.Lock()
	defer aa.cmdRegLock.Unlock()
	cmds := aa.cmdRegReadMap[asicReg]
	var res *asicCmd
	var lower uint64 = math.MaxUint64
	for _, cmd := range cmds {
		if cmd.seq <= lower {
			lower = cmd.seq
			res = cmd
		}
	}
	return res
}

// createPayloadEntriesInMap creates payload entries in the following maps
// aa.cmdRegReadMap and aa.cmdReverseRegReadMap
func (aa *AuraAsicIO) createPayloadEntriesInMap(cmd *asicCmd) {
	aa.cmdRegLock.Lock()
	payloads := make([]asicReadPayload, 0)
	for _, asicId := range cmd.targets {
		for _, addr := range cmd.addrs {
			payload := asicReadPayload{addr: addr, asicId: asicId}
			aa.cmdRegReadMap[payload] = append(aa.cmdRegReadMap[payload], cmd)
			payloads = append(payloads, payload)
		}
	}
	aa.cmdReverseRegReadMap[cmd] = payloads
	aa.cmdRegLock.Unlock()

	aa.cmdHistoryLock.Lock()
	aa.cmdHistory = append(aa.cmdHistory, cmd)
	cmd.requestTime = time.Now()
	aa.cmdHistoryLock.Unlock()
}

// removePayloadsFromMapandRelease removes payloads associated with the given command from maps
func (aa *AuraAsicIO) removePayloadsFromMapandRelease(cmd *asicCmd, val int) {
	aa.cmdRegLock.Lock()
	defer aa.cmdRegLock.Unlock()
	payloads := aa.cmdReverseRegReadMap[cmd]
	for _, payload := range payloads {
		cmds := aa.cmdRegReadMap[payload]
		for i, cmdValue := range cmds {
			if cmd == cmdValue {
				aa.cmdRegReadMap[payload] = append(cmds[:i], cmds[i+1:]...)
				break
			}
		}
	}
	delete(aa.cmdReverseRegReadMap, cmd)
	aa.releaseCommand(cmd, val)
}

// updatePayload updates the command with the response and marks it as done if all responses received
func (aa *AuraAsicIO) updatePayload(resp *responseCfgType) *asicCmd {
	// Software Initiated Read Error for ASICs
	/*
		for _, errAsic := range aa.swReadErrorAsics {
			if errAsic == resp.Id {
				return nil
			}
		}

	*/

	cmd := aa.getCmdRegReadFromPayload(asicReadPayload{addr: resp.Address, asicId: resp.Id})
	if cmd == nil {
		// Comment: too many log messages when cmd timeout happens
		// log.Errorf("board-%d updatePayload couldn't find command for resp %+v", aa.brdChainId, resp)
		return cmd
	}

	var asic_idx, addr_idx int
	if !cmd.readall {
		for idx, val := range cmd.targets {
			if resp.Id == val {
				asic_idx = idx
				break
			}
		}
	} else {
		asic_idx = devhdr.ChipIDtoIndex(aa.hbAsicConfig, resp.Id)
	}
	for idx, val := range cmd.addrs {
		if resp.Address == val {
			addr_idx = idx
		}
	}
	cmd.data[len(cmd.addrs)*asic_idx+addr_idx] = int64(resp.Data)
	if cmd.responseTime == (time.Time{}) {
		cmd.responseTime = time.Now()
	}
	cmd.resps++
	if cmd.resps == (len(cmd.targets) * len(cmd.addrs)) {
		cmd.finishTime = time.Now()
		cmd.done = true
		aa.removeCommand(cmd, 0)

		// Comment: due to OS scheduling, cmd may be timedout from time to time due to some resource spike such as the number
		//      of active processes/CPU/memory). If this timeout happens, we may get invalid cmd response which impact the error
		//      handling in dvfs based on counter/temp/volt READ.
		//      We monitor the cmdAliveTime and cmdTimedoutTime and use this variables to determine a timeout if false positive.
		//
		aa.cmdAliveTimeLock.Lock()
		aa.cmdAliveTime = time.Now()
		aa.cmdAliveTimeLock.Unlock()
	}
	return cmd
}

// removeCommand removes the given command from cmdHistory and associated maps
func (aa *AuraAsicIO) removeCommand(cmdInput *asicCmd, val int) {
	aa.cmdHistoryLock.Lock()
	for idx, cmd := range aa.cmdHistory {
		if cmdInput == cmd {
			aa.cmdHistory = append(aa.cmdHistory[:idx], aa.cmdHistory[idx+1:]...)
			break
		}
	}
	aa.cmdHistoryLock.Unlock()
	aa.removePayloadsFromMapandRelease(cmdInput, val)
}

// BlockingWrite performs blocking write to ASIC
func (aa *AuraAsicIO) BlockingWrite(asicId, addr, cmd uint8, data uint32, broadcast bool) error {
	if broadcast {
		cmd |= CMD_BROADCAST
	}
	msg := prepareAsicCmd(asicId, cmd, addr, data)
	return aa.write(msg)
}

// BlockingReadWrite performs blocking read/write to ASIC
func (aa *AuraAsicIO) BlockingReadWrite(asicId, addr, cmd uint8, data uint32) error {
	msg := prepareAsicCmd(asicId, cmd, addr, data)
	return aa.write(msg)
}

// NonBlockingWrite perform asynchronous write to ASICs
func (aa *AuraAsicIO) NonBlockingWrite(targets, addrs []uint8, data []int64, broadcast bool) error {
	cmd := CMD_WRITE
	if broadcast {
		cmd |= CMD_BROADCAST
	}
	asicCommand := newAsicCmd(targets, cmd, addrs, data)
	// put it to input channel
	aa.chinput <- asicCommand
	return nil
}

// NonBlockingRead perform asynchronous read to ASICs
func (aa *AuraAsicIO) NonBlockingRead(targets, addrs []uint8, data []int64, cmd uint8) (int, []int64, error) {
	asicCommand := newAsicCmd(targets, cmd, addrs, data)
	// put it to input channel
	aa.chinput <- asicCommand
	res := <-asicCommand.ch
	if asicCommand.data == nil {
		return -1, []int64{0}, fmt.Errorf("failed to read register")
	}
	return res, asicCommand.data, nil
}

func (aa *AuraAsicIO) getRegReadTimeout(times int64) time.Duration {
	base := int64(RSP_LEN_CFG * 10000000 * 5 / aa.baudRate)
	ret := base * (times + 20)
	// at least wait for 1ms as time is not accurate
	if ret < 1000 {
		ret = 1000
	}
	return time.Duration(ret * int64(time.Microsecond))
}

func (aa *AuraAsicIO) BlockingRead(asicId, addr, cmd uint8, data uint32) (uint32, error) {
	_ = aa.WriteIdle(IDLE_BYTES)
	if ok := aa.BlockingWrite(asicId, addr, cmd, data, false); ok != nil {
		return 0, ok
	}
	timeout := aa.getRegReadTimeout(40)
	a := time.Now()
	for {
		time.Sleep(time.Microsecond)
		v, err := aa.getCfgResp(asicId, cmd, addr)

		if err == nil {
			return v, nil
		}
		if time.Since(a) > timeout {
			break
		}
	}
	return 0, fmt.Errorf("timeout talking with Asic")
}

func (aa *AuraAsicIO) CloseASICIO() {
	aa.chinput <- nil
	aa.chResp <- nil
	time.Sleep(200 * time.Millisecond)
}

// 	cmdreader is the go routine to read and parse cmd response.
// 	due to the reasons below:
//	1)  cmdreader could be run in time slice determined by OS. The default size of time slice is 100ms. So time difference
//		between two continuous cmdreader iterations could be as large as N X 100ms where N is the active processes.
//	2)  cmdreader could wait for channel output for response.
//	Due to this potential long duration between two cmdreader, a cmd could be timed out at the cmdTimerCheck go routine
//	This false cmd timeout will then cause the NULL or incomplete output of readRegsPipelined and then cuase the zero hash
//	and malfunction of HB.
//
//	To avoid or mitigate this false timeout, we monitor the following time/duration in cmdreader loop:
//	1. aa.cmdBlackoutWindowTime and aa.cmdBlackoutWindowDuration
//	2. aa.cmdReadTime
//
//	When doing cmd_timeout, we are checking the time difference between cmd.requestTime and the current time. So the
//	following false cmd_time scenario could be identified with this new time/dutation:
//
//	Scenario 1:  (to-be-timeout cmd is submitted before blackout window)
//		due to this cmdreader being scheduled out and not running during that blackout window, we should not
//		timeout this cmd, instead of update the requestTime and check the timeout later.
//
//	Scenario 2:  cmdreader is still in blackout state (no cmdreader activity around/before the time of timeout checking)
//		cmd.requestTime < aa.cmdReadTime << cur_time (timeout checking)
//

func (aa *AuraAsicIO) cmdreader() {
	var cmd_rd_time time.Time
	cmd_rd_time = time.Now()
	for {
		if time.Since(cmd_rd_time).Milliseconds() > 50 {
			aa.cmdAliveTimeLock.Lock()
			aa.cmdBlackoutWindowTime = cmd_rd_time // Update the cmdreader blackout_window
			aa.cmdBlackoutWindowDuration = time.Since(cmd_rd_time)
			aa.cmdAliveTimeLock.Unlock()
		}

		cmd_rd_time = time.Now()

		aa.cmdAliveTimeLock.Lock()
		aa.cmdReadTime = cmd_rd_time // Update the last activity time for cmdreader
		aa.cmdAliveTimeLock.Unlock()

		var resp *responseCfgType
		select {
		case ac := <-aa.chResp:
			if ac == nil {
				// this only happens at terminating.
				log.Infof("bd %d: terminating cmdreader", aa.brdChainId)
				break
			}
			resp, _ = aa.checkCfgRespRaw(ac)
			if resp == nil {
				continue
			}
			aa.updatePayload(resp)
		}
	}
}

func (aa *AuraAsicIO) cmdwriter() {
	seq := uint64(0)
	var ri *asicCmd
	for {
		// poll high priority queue first, the default branch makes it non-blocking
		select {
		case ri = <-aa.chinput0:
		default:
			ri = nil
		}

		if ri == nil {
			select {
			case ri = <-aa.chinput0:
			case ri = <-aa.chinput:
			}
		}

		if ri == nil {
			// this only happens at terminating.
			break
		}

		is_read := ri.command == CMD_READ || ri.command == CMD_READWRITE // only READWRITE 0 for now.
		if !is_read && ri.command != CMD_RETURNHIT && ri.command != CMD_WRITE && ri.command != CMD_LOAD0 && ri.command != (CMD_WRITE|CMD_BROADCAST) {
			ri.ch <- 0
			log.Errorf("unknown command %x", ri.command)
			continue
		}

		if ri.targets == nil {
			if ri.command == CMD_LOAD0 {
				_ = aa.write(ri.load)
				log.Debugf("issued load0 %x", ri.load)
			} else if ri.command&CMD_WRITE == CMD_WRITE {
				if ri.command&CMD_BROADCAST != CMD_BROADCAST {
					log.Errorf("write command without chipid must be broadcast, addr %x", ri.addrs)
					continue
				}
				for i, addr := range ri.addrs {
					msg := prepareAsicCmd(0, ri.command, addr, uint32(ri.data[i]))
					_ = aa.write(msg)
				}
			}
		} else {
			if ri.command == CMD_LOAD0 {
				log.Errorf("LOAD0 command must be broadcast")
			}
		}

		if is_read {
			// move to next stage of pipeline for response.
			seq++
			ri.seq = seq
			aa.prepareCmdData(ri)
			aa.createPayloadEntriesInMap(ri)
		}

		for i, target := range ri.targets {
			for j, addr := range ri.addrs {
				if ri.command == CMD_WRITE {
					msg := prepareAsicCmd(target, CMD_WRITE, addr, uint32(ri.data[i*len(ri.addrs)+j]))
					_ = aa.write(msg)
				} else {
					_ = aa.WriteIdle(IDLE_BYTES)
					msg := prepareAsicCmd(target, ri.command, addr, 0)
					_ = aa.write(msg)
					// leave enough idle time for response before next command
					if ri.command == CMD_RETURNHIT {
						_ = aa.WriteIdle(RSP_LEN_HIT)
					}
				}
			}
		}
		ri.issued = true
	}
}

func (aa *AuraAsicIO) asicRead() {
	buf := make([]byte, 1024)
	resp_magic := []byte{0x54, 0x76, 0xc0, 0xda}
	frameStart := false
	var n int

	for {
		if aa.singleThread {
			// use sys.unix.Poll() to avoid blocking
			pollfd := []unix.PollFd{{Fd: int32(aa.devFile.Fd()), Events: unix.POLLIN}}
			ret, _ := unix.Poll(pollfd, 0)
			if ret > 0 && pollfd[0].Revents&unix.POLLIN != 0 {
				n, _ = aa.devFile.Read(buf)
			} else {
				break
			}
		} else {
			n, _ = aa.devFile.Read(buf)
		}

		//log.Infof("asicRead %dB: %x", n, buf[:n])
	leftover:
		if n > 0 {
			if aa.received == nil {
				frameStart = false
				if n > len(buf) { // Watch out for overrun case - shouldn't happen, but it did once...
					n = len(buf)
				}
				resp := make([]byte, n)
				copy(resp, buf[:n])
				aa.received = &resp
			} else {
				*aa.received = append(*aa.received, buf[:n]...)
			}

			acc := len(*aa.received)
			if !frameStart {
				if acc > 4 {
					idx := bytes.Index(*aa.received, resp_magic)
					if idx == RSP_LEN_CFG-1 || idx == (RSP_LEN_CFG-1)*2 || (idx < 0 && (acc == RSP_LEN_CFG-1 || acc == (RSP_LEN_CFG-1)*2)) {
						// there's a high chance that 1 byte of the unique number is dropped, need to debug further
						b0 := (*aa.received)[0]
						b1 := (*aa.received)[1]
						b2 := (*aa.received)[2]
						if ((b1 == resp_magic[2] && b2 == resp_magic[3] && (b0 == resp_magic[0] || b0 == resp_magic[1])) ||
							(b0 == resp_magic[0] && b1 == resp_magic[1] && (b2 == resp_magic[2] || b2 == resp_magic[3]))) &&
							((*aa.received)[4] == CMD_READ || (*aa.received)[4] == CMD_READWRITE) && (*aa.received)[5] == 0 {
							log.Infof("B%d recovered from 1B loss at 0-3", aa.brdChainId)
							r := append(resp_magic[0:4], (*aa.received)[3:acc]...)
							aa.received = &r
							acc++
							idx = 0
						}
					}

					if idx > 0 {
						log.Infof("bd %d dropped %dB before magic %x", aa.brdChainId, idx, (*aa.received)[:idx])
						r := (*aa.received)[idx:acc]
						aa.received = &r
						acc = len(*aa.received)
						frameStart = true
					} else if idx == 0 {
						frameStart = true
					}
				}
			}

			// drop the whole received bytes if command start magic is not detected
			if !frameStart {
				if acc > 1024 {
					r := (*aa.received)[acc-3 : acc]
					aa.received = &r
				}
				continue
			}

			if aa.received != nil && len(*aa.received) >= 6 {
				r := *aa.received
				if r[5] >= CMD_LOAD0+CMD_RETURNHIT && r[5] <= CMD_LOAD3+CMD_RETURNHIT {
					// this is a hit result
					if len(r) == RSP_LEN_HIT {
						aa.putHitRawResult(aa.received)
						aa.received = nil
					} else if len(r) > RSP_LEN_HIT {
						copy(buf, (*aa.received)[RSP_LEN_HIT:])
						n = len(r) - RSP_LEN_HIT
						*aa.received = (*aa.received)[:RSP_LEN_HIT]
						aa.putHitRawResult(aa.received)
						aa.received = nil
						goto leftover
					}
				} else {
					// this is a command result
					if len(r) == RSP_LEN_CFG {
						aa.putCfgRawResult(aa.received)
						aa.received = nil
					} else if len(r) > RSP_LEN_CFG {
						cpylen := RSP_LEN_CFG
						// handle the case of one byte loss
						if (*aa.received)[RSP_LEN_CFG-1] == 0x54 && (*aa.received)[RSP_LEN_CFG] == 0x76 {
							cpylen -= 1
						}
						copy(buf, (*aa.received)[cpylen:])
						n = len(r) - cpylen
						*aa.received = (*aa.received)[:cpylen]
						aa.putCfgRawResult(aa.received)
						aa.received = nil
						goto leftover
					}
				}
			}
		}
	}
}

func (aa *AuraAsicIO) ReqestHitResult(asicId uint8) error {
	ac := newAsicCmd([]uint8{asicId}, CMD_RETURNHIT, []uint8{0}, nil)
	aa.chinput0 <- ac
	return nil
}

func (aa *AuraAsicIO) AsicLoad(difficulty uint8, seq uint8, job [80]byte) error {
	cmdLoad := commandLoadType{
		Command_unique: CMD_UNIQUE,
		Id:             0,
		Command:        CMD_LOAD0 | CMD_BROADCAST,
		Nbits:          difficulty,
		Sequence:       seq,
		Load:           job,
	}
	msg, _ := Pack(&cmdLoad)
	cmdLoad.Crc = crc32.Checksum(msg[:len(msg)-4], crc32.IEEETable) ^ 0xFFFFFFFF
	msg, _ = Pack(&cmdLoad)
	cmd := &asicCmd{
		command: CMD_LOAD0,
		addrs:   []uint8{0},
		ch:      make(chan int),
		load:    msg,
	}
	aa.chinput0 <- cmd
	return nil
}

func NewAsicIOInit(baudRate uint32, devName string, brdChainId uint8, slotId uint8, asicIdCfg *devhdr.HashBoardAsicIdConfig) (*AuraAsicIO, error) {
	var asicIo AuraAsicIO
	var err error
	// uart info
	asicIo.devName = devName
	asicIo.baudRate = baudRate
	if asicIo.devFile, err = os.OpenFile(devName, os.O_RDWR|os.O_SYNC, 0644); err != nil {
		return nil, fmt.Errorf("error accessing device %v", devName)
	}
	// board slot information
	asicIo.hbAsicConfig = asicIdCfg
	asicIo.brdChainId = brdChainId
	asicIo.slotId = slotId
	asicIo.singleThread = false
	asicIo.received = nil
	asicIo.totalAsicsOnHB = 2 + uint(asicIdCfg.ChipsLow.High-asicIdCfg.ChipsLow.Low+asicIdCfg.ChipsHi.High-asicIdCfg.ChipsHi.Low)
	// initialize fifo for data transfer from uart
	asicIo.qHit = NewFifo()
	asicIo.qResp = NewFifo()

	asicIo.chinput0 = make(chan *asicCmd, 8)
	asicIo.chinput = make(chan *asicCmd, 8)
	asicIo.chResp = make(chan *[]byte, 8)
	asicIo.cmdHistory = make([]*asicCmd, 0)
	asicIo.cmdRegReadMap = make(map[asicReadPayload][]*asicCmd)
	asicIo.cmdReverseRegReadMap = make(map[*asicCmd][]asicReadPayload)
	asicIo.elapsedTime = make([]time.Duration, 0)
	return &asicIo, nil
}

func (aa *AuraAsicIO) EnableAsyncRW() {
	go aa.asicRead()
	go aa.cmdreader()
	go aa.cmdwriter()
	go aa.cmdTimeoutChecker()
}
