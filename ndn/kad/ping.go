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
	"time"
	"sync"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/ethereum/go-ethereum/log"
)

const (
	FLUSH_INTERVAL = 10*time.Second
)
type deadpeer struct {
	peer	*kadpeer
	lost	int64
}

type deadpeerlist struct {
	list map[ID]*deadpeer
	mutex sync.Mutex
}

func newdeadpeerlist() *deadpeerlist {
	return &deadpeerlist{list: make(map[ID]*deadpeer)}
}

func (l *deadpeerlist) get(id ID) (*kadpeer,bool) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if p,ok := l.list[id]; ok {
		return p.peer,ok
	}
	return nil, false
}

func (l *deadpeerlist) remove(id ID) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	delete(l.list, id)
}

func (l *deadpeerlist) add(p *kadpeer) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.list[p.id] = &deadpeer{peer:p, lost: time.Now().UnixNano()}
}


//remove peers who have been lost for long time
func (l *deadpeerlist) flush() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for id, p := range l.list {
		if time.Now().UnixNano() - p.lost > LOST_LONGEVITY {
			delete(l.list, id)
		}
	}
}

type servicequerier	interface {
	findnode(ID, int, *kadpeer, chan bool) ([]*kadpeer, error) //findnode query
	ping(*kadpeer, chan bool) error //ping query
	peerstatechange(*kadpeer, bool) //notify peer state change
}

type queryingservice struct {
	waitinglist		map[ID]*kadpeer
	askinglist		map[ID]*kadpeer

	deadlist		*deadpeerlist	
	querier			servicequerier
	pool			utils.WorkerPool

	quit			chan bool
	pingch			chan struct{}
	pingsig			bool
	mutex			sync.Mutex
	id				ID
}

func newqueryingservice(id ID, querier servicequerier, pool utils.WorkerPool) *queryingservice {
	return &queryingservice{
		id:				id,
		querier:		querier,
		pool:			pool,
		deadlist:		newdeadpeerlist(),
		waitinglist:	make(map[ID]*kadpeer),
		askinglist:		make(map[ID]*kadpeer),
		quit:			make(chan bool),
		pingch:			make(chan struct{},1),
		pingsig:		false,
	}
}

func (s *queryingservice) start() {
	go s.loop()
}

func (s *queryingservice) pingpeer(p* kadpeer) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if p.recentlyseen() {
		return
	}
	if s.id == p.id {
		return //do not ping self
	}

	if _, ok := s.askinglist[p.id]; ok {
		return
	}
	if _, ok := s.waitinglist[p.id]; ok {
		return
	}
	s.waitinglist[p.id] = p

	if !s.pingsig {
		s.pingsig = true
		s.pingch <- struct{}{}
	}
}

//add peer to waitinglist for pinging
func (s *queryingservice) pingpeers(peers []*kadpeer) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, p := range peers {
		if p.id == s.id {
			continue //do not ping self
		}
		if p.recentlyseen() {
			continue
		}

		if _, ok := s.askinglist[p.id]; ok {
			continue
		}
		if _, ok := s.waitinglist[p.id]; ok {
			continue
		}
		s.waitinglist[p.id] = p
	}

	if !s.pingsig {
		s.pingsig = true
		s.pingch <- struct{}{}
	}

}

//a peer is dropped 
func (s *queryingservice) drop(p *kadpeer) {
	s.deadlist.remove(p.id)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.waitinglist, p.id)
	delete(s.askinglist, p.id)
}

//ping return handler
func (s *queryingservice) onreplied(p *kadpeer, err error) {
	s.mutex.Lock()
	//remove from querying list
	delete(s.askinglist, p.id)
	s.mutex.Unlock()

	if err == nil {
		p.setseen()
		//peer is live
		s.deadlist.remove(p.id)
		s.querier.peerstatechange(p, true) //notify that peer is live
	} else { //peer is dead
		s.deadlist.add(p)
		s.querier.peerstatechange(p, false) //notify that peer is dead
	}
}

//launch a findnode query
func (s *queryingservice) findnode(p *kadpeer, target ID, k int, fn findnodeCallbackHandler) {

	callback := func(peers []*kadpeer, err error) {
		s.onreplied(p, err) //notify peer state
		if err == nil {
			s.pingpeers(peers) //ping all peers in the return list
		}
		fn(peers, err) //call the inner callback function
	}

	job := newfindnodeJob(s.querier, target,k, p, callback)
	if !s.pool.Do(job) {
		//pool is dead, still we need to call the callback function
		fn([]*kadpeer{}, ErrJobAborted)
	}

}

func (s *queryingservice) loop() {
	flushtimer := time.NewTimer(FLUSH_INTERVAL)
	LOOP:
	for {
		select {
		case <- s.quit:
			break LOOP
		case <- flushtimer.C:
			//remove dead42long peers
			s.deadlist.flush()
		case <- s.pingch:
			//do pinging
			s.dopingpeers()
		}
	}
}

func (s *queryingservice) dopingpeers() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	for _, p := range s.waitinglist {
		if p.recentlyseen() {
			log.Info(fmt.Sprintf("%s was recently pinged", p))
			continue
		}

		if s.pool.Do(newpingJob(s.querier, p, s.onreplied)) {
			//add peer to pinging list
			s.askinglist[p.record.Identity()] = p
		}
	}

	//clear ping-waiting list
	s.waitinglist = make(map[ID]*kadpeer)
	s.pingsig	= false
}

func (s *queryingservice) stop() {
	close(s.quit)
}


type pingJob struct {
	querier		servicequerier
	onpinged	func(*kadpeer, error)
	p 			*kadpeer
	abort 		chan bool //to cancel a pinging job
}

func newpingJob(querier servicequerier, p *kadpeer, onpinged func(*kadpeer, error)) *pingJob {
	return &pingJob{
		querier:	querier,
		p:			p,
		abort:		make(chan bool), 
		onpinged:	onpinged,
	}
}

func (j *pingJob) Execute() {
	err := j.querier.ping(j.p, j.abort)
	if j.onpinged != nil {
		j.onpinged(j.p, err)
	}
}

func (j *pingJob) Kill() {
	close(j.abort)
}

type findnodeJob struct {
	querier	servicequerier
	p		*kadpeer //peer to ask
	target 	ID //id to ask
	k		int //number of peers to retrieve
	fn		func([]*kadpeer,error)
	abort	chan bool
}


func newfindnodeJob(querier	servicequerier, target ID, k int, p *kadpeer, fn func([]*kadpeer, error)) *findnodeJob {
	return &findnodeJob{
		querier:	querier,
		p:			p,
		target: 	target,
		k:			k,
		fn:			fn,
		abort:		make(chan bool),
	}
}

func (j *findnodeJob) Execute() {
	peers, err := j.querier.findnode(j.target,j.k, j.p, j.abort)
	j.fn(peers, err)
}


func (j *findnodeJob) Kill() {
	close(j.abort)
}


