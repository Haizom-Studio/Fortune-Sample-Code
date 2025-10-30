package jsonrpc

import (
	"eval_miner/log"
	"eval_miner/util"
	"net"
	"sync"
	"time"
)

const (
	MAX_RECEIVE_BUF = 65536
)

type TCPClient struct {
	Addr          string
	Reconnect     bool
	Conn          net.Conn
	TxBytes       int
	RxBytes       int
	Errors        int
	RedialCount   int
	LastErrorTS   float64
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	DialTimeout   time.Duration
	AppendNewline bool
	mx            sync.Mutex
}

func NewTCPClient(addr string) *TCPClient {
	var err error
	var my TCPClient = TCPClient{
		Addr:          addr,
		Reconnect:     true,
		AppendNewline: true,
	}

	my.ReadTimeout = time.Second * 15
	my.WriteTimeout = time.Second * 15
	my.DialTimeout = time.Second * 1 // timeout faster for internal conn and UT

	myDialer := net.Dialer{Timeout: my.DialTimeout}
	my.Conn, err = myDialer.Dial("tcp", my.Addr)
	if err != nil {
		log.Debugf("can't connect to %s err %v", my.Addr, err)
	}

	return &my
}

func (my *TCPClient) redial() error {
	var err error
	if my.Conn != nil {
		my.Conn.Close()
	}
	myDialer := net.Dialer{Timeout: my.DialTimeout}
	my.Conn, err = myDialer.Dial("tcp", my.Addr)
	my.RedialCount++
	if err != nil {
		log.Debugf("can't connect to %s err %v", my.Addr, err)
		my.Errors++
		my.LastErrorTS = util.NowInSec()
	} else {
		my.Errors = 0
	}

	return err
}

func (my *TCPClient) sendAndReceive(reqbuf []byte) ([]byte, int, error) {
	var err error
	var n int

	if my.Conn == nil || my.Errors > 0 {
		err = my.redial()
		if err != nil {
			return nil, 0, err
		}
	}

	err = my.Conn.SetReadDeadline(time.Now().Add(my.WriteTimeout))
	if err != nil {
		log.Debugf("err %v", err)
	}

	if my.AppendNewline {
		n = len(reqbuf)
		if n > 0 {
			if reqbuf[n-1] != '\n' {
				reqbuf = append(reqbuf, '\n')
			}
		}
	}

	n, err = my.Conn.Write(reqbuf)
	if err != nil {
		log.Errorf("Sent error %v", err)
		my.Errors++
		my.LastErrorTS = util.NowInSec()
		return nil, 0, err
	}
	my.TxBytes += n

	reply := make([]byte, MAX_RECEIVE_BUF)

	err = my.Conn.SetReadDeadline(time.Now().Add(my.ReadTimeout))
	if err != nil {
		log.Debugf("err %v", err)
	}
	n, err = my.Conn.Read(reply)
	if err != nil {
		log.Errorf("Rx error %v", err)
		my.Errors++
		my.LastErrorTS = util.NowInSec()
		return nil, 0, err
	}

	return reply[:n], n, nil
}

func (my *TCPClient) SendAndReceive(reqbuf []byte) ([]byte, int, error) {
	my.mx.Lock()
	defer my.mx.Unlock()

	reply, n, err := my.sendAndReceive(reqbuf)
	// retry once so it is transparent for clients when socket connection is down
	if err != nil {
		reply, n, err = my.sendAndReceive(reqbuf)
	}
	return reply, n, err
}

func (my *TCPClient) Shutdown() {
	my.mx.Lock()
	defer my.mx.Unlock()

	if my.Conn != nil {
		my.Conn.Close()
	}
}
