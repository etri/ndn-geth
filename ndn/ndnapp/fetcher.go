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
//	"github.com/usnistgov/ndn-dpdk/ndn"
//	"fmt"
//	"github.com/ethereum/go-ethereum/log"
)

type SimpleSegment struct {
	Wire	[]byte
	Total	uint16
	Id		uint16
}

func NewSimpleSegment(wire []byte, id uint16, num uint16) *SimpleSegment {
	return &SimpleSegment {
		Wire:	wire,
		Total:	num,
		Id:		id,
	}
}

func (seg *SimpleSegment) GetNumSegments() uint16 {
	return seg.Total
}

func (seg *SimpleSegment) GetSegmentId() uint16 {
	return seg.Id
}

func (seg *SimpleSegment) GetWire() []byte {
	return seg.Wire
}


func (app *appConsumer) fetchSegment(query Query, route interface {}, segId uint16, quit chan bool) (segment ObjSegment,err error) {
	msg := query.WriteRequest(segId)
	var reply Response
	if reply, err = app.Request(msg, route, quit); err == nil {
		if reply == nil { //the server responses with an empty object
			err = ErrNoObject
			return
		}
		if ex, ok := reply.(ResponseWithDataSegment); ok {
			segment, err = ex.ExtractSegment() 
		} else {
			err = ErrUnexpected
		}
	}
	return
}

func (app *appConsumer) FetchObject(query Query, route interface{}, quit chan bool) (obj []byte, err error) {
	var segment ObjSegment
	num := 0
	done := false
	for !done {
		if segment, err =	app.fetchSegment(query, route, uint16(num), quit); err != nil {
			break
		}
		obj = append(obj, segment.GetWire()...)	
		if segment.GetNumSegments() <= segment.GetSegmentId()+1 {
			done = true
			break
		}
		num++
	}
	return
}

func (app *asyncAppConsumer) fetchSegment(query Query, route interface{}, segId uint16, fn func(uint16, ObjSegment, error)) error {

	msg := query.WriteRequest(segId)

	handler := func (req Request, rinfo interface{}, resp Response, err1 error) {
		err := err1
		var segment ObjSegment
		if err == nil &&  resp == nil { //the server responses with an empty object
			err = ErrNoObject
		} 
		if err == nil {
			if ex, ok := resp.(ResponseWithDataSegment); ok {
				segment, err = ex.ExtractSegment() 
			} else {
				err = ErrUnexpected
			}
		}
		fn(segId, segment, err)
	}
	return app.Request(msg, route, handler)
}

type SegmentFetchingResult struct {
	segment		ObjSegment
	id			uint16
	err			error
}

func (app *asyncAppConsumer) FetchObject(query Query, route interface {}, quit chan bool, numsegments uint16) (obj []byte, err error) {
	var segments [][]byte
	var seg ObjSegment
	total := numsegments
	start := 0
	if total <= 1 { //get the first segment
		if seg, err = app.worker.fetchSegment(query, route, 0, quit); err != nil {
			return
		}
		total = seg.GetNumSegments()

		if total == 1 {
			obj = seg.GetWire()
			return
		}

		start = 1
	}
	
	segments = make([][]byte, total)
	if start == 1 {
		segments[0] = seg.GetWire()
	}
	resCh := make(chan *SegmentFetchingResult, int(total)-start)


	fn := func(segid uint16, segment ObjSegment, err1 error) {
		resCh <- &SegmentFetchingResult{segment: segment, id: segid, err: err1}
	}

	for i:=start; i< int(total); i++ {
		if err = app.fetchSegment(query, route, uint16(i), fn); err != nil {
			return
		}
	}
	counter := start
	LOOP:
	for {
		select {
		case res := <- resCh:
			if res.err != nil {
				err = res.err
				break LOOP
			}
			if res.id != res.segment.GetSegmentId() || total != res.segment.GetNumSegments() {
				err = ErrUnexpected
				break LOOP
			}
			//log.Info(fmt.Sprintf("segment %d/%d", res.id+1, numsegments))
			segments[int(res.id)] = res.segment.GetWire()
			counter++
			if counter == int(total) {
			//	log.Info("Enough segments")
				break LOOP
			}
			
		case <- quit:
			err = ErrJobAborted
			break LOOP
		}
	}
	if err == nil {
		for _, segment := range segments {
			obj = append(obj, segment...)
		}
	}
	return
}

//get a segment ([]byte) and injected into a Response message
type ObjManager interface {
	GetSegment(Query, uint16) (ResponseWithDataSegment, error)
}

