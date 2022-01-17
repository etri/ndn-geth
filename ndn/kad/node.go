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


package kad

import (
	"fmt"
	"sync"
	"time"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/ethereum/go-ethereum/ndn/kad/rpc"
)
const (
	SHORT_LOOKUP_WAIT = 5*time.Second
	LOOKUP_WAIT = 30*time.Second
	LOOKUP_K = 5
	NUM_FINDNODE_ATTEMPS = 2
	NUM_PING_ATTEMPS = 2
)
type kadnode struct {
	self		*kadpeer
	rpc			rpc.KadRpcClient //rpc client
	rtable 		*rtable		//routing table
	bootnodes	[]*kadpeer //boot nodes

	ls			*lookupservice //for requesting a lookup
	qs			*queryingservice
	seenlist	[]NodeRecord //seen peers
	seench		chan struct {}
	seensig		bool
	peereventfn	PeerEventCallbackFn
	lpool		utils.JobSubmitter //worker pool for rpc-jobs and lookup-jobs
	rpcpool		utils.JobSubmitter //worker pool for rpc-jobs
	//NOTE: make sure we have a single lookup task, otherwise deadlock may
	//arise (lookup jobs occupy all workers so that rpc jobs never get queued

	quit		chan bool
	dead		bool	//node was killed
	mutex		sync.Mutex //for node status (dead)
	smutex		sync.Mutex //for seenlist
	wg			sync.WaitGroup
}

func newkadnode(me NodeRecord, rpc rpc.KadRpcClient) *kadnode {
	node := &kadnode {
		self:		PeerFromRecord(me),
		rpc: 		rpc,
		quit: 		make(chan bool),
		lpool:		utils.NewWorkerPool(5),
		rpcpool:		utils.NewWorkerPool(20),
		dead:		false,
		seensig:	false,
		seench:		make(chan struct{},1),
	}
	node.qs = newqueryingservice(me.Identity(), node, node.rpcpool)
	//create routing table
	node.rtable = newrtable(node.self, node.qs.pingpeer)

	//create lookup algorithm
	algo := newkadlookupalgo(node.rtable.closest, node.qs.findnode)

	//create lookup service for executing lookup queries
	node.ls = newlookupservice(algo, node.lpool)
	return node
}

func (node *kadnode) String() string {
	return node.self.String()
}

func (node *kadnode) markseenpeers() {
	node.smutex.Lock()
	defer node.smutex.Unlock()

	for _, rec := range node.seenlist {
		node.seepeer(rec)
	}
	node.seenlist =[]NodeRecord{}
	node.seensig = false
}

func (node *kadnode) seepeer(rec NodeRecord) {
	//get peer from routing table
	p := node.rtable.get(rec.Identity(), false)

	if p == nil {
		p = PeerFromRecord(rec)
		//add its to routing table
		if node.rtable.seen(p) {
			node.notifypeerevent(p, true)
		}
	} else {
		p.update(rec)
		//remove from deadlist
		node.qs.deadlist.remove(p.id)
	}
	p.setseen()
}


func (node *kadnode) notifypeerevent(p *kadpeer, live bool) {
	//peer events are infrequent event, we create a go routine to handle them
	//for the shake of simplicity
	go func() {
		//TODO: send event using chanel
		if node.peereventfn != nil {
			node.peereventfn(p.Record(), live)
		}
	}()
}

//bootstraping the node
func (node *kadnode) start(bootnodes []NodeRecord) error {
	node.mutex.Lock()
	defer node.mutex.Unlock()
	if node.dead { //node was killed, can't start
		return ErrJobAborted
	}

	if len(bootnodes) > 0 {
		nodes := make([]*kadpeer, len(bootnodes))
		i := 0
		for _, rec := range bootnodes {
			if rec.Identity() != node.self.id {
				nodes[i] = PeerFromRecord(rec)
				i++
			}
		}
		node.bootnodes = nodes[:i]
	}
	go node.mainloop()
	return nil
}

//Push the bootnodes to the being pinged queue. Once a peer response, it will
//be added to the routing table
func (node *kadnode) seedrtable(bootnodes []*kadpeer) {
	//log.Info(fmt.Sprintf("%s is seeding routing table with bootnodes", node))
	node.qs.pingpeers(bootnodes)
}

