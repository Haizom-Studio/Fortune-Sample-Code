package chip

const (
	SEQ_MAX             = 255
	SEQ_HASHRATE_UPDATE = 9999

	CHIP_MAX = 256
)

type Message struct {
	Seq         uint
	Diff        uint
	Chip        uint
	Engine      uint
	Board       uint
	TrueHit     [CHIP_MAX]uint
	GenHit      [CHIP_MAX]uint
	HitRate     [CHIP_MAX]float32
	Body        string
	VersionMask uint32
}
