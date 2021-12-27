/*
 * Copyright (c) 2019-2021,  HII of ETRI.
 *
 * This file is part of geth-ndn (Go Ethereum client for NDN).
 * author: tqtung@gmail.com 
 *
 * geth-ndn is free software: you can redistribute it and/or modify it under the terms
 * of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * geth-ndn is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY;
 * without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR
 * PURPOSE.  See the GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License along with
 * geth-ndn, e.g., in COPYING.md file.  If not, see <http://www.gnu.org/licenses/>.
 */


package ndnapp

import (
	"net"
	"fmt"
	"sync"
	"time"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/log"
)

const (
	INTEREST_TIMEOUT_GRANULARITY = 500*time.Millisecond //check for interest timeout every 500ms
)


// Sender sends interest and data packets.
// This is the minimum abstraction for NDN nodes.
type Sender interface {
	SendInterest(*ndn.Interest) (*ndn.Data, error)
	AsyncSendInterest(i *ndn.Interest) chan InterestData
	SendData(*ndn.Data) error
	SendNack(*ndn.Interest, uint8) error
	Traffic() (uint64, uint64)
	ResetTraffic()
}



// Face implements Sender.
type Face interface {
	Sender
//	LocalAddr() net.Addr
//	RemoteAddr() net.Addr
	Close() error
}

type face struct {
	conn net.Conn
	writer *tlvwriter
	reader	*tlvreader

	recv chan<- *ndn.Interest
	pit	pit
	ts	*timersort
	wg sync.WaitGroup
	quit chan bool
}
type InterestData struct {
	D *ndn.Data
	Err	error
}

// NewFace creates a face from net.Conn.
//
// recv is the incoming interest queue.
// If it is nil, incoming interests will be ignored.
// Otherwise, this queue must be handled before it is full.
func NewFace(transport net.Conn, recv chan<- *ndn.Interest) Face {
	f := &face{
		conn:   transport,
		writer: newtlvwriter(transport),
		reader: newtlvreader(transport),
		recv:   recv,
		quit:	make(chan bool),
		ts:		newtimersort(),
	}
	go f.mainloop()
	go f.tickerloop()
	return f
}

func (f *face) Close() error {
	err := f.conn.Close()
	close(f.quit)
	f.wg.Wait()
	return err
}

func (f *face) Traffic() (uint64, uint64) {
	return f.writer.Traffic(), f.reader.Traffic()
}

func (f *face) ResetTraffic() {
	f.reader.Reset()
	f.writer.Reset()
}

//ticker loop regularly check for expired pending interests and remove them from the pending interest table
func (f *face) tickerloop() {
	f.wg.Add(1)
	defer f.wg.Done()

	ticker := time.NewTicker(INTEREST_TIMEOUT_GRANULARITY)
	//dumpticker := time.NewTicker(10*time.Second)
	LOOP:
	for {
		select {
		case <- ticker.C:
			//detect expired interest from pit
			ilist := f.ts.checkexpiring()
			for _, i := range ilist {
				//remove expired interest from pit
				f.pit.delentry(i.pi, i.node, true)
			}
		//case <- dumpticker.C:
		//	log.Info(fmt.Sprintf("Dump pending interests at %d",time.Now().UnixNano()))
		//	f.ts.dump()
		case <- f.quit:
			break LOOP
		}
	}
}

//mainloop read NDN packet from the network connection
func (f *face) mainloop() {
	f.wg.Add(1)
	defer f.wg.Done()
	LOOP:
	for {
		select {
		case <- f.quit:
			break LOOP

		default:
			pack, err := f.reader.Read()
			if err != nil {
				log.Info(err.Error())
				break LOOP
			}
			if pack.Interest != nil {
				f.recvInterest(pack.Interest)
			} else if pack.Data != nil {
				f.recvData(pack.Data)
			} else if pack.Nack != nil {
				log.Info(fmt.Sprintf("NACK (%d) for %s\n",pack.Nack.Reason, PrettyName(pack.Nack.Interest.Name)))
				f.pit.satisfy(pack.Nack.Interest.Name, nil)
			} else {
				log.Info("Something else")
			}
		}
	}
}

func (f *face) recvData(d *ndn.Data) {
	f.pit.satisfy(d.Name, d)
	return 
}

func (f *face) recvInterest(i *ndn.Interest) {
	if f.recv != nil {
		f.recv <- i
	}
}

func (f *face) SendData(d *ndn.Data) error {
	return f.writer.Write(d)
}

func (f *face) SendNack(i *ndn.Interest, reason uint8) error {
	nack := ndn.MakeNack(i, reason)
	return f.writer.Write(nack.ToPacket())
}

func (f *face) AsyncSendInterest(i *ndn.Interest) chan InterestData  {
	i.Lifetime = INTEREST_LIFE_TIME
	//log.Info(fmt.Sprintf("asend Interest %s", PrettyName(i.Name)))
	ret := make(chan InterestData, 1)
	pi := newpendinginterest(i, ret)
	node := f.pit.insert(pi)

	err := f.writer.Write(i) 

	if err != nil {
		log.Info(fmt.Sprintf("Failed to write %s", PrettyName(i.Name)))
		f.pit.delentry(pi, node, false)
		ret <- InterestData{D: nil, Err: err}
	} else {	//add expiring time to the priority queue
		f.ts.push(pi, node)
	}
	//log.Info(fmt.Sprintf("%s is sent", PrettyName(i.Name)))
	return ret
}


func (f *face) SendInterest(i *ndn.Interest) (*ndn.Data, error) {
	result := <- f.AsyncSendInterest(i)
	return result.D, result.Err
}

