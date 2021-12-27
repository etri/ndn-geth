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
	"fmt"
	"sync"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/ndn/utils"
)

const (
	MIXER_INTEREST_BUFFER_SIZE = 1024
)

type ProducerRequest struct {
	Msg			Request
	Packet		*ndn.Interest
	Producer	AppProducer
}

type ProducerHandler func(*ProducerRequest)


type appProducer struct {
	decoder 	RequestDecoder
	sender		Sender
	fn			ProducerHandler
	recv		chan *ndn.Interest
	quit		chan struct {}
	mutex		sync.Mutex
	running		bool
	prefix		ndn.Name
	pool		utils.WorkerPool
	manager		ObjManager
}

func NewAppProducer(prefix ndn.Name, sender Sender, decoder RequestDecoder, fn ProducerHandler, manager ObjManager, pool utils.WorkerPool) AppProducer {
	return &appProducer{
		prefix:		CopyName(prefix),
		decoder:	decoder,
		sender:		sender,
		manager:	manager,
		pool:		pool,
		recv:		make(chan *ndn.Interest, MIXER_INTEREST_BUFFER_SIZE),
		fn:			fn,
		quit:		make(chan struct{}),
		running:	false,
	}
}
//Face where the producer send its packets
func (app *appProducer) Sender() Sender {
	return app.sender
}

//identity to differentiate it from other producers in a Mixer
func (app *appProducer) identity() string {
	return PrettyName(app.prefix)
}

//check if the producer is responsible for a given Interest packet
func (app *appProducer) canprocess(i *ndn.Interest) bool {
	return app.prefix.IsPrefixOf(i.Name)
}

//send a response Data packet enclosing msg
func (app *appProducer) Serve(i *ndn.Interest, msg Response) {
	if msg != nil {
		d := msg.WriteData(i)
		app.sender.SendData(&d)
	} else {
		d := ndn.MakeData(i) //make an empty Data packet
		app.sender.SendData(&d)
	}
}

//called by mixer
func (app *appProducer) handler() func(*ndn.Interest) {
	return app.handle
}

//called by mixer
func (app *appProducer) start() {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	if app.running {
		return
	}
	go app.run()
	app.running = true
	log.Info(fmt.Sprintf("%s producer has started", PrettyName(app.prefix)))
}

//called by mixer
func (app *appProducer) stop() {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	if app.running {
		app.quit <- struct{}{}
		app.running = false
	}
}

func (app *appProducer) process(i *ndn.Interest) {
	if msg, err := app.decoder.DecodeInterest(i); err == nil {
		if app.pool != nil {
			app.pool.Do(&serveSegmentJob{
				msg:		msg,
				i:			i,
				producer:	app,
			})
		} else {
			app.servesegment(msg, i)
			//app.fn(&ProducerRequest{Msg: msg, Packet: i, Producer: app})
		}
	} 
}


func (app *appProducer) servesegment(msg Request, i *ndn.Interest) {
	if app.manager == nil {
		if app.fn !=nil {
			app.fn(&ProducerRequest{Msg: msg, Packet: i, Producer: app})
		}
		return
	}
	if qx, ok := msg.(HasQuery); ok {
		if query, segId, err := qx.ExtractQuery(); err == nil {
			if reply, err := app.manager.GetSegment(query, segId); err == nil { //get segment
			//send back a Data packet with the segment embeded
				app.Serve(i, reply)
				return
			} else {
			//object segment is not found, send a NACK packet
				app.sender.SendNack(i, uint8(150))
				return
			}
		}
	}
	//not a query, call handler to handle it
	if app.fn !=nil {
		app.fn(&ProducerRequest{Msg: msg, Packet: i, Producer: app})
	}
}

func (app *appProducer) handle(i *ndn.Interest) {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	if app.running {
		app.recv <- i
	}
}

func (app *appProducer) run() {
	LOOP:
	for {
		select {
		case <- app.quit:
			break LOOP
		case i := <- app.recv:
			app.process(i)
		}
	}
}

type serveSegmentJob struct {
	msg			Request
	i			*ndn.Interest
	producer	*appProducer
}


func (s *serveSegmentJob) Execute() {
	s.producer.servesegment(s.msg, s.i)
}
