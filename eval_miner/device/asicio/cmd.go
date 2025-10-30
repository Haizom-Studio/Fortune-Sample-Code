package asicio

import (
	"fmt"
	"hash/crc32"

	"eval_miner/log"
)

func newAsicCmd(targets []uint8, cmd uint8, addrs []uint8, data []int64) *asicCmd {
	if cmd == CMD_WRITE && len(data) != len(addrs) {
		log.Errorf("for cmd write len of data not match with addrs %v %v", len(data), len(addrs))
		return nil
	}
	return &asicCmd{
		targets: targets,
		command: cmd,
		addrs:   addrs,
		ch:      make(chan int),
		data:    data,
	}
}

func prepareAsicCmd(id uint8, cmd uint8, addr uint8, data uint32) []byte {
	cmd_cfg := commandCfgType{
		Command_unique: CMD_UNIQUE,
		Id:             id,
		Command:        cmd,
		Spare:          0,
		Address:        addr,
		Data:           data,
	}
	msg, _ := Pack(&cmd_cfg)
	cmd_cfg.Crc = crc32.Checksum(msg[:len(msg)-4], crc32.IEEETable) ^ 0xFFFFFFFF
	msg, _ = Pack(&cmd_cfg)
	return msg
}

func (aa *AuraAsicIO) checkCfgRespRaw(v *[]byte) (*responseCfgType, error) {
	var resp responseCfgType
	loss_offset := 0
	// some fixed value fields may be able to recover
	if len(*v) == RSP_LEN_CFG-1 {
		if (*v)[4] != CMD_READ && (*v)[5] == 0 {
			// try to recover the cmd field (offset 5)
			a := append((*v)[:6], (*v)[5:]...)
			a[5] = CMD_READ
			loss_offset = 5
			(*v) = a
		} else if (*v)[5] == CMD_READ && (*v)[6] != 0 {
			// try to recover the spare field (offset 6)
			a := append((*v)[:7], (*v)[6:]...)
			a[6] = 0
			loss_offset = 6
			(*v) = a
		} else {
			log.Errorf("B%d dropped 1byte-loss resp %x", aa.brdChainId, *v)
			return nil, fmt.Errorf("partial unpacked")
		}
	}

	n, err := Unpack(*v, &resp)
	if err != nil {
		return nil, err
	}
	if n != RSP_LEN_CFG {
		return nil, fmt.Errorf("partial unpacked")
	}
	// check crc32
	input := (*v)[:(RSP_LEN_CFG - 4)]
	cksum := crc32.Checksum(input, crc32.IEEETable) ^ 0xFFFFFFFF

	if resp.Crc != cksum {
		log.Errorf("B%d dropped resp for crc32 %x, calculated %x", aa.brdChainId, resp, cksum)
		return nil, fmt.Errorf("unmatched crc32")
	} else if loss_offset > 0 {
		log.Infof("B%d recovered from 1B loss at offset %d", aa.brdChainId, loss_offset)
	}
	return &resp, nil
}

func (aa *AuraAsicIO) CheckCfgResp() (*responseCfgType, error) {
	r := aa.qResp.Pop()
	if r == nil {
		if aa.singleThread {
			aa.asicRead()
			r = aa.qResp.Pop()
		}
		if r == nil {
			return nil, fmt.Errorf("no response yet")
		}
	}
	return aa.checkCfgRespRaw(r.(*[]byte))
}

func (aa *AuraAsicIO) getCfgResp(id uint8, cmd uint8, addr uint8) (uint32, error) {
	resp, err := aa.CheckCfgResp()
	if err != nil {
		return 0, err
	}
	if resp.Id != id || resp.Command != cmd && resp.Address != addr {
		return 0, fmt.Errorf("unmatched result")
	}
	return resp.Data, nil
}

func (aa *AuraAsicIO) putCfgRawResult(ptr *[]byte) {
	if aa.singleThread {
		aa.qResp.Push(ptr)
	} else {
		aa.chResp <- ptr
	}
}

func (aa *AuraAsicIO) putHitRawResult(ptr *[]byte) {
	if (*ptr)[7] == 0 {
		// this is a hit from the 0 job
		log.Debug("hit msg ignored for seq 0")
	} else {
		aa.qHit.Push(ptr)
	}
}

func (aa *AuraAsicIO) CheckHitResult() (*ResponseHitType, error) {
	r := aa.qHit.Pop()
	if r == nil {
		return nil, nil
	}

	v := r.(*[]byte)
	var resp ResponseHitType
	n, err := Unpack(*v, &resp)
	if err != nil {
		return nil, err
	}

	if n != RSP_LEN_HIT {
		return nil, fmt.Errorf("partial unpacked")
	}

	// for the result got from subsequencial reg read, CRC is not set
	// check crc32
	input := (*v)[:(RSP_LEN_HIT - 4)]
	cksum := crc32.Checksum(input, crc32.IEEETable) ^ 0xFFFFFFFF
	if resp.Crc != cksum {
		log.Error("unmatched crc32")
		return &resp, fmt.Errorf("unmatched crc32")
	}

	return &resp, nil
}

func (aa *AuraAsicIO) write(msg []byte) error {
	var err error
	for retry := 3; retry > 0; retry-- {
		_, err = aa.devFile.Write(msg)
		if err == nil {
			if retry < 3 {
				log.Infof("write: succeeded on retry %d", 3-retry)
			}
			return err
		}
	}
	return err
}

func (aa *AuraAsicIO) WriteIdle(n int) error {
	msg := make([]byte, n)
	return aa.write(msg)
}

func (aa *AuraAsicIO) prepareCmdData(cmd *asicCmd) {
	var datalen int
	if cmd.readall {
		datalen = len(cmd.addrs) * int(aa.totalAsicsOnHB)
	} else {
		datalen = len(cmd.addrs) * len(cmd.targets)
	}
	cmd.data = make([]int64, datalen)
	for i := 0; i < datalen; i++ {
		cmd.data[i] = -1
	}
}

func (aa *AuraAsicIO) ClearCfgResp() {
	aa.qResp.Clear()
}

func (aa *AuraAsicIO) SetBlockingReadMode(blockMode bool) {
	if blockMode {
		aa.singleThread = true
		return
	}
	aa.singleThread = false
}

func (aa *AuraAsicIO) SetSetBaudRate(baudRate uint32) {
	aa.baudRate = baudRate
}
