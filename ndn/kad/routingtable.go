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
	"container/heap"
	"sync"
//	"github.com/ethereum/go-ethereum/log"
)
type peerpinger interface {	
	addpeer4ping(p *kadpeer)
}
type bucket struct {
	peers []*kadpeer
	replacements []*kadpeer
	mutex sync.Mutex
}

func (b *bucket) size() int {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return len(b.peers)
}

func (b *bucket) get(id ID, onlybucket bool) *kadpeer {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	var ret *kadpeer
	for _, p := range b.peers {
		if p.id == id {
			ret = p
			break
		}
	}
	if !onlybucket && ret == nil {
		//get from replacements
		for _, p := range b.replacements {
			if p.id == id {
				ret = p
				break
			}
		}

	}

	return ret
}
func (b *bucket) getpeers() []*kadpeer {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.peers
}

//add a peer to the bucket, return a peer to be pinged for replacemetn
//second return value is true if the peer is added but not to the replacement list
func (b *bucket) add(peer *kadpeer) (*kadpeer,bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for _, p := range b.peers {
		if p.id == peer.id {
			//fmt.Println(p.node.String(), "already existed")
			return nil, false //peer 
		}
	}
	l := len(b.peers)
	if l >= BUCKETSIZE {
		//add peer to replacement
		l1 := len(b.replacements)
		for _, p := range b.replacements {
			if p.id == peer.id {

				return b.peers[0], false
			}
		}

		if l1 >= REPSIZE {
			//shift the list to left and add the new peer to the right most position
			for i:=0; i< l1-1; i++ {
				b.replacements[i] = b.replacements[i+1]
			}
			b.replacements[l1-1] = peer
		} else { //add to the end of the replacements
			b.replacements = append(b.replacements, peer)
		}

		//return the first peer of the bucket for pinging
		return b.peers[0], false
	} else {
		//fmt.Println(peer.node.String(), "is add to a bucket of")
		b.peers = append(b.peers, peer) //add peer to the last of the bucket
		return nil, true
	}
}

//remove a dead peer from bucket
//return true if peer is removed from the bucket (not replacement list)
func (b *bucket) remove(id ID) (removed *kadpeer, inbucket bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	inbucket = false
	found := -1 //index of found peer
	for i, p := range b.peers {
		if p.id == id {
			found = i
			removed = p
			break
		}
	}
	//peer is in the bucket
	if found >=0 {
		if len(b.replacements) > 0 {
			l := len(b.peers)
			for i:=found; i<l-1; i++ {
				b.peers[i] = b.peers[i+1]
			}
			//add the first peer from the replacements
			b.peers[l-1] = b.replacements[0]
			//update the replacements
			b.replacements = b.replacements[1:]
		} else { //no replacements, just remove the peer
			b.peers = append(b.peers[:found], b.peers[found+1:]...)
		}
		inbucket = true
		return 
	}

	for i, p := range b.replacements {
		if p.id == id {
			found = i
			removed = p
			break
		}
	}

	if found >=0 { //peer is in the replacement list
		b.replacements = append(b.replacements[:found],b.replacements[found+1:]...)
	}

	//peer is not in the list (strange)

	return 
}


type rtable struct {
	self		*kadpeer	
	mutex		sync.Mutex
	level	 	int //number of etries
	k			int //max number of peers/bucket
	buckets 	[]bucket
	ping		func(*kadpeer)
}


func newrtable(self *kadpeer, ping func(*kadpeer)) *rtable {
	r := &rtable{
		self: self,
		buckets: make([]bucket, NUMBUCKETS),
		ping: ping,
	}
	r.seen(self)
	return r
}

//get the bucket where the id belongs to
func (r *rtable) bucket(id ID) *bucket {
	kid := LogDist(id, r.self.id)
	if kid <= BUCKETMINDISTANCE {
		return &r.buckets[0]
	} else {
		return &r.buckets[kid-BUCKETMINDISTANCE-1]
	}
}

//get the total number of peers in the table
func (r *rtable) size() int {
	n:= 0
	for _, b := range r.buckets {
		n += b.size() 
	}
	return n
}

// is routing table empty
func (r *rtable) isempty() bool {
	for _, b := range r.buckets {
		if b.size() > 0 {
			return false
		}
	}
	return true
}

//add a seen peer, if bucket is full, order the first peer in the bucket to be
//pinged
// return true if the peer is added (not to the replacement lists)
func (r *rtable) seen(p *kadpeer) bool {
	bucket := r.bucket(p.id)
	first, added := bucket.add(p)
	if first != nil {
		r.ping(first) //add peer to pinging queue
	}
	return added
}

func (r *rtable) remove(id ID) (removed *kadpeer, inbucket bool) {
	bucket := r.bucket(id)
	return bucket.remove(id)
}

//pick a random peer
//pick a randome peer for pinging
func (r *rtable) pick() *kadpeer {
	return nil
	//TODO: to be implemented
}

//get peer with given id, return nil if peer is not found
func  (r *rtable) get(id ID, onlybucket bool) *kadpeer {
	bucket := r.bucket(id)
	return  bucket.get(id, onlybucket)
}


//get k closest peers to a given id from the table
func (r *rtable) closest(target ID, k int) []*kadpeer {
	//log.Info(fmt.Sprintf("routing table has %d entries\n", r.size()))
	queue := Pq{} 
	i := 0
	for _,b := range r.buckets {
		peers := b.getpeers()
		bq := make(Pq, len(peers))
		for j,p := range peers {
			bq[j] = &PqPeer{
				p:	p,
				target: target,
				index:	i+j,
			}
		}
		i += len(peers)
		queue = append(queue, bq...)
	}

	heap.Init(&queue)

	l := 0 
	peers := make([]*kadpeer, k)
	for queue.Len()>0 {
		p := heap.Pop(&queue).(*PqPeer)
		peers[l] = p.p
		l++
		if l>=k {
			break
		}
	}
	return peers[:l]
}

//dump table information
func (r *rtable) dump() {
	s := fmt.Sprintf("dump routing table for %s:", r.self)
	for _, b := range r.buckets {
		for _, p := range b.getpeers() {
			s = fmt.Sprintf("%s %s", s, p)
		}
	}
	fmt.Println(s)
}
