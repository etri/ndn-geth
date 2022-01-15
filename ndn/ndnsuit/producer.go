package ndnsuit

import (
	//"fmt"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

type ProducerRequest struct {
	Msg			Request
	Packet		*ndn.Interest
	Producer	Producer
}

type ProducerHandler func(*ProducerRequest)

//get a segment ([]byte) and injected into a Response message
type ObjManager interface {
	GetSegment(Query, uint16) (ResponseWithDataSegment, error)
}

type Producer interface {
	Serve(*ndn.Interest, Response) //build then send Data packet
	Sender() Sender
	Prefix() ndn.Name

	setSender(Sender)
	handler() func (*ndn.Interest) //return a handler to process a received Interest
	process(*ndn.Interest) //process a received Interest
	clean() //kill worker if created
}

type producerImpl struct {
	decoder 	RequestDecoder
	sender		Sender
	worker		utils.JobSubmitter //pool of workers to handle requests
	manager		ObjManager
	fn			ProducerHandler
	prefix		ndn.Name	
	cleaner		func()
}

func NewProducer(prefix ndn.Name, decoder RequestDecoder, fn ProducerHandler, pool utils.JobSubmitter, manager ObjManager) Producer {
	p := &producerImpl{
		prefix:		prefix,
		decoder:	decoder,
		fn:			fn,
		cleaner:	nil,
		manager:	manager,
	}
	if pool == nil {
		//if no worker pool is provided, create a single-worker pool to handle
		//Interests
		pool = utils.NewSingleWorker()
		//set a cleaner function to stop the worker
		p.cleaner = func() {
			pool.Stop()
		}
	}
	p.worker = pool
	return p
}

func (p *producerImpl) clean() {
	if p.cleaner != nil {
		p.cleaner()
	}
}

func (p *producerImpl) setSender(s Sender) {
	p.sender = s
}

//Face where the producer send its packets
func (p *producerImpl) Sender() Sender {
	return p.sender
}

func (p *producerImpl) Prefix() ndn.Name {
	return p.prefix
}
//send a response Data packet enclosing msg
func (p *producerImpl) Serve(i *ndn.Interest, msg Response) {
	if msg != nil {
		d := msg.WriteData(i)
		p.sender.SendData(&d)
	} else {
		d := ndn.MakeData(i) //make an empty Data packet
		p.sender.SendData(&d)
	}
}

func (p *producerImpl) process(i *ndn.Interest) {
	if req, err := p.decoder.DecodeInterest(i); err != nil {
		//fail to decode the Interest, do nothing
		return
	} else { //a request is decoded
		if p.manager == nil {
			// no implementation of object serving
			if p.fn != nil { //just call the handler
				p.fn(&ProducerRequest{Msg: req, Packet: i, Producer: p})
			}
			return
		}
		// there's an implementation for object serving, try to serve a segment
		if qx, ok := req.(HasQuery); ok { //if the request has a query
			if query, segId, err := qx.ExtractQuery(); err == nil {
				//ask the object manager to build a response message for the
				//requested segment
				if reply, err := p.manager.GetSegment(query, segId); err == nil { //get segment
					//send back a Data packet with the segment embeded
					p.Serve(i, reply)
					return
				} else {
					//object segment is not found, send a NACK packet
					p.sender.SendNack(i, uint8(150))
					return
				}
			}
		}
		//the request does not have a query, just call the handler
		if p.fn != nil {
			p.fn(&ProducerRequest{Msg: req, Packet: i, Producer: p})
		}
	}
}

func (p *producerImpl) handler() func (*ndn.Interest) {
	return func (i *ndn.Interest) {
		p.worker.Submit(&ProducerServingJob{
				i:			i,
				producer:	p,
		})
	}
}

type ProducerServingJob struct {
	i			*ndn.Interest
	producer	Producer
}


func (s *ProducerServingJob) Execute() {
	s.producer.process(s.i)
}
