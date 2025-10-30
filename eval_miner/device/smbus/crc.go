package smbus

import "fmt"

const (
	crcInit = 0x00
	READ    = 0x01
	WRITE   = 0x00
)

// calcPEC calculates the PEC per SMBUS protocol
func CalcPEC(addr uint8, rdwr uint8, data []byte) (uint8, error) {
	var crc uint8 = crcInit

	if rdwr > READ {
		return 0, fmt.Errorf("Invalid rdwr value: %d", rdwr)
	}

	if addr > 0x7f {
		return 0, fmt.Errorf("Invalid address value: %d", addr)
	}

	// PEC is calculated on the address and the data
	mydata := append([]byte{addr<<1 + rdwr}, data...)

	for _, b := range mydata {
		index := crc ^ b
		crc = CRC_mg_au8CrcTable[index]
	}

	return crc, nil
}

// CalculateCRC8 calculates the CRC8 on a byte array
// Use this to generate the lookup table, or if you are paranoid.
func CalcCRC8(data []byte) uint8 {
	var crc uint8 = crcInit

	for _, b := range data {
		crc ^= b

		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}

	return crc
}

// AppendPEC appends the PEC to a byte array
func AppendPEC(addr, rdwr uint8, data []byte) ([]byte, error) {
	pec, err := CalcPEC(addr, rdwr, data)
	if err != nil {
		return nil, err
	}
	return append(data, pec), nil
}

// CheckPEC checks the PEC on a byte array
func CheckPEC(addr, rdwr uint8, data []byte) error {
	if len(data) < 2 {
		return fmt.Errorf("data slice too small")
	}

	// Calculate the PEC
	pec, err := CalcPEC(addr, rdwr, data[:len(data)-1])
	if err != nil {
		return err
	}

	// Compare the PEC
	if pec != data[len(data)-1] {
		return fmt.Errorf("PEC mismatch: %02x != %02x", pec, data[len(data)-1])
	}

	return nil
}
