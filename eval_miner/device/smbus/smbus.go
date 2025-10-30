// Package smbus is a wrapper around the periph.io library for I2C communication.
// It avoids using cgo, unsafe and syscalls.
package smbus

import (
	"sync"

	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/host/v3"
)

// SysIF is the system interface to the I2C bus.
type SysIF struct {
	BusFile  string
	Bus      i2c.BusCloser
	i2cmu    *sync.Mutex
	Devices  []uint16
	useTxPEC bool
	useRxPEC bool
}

func New(busFile string) (*SysIF, error) {
	if _, err := host.Init(); err != nil {
		return nil, err
	}

	bus, err := i2creg.Open(busFile)
	if err != nil {
		return nil, err
	}
	return &SysIF{
		BusFile:  busFile,
		Bus:      bus,
		i2cmu:    &sync.Mutex{},
		Devices:  make([]uint16, 0),
		useTxPEC: false,
		useRxPEC: false,
	}, nil
}

// SetRxPEC enables or disables the use of PEC.
func (s *SysIF) SetRxPEC(usePEC bool) {
	s.useRxPEC = usePEC
}

// SetTxPEC enables or disables the use of PEC.
func (s *SysIF) SetTxPEC(usePEC bool) {
	s.useTxPEC = usePEC
}

// Close the I2C bus.
func (s *SysIF) Close() {
	s.i2cmu.Lock()
	defer s.i2cmu.Unlock()
	s.Bus.Close()
}

// A typical I2C address is 7 bits long. The 8th bit is used to indicate read
// or write. The LSB is the read/write bit. 0 = write, 1 = read.
// However, the periph.io library expects an address that is 16 bits long. The
// reason is not known. We are going to use this 16-bit address.

// Read8 reads a byte from the I2C bus.
func (s *SysIF) ReadN(addr uint16, cmd uint8, nbytes int) ([]byte, error) {
	s.i2cmu.Lock()
	defer s.i2cmu.Unlock()

	// not sure why an I2C addr is uint16
	d := &i2c.Dev{Addr: addr, Bus: s.Bus}

	read := make([]byte, nbytes)
	cmdBytes := []byte{cmd}
	if err := d.Tx(cmdBytes, read); err != nil {
		return nil, err
	}

	// check the PEC
	if s.useRxPEC {
		data := append(cmdBytes, read...)
		if err := CheckPEC(uint8(addr), READ, data); err != nil {
			return nil, err
		}
	}

	// add the device to the list of devices
	s.Devices = append(s.Devices, addr)
	return read, nil
}

// WriteN bytes to the I2C bus.
func (s *SysIF) WriteNwC(addr uint16, cmd uint8, data []byte) (int, error) {
	s.i2cmu.Lock()
	defer s.i2cmu.Unlock()

	d := &i2c.Dev{Addr: addr, Bus: s.Bus}
	bytes := append([]byte{cmd}, data...)

	if s.useTxPEC {
		// append the PEC
		var err error
		bytes, err = AppendPEC(uint8(addr), WRITE, bytes)
		if err != nil {
			return 0, err
		}
	}

	// write the data
	count, err := d.Write(bytes)
	if err != nil {
		return 0, err
	}

	// add the device to the list of devices
	s.Devices = append(s.Devices, addr)
	return count, nil
}

// WriteN bytes to the I2C bus and don't care about bytes written
func (s *SysIF) WriteN(addr uint16, cmd uint8, data []byte) error {
	_, err := s.WriteNwC(addr, cmd, data)
	return err
}

// ReadByte reads a byte from the I2C bus.
func (s *SysIF) ReadByte(addr uint16, cmd uint8) (byte, error) {
	ret, err := s.ReadN(addr, cmd, 1)
	if err != nil {
		return 0, err
	}
	return ret[0], nil
}

// WriteByte writes a byte to the I2C bus.
func (s *SysIF) WriteByte(addr uint16, cmd uint8, data byte) error {
	return s.WriteN(addr, cmd, []byte{data})
}

func (s *SysIF) ReadWord(addr uint16, cmd uint8) (uint16, error) {
	ret, err := s.ReadN(addr, cmd, 2)
	if err != nil {
		return 0, err
	}
	return uint16(ret[0]) | uint16(ret[1])<<8, nil
}

func (s *SysIF) WriteWord(addr uint16, cmd uint8, data uint16) error {
	return s.WriteN(addr, cmd, []byte{uint8(data), uint8(data >> 8)})
}
