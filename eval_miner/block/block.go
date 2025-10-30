package block

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
)

type BlockHeader struct {
	Version        uint32   // 0:3
	PrevHash       [32]byte // 4:35
	MerkleRootHash [32]byte // 36:67
	Time           uint32   // 68:71
	NBits          uint32   // 72:75
	Nonce          uint32   // 76:79
}

var ErrBytesLenNot80 = errors.New("ErrBytesLenNot80")

func Byte2BlockHeader(b []byte) (*BlockHeader, error) {
	if len(b) != 80 {
		return nil, ErrBytesLenNot80
	}

	bh := BlockHeader{
		Version: binary.LittleEndian.Uint32(b[0:4]),
		Time:    binary.LittleEndian.Uint32(b[68:72]),
		NBits:   binary.LittleEndian.Uint32(b[72:76]),
		Nonce:   binary.LittleEndian.Uint32(b[76:80]),
	}
	copy(bh.PrevHash[:], b[4:36])
	copy(bh.MerkleRootHash[:], b[36:68])

	return &bh, nil
}

func BlockHeader2Byte(bh BlockHeader) []byte {
	b := []byte{}

	a := make([]byte, 4)
	binary.LittleEndian.PutUint32(a, bh.Version)
	b = append(b, a...)

	b = append(b, bh.PrevHash[:]...)
	b = append(b, bh.MerkleRootHash[:]...)

	binary.LittleEndian.PutUint32(a, bh.Time)
	b = append(b, a...)

	binary.LittleEndian.PutUint32(a, bh.NBits)
	b = append(b, a...)

	binary.LittleEndian.PutUint32(a, bh.Nonce)
	b = append(b, a...)

	return b
}

func SwapByteInSHA256(sha_in []byte) []byte {
	if len(sha_in) != 32 {
		return nil
	}

	sha_out := make([]byte, len(sha_in))
	i := 0
	for i < 32 {
		sha_out[i] = sha_in[32-i-1]
		i++
	}
	return sha_out
}

func GetBlockHeader(Version uint32, PrevHash string, MRH string, Time uint32, NBits uint32, Nonce uint32) BlockHeader {
	ph_data, _ := hex.DecodeString(PrevHash)
	mrh_data, _ := hex.DecodeString(MRH)

	bh := BlockHeader{
		Version: Version,
		Time:    Time,
		NBits:   NBits,
		Nonce:   Nonce,
	}

	copy(bh.PrevHash[:], ph_data)
	copy(bh.MerkleRootHash[:], mrh_data)

	return bh
}
