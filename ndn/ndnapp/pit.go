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
	"github.com/ethereum/go-ethereum/log"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

const (
	INTEREST_LIFE_TIME = 4*time.Second//ndn.DefaultInterestLifetime 
)

type pit struct {
	root pitnode
	mutex sync.Mutex
}


type pitnode struct {
	val map[chan InterestData]*pendinginterest
	children map[string]*pitnode
	parent *pitnode
	level int
}

func (pn *pitnode) isempty() bool {
	return pn.val == nil && pn.children == nil
}

type pendinginterest struct {
	i *ndn.Interest
	expired	int64 //expiring time
	from	int64
	ch	chan InterestData
	satisfied bool
	mutex	sync.Mutex
}

func newpendinginterest(i *ndn.Interest, ch chan InterestData) *pendinginterest {
	i.ApplyDefaultLifetime()


	now := time.Now().UnixNano()
	expired := now + int64(i.Lifetime)
	if ch == nil {
		ch = make(chan InterestData,1)
	}
	return &pendinginterest {
		i: i,
		expired: expired,
		from:	now,
		ch:ch,
		satisfied: false,
	}
}

func (pi *pendinginterest) gone() {
	pi.mutex.Lock()
	defer pi.mutex.Unlock()
	pi.satisfied = true
}
func (pi *pendinginterest) dump() {
	pi.mutex.Lock()
	defer pi.mutex.Unlock()
	log.Info(fmt.Sprintf("%s at %d, s=%t\n", PrettyName(pi.i.Name), pi.expired, pi.satisfied))
}

func (pi *pendinginterest) isgone() bool {
	pi.mutex.Lock()
	defer pi.mutex.Unlock()
	return pi.satisfied
}

func (pi pendinginterest) satisfy(data *ndn.Data, err error) {
	pi.ch <- InterestData{data, err}
}

func (p *pit) insert(pi *pendinginterest) *pitnode {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	components := pi.i.Name
	node := &p.root	

	for i:=0; i< len(components); i++ {
		if node.children == nil {
			node.children = make(map[string] *pitnode)
			newnode := &pitnode{level: i, parent: node}
			node.children[components[i].String()] = newnode 
			node = newnode
			continue
		}

		child, ok := node.children[components[i].String()]
		if ok {
			node = child
		} else {
			newnode := &pitnode{level: i, parent: node}
			node.children[components[i].String()] = newnode
			node = newnode
		}
	}
	//now node is point to the newly inserted node
	//Add the Interest to the current entry on this node
	if node.val == nil {
		node.val = make(map[chan InterestData]*pendinginterest)
	}
	node.val[pi.ch] = pi

	return node
}
//try to satisfy the interest requesting the returning data packet
func (p *pit) satisfy(components ndn.Name, d *ndn.Data) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	node := &p.root	
	for i:=0; i<len(components); i++ {
		if node.children != nil {
			if child, ok := node.children[components[i].String()]; ok {
				node = child
			}
		}
		if node.val != nil {
			for ch, pi := range node.val {
				if pi.i.CanBePrefix == true {
					//match the data as interest name can be a prefix 
					if d == nil {
						pi.satisfy(nil, ErrNack)
					} else {
						pi.satisfy(d, nil)
					}
					delete(node.val, ch)
					//log.Info(fmt.Sprintf("%s is done as prefix-matched",
					//PrettyName(pi.i.Name)))
					pi.gone()
				} else if i == len(components)-1 {
					//match the data on full name
					if d == nil {
						pi.satisfy(nil, ErrNack)
					} else {
						pi.satisfy(d, nil)
					}

					delete(node.val, ch)
					//log.Info(fmt.Sprintf("%s is done as full-matched",
					//PrettyName(pi.i.Name)))
					pi.gone()
					//TODO: need to handle Interest with digest in its name
				}
			}
			if len(node.val)==0 {
				node.val = nil
			}
		}

	}
}

//delnode is called to clear up a pending interest that was not sent
func (p *pit) delentry(pi *pendinginterest, me *pitnode, expired bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	key := pi.i.Name
	//delete the entry
	delete(me.val, pi.ch)

	if expired {
		log.Info(fmt.Sprintf("Timeout interest:%s", PrettyName(pi.i.Name)))
		pi.satisfy(nil,ErrTimeout)
	}

	if len(me.val) == 0 {
		me.val = nil
	}

	//remove nodes if empty
	cur := me
	for cur !=nil  && cur.isempty() {
		parent := cur.parent
		delete(parent.children, key[parent.level].String())
		cur = parent
	}
}

