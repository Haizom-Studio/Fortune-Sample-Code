package block

import "math"

func CalcDifficulty(hash [32]byte) uint64 {
	if len(hash) < 32 {
		return 0
	}
	if hash[31] != 0 {
		return 0
	}
	if hash[30] != 0 {
		return 0
	}
	if hash[29] != 0 {
		return 0
	}
	if hash[28] != 0 {
		return 0
	}
	difficulty := uint64(1)
	i := 27
	for i > 0 {
		if hash[i] == 0 {
			difficulty *= 256
		} else {
			break
		}
		i--
	}
	j := 0
	c := hash[i]
	for j < 8 {
		if c&0x80 == 0x80 {
			break
		} else {
			c = c * 2
			difficulty = difficulty * 2
		}
	}
	return difficulty
}

func NBitsToTarget(nbits uint32) []byte {
	b := make([]byte, 32)
	significand := make([]byte, 4)
	significand[2] = byte((0x00ff0000 & nbits) >> 16)
	significand[1] = byte((0x0000ff00 & nbits) >> 8)
	significand[0] = byte((0x000000ff & nbits) >> 0)
	exponent := (0xff000000 & nbits) >> 24

	if exponent > 0x1d {
		exponent = 0
	} else if exponent >= 3 {
		exponent = exponent - 3
	} else {
		exponent = 0
	}

	//significand * (256 ^ (exponent - 3))

	// b is big endian, b[0] is most significant byte, exponent is the number of 0 on the right side
	b[31-exponent] = significand[0]
	b[31-exponent-1] = significand[1]
	b[31-exponent-2] = significand[2]

	return b
}

func NBitsToDifficulty(nbits uint32) float64 {
	exponent := (nbits >> 24) & 0xff
	if exponent > 0x1d {
		exponent = 0x1d
	}
	exponent_diff := int(8 * (0x1d - exponent))
	significand := float64(nbits & 0xffffff)
	diff := math.Ldexp(0x00FFFF/significand, exponent_diff)
	return diff
}
