package ndnsuit

import (
//	"fmt"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

//sync consumer interface
type Consumer interface {
	//send a blocking request and wait to receive a response and error
	Request(Request, interface{}, chan bool) (Response, error)
}

//async consumer interface
type AsyncConsumer interface {
	//send a non-blocking request
	Request(Request, interface{}, func(Request, interface{}, Response, error)) error

}

type consumerImpl struct {
	decoder		ResponseDecoder
	sender		Sender
}


func NewConsumer(decoder ResponseDecoder, sender Sender) Consumer {
	return &consumerImpl{decoder: decoder, sender: sender}
}


func (c *consumerImpl) Request(msg Request, route interface{}, quit chan bool) (reply Response, err error) {
	i := msg.WriteInterest(route) 
	var d *ndn.Data
	if quit != nil {
		ch := c.sender.AsyncSendInterest(&i)
		select {
		case outcome := <- ch:
			d = outcome.D
			err = outcome.Err
		case <- quit:
			 err = ErrJobAborted
		}

	} else {
		d, err = c.sender.SendInterest(&i)
	}
	if err == nil {
		reply, err = c.decoder.DecodeData(d)
	}
	return
}

type asyncConsumerImpl struct {
	worker		Consumer
	pool		utils.JobSubmitter
}


func NewAsyncConsumer(c Consumer, pool utils.JobSubmitter) AsyncConsumer {
	return &asyncConsumerImpl{
		worker:		c,
		pool:		pool,
	}
}

func (c *asyncConsumerImpl) Request(msg Request, route interface{}, fn func(Request, interface{}, Response, error)) error {
	if c.pool.Submit(newConsumerJob(msg, route, c.worker, fn)) {
		return nil	
	}
	return ErrJobAborted
}

type consumerJob struct {
	consumer	Consumer
	msg 		Request
	route		interface{}
	quit 		chan bool
	fn			func(Request, interface{}, Response, error)	
}

func newConsumerJob(msg Request, route interface{}, consumer Consumer, fn func(Request, interface{}, Response,error)) *consumerJob {
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
