package jsonrpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"

	"eval_miner/job"
	log "eval_miner/log"
)

type Request struct {
	Version *string     `json:"jsonrpc,omitempty"`
	ID      uint64      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type Response struct {
	Version string        `json:"jsonrpc"`
	ID      uint64        `json:"id"`
	Result  interface{}   `json:"result"`
	Error   interface{}   `json:"error"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type ClientHandlerFunc func(Response, job.ByteStats) error

type Client struct {
	Conn    net.Conn
	ID      uint64
	Encoder *json.Encoder
	Decoder *json.Decoder
	handler ClientHandlerFunc
	Rxchan  chan string
	Stats   job.ByteStats
	bStop   bool
}

func (c *Client) Stop() {
	if c.Conn != nil {
		c.Conn.Close()
	}

	c.bStop = true
}

func (c *Client) RecvAndHandle(conn net.Conn) {
	r := bufio.NewReader(conn)
	log.Info("Connected to ", conn.RemoteAddr())
	c.bStop = false

	for {
		if c.bStop {
			break
		}
		buf, err := r.ReadBytes('\n')
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			log.Infof("RecvAndHandle EOF %v", err)
			break
		} else if err != nil {
			log.Infof("RecvAndHandle Err %v", err)
			break
		}

		bytes := len(buf)
		s := job.ByteStats{
			N:     1,
			Bytes: uint64(bytes),
		}

		log.Debugf("RecvAndHandle read %d bytes", bytes)

		resp := Response{}

		log.Debugf("%v", string(buf))

		err = json.Unmarshal(buf, &resp)
		if err != nil {
			log.Error(err)
		}

		log.Debugf("%v", resp)

		err = c.handler(resp, s)
		if err != nil {
			log.Error(err)
		}
	}

	log.Info("Client disconnected from ", conn.RemoteAddr())
	conn.Close()
	c.Rxchan <- "EOF"
}

var ErrTx = errors.New("ErrTx")
var ErrEncode = errors.New("ErrEncode")

func (c *Client) Call(method string, txid int, params interface{}, response *interface{}) (uint64, job.ByteStats, error) {

	var req Request
	buf := new(bytes.Buffer)

	if txid < 0 {
		req.ID = c.ID
	} else {
		req.ID = uint64(txid)
	}
	c.ID = c.ID + 1
	//	req.Version = "2.0"
	req.Method = method
	req.Params = params

	s := job.ByteStats{}

	err := json.NewEncoder(buf).Encode(&req)
	if err != nil {
		return 0, s, ErrEncode
	}

	log.Debugf("RPC call: %s", buf.String())

	bytes, errW := c.Conn.Write(buf.Bytes())
	s = job.ByteStats{
		N:     1,
		Bytes: uint64(bytes),
	}
	if errW != nil {
		log.Errorf("Error writing to stream.")
		err = ErrTx
	} else {
		log.Debugf("%d bytes writen", bytes)
	}

	return req.ID, s, err
}

func NewClient(network string, address string, handler ClientHandlerFunc) (*Client, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	c := Client{
		Conn:    conn,
		ID:      0,
		handler: handler,
	}

	c.Rxchan = make(chan string, 1)

	// rx thread
	go c.RecvAndHandle(conn)

	return &c, err
}
