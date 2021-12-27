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
//	"fmt"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/usnistgov/ndn-dpdk/ndn"
//	"github.com/ethereum/go-ethereum/log"
)


type appConsumer struct {
	decoder		ResponseDecoder
	sender		Sender
}


func NewAppConsumer(decoder ResponseDecoder, sender Sender) AppConsumer {
	return &appConsumer{decoder: decoder, sender: sender}
}


func (app *appConsumer) Request(msg Request, route interface{}, quit chan bool) (reply Response, err error) {
	i := msg.WriteInterest(route) 
	var d *ndn.Data
	if quit != nil {
		ch := app.sender.AsyncSendInterest(&i)
		select {
		case outcome := <- ch:
			d = outcome.D
			err = outcome.Err
		case <- quit:
			 err = ErrJobAborted
		}

	} else {
		d, err = app.sender.SendInterest(&i)
	}
	if err == nil {
		reply, err = app.decoder.DecodeData(d)
	}
	return
}

func (app *appConsumer) Attach(pool utils.WorkerPool) AsyncAppConsumer {
	return newAsyncAppConsumer(app, pool)
}

type asyncAppConsumer struct {
	worker		AppConsumer
	pool		utils.WorkerPool
}


func newAsyncAppConsumer(consumer AppConsumer, pool utils.WorkerPool) AsyncAppConsumer {
	return &asyncAppConsumer{
		worker:		consumer,
		pool:		pool,
	}
}

func (app *asyncAppConsumer) Request(msg Request, route interface{}, fn func(Request, interface{}, Response, error)) error {
	if app.pool.Do(newConsumerJob(msg, route, app.worker, fn)) {
		return nil	
	}
	return ErrJobAborted
}

type consumerJob struct {
	consumer	AppConsumer
	msg 		Request
	route		interface{}
	quit 		chan bool
	fn			func(Request, interface{}, Response, error)	
}

func newConsumerJob(msg Request, route interface{}, consumer AppConsumer, fn func(Request, interface{}, Response,error)) *consumerJob {
	return &consumerJob{
		msg:		msg,
		route:		route,
		consumer:	consumer,
		fn:			fn,
		quit:		make(chan bool),
	}
}
func (j *consumerJob) Execute() {
	reply, err := j.consumer.Request(j.msg,j.route, j.quit)	
	if j.fn != nil {
		j.fn(j.msg, j.route, reply, err)
	}
}

func (j *consumerJob) Kill() {
	close(j.quit)
}
