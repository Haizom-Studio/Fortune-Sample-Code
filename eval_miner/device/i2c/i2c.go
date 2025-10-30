package i2c

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

const (
	BUS_CTRL_BOARD = 4 // for temp sensor & eeprom
	BUS_CTRL_PSU   = 5 // for PSU

	BUS_HASH_BOARD_BASE = 1
)

const (
	i2c_SLAVE = 0x0703
)

type I2cDev struct {
	devConn conn
}

// internal implementation are simplified from "golang.org/x/exp/io/i2c" to avoid the dependency issue

// Conn represents an active connection to an I2C device.
type conn interface {
	// Tx first writes w (if not nil), then reads len(r)
	// bytes from device into r (if not nil) in a single
	// I2C transaction.
	Tx(w, r []byte) error
	
	// Read reads r bytes from the device
	Read(r []byte) (int, error)

	// Write writes w bytes to the device
	Write(w []byte) (int, error)

	// Close closes the connection.
	Close() error
}

func open(dev string, addr int) (conn, error) {
	f, err := os.OpenFile(dev, os.O_RDWR, os.ModeDevice)
	if err != nil {
		return nil, err
	}
	conn := &devfsConn{f: f}

	if err := conn.ioctl(i2c_SLAVE, uintptr(addr)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("error opening the address (%v) on the bus (%v): %v", addr, dev, err)
	}
	return conn, nil
}

// devfsConn implements conn interface
type devfsConn struct {
	f *os.File
}

func (c *devfsConn) Tx(w, r []byte) error {
	if w != nil {
		if _, err := c.f.Write(w); err != nil {
			return err
		}
		_ = c.f.Sync()
	}
	if r != nil {
		if _, err := io.ReadFull(c.f, r); err != nil {
			return err
		}
	}
	return nil
}

func (c *devfsConn) Read(r []byte) (int, error) {
	return io.ReadFull(c.f, r)
}

func (c *devfsConn) Write(w []byte) (int, error) {
	cnt, err := c.f.Write(w)
	_ = c.f.Sync()
	return cnt, err
}

func (c *devfsConn) Close() error {
	return c.f.Close()
}

func (c *devfsConn) ioctl(arg1, arg2 uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, c.f.Fd(), arg1, arg2); errno != 0 {
		return syscall.Errno(errno)
	}
	return nil
}

// external interface: New/Read/Write/Close
func NewI2cDev(bus int, addr int) (dev *I2cDev, err error) {
	d, err := open(fmt.Sprintf("/dev/i2c-%d", bus), addr)
	if err != nil {
		return nil, err
	}
	return &I2cDev{d}, nil
}

func (dev *I2cDev) ReadReg(reg byte, buf []byte) error {
	return dev.devConn.Tx([]byte{reg}, buf)
}

func (dev *I2cDev) WriteReg(reg byte, buf []byte) error {
	return dev.devConn.Tx(append([]byte{reg}, buf...), nil)
}

func (dev *I2cDev) Read(r []byte) (int, error) {
	return dev.devConn.Read(r)
}

func (dev *I2cDev) Write(w []byte) (int, error) {
	return dev.devConn.Write(w)
}

func (dev *I2cDev) Close() error {
	return dev.devConn.Close()
}
