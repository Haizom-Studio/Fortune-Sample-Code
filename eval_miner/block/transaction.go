package block

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
)

/*
 	1 byte, 0 - 252
	3 bytes, 253 - 0xffff, 0xfd uint16
	5 bytes, 0x10000 - 0xffffffff 0xfe uint32
	9 bytes, 0x100000000 && <= 0xffffffffffffffff 0xff uint64
*/
type CompactSizeUint struct {
	Raw   []byte
	Value uint64
	Ok    bool
}

var ErrCSUTooSmall = errors.New("CompactSizeUint too small")

func (CSUint *CompactSizeUint) Decode(b []byte) (int, error) {
	err := ErrCSUTooSmall
	pos := 0
	len := len(b)

	if len < 1 {
		return pos, err
	}

	switch uint(b[0]) {
	case 0xff:
		if len < 9 {
			return pos, err
		}
		CSUint.Raw = b[0:9]
		CSUint.Value = binary.LittleEndian.Uint64(CSUint.Raw[1:9])
		pos += 9
	case 0xfe:
		if len < 5 {
			return pos, err
		}
		CSUint.Raw = b[0:5]
		CSUint.Value = uint64(binary.LittleEndian.Uint32(CSUint.Raw[1:5]))
		pos += 5
	case 0xfd:
		if len < 3 {
			return pos, err
		}
		CSUint.Raw = b[0:3]
		CSUint.Value = uint64(binary.LittleEndian.Uint16(CSUint.Raw[1:3]))
		pos += 3
	default:
		CSUint.Raw = b[0:1]
		CSUint.Value = uint64(CSUint.Raw[0])
		pos += 1
	}

	CSUint.Ok = true
	return pos, nil
}

type OutPoint struct {
	Hash  [32]byte
	Index uint32
	Ok    bool
}

type TxIn struct {
	PrevOutput      OutPoint
	ScriptBytes     CompactSizeUint
	SginatureScript []byte
	Sequence        uint32
	Ok              bool
}

type TxOut struct {
	Value         int64
	PKScriptBytes CompactSizeUint
	PKScript      []byte
	Ok            bool
}

var ErrTxOutTooSmall = errors.New("TxOut too small")

func (out *TxOut) Decode(b []byte) (int, error) {
	err := ErrTxOutTooSmall
	pos := 0
	len := len(b)

	n := 8
	if len < n {
		return pos, err
	}
	out.Value = int64(binary.LittleEndian.Uint64(b[0:9]))
	pos += n
	len -= n

	n, err = out.PKScriptBytes.Decode(b[pos:])
	if err != nil {
		return pos, err
	}
	pos += n
	len -= n

	n = int(out.PKScriptBytes.Value)
	if n < 0 || len < n {
		return pos, err
	}
	out.PKScript = b[pos : pos+n]
	pos += n
	//len -= n

	out.Ok = true
	return pos, nil
}

type TxRaw struct {
	Version    uint32
	TxInCount  CompactSizeUint
	TxIn       []TxIn
	TxOutCount CompactSizeUint
	TxOut      []TxOut
	LockTime   uint32
	Ok         bool
}

type TxInCoinBase struct {
	Hash           [32]byte // 32-byte null
	Index          uint32   // 0xffffffff
	ScriptBytes    CompactSizeUint
	HeightBytes    CompactSizeUint
	Height         []byte
	CoinBaseScript []byte
	Sequence       uint32
	Ok             bool
}

func (in *TxInCoinBase) GetHeight() uint64 {
	b := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	n := len(in.Height)
	copy(b[0:n], in.Height[0:n])
	heightValue := binary.LittleEndian.Uint64(b[0:])

	return heightValue
}

var ErrTxInCoinBaseTooSmall = errors.New("TxInCoinBase too small")

func (in *TxInCoinBase) Decode(b []byte) (int, error) {
	err := ErrTxInCoinBaseTooSmall
	pos := 0
	len := len(b)

	n := 32
	if len < n {
		return pos, err
	}
	copy(in.Hash[:], b[pos:pos+n])
	pos += n
	len -= n

	n = 4
	if len < n {
		return pos, err
	}
	in.Index = binary.LittleEndian.Uint32(b[pos : pos+n])
	pos += n
	len -= n

	n, err = in.ScriptBytes.Decode(b[pos:])
	if err != nil {
		return pos, err
	}
	pos += n
	len -= n

	n, err = in.HeightBytes.Decode(b[pos:])
	if err != nil {
		return pos, err
	}
	pos += n
	len -= n
	// need to know the bytes used by HeightBytes
	n0 := n

	n = int(in.HeightBytes.Value)
	if len < n {
		return pos, err
	}
	in.Height = b[pos : pos+n]
	pos += n
	len -= n

	if int(in.ScriptBytes.Value) < n0+int(in.HeightBytes.Value) {
		return pos, err
	}
	n = int(in.ScriptBytes.Value) - n0 - int(in.HeightBytes.Value)
	if len < n {
		return pos, err
	}
	in.CoinBaseScript = b[pos : pos+n]
	pos += n
	len -= n

	n = 4
	if len < n {
		return pos, err
	}
	in.Sequence = binary.LittleEndian.Uint32(b[pos : pos+n])
	pos += n
	//len -= n

	in.Ok = true
	return pos, nil
}

type TxCoinBase struct {
	Version      uint32
	TxInCount    CompactSizeUint // always 1
	TxInCoinBase TxInCoinBase
	TxOutCount   CompactSizeUint
	TxOut        []TxOut
	LockTime     uint32
	Ok           bool
}

var ErrTxCoinBaseTooSmall = errors.New("TxCoinBase too small")

func (tx *TxCoinBase) Decode(b []byte) (int, error) {
	err := ErrTxCoinBaseTooSmall
	pos := 0
	len := len(b)
	n := 0

	if len < 4 {
		return 0, err
	}
	tx.Version = binary.LittleEndian.Uint32(b[0:4])
	len -= 4
	pos += 4

	n, err = tx.TxInCount.Decode(b[pos:])
	if err != nil {
		return pos, err
	}
	pos += n
	len -= n

	n, err = tx.TxInCoinBase.Decode(b[pos:])
	if err != nil {
		return pos, err
	}
	pos += n
	len -= n

	n, err = tx.TxOutCount.Decode(b[pos:])
	if err != nil {
		return pos, err
	}
	pos += n
	len -= n

	i := 0
	tx.TxOut = make([]TxOut, tx.TxOutCount.Value)
	for i < int(tx.TxOutCount.Value) {
		n, err = tx.TxOut[i].Decode(b[pos:])
		if err != nil {
			return pos, err
		}
		pos += n
		len -= n

		i++
	}

	if len < 4 {
		return pos, err
	}
	tx.LockTime = binary.LittleEndian.Uint32(b[pos : pos+4])
	pos += 4
	//len -= 4

	tx.Ok = true
	return pos, nil
}

func (tx *TxCoinBase) DecodeString(hexstr string) (int, error) {
	hex0, err := hex.DecodeString(hexstr)

	if err != nil {
		return 0, err
	}

	return tx.Decode(hex0)
}
