package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	log "eval_miner/log"
)

type APIRequest struct {
	Command   string      `json:"command"`
	Parameter interface{} `json:"parameter"`
}

type ServerHandlerFunc func(*Server, net.Conn, *APIRequest, []byte, error) error

type Server struct {
	listener       net.Listener
	done           chan interface{}
	wg             sync.WaitGroup
	handler        ServerHandlerFunc
	bConnKeepAlive bool
	AppendNewline  bool
	ReadTimeout    time.Duration
	// WriteTimeout   time.Duration
	// DialTimeout    time.Duration
}

func NewServer(addr string, handler ServerHandlerFunc, bKeepAlive bool) *Server {
	s := &Server{
		done:          make(chan interface{}),
		AppendNewline: true,
		ReadTimeout:   time.Millisecond * 100,
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Errorf("%v", err)
		return nil
	}
	s.listener = l
	s.wg.Add(1)
	if handler == nil {
		s.handler = DefaultServerHandler
	} else {
		s.handler = handler
	}
	s.bConnKeepAlive = bKeepAlive
	return s
}

func (s *Server) ListenAndServe() {
	if s == nil {
		return
	}

	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Errorf("Accept error %v", err)
			}
		} else {
			s.wg.Add(1)
			go func() {
				s.handleConection(conn)
				s.wg.Done()
			}()
		}
	}
}

func (s *Server) Shutdown(ctx context.Context) {
	if s == nil {
		return
	}
	close(s.done)
	s.listener.Close()
	s.wg.Wait()
}

func (s *Server) handleConection(conn net.Conn) {
	log.Debug("Connection from ", conn.RemoteAddr())

	for {
		buf := make([]byte, 0, 65536) // big buffer
		tmp := make([]byte, 16384)    // using small tmo buffer for demonstrating
		var err error = nil
		n := 0
		for {
			err = conn.SetReadDeadline(time.Now().Add(s.ReadTimeout))
			if err != nil {
				log.Debugf("err %v", err)
			}
			n, err = conn.Read(tmp)
			buf = append(buf, tmp[:n]...)
			log.Debugf("read(%v):%v  buf(%v) %v", n, string(tmp[:n]), len(buf), string(buf[:]))
			// break on '\n'
			if bytes.Contains(buf, []byte{'\n'}) {
				break
			}
			// check if the err is timeout
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// keep accepting data if buf is 0
				if len(buf) == 0 {
					continue
				} else {
					if s.AppendNewline {
						n = len(buf)
						if n > 0 {
							if buf[n-1] != '\n' {
								buf = append(buf, '\n')
							}
						}
					}
					// clear the err for timeout deadlines
					err = nil
					break
				}
			}
			// break on other errors
			if err != nil {
				break
			}
		}
		log.Debugf("err %v read %v bytes: buf %v", err, len(buf), string(buf[:]))

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			log.Infof("handleConnection EOF %v", err)
			break
		} else if err != nil {
			log.Infof("handleConnection Err %v", err)
			break
		}
		req := APIRequest{}

		err = json.Unmarshal(buf, &req)
		if err != nil {
			log.Error(err)
		}

		err = s.handler(s, conn, &req, buf, err)
		if err != nil {
			log.Error(err)
		}

		if !s.bConnKeepAlive {
			// one connection per command as default
			break
		}
	}

	log.Debug("Server disconnected from ", conn.RemoteAddr())
	conn.Close()
	//	conn.rxchan <- "EOF"
}

func DefaultServerHandler(s *Server, conn net.Conn, req *APIRequest, rawbuf []byte, err error) error {
	resp := fmt.Sprintf("received from %v: %v, error: %s", conn.RemoteAddr(), req, err)
	log.Info(resp)
	_, err = conn.Write([]byte(resp))

	return err
}
