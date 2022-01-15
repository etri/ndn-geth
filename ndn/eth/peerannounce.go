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


package eth

import (
	"time"
	"math/rand"
	"sync"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

type PendingAck struct {
	interest 	*ndn.Interest
	producer	ndnsuit.Producer
}


type peersendingloop struct {
	c			*Controller
	p			*peer
	unregister	func(*peer) //unregisterig peer
	tlist		[]*PushItem //pending tx announcement
	blist		[]*PushItem //pending block announcement
	alist		[]*PushItem //accumulated list for next sending
	all			map[common.Hash]*PushItem
	itemCh		chan []*PushItem
	ackCh		chan *PendingAck
	failedcnt	int
	forced		bool
	quit		chan bool
	wg			sync.WaitGroup
}

func newpeersendingloop(p *peer, c *Controller, unregister func(*peer)) *peersendingloop {
	loop := &peersendingloop {
		p:			p,
		c:			c,
		unregister:	unregister,
		all:		make(map[common.Hash]*PushItem),
		itemCh:		make(chan []*PushItem, 1024),
		ackCh:		make(chan *PendingAck, 256),
		quit: 		make(chan bool),
	}
	go loop.run()
	return loop
}

func (l *peersendingloop) close() {
	close(l.quit)
	l.wg.Wait()
}

func (l *peersendingloop) sendItems(items []*PushItem) {
	l.itemCh <- items
}

func (l *peersendingloop) sendAck(i *ndn.Interest, producer ndnsuit.Producer) {
	l.ackCh <- &PendingAck{
		interest:	i,
		producer:	producer,
	}
}

func (l *peersendingloop) sendInterest(items []*PushItem) chan error {
	//send the items in the list
	nonce := rand.Uint64()
	pmsg := &PushMsg {
			NodeId:		l.c.id,
			Items:		items,
			Nonce:		nonce,
		}
	pmsg.Sign(l.c.crypto, l.p.secret[:])
	//pmsg.Encrypt(l.c.crypto, l.p.secret[:])

	msg := &EthMsg {
		MsgType:	TtPush,
		Content:	pmsg,
	}
	
	ch := make(chan error,1)

	if err := l.c.asendEthInterest(l.p, msg, false, func(req ndnsuit.Request, route interface{}, rep ndnsuit.Response, err1 error) {
		//the response is an acknowledgement, it may contains a PushMsg or a Bye message
		if rep != nil {
			if msg, ok := rep.(*EthMsg); ok {
				//forward to controller for handling the embeded message
				l.c.forwardEthAckMsg(l.p, msg)
			}
		}
		ch <- err1
	}); err !=nil {
		ch <- err
	}
	return ch
}

// accumulate items into alist for next sending 
func (l *peersendingloop) accumulate() (totalsize int, hasblock bool) {
	var item *PushItem
	newlist := []*PushItem{}
	//estimate total size of the last item accumulation
	totalsize = 0
	for _, item = range l.alist {
		if !l.p.hasitem(item) {
			totalsize += item.Size()
			newlist = append(newlist, item)
		} else {
			//p knowns the item hash recently
			delete(l.all, item.hash) 
		}
	}

	hasblock = false

	for len(l.blist)>0 {
		item = l.blist[0]
		if l.p.hasitem(item) {
			//peer knowns the item; pass it
			l.blist = l.blist[1:] //pop the first item
			delete(l.all, item.hash) 
			continue
		}
		if totalsize + item.Size() > MAX_PUSH_CONTENT_SIZE {
			l.alist = newlist
			return
		}
		hasblock = true
		newlist = append(newlist, item)
		totalsize += item.Size()
		l.blist = l.blist[1:] //pop the first item
	}

	for len(l.tlist)>0 {
		item = l.tlist[0]
		if l.p.hasitem(item) {
			//peer knowns the item; pass it
			l.tlist = l.tlist[1:] //pop the first item
			delete(l.all, item.hash) 
			continue
		}

		if totalsize + item.Size() > MAX_PUSH_CONTENT_SIZE {
			l.alist = newlist[:]
			return
		}
		newlist = append(newlist, item)
		totalsize += item.Size()
		l.tlist = l.tlist[1:] //pop the first item
	}
	l.alist = newlist
	return
}

func (l *peersendingloop) addItems(items []*PushItem) {
	for _, item := range items {
	//encode to bytes to known the item size
		item.EncodeWire()
		if item.ForBlock() {
			if _, ok := l.all[item.hash]; !ok {
				l.blist = append(l.blist, item)
				l.all[item.hash] = item
			}
		} else {
			if _, ok := l.all[item.hash]; !ok {
				l.tlist = append(l.tlist, item)
				l.all[item.hash] = item
			}
		}
	}

	//in case the lists are overloaded
	if len(l.blist) > MAX_QUEUED_BLKS {
		start := len(l.blist)-MAX_QUEUED_BLKS
		for i:=0; i<start; i ++ {
			delete(l.all,l.blist[i].hash)
		}
		l.blist = l.blist[start:]
	}
	if len(l.tlist) > MAX_QUEUED_TXS {
		start := len(l.tlist)-MAX_QUEUED_BLKS
		for i:=0; i<start; i ++ {
			delete(l.all,l.tlist[i].hash)
		}

		l.tlist = l.tlist[len(l.tlist)-MAX_QUEUED_TXS:]
	}
}

func (l *peersendingloop) updatesent() {
	l.p.markitems(l.alist)
	for _, item := range l.alist {
		delete(l.all, item.hash)
	}
	l.alist = []*PushItem{}
}

func  (l *peersendingloop) run() {
	l.wg.Add(1)
	defer l.wg.Done()

	pushtimer := time.NewTimer(PUSH_FORCED_INTERVAL) //timer to  force announcing tx hashes
	forced := false //whether push is forced by the timer

	var outcomeCh chan error //send outcome chanel

	LOOP:
	for {
		
		if outcomeCh == nil {
			//try sending if the last one has completed
			if totalsize, hasblock := l.accumulate(); totalsize > 0 {
				if forced || hasblock || len(l.alist) > MIN_PUSH_ITEMS  {
					//log.Info(fmt.Sprintf("send %d in %d items",totalsize, len(l.alist)))
					outcomeCh = l.sendInterest(l.alist)
					l.updatesent()
					//log.Info(fmt.Sprintf("push %d items", len(l.alist)))
					forced = false
					pushtimer.Reset(PUSH_FORCED_INTERVAL)
				} 
			}
		}
		

		select {
			case ack := <- l.ackCh:
				if totalsize, _ := l.accumulate(); totalsize > 0 {
					nonce := rand.Uint64()
					pmsg := &PushMsg {
									NodeId:		l.c.id,
									Items:		l.alist,
									Nonce:		nonce,
								}

					pmsg.Sign(l.c.crypto, l.p.secret[:])
				//	pmsg.Encrypt(l.c.crypto, l.p.secret[:])
					msg := &EthMsg {
						MsgType:	TtPush,
						Content:	pmsg,
					}
					ack.producer.Serve(ack.interest, msg)
					l.updatesent()
					forced = false
					pushtimer.Reset(PUSH_FORCED_INTERVAL)
				} else {
					//send empty ack
					ack.producer.Serve(ack.interest, nil)
				}
			case <- l.quit:
				//log.Info("receive quit signal, break the loop")
				break LOOP

			case <- pushtimer.C:
				//force tx announcement
				forced = true
			case items := <- l.itemCh:
				l.addItems(items)	

			case outcome := <- outcomeCh:
				//receive sending interest feedback
				if outcome != nil {
				//	log.Info(fmt.Sprintf("Announcement error: %s, drop %s", outcome.Error(), l.p))
					break LOOP

				}
				//announcement has been sent!
				outcomeCh = nil
		}
	}
	//log.Info("start unregistering p")
	//unregister peer when the loop break
	l.unregister(l.p)
}

