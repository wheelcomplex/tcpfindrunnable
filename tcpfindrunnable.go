//

// tcp echo client/server for go lang runtime findrunnable example

package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

// state machine for echo server/client
const (
	STATE_INIT_SEQ int = iota
	STATE_READ_RESET
	STATE_WRITE_RESET
	STATE_WRITE_SEQ
	STATE_READ_SEQ
)

// uint64 size is 8
const IDENTIFY_SIZE int = 8

//
type Echo struct {
	isclient     bool
	title        string
	seq          *uint64
	connected    *int64
	disconnected *int64
	request      *int64
	response     *int64
	closing      bool
}

//
func NewEcho(isclient bool) *Echo {
	var seq uint64
	var connected, disconnected, request, response int64
	var title string
	if isclient {
		title = "tcp echo client"
	} else {
		title = "tcp echo server"
	}
	p := &Echo{
		title:        title,
		isclient:     isclient,
		seq:          &seq,
		connected:    &connected,
		disconnected: &disconnected,
		request:      &request,
		response:     &response,
	}
	go p.stat(10)
	return p
}

// stat
func (p *Echo) stat(interval int) {
	tk := time.NewTicker(time.Duration(interval) * time.Second)
	defer tk.Stop()
	var connected, disconnected, request, response int64
	var preconnected, predisconnected, prerequest, preresponse int64
	prets := time.Now()
	timesecond := int64(time.Second)
	for p.closing == false {
		connected = atomic.LoadInt64(p.connected)
		disconnected = atomic.LoadInt64(p.disconnected)
		request = atomic.LoadInt64(p.request)
		response = atomic.LoadInt64(p.response)
		if connected != preconnected || disconnected != predisconnected || request != prerequest || response != preresponse {
			esp := time.Now().Sub(prets)
			fmt.Printf(" --- %s status(%v) --- \n", p.title, esp)
			fmt.Printf("connected:\t%d,\t%d\n", connected, (connected-preconnected)*timesecond/int64(esp))
			fmt.Printf("disconnected:\t%d,\t%d\n", disconnected, (disconnected-predisconnected)*timesecond/int64(esp))
			fmt.Printf("request:\t%d,\t%d\n", request, (request-prerequest)*timesecond/int64(esp))
			fmt.Printf("response:\t%d,\t%d\n", response, (connected-preresponse)*timesecond/int64(esp))
			fmt.Printf(" --- --- \n")
		}
		preconnected = connected
		predisconnected = disconnected
		prerequest = request
		preresponse = response
		prets = time.Now()
		<-tk.C
	}
}

// OnConnected
func (p *Echo) OnConnected(link *net.TCPConn) error {
	atomic.AddInt64(p.connected, 1)
	defer atomic.AddInt64(p.disconnected, 1)
	if p.isclient {
		return p.client(link)
	}
	return p.server(link)
}

// client send seq and read+verify response
func (p *Echo) client(link *net.TCPConn) error {
	var err error
	var seq, vseq uint64
	var state int
	var totalByteIn, byteIn, totalByteOut, byteOut int

	var echobuf = make([]byte, IDENTIFY_SIZE)

	state = STATE_INIT_SEQ

	for p.closing == false {
		switch state {
		case STATE_INIT_SEQ:
			// seq start from 2, step by 2
			seq = atomic.AddUint64(p.seq, 2)
			vseq = seq + 1
			binary.BigEndian.PutUint64(echobuf, seq)
			state = STATE_WRITE_RESET
			fallthrough
		case STATE_WRITE_RESET:
			byteOut = 0
			totalByteOut = 0
			fallthrough
		case STATE_WRITE_SEQ:
			byteOut, err = link.Write(echobuf[totalByteOut:])
			if err != nil {
				return err
			}
			totalByteOut += byteOut
			if totalByteOut < IDENTIFY_SIZE {
				continue
			}
			atomic.AddInt64(p.request, 1)
			state = STATE_READ_RESET
			fallthrough
		case STATE_READ_RESET:
			byteIn = 0
			totalByteIn = 0
			fallthrough
		case STATE_READ_SEQ:
			byteIn, err = link.Read(echobuf[totalByteIn:])
			if err != nil {
				return err
			}
			totalByteIn += byteIn
			if totalByteIn < IDENTIFY_SIZE {
				continue
			}
			atomic.AddInt64(p.response, 1)
			seq = binary.BigEndian.Uint64(echobuf)
			// client side, check response
			if vseq == seq {
				state = STATE_INIT_SEQ
				// next round
				continue
			} else {
				fmt.Printf("client, %s closed for seq mismatch(%d - %d).\n", link.RemoteAddr().String(), vseq, seq)
				return err
			}
		}
	}
	return err
}

// server
func (p *Echo) server(link *net.TCPConn) error {
	var err error
	var seq uint64
	var state int
	var totalByteIn, byteIn, totalByteOut, byteOut int

	var echobuf = make([]byte, IDENTIFY_SIZE)

	state = STATE_READ_RESET

	for p.closing == false {
		switch state {
		case STATE_READ_RESET:
			byteIn = 0
			totalByteIn = 0
			state = STATE_READ_SEQ
			fallthrough
		case STATE_READ_SEQ:
			byteIn, err = link.Read(echobuf[totalByteIn:])
			if err != nil {
				return err
			}
			totalByteIn += byteIn
			if totalByteIn < IDENTIFY_SIZE {
				continue
			}
			atomic.AddInt64(p.request, 1)
			seq = binary.BigEndian.Uint64(echobuf)
			if seq < 1 {
				fmt.Printf("server, %s closed for invalid seq(%d).\n", link.RemoteAddr().String(), seq)
				return err
			}
			// server side, seq +1 and send response
			seq++
			binary.BigEndian.PutUint64(echobuf, seq)
			state = STATE_WRITE_RESET
			fallthrough
		case STATE_WRITE_RESET:
			state = STATE_WRITE_SEQ
			byteOut = 0
			totalByteOut = 0
			fallthrough
		case STATE_WRITE_SEQ:
			byteOut, err = link.Write(echobuf[totalByteOut:])
			if err != nil {
				return err
			}
			totalByteOut += byteOut
			if totalByteOut < IDENTIFY_SIZE {
				continue
			}
			atomic.AddInt64(p.response, 1)
			state = STATE_READ_RESET
			// next round
			continue
		}
	}
	return err
}

// Close all handled connections
func (p *Echo) Close() error {
	p.closing = true
	return nil
}

//

func main() {

}
