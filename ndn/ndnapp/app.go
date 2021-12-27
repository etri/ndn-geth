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
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/ndn/utils"
)


type Request interface {
	WriteInterest(interface{}) ndn.Interest
}

type Response interface {
	WriteData(*ndn.Interest) ndn.Data
}

type RequestDecoder interface {
	DecodeInterest(*ndn.Interest) (Request, error)
}

type ResponseDecoder interface {
	DecodeData(*ndn.Data) (Response, error)
}
type Decoder interface {
	RequestDecoder
	ResponseDecoder
}

type Query interface {
	WriteRequest(uint16) Request
}
type HasId interface {
	GetId() string
}

type ResponseWithDataSegment interface {
	Response
	ExtractSegment() (ObjSegment, error) //extract ObjSegment
}

type HasQuery interface {
	ExtractQuery() (Query, uint16, error)
}

type ObjSegment interface {
	GetNumSegments() uint16
	GetSegmentId() uint16
	GetWire()	[]byte
}


type AppProducer interface {
	Serve(*ndn.Interest, Response) //build then send Data packet

	handler() func(*ndn.Interest)
	start()
	stop()
	canprocess(*ndn.Interest) bool
	identity() string
	Sender() Sender
}

type AppConsumer interface {
	Request(Request, interface{}, chan bool) (Response, error)
	fetchSegment(Query, interface {}, uint16, chan bool) (ObjSegment, error)
	FetchObject(Query, interface{}, chan bool) ([]byte, error)
	Attach(utils.WorkerPool) AsyncAppConsumer
}

type AsyncAppConsumer interface {
	fetchSegment(Query, interface{}, uint16, func(uint16, ObjSegment, error)) error 
	FetchObject(Query, interface {}, chan bool, uint16) ([]byte, error) 
	Request(Request, interface{}, func(Request, interface{}, Response, error)) error
}


