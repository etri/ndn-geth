package ndnsuit

import (
//	"fmt"
	"github.com/ethereum/go-ethereum/ndn/utils"
//	"github.com/usnistgov/ndn-dpdk/ndn"
)

type Query interface {
	//turn an object query into a request
	WriteRequest(uint16) Request
}

type HasId interface {
	GetId() string
}

//extend Response interface to support embeding an object segment
type ResponseWithDataSegment interface {
	Response
	ExtractSegment() (ObjSegment, error) //extract ObjSegment
}

//interface to extract a query from a Request
type HasQuery interface {
	ExtractQuery() (Query, uint16, error)
}

type ObjSegment interface {
	GetNumSegments() uint16
	GetSegmentId() uint16
	GetWire()	[]byte
}

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



type ObjConsumer interface {
	Consumer
	//fetch a single segment of an object (blocking)
	fetchSegment(Query, interface {}, uint16, chan bool) (ObjSegment, error)
	//request a binary object using a query (block util the object returns)
	FetchObject(Query, interface{}, chan bool) ([]byte, error)
}

type ObjAsyncConsumer interface {
	AsyncConsumer
	//fetch a single segment of an object (non-blocking)
	fetchSegment(Query, interface{}, uint16, func(uint16, ObjSegment, error)) error 
	//non-blocking request a binary object using a query
	FetchObject(Query, interface {}, chan bool, uint16) ([]byte, error) 
}

type objConsumerImpl struct {
	*consumerImpl
}

func NewObjConsumer(decoder ResponseDecoder, sender Sender) ObjConsumer {
	c :=  &objConsumerImpl{
		&consumerImpl{ 
			decoder:	decoder,
		sender:		sender,
		},
	}
	return c 
}

func (c *objConsumerImpl) fetchSegment(query Query, route interface {}, segId uint16, quit chan bool) (segment ObjSegment, err error) {
	//turn the query into a Request
	msg := query.WriteRequest(segId)
	var reply Response

	//request a segment
	if reply, err = c.Request(msg, route, quit); err == nil {
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

func (c *objConsumerImpl) FetchObject(query Query, route interface{}, quit chan bool) (obj []byte, err error) {
	var segment ObjSegment
	num := 0 //first segment
	done := false
	for !done {
		if segment, err =	c.fetchSegment(query, route, uint16(num), quit); err != nil {
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

type objAsyncConsumerImpl struct {
	*asyncConsumerImpl
}

func NewObjAsyncConsumer(c Consumer, pool utils.JobSubmitter) ObjAsyncConsumer {
	return &objAsyncConsumerImpl {
			&asyncConsumerImpl {
			worker:		c,
			pool:		pool,
		},
	}
}

func (c *objAsyncConsumerImpl) fetchSegment(query Query, route interface{}, segId uint16, fn func(uint16, ObjSegment, error)) error {

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
	return c.Request(msg, route, handler)
}

type SegmentFetchingResult struct {
	segment		ObjSegment
	id			uint16
	err			error
}

func (c *objAsyncConsumerImpl) FetchObject(query Query, route interface {}, quit chan bool, numsegments uint16) (obj []byte, err error) {

	var segments [][]byte
	var resCh chan *SegmentFetchingResult

	total := numsegments	

	fn := func(segid uint16, segment ObjSegment, err1 error) {
		resCh <- &SegmentFetchingResult{segment: segment, id: segid, err: err1}
	}

	if total > 0 {
		segments = make([][]byte, total)
		resCh = make(chan *SegmentFetchingResult, int(total))
		for i:=0; i < int(total); i++ {
			if err = c.fetchSegment(query, route, uint16(i), fn); err != nil {
				return
			}
		}
	} else {
		resCh = make(chan *SegmentFetchingResult, 1)
		if err = c.fetchSegment(query, route, 0, fn); err != nil {
			return
		}

	}

	
	counter := 0
	LOOP:
	for {
		select {
		case res := <- resCh:
			if res.err != nil {
				err = res.err
				break LOOP
			}

			if res.id != res.segment.GetSegmentId() {
				err = ErrUnexpected
				break LOOP
			}

			if numsegments == 0  && res.id == 0 { //first segment
				total = res.segment.GetNumSegments() 
				if total <= 0 {
					err = ErrUnexpected
					break LOOP
				}

				segments = make([][]byte, total)
				segments[0] = res.segment.GetWire()
				counter += 1

				if total > 1 {
					resCh = make(chan *SegmentFetchingResult, int(total)-1)
					for i:=1; i < int(total); i++ {
						if err = c.fetchSegment(query, route, uint16(i), fn); err != nil {
							break LOOP
						}
					}
				}

				//create segment
			} else {
				if total != res.segment.GetNumSegments() {
					err = ErrUnexpected
					break LOOP
				}
				//log.Info(fmt.Sprintf("segment %d/%d", res.id+1, numsegments))
				segments[int(res.id)] = res.segment.GetWire()
				counter++
			}
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

