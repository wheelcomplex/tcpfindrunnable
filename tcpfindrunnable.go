//

// tcp echo client/server for go lang runtime findrunnable example

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	_ "net/http/pprof"
)

//

func main() {
	var duplex bool
	var help bool
	var httpprofile string
	var cpus int

	flag.BoolVar(&help, "h", false, "show help")
	flag.BoolVar(&duplex, "D", false, "run in duplex mode")
	flag.StringVar(&httpprofile, "P", "", "host:port for http profile")
	flag.IntVar(&cpus, "C", 0, "running with N CPUs/threads, zero for all cpus")

	hostport := "127.0.0.1:5180"

	flag.Parse()
	if help {
		flag.PrintDefaults()
		os.Exit(1)
	}

	//
	if cpus <= 0 {
		cpus = runtime.NumCPU()
	}
	runtime.GOMAXPROCS(cpus)
	cpus = runtime.GOMAXPROCS(-1)

	destAddr, err := net.ResolveTCPAddr("tcp", hostport)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if httpprofile != "" {
		httpProfile(httpprofile)
	}

	var wg sync.WaitGroup
	l, err := net.ListenTCP("tcp", destAddr)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	defer l.Close()

	server := NewEcho(duplex, 10)

	go func() {
		for idx := 0; idx < cpus; idx++ {
			// accept
			conn, err := l.AcceptTCP()
			if err != nil {
				fmt.Println(err.Error())
			}
			wg.Add(1)
			go server.server(conn, &wg)
		}
	}()
	for idx := 0; idx < cpus; idx++ {
		// dial to
		conn, err := net.DialTCP("tcp", nil, destAddr)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		wg.Add(1)
		go client(duplex, conn, &wg)
	}

	fmt.Printf("%s running on %s, with %d workers, press <CTL+C> to exit ...\n", server.Title(), destAddr.String(), cpus)
	wg.Wait()
	fmt.Printf("all done\n")
}

//

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

// client send seq and read+verify response
func client(isduplex bool, link *net.TCPConn, wg *sync.WaitGroup) error {
	defer wg.Done()
	var err error
	var seq, vseq uint64
	var state int
	var totalByteIn, byteIn, totalByteOut, byteOut int

	var echobuf = make([]byte, IDENTIFY_SIZE)

	state = STATE_INIT_SEQ

	var seed uint64

	seqout := &seed

	for {
		switch state {
		case STATE_INIT_SEQ:
			// seq start from 2, step by 2
			seq = atomic.AddUint64(seqout, 2)
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
			if isduplex == false {
				state = STATE_INIT_SEQ
				continue
			}
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

//
type Echo struct {
	isduplex bool
	title    string
	seq      *uint64
	request  *int64
	response *int64
}

//
func NewEcho(isduplex bool, interval int) *Echo {
	var seq uint64
	var request, response int64
	var title string
	title = "tcp echo client/server"
	if isduplex {
		title = title + ", duplex mode"
	} else {
		title = title + ", mono mode"
	}
	p := &Echo{
		title:    title,
		isduplex: isduplex,
		seq:      &seq,
		request:  &request,
		response: &response,
	}
	if interval > 0 {
		go p.stat(interval)
	}
	return p
}

// Title
func (p *Echo) Title() string {
	return p.title
}

// stat
func (p *Echo) stat(interval int) {
	tk := time.NewTicker(time.Duration(interval) * time.Second)
	defer tk.Stop()
	var request, response int64
	var prerequest, preresponse int64
	prets := time.Now()
	timesecond := int64(time.Second)
	for {
		<-tk.C
		request = atomic.LoadInt64(p.request)
		response = atomic.LoadInt64(p.response)
		if request != prerequest || response != preresponse {
			esp := time.Now().Sub(prets)
			fmt.Printf(" --- %s status(%v) --- \n", p.title, esp)
			fmt.Printf("request:\t%d,\t%d\n", request, (request-prerequest)*timesecond/int64(esp))
			fmt.Printf("response:\t%d,\t%d\n", response, (response-preresponse)*timesecond/int64(esp))
			fmt.Printf(" --- --- \n")
		}
		prerequest = request
		preresponse = response
		prets = time.Now()
	}
}

// server
func (p *Echo) server(link *net.TCPConn, wg *sync.WaitGroup) error {
	defer wg.Done()
	var err error
	var seq uint64
	var state int
	var totalByteIn, byteIn, totalByteOut, byteOut int

	var echobuf = make([]byte, IDENTIFY_SIZE)

	state = STATE_READ_RESET

	for {
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
			if p.isduplex == false {
				state = STATE_READ_RESET
				continue
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

//

//
func httpProfile(profileurl string) {
	binpath, _ := filepath.Abs(os.Args[0])
	fmt.Printf("\n http profile: [ http://%s/debug/pprof/ ]\n", profileurl)
	fmt.Printf("\n http://%s/debug/pprof/goroutine?debug=1\n\n", profileurl)
	fmt.Printf("\n go tool pprof %s http://%s/debug/pprof/profile\n\n", binpath, profileurl)
	fmt.Printf("\n go tool pprof %s http://%s/debug/pprof/heap\n\n", binpath, profileurl)
	go func() {
		if err := http.ListenAndServe(profileurl, nil); err != nil {
			fmt.Printf("\n http/pprof: %s\n", err.Error())
			os.Exit(1)
		}
	}()
}

//
//
//
