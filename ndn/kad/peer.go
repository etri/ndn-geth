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
)
const (
	PING_LONGEVITY = int64(10*time.Second) 
	LOST_LONGEVITY = int64(300*time.Second) //time period where dead peer lasts
)

type NodeRecord interface {
	Identity() 	ID
	String()	string // pretty name for logging
	PublicKey() []byte
	Address()	string //transport address (ndn host prefix)
}

type KadPeer interface {
	Record() NodeRecord //node record
	Seen()		//update last seen time
}

type kadpeer struct {
	record	NodeRecord
	id		ID
	seen	int64    //time at last seen, if it was not long ago, wait, don't ping
	mutex	sync.Mutex
}


func PeerFromRecord(rec NodeRecord) *kadpeer {
	ret := &kadpeer{
			record:	rec, 
			id:		rec.Identity(), 
			seen: 	0,
		}
	return ret
}

//pretty name
func (p* kadpeer) String() string {
	return p.record.String()
}

func (p* kadpeer) Record() NodeRecord {
	return p.record
}

func (p *kadpeer) Seen() {
	p.setseen()
}

func (p *kadpeer) setseen() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.seen = time.Now().UnixNano()
}

func (p *kadpeer) recentlyseen() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return time.Now().UnixNano() - p.seen < PING_LONGEVITY
}

func (p *kadpeer) update(rec NodeRecord) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.record = rec
}

