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
	"fmt"
	"time"
	"sync"
	"container/heap"
	"github.com/ethereum/go-ethereum/log"
)

//pending interest and its status
type interestat struct {
	pi 		*pendinginterest
	node 	*pitnode //its location in the PIT tree
	index	int //its index in located node
}


//priority queue of pending interest; sorted by expiring time
type ipq []*interestat

func (q ipq) Len() int {
	return len(q)
}

func (q ipq) Less(i,j int) bool {
	return q[i].pi.expired < q[j].pi.expired
}


func (q ipq) Swap(i,j int) {
	q[i],q[j] = q[j],q[i]
	q[i].index = i
	q[j].index = j
}

func (q *ipq) Push(x interface{}) {
	n := len(*q)
	item := x.(*interestat)
	item.index = n
	*q = append(*q, item)
}

func (q *ipq) Pop() interface{} {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*q = old[0:n-1]

	return item
}

//protect the timer priority queue with mutex
type timersort struct {
	queue ipq
	mutex sync.Mutex
}

func newtimersort() *timersort {
	ret := &timersort{}
	heap.Init(&ret.queue)
	return ret
}


func (ts *timersort) push(pi *pendinginterest, n *pitnode) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	heap.Push(&ts.queue, &interestat{pi: pi, node: n});
}


func (ts *timersort) checkexpiring() []*interestat {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	expired := []*interestat{}
	now := time.Now().UnixNano()
	for ts.queue.Len()>0 {
		item := ts.queue[0] 
		pi := item.pi
		if pi.isgone() {
			//log.Info(fmt.Sprintf("%s is already gone", pi.i.Name.String()))
			heap.Pop(&ts.queue)
		} else if pi.expired <= now {
			log.Info(fmt.Sprintf("%s is expired after %d", PrettyName(pi.i.Name), now-pi.from))
			expired = append(expired, item)
			heap.Pop(&ts.queue)
		} else {
			break
		}
	}

	return expired
}

func (ts *timersort) dump() {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	for i := 0; i< len(ts.queue); i++ {
		pi := ts.queue[i].pi
		pi.dump()
	}
}
