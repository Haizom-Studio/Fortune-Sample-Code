package smbus

/* adapter makes this package compatible with gcminer */

import (
	"fmt"
)

// default string length is 16 bytes
const DEFAULT_STR_LEN = 16
const WORD_LEN = 2
const REG_LEN = 1

type Conn struct {
	sysIF    *SysIF
	addr     uint8
	useRxPEC bool
	useTxPEC bool
}

// Open a connection to the I2C bus
func Open(bus int, addr uint8) (*Conn, error) {
	i2cfile := fmt.Sprintf("/dev/i2c-%d", bus)

	sysIF, err := New(i2cfile)
	if err != nil {
		return nil, err
	}

	conn := &Conn{
		sysIF:    sysIF,
		addr:     addr,
		useRxPEC: false,
		useTxPEC: false,
	}

	return conn, nil
}

// SetRxPEC enables or disables the use of PEC
func (c *Conn) SetRxPEC(usePEC bool) {
	c.sysIF.SetRxPEC(usePEC)
	c.useRxPEC = usePEC
}

// SetTxPEC enables or disables the use of PEC
func (c *Conn) SetTxPEC(usePEC bool) {
	c.sysIF.SetTxPEC(usePEC)
	c.useTxPEC = usePEC
}

// Close the connection
func (c *Conn) Close() error {
	return c.sysIF.Bus.Close()
}

// RAW write. This function is not recommended. Properly specify
// the device command instead
func (c *Conn) Write(buf []byte) (int, error) {
	if len(buf) < 1 {
		return 0, fmt.Errorf("smbus: buffer slice too small")
	}

	cmd := buf[0]
	return c.sysIF.WriteNwC(uint16(c.addr), cmd, buf[1:])
}

// Write a Byte
func (c *Conn) WriteByte(cmd, b byte) (int, error) {
	if err := c.sysIF.WriteByte(uint16(c.addr), cmd, b); err != nil {
		return 0, err
	}
	return 1, nil
}

// Write a 16-bit word
func (c *Conn) WriteWord(addr, cmd byte, data uint16) error {
	if err := c.sysIF.WriteWord(uint16(addr), cmd, data); err != nil {
		return err
	}
	return nil
}

// Write a 8-bit register
func (c *Conn) WriteReg(addr, cmd, data uint8) error {
	c.addr = addr
	return c.sysIF.WriteByte(uint16(addr), cmd, data)
}

// Read array of bytes
// Byte order interpretation is left up to the user. Order is typically LSB first.
func (c *Conn) Read(p []byte) (int, error) {
	if len(p) < 1 {
		return 0, fmt.Errorf("smbus: buffer slice too small")
	}

	cmd := p[0]

	ret, err := c.sysIF.ReadN(uint16(c.addr), cmd, len(p))
	if err != nil {
		return 0, err
	}
	copy(p, ret)

	return len(ret), nil
}

// Read a 16-bit word
func (c *Conn) ReadWord(addr, cmd uint8) (uint16, error) {
	c.addr = addr
	b, err := c.sysIF.ReadN(uint16(addr), cmd, WORD_LEN)
	if err != nil {
		return 0, err
	}

	return uint16(b[1])<<8 | uint16(b[0]), nil
}

// Read a 8-bit register
func (c *Conn) ReadReg(addr, cmd uint8) (uint8, error) {
	c.addr = addr
	b, err := c.sysIF.ReadN(uint16(addr), cmd, REG_LEN)
	if err != nil {
		return 0, err
	}

	return b[0], nil
}

// Read a byte, alias for ReadReg
func (c *Conn) ReadByte(addr, cmd uint8) (uint8, error) {
	return c.ReadReg(addr, cmd)
}

// Read a 16 byte block of string
// Byte order interpretation is left up to the user
func (c *Conn) ReadBlockData(addr, cmd uint8) ([]byte, error) {
	c.addr = addr
	return c.sysIF.ReadN(uint16(addr), cmd, DEFAULT_STR_LEN)
}

// Write a data block
func (c *Conn) WriteBlockData(addr, cmd uint8, buf []byte) error {
	c.addr = addr
	return c.sysIF.WriteN(uint16(addr), cmd, buf)
}

func (c *Conn) ReadN(addr, cmd uint8, n int) ([]byte, error) {
	return c.sysIF.ReadN(uint16(addr), cmd, n)
}
