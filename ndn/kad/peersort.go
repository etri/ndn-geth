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
//	"fmt"
	"time"
	"sync"
	"container/heap"
//	"github.com/ethereum/go-ethereum/log"
)

const (
	QUERY_GRANULARITY = int64(10*time.Second)
)
type PqPeer struct {
	p 			*kadpeer
	target 		ID
	index 		int
	queried 	int64
}

func (i *PqPeer) mark() {
	i.queried = time.Now().UnixNano()
}

func (i *PqPeer) recentlyqueried() bool {
	return time.Now().UnixNano() - i.queried < QUERY_GRANULARITY
}


type Pq []*PqPeer

func (pq Pq) Len() int {
	return len(pq)
}

func (pq Pq) Less(i,j int) bool {
	return DistCmp(pq[i].target, pq[i].p.id, pq[j].p.id) <= 0
}


func (pq Pq) Swap(i, j int) {
	pq[i],pq[j] = pq[j],pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *Pq) Push(x interface{}) {
	n := len(*pq)
	item := x.(*PqPeer)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *Pq) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0:n-1]
	return item
}


type peersort struct {
	target ID
	queue	Pq
	peers	map[ID]*PqPeer
	mutex	sync.Mutex
}

func newpeersort(target ID, peers []*kadpeer) *peersort {
	ret := &peersort {
		queue: make(Pq, len(peers)),
		peers: make(map[ID]*PqPeer),
		target: target,
	}
	j := 0
	for _, p := range peers {
		if _,ok := ret.peers[p.id]; ok {
			continue
		}
		item := &PqPeer{
			p: p,
			target: target,
			index: j,
			queried: 0,
		}
		ret.queue[j] = item
		ret.peers[p.id] = item
		j++
	}
	heap.Init(&ret.queue)
	return ret	
}
//add a new peer
func (ps *peersort) push(p *kadpeer) {
	defer ps.mutex.Unlock()
	ps.mutex.Lock()
	if _, ok := ps.peers[p.id]; ok {
		return
	}
	item := &PqPeer{
		p: p,
		target: ps.target,
		queried: 0,
	}
	ps.peers[p.id] = item
	heap.Push(&ps.queue, item)
}
//add a queried peer
func (ps *peersort) repush(qp *PqPeer) {
	defer ps.mutex.Unlock()
	ps.mutex.Lock()

	if _, ok := ps.peers[qp.p.id]; ok {
		//should never happen
		return
	}
	//qp.mark()
	ps.peers[qp.p.id] = qp
	heap.Push(&ps.queue, qp)
}


//add a list of peers
func (ps *peersort) update(peers []*kadpeer) {
	defer ps.mutex.Unlock()
	ps.mutex.Lock()
	//fmt.Println("Updating list...", dumppeers(peers))
	for _,p := range peers {
		if _, ok := ps.peers[p.id];ok {
			continue
		}
		item := &PqPeer{
			p: p,
			target: ps.target,
			queried: 0,
		}
		ps.peers[p.id] = item
		heap.Push(&ps.queue, item)
	}
}

/*
//remove a dead peer
func (ps *peersort) remove(id ID) {
	defer ps.mutex.Unlock()
	ps.mutex.Lock()

	item, ok := ps.peers[id]
	if ok {
		index := item.index
		delete(ps.peers, id)
		log.Info(fmt.Sprintf("remove peer %s index %d from nblist", item.p.node.String(), index))
		heap.Remove(&ps.queue, index)
	}
}
//mark peer as seen 
func (ps *peersort) mark(id ID) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	if item, ok := ps.peers[id]; ok {
		item.mark()
	}
}
*/

//get k closest neighbor
func (ps *peersort) closest(k int) []*PqPeer {
	defer ps.mutex.Unlock()
	ps.mutex.Lock()
	
	peers := make([]*PqPeer, k) 
	i := 0
	for len(ps.queue)>0 {
		item := heap.Pop(&ps.queue).(*PqPeer)
		delete(ps.peers, item.p.id)
		//if !item.p.recentlylost() {
			peers[i] = item
			i++
		//}
		if i>=k {
			break
		}
	}
	
	return peers[:i]
}


//get k closest neighbor with a criteria
func (ps *peersort) closestex(k int, check func(p *PqPeer) bool) []*kadpeer {
	defer ps.mutex.Unlock()
	ps.mutex.Lock()
	l := min(k,len(ps.queue))
	ret := make([]*kadpeer, l)
	j := 0
	for i:=0; i<l; i++ {
		peer := ps.queue[i]
		if check != nil && check(peer) {
			ret[j] = peer.p
			j++
		}
	}
	return ret[:j]
}

func notqueried(p *PqPeer) bool {
	return !p.recentlyqueried()
}


func notseen(p *PqPeer) bool {
	return !p.p.recentlyseen()
}

