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
	"encoding/hex"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)


type peerSet struct {
	peers			map[string]*peer //connected peers
	pendings		map[string]*peer //handshaking peers

	mutex			sync.Mutex
	c				*Controller
	numins			int
	numouts			int
}

func (ps *peerSet) Len() int {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	return len(ps.peers)
}

func (ps *peerSet) Ins() int {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	return ps.numins
}

func (ps *peerSet) Outs() int {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	return ps.numouts
}

func newpeerset(c *Controller) *peerSet{
	return &peerSet{
		peers: 		make(map[string]*peer),
		pendings:	make(map[string]*peer),
		c:			c,
		numins:		0,
		numouts:	0,
	}
}

//make a new peer or return a peer in the list (either pending or connected)
func (ps *peerSet) makenewpeer(id string, pub []byte, prefix string, dir bool) *peer {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	if p, ok := ps.peers[id]; ok {
		//make sure we do not register peer twice
		return p
	}

	if p, ok := ps.pendings[id]; ok {
		//make sure we do not register peer twice
		return p
	}
	p :=  newpeer(id, pub, prefix, dir)
	ps.pendings[id] = p
	return p
}

//register a new peer, return false if peer was already registered
func (ps *peerSet) register(p *peer) bool {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	//remove it from pending list
	delete(ps.pendings, p.id)

	if _, ok := ps.peers[p.id]; ok {
		//make sure we do not register peer twice
		return false
	}

	log.Info(fmt.Sprintf("register new peer %s(%s)", p.name,p.id[:5]))

	p.setConnected() //update it status to 'connected'

	ps.peers[p.id] = p
	if p.direction() {
		ps.numouts++
	} else {
		ps.numins++
	}
	p.startsendingloop(ps.c, ps.unregister) //start data announcing loop for the peer
	ps.dumppeerlist()
	return true
}


//unregister is called when sendingLoop exits
func (ps *peerSet) unregister(p *peer) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	if _,ok := ps.peers[p.id]; !ok {
		//unknown peer, do nothing
		return
	}

	if p.direction(){
		ps.numouts--
	} else {
		ps.numins--
	}

	delete(ps.peers, p.id)
}

//remove a peer from pending list after a failed handshaking
func (ps *peerSet) reject(p *peer) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	delete(ps.pendings, p.id)
}

func (ps *peerSet) dumppeerlist() {
	if len(ps.peers) > 0 {
		peerlist := fmt.Sprintf("%s's peers: ", ps.c.prefix)
		for _, p := range ps.peers {
			peerlist = fmt.Sprintf("%s %s(%s)", peerlist, p.name, hex.EncodeToString(p.secret[:])[:6])
		}
		log.Info(peerlist)
	}
}

/*
func dumppeerlist(peers []*peer) string {
	peerlist := ""
	for _, p := range peers {
		peerlist = fmt.Sprintf("%s %s", peerlist, p.name)
	}
	return peerlist
}
*/

/*
//get a random peer from peerlist
func (ps *peerSet) randomPeer(num uint64) *peer {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	peers := []*peer{}
	for _, p := range ps.peers {
		if p.Bnum()>= num {
			peers = append(peers, p)
		}
	}


	if len(peers) == 0 {
		return nil
	}
	index := rand.Intn(len(peers))
	return peers[index]
}
*/

func (ps *peerSet) getRandomPeer(offering bool) *peer {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	peers := []*peer{}
	for _, p := range ps.peers {
		if p.direction() == offering {
			peers = append(peers, p)
		}
	}
	if len(peers) >0 {
		index := rand.Intn(len(peers))
		return peers[index]
	}
	return nil
}

func (ps *peerSet) allpeers() (peers []*peer) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	peers = make([]*peer, len(ps.peers))
	i := 0
	for _, p := range ps.peers {
		peers[i] = p
		i++
	}
	return
}

//returns list of peers no knowing a block hash
func (ps *peerSet) peersWithoutBlock(hash common.Hash) (peers []*peer) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	pwb := []*peer{}
	for _, p := range ps.peers {
		if !p.kblks.Contains(hash) {
			pwb = append(pwb, p)
		}
	}
	if len(pwb) > 0 {
		peers = make([]*peer, len(pwb))
		indexes := rand.Perm(len(pwb))
		for i, j := range indexes {
			peers[i] = pwb[j]
		}
	}
	return
}

//returns list of peers no knowing a transaction hash
func (ps *peerSet) peersWithoutTx(hash common.Hash) (peers []*peer) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	pwt := []*peer{}
	for _, p := range ps.peers {
		if !p.ktxs.Contains(hash) {
			pwt = append(pwt, p)
		}
	}
	if len(pwt)>0 {
		peers = make([]*peer, len(pwt))
		indexes := rand.Perm(len(pwt))
		for i, j := range indexes {
			peers[i] = pwt[j]
		}
	}
	return
}

func (ps *peerSet) getPeer(id string) *peer {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	if p, ok := ps.peers[id]; ok {
		return p
	}
	return nil
}


func (ps *peerSet) bestPeer() (best *peer) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	if len(ps.peers) < 1 {
		return nil
	}
	var maxtd *big.Int	
	maxtd = big.NewInt(0)
	for _, p := range ps.peers {
		if maxtd.Cmp(p.td) < 0 {
			best = p
			maxtd = p.td
		}
	}

	return best
}


