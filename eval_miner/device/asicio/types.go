package asicio

import (
	"os"
	"sync"
	"time"

	"eval_miner/device/devhdr"
)

const (
	CMD_NOTHING   uint8 = 0
	CMD_WRITE     uint8 = 1
	CMD_READ      uint8 = 2 // read a register
	CMD_READWRITE uint8 = 3 // read a register and after reading write it(typically with 0)
	CMD_LOAD0     uint8 = 4 // this will load a copy into chunk1 copy0,1,2,3
	CMD_LOAD1     uint8 = 5 // this will load a copy into chunk1 copy1 only
	CMD_LOAD2     uint8 = 6 // this will load a copy into chunk1 copy2 only
	CMD_LOAD3     uint8 = 7 // this will load a copy into chunk1 copy3 only

	CMD_RETURNHIT uint8 = 0x40 // If bit 6 is set of command word, miner will respond with most recent hit info. Don't use with CMD_BROADCAST
	CMD_BROADCAST uint8 = 0x80 // If bit 7 is set of command word, id will be ignored and all miners will accept the command
)

const (
	CMD_UNIQUE = 0x12345678
	RSP_UNIQUE = 0xdac07654
	HIT_UNIQUE = 0xdac07654

	RSP_LEN_CFG = 16
	RSP_LEN_HIT = 92
	IDLE_BYTES  = 20
)

type asicReadPayload struct {
	addr   uint8
	asicId uint8
}

type commandCfgType struct {
	Command_unique uint32
	Id             uint8 // 8b id of the chip to listen to this command, ignored if CMD_BROADCAST
	Command        uint8
	Spare          uint8 // this should be driven to 0 but will be ignored in V1.0 of hardware
	Address        uint8
	Data           uint32 // data unique to cfg
	Crc            uint32
}

type commandLoadType struct {
	Command_unique uint32
	Id             uint8
	Command        uint8
	Nbits          uint8    // this is the difficulty
	Sequence       uint8    // this is unused by asic but carried around to help software identify context of hit
	Load           [80]byte // 80B header, nonce field will be ignored for loads
	Crc            uint32
}

type responseCfgType struct {
	Response_unique uint32
	Id              uint8 // 8b id of the chip responding
	Command         uint8 // copy of command field which caused the response, CMD_RETURNHIT will always be clear
	Spare           uint8 // copy of spare   field which caused the response
	Address         uint8 // copy of address field which caused the response
	// data unique to cfg
	Data uint32
	Crc  uint32
}

type ResponseHitType struct {
	Hit_unique uint32
	Id         uint8 // 8b id of the chip responding
	Command    uint8 // which of the 4 chunk1 values had the hit{CMD_LOAD0, CMD_LOAD1, CMD_LOAD2, CMD_LOAD3}, also CMD_RETURNHIT will be set as well
	Nbits      uint8 // how many zeros were present, this field will be 0 if no hit
	Sequence   uint8 // sequence associated with context that caused the hit
	Result     [80]byte
	Crc        uint32
}

type asicCmd struct {
	seq          uint64 // sequence number for debugging
	requestTime  time.Time
	responseTime time.Time
	finishTime   time.Time

	// in
	readall bool    // read all chips
	targets []uint8 // target asic id or nil for broadcast
	command uint8   // same as Command in struct command_cfgtype
	addrs   []uint8 // addresses to send the command
	load    []byte  // for CMD_LOAD only

	// in/out
	data []int64 // results for register readings
	// out
	resps     int // number of responses received
	done      bool
	issued    bool // in case command queue is full, some cmd could be rejected
	completed bool
	// caller is waiting on this ch to get the result
	ch chan int
}

type AuraAsicIO struct {
	brdChainId   uint8
	slotId       uint8
	singleThread bool

	received *[]byte
	// Actual Chips on the hashboard theoretical number
	totalAsicsOnHB uint
	hbAsicConfig   *devhdr.HashBoardAsicIdConfig
	// IO device for communicating with device
	devName  string
	devFile  *os.File
	baudRate uint32

	// Channel communication between asicReads and cmdReader
	chResp   chan *[]byte
	chinput0 chan *asicCmd // high priority input queue
	chinput  chan *asicCmd

	// FIFOs to track response from ASIC IOs
	qHit  *Fifo
	qResp *Fifo // for single thread case only
	qDiag *Fifo

	// Register read tracks
	cmdRegLock           sync.Mutex
	cmdRegReadMap        map[asicReadPayload][]*asicCmd
	cmdReverseRegReadMap map[*asicCmd][]asicReadPayload
	cmdHistoryLock       sync.Mutex
	cmdHistory           []*asicCmd
	elapsedTime          []time.Duration
	elapsedTimeSum       time.Duration

	// the mutex used to update the variables below from different go routine
	cmdAliveTimeLock sync.Mutex
	// the last timestamp a complete cmd response is seen
	cmdAliveTime time.Time
	// the last timestamp a cmd response is seen
	cmdReadTime time.Time

	// the last window where no cmdreader activity is seen
	cmdBlackoutWindowTime time.Time
	// the duration of that blackout window
	cmdBlackoutWindowDuration time.Duration
}