//the main loop
func (node *kadnode) mainloop() {
	node.wg.Add(1)
	defer node.wg.Done()
	
	node.qs.start()
	defer node.qs.stop()

	refreshtimer := time.NewTimer(SHORT_LOOKUP_WAIT)

	node.seedrtable(node.bootnodes)
	firstlookup := true
	
	lookupcallback := func(peers []*kadpeer, err error) {
		select {
		case <- node.quit:
			return
		default:
			if err == nil {
				refreshtimer.Reset(LOOKUP_WAIT)
			} else {
				refreshtimer.Reset(SHORT_LOOKUP_WAIT)
			}
		}
	}

	LOOP:
	for {
		select {
		case <- node.quit:
			break LOOP

		case <-refreshtimer.C: //time to do a lookup
			if node.rtable.isempty() { //routing table is empty, we need to seed it first
				firstlookup = true 
				node.seedrtable(node.bootnodes)
				refreshtimer.Reset(SHORT_LOOKUP_WAIT)
			} else {
				var id ID
				if firstlookup {
					firstlookup = false
					id = node.self.id //always self-lookup if the routing table is empty
				} else {
					id = RandomId(node.self.id) //pick a random targeting ID
				}
				//log.Info("DO LOOK UP")
				node.ls.lookup(id, LOOKUP_K, lookupcallback)
			}
		case <- node.seench:
			node.markseenpeers()
		}
	}
}

func (node *kadnode) stop() {
	node.mutex.Lock()
	if node.dead {
		node.mutex.Unlock()
		return
	}

	node.dead = true
	node.mutex.Unlock()

	node.lpool.Stop()
	node.rpcpool.Stop()
//	node.rtable.dump()
	close(node.quit)
	node.wg.Wait()
}
func (node *kadnode) isDead() bool {
	node.mutex.Lock()
	defer node.mutex.Unlock()
	return node.dead
}

//ping a peer, try a few time before announcing dead
func (node *kadnode) ping(p *kadpeer, abort chan bool) (err error) {
	for numtries:=1; numtries <= NUM_PING_ATTEMPS; {
		if _, err = node.rpc.Ping(p, abort); err == nil {
			break
		}
		log.Info(fmt.Sprintf("%s pings %s - error (trial %d): %s", node, p, numtries, err.Error()))
		numtries++
	}

	return
}

//findnode using rpc, try a few times
func (node *kadnode) findnode(target ID, k int, p *kadpeer, abort chan bool) (peers []*kadpeer, err error){
	if node.self.id == p.id {
		return node.rtable.closest(target, k), nil
	}

	var response *rpc.FindnodeResponse

	LOOP:
	for numtries :=1; numtries <= NUM_FINDNODE_ATTEMPS;  {
		//log.Info(fmt.Sprintf("ask %s, attemps %d", p, numtries))
		select {
		case <- abort:
			err = ErrJobAborted
			break LOOP
		default:
			if response, err = node.rpc.Findnode(p, target.String(), uint8(k), abort); err == nil {
				peers = node.makepeers(response.Peers)
				break LOOP
			}
			numtries++
		}
	}

	return
}

//change peer status while running kademlia algorithms
func (node *kadnode) peerstatechange(p *kadpeer, live bool) {
	if live {
		if node.rtable.seen(p) {
			node.notifypeerevent(p, live)
		}
	} else {
		//log.Info(fmt.Sprintf("peer %s is removed", p))
		if _, inbucket := node.rtable.remove(p.id); inbucket {
			node.notifypeerevent(p, live)
		}
	}
}

//drop a peer by an external request
func (node *kadnode) droppeer(id ID) {
	if p, _ := node.rtable.remove(id); p != nil {
		node.qs.drop(p)
	}
}

//filter self and recently dead peers. Create peers if they are new, otherwise,
//get them from the routing table
func (node *kadnode) makepeers(peers []rpc.NdnNodeRecordInfo) []*kadpeer {
	var p *kadpeer
	var err error
	var id ID
	var rec NodeRecord
	//fmt.Println(len(peers), "peers is received")
	if l:= len(peers); l>0 {
		ret := make([]*kadpeer,l)
		k := 0
		for i:=0; i<l; i++ {
			if rec, err = NdnNodeRecordUnmarshaling(peers[i]); err != nil {
				continue
			}
			id = rec.Identity()
			//ignore if it is self identity
			//if id == node.rec.Identity() {
			//	continue
			//}
			//ignore if it is a recently lost peer
			//if p, ok = node.lostpeers.get(id); ok && p.recentlylost() {
			//	continue
			//}

			//check if peer is known from routing table
			p = node.rtable.get(id, false)
			if p == nil {
				//otherwise create peer
				p = PeerFromRecord(rec)
			}
			ret[k] = p
			k++
		}
		return ret[:k]
	}
	return []*kadpeer{}
}

