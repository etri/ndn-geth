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


package ndn

import (
//	"fmt"
	"time"
	"math/big"
//	"encoding/hex"
	"sync"
	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/common"
//	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/ndn/ndnapp"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/dhkx"
)

const (
	MAX_KNOWN_TXS = 4096 //maximum number of known tx hashes can be cached at a peer
	MAX_KNOWN_BLKS= 256 //maximum number of known block hashes can be cached at a peer
	MAX_QUEUED_TXS = 4096 //maximum number of tx/hash queued for announcing
	MAX_QUEUED_BLKS = 256 //maximum number of block/header/hash queued for announcing
	MIN_PUSH_ITEMS = 10 //minimum number of hashes to be announced if timer is not yet fired
	//MAX_PUSH_CONTENT_SIZE = 8000 //max size of content can be packed in a NDN packet
	MAX_PUSH_CONTENT_SIZE = 1000 //max size of content can be packed in a NDN packet
	PUSH_FORCED_INTERVAL = 200*time.Millisecond
)

const (
	PEER_HANDSHAKING = 0 //newly created peer, in handhaksing mode
	PEER_CONNECTED = 1 //finished handshaking, ready to work
	PEER_CLOSED = 2 //peer is closed

)
type peer struct {
	id			string //Kakdemlia node identity
	pub			[]byte //peer public key
	name		string //peer name for logging
	prefix		ndn.Name //peer name prefix
	g			*dhkx.DHGroup //group for key exchange
	dhprv		*dhkx.DHKey //prv for key exchange
	secret		[32]byte //shared secret
	ctime		time.Time //connected time

	dir			bool //offering or offered

	head		common.Hash //latest block hash
	td			*big.Int    //chain difficulty 
	bnum		uint64      //chain height

	status		int	//peer was killed

	kmutex		sync.Mutex //protect kill
	dmutex		sync.Mutex //protect data

	ktxs		mapset.Set	 //known transactions
	kblks		mapset.Set	 //known blocks
	
	loop		*peersendingloop //loop for sending announcements
}


func (p *peer) Id() string {
	return p.id
}

func (p *peer) direction() bool {
	return p.dir
}

func newpeer(id string, pub []byte, prefix string, offering bool) *peer {
	p := &peer{
		id: 		id,	
		pub:		pub,
		name:		prefix,
		prefix:		ndn.ParseName(prefix),	
		bnum:		0,
		td:			big.NewInt(0),
		dir:		offering,
		status:		PEER_HANDSHAKING,
		ktxs:		mapset.NewSet(),
		kblks:		mapset.NewSet(),
		ctime:		time.Now(),
	}
	p.g, _ = dhkx.GetGroup(0)
	p.dhprv, _ = p.g.GeneratePrivateKey(nil)
	return p
}
func (p *peer) isConnected() bool {
	p.dmutex.Lock()
	defer p.dmutex.Unlock()
	return p.status == PEER_CONNECTED
}
func (p *peer) setConnected() {
	p.dmutex.Lock()
	defer p.dmutex.Unlock()
	p.status = PEER_CONNECTED
}
func (p *peer) makesecret(dhpub []byte) {
	p.dmutex.Lock()
	defer p.dmutex.Unlock()

	pub := dhkx.NewPublicKey(dhpub)
	k, _ := p.g.ComputeKey(pub, p.dhprv)
	copy(p.secret[:], k.Bytes())
	//log.Info(fmt.Sprintf("secret: %s-%s(%d)", p.name, hex.EncodeToString(p.secret[:])[:6], len(p.secret)))
	//update connected time
}

func (p *peer) String() string {
	return p.name 
}

func (p *peer) Bnum() uint64 {
	p.dmutex.Lock()
	defer p.dmutex.Unlock()

	return p.bnum
}

func (p *peer) Td() *big.Int {
	p.dmutex.Lock()
	defer p.dmutex.Unlock()

	return p.td
}

func (p *peer) Head() (head common.Hash, num uint64, td *big.Int) {
	p.dmutex.Lock()
	defer p.dmutex.Unlock()

	copy(head[:], p.head[:])	
	td = new(big.Int).Set(p.td)
	num = p.bnum
	return head, num, td
}

func (p *peer) SetHead(head common.Hash, td *big.Int, bnum uint64) bool {
	p.dmutex.Lock()
	defer p.dmutex.Unlock()
	if p.td.Cmp(td) < 0 {
		copy(p.head[:], head[:])
		p.td.Set(td)
		p.bnum = bnum
		return true
	}
	return false
}

func (p *peer) hasitem(item *PushItem) bool {
	p.dmutex.Lock()
	defer p.dmutex.Unlock()
	if item.ForBlock() {
		return p.kblks.Contains(item.hash)
	} else {
		return p.ktxs.Contains(item.hash)
	}
}

func (p *peer) markitems(items []*PushItem) {
	p.dmutex.Lock()
	defer p.dmutex.Unlock()
	for _,item := range items {
		if item.ForBlock() {
			for p.kblks.Cardinality() >= MAX_KNOWN_BLKS {
				p.kblks.Pop()
			}
			p.kblks.Add(item.Hash())

		} else {
			for p.ktxs.Cardinality() >= MAX_KNOWN_TXS {
				p.ktxs.Pop()
			}
			p.ktxs.Add(item.Hash())
		}
	}

}

//queue an announcement to send
func (p *peer) doSend(item *PushItem) {
	defer p.kmutex.Unlock()
	p.kmutex.Lock()
	
	//NOTE: make sure we do not announce to a closed peer
	if p.status != PEER_CONNECTED {
		return
	}

	if p.hasitem(item) {
		return
	}
		
	p.loop.sendItems([]*PushItem{item})
}

//queue an acknowledment to send
func (p *peer) sendAck(i *ndn.Interest, producer ndnapp.AppProducer) {
	defer p.kmutex.Unlock()
	p.kmutex.Lock()
	
	//NOTE: make sure we do not announce to a closed peer
	if p.status != PEER_CONNECTED {
		return
	}
	p.loop.sendAck(i, producer)

}

//queue tx announcements (when the peer is connected)
func (p *peer) announceTxs(hashes []common.Hash) {
	p.kmutex.Lock()
	defer p.kmutex.Unlock()

	if p.status != PEER_CONNECTED {
		return
	}

	//NOTE: the peer knows none of the hashes, do no check
	if len(hashes) > 0 {
		items := make([]*PushItem, len(hashes))
		for i, h := range hashes {
			items[i] = &PushItem{
				IType:		ITypeTxHash,
				content:	h,
				hash:		h,
			}
		}
		p.loop.sendItems(items)
	}
}

//stop the running loop and blocked until the peer is unregistered from the peerSet
func (p *peer) close() {
	p.kmutex.Lock()
	defer p.kmutex.Unlock()
	if p.status != PEER_CONNECTED {
	//	log.Info(fmt.Sprintf("%s already closed", p))
		return
	}
	p.status = PEER_CLOSED
	if p.loop != nil {
		//NOTE: when loop break, peer is unregister from peer set
	//	log.Info(fmt.Sprintf("%s is closing the loop", p))
		p.loop.close() 
	}
}

//start announcement loop, called after the peer is registered to peerlist
func (p *peer) startsendingloop (c *Controller, unregister func(*peer)) {
	p.kmutex.Lock()
	defer p.kmutex.Unlock()
	if p.loop == nil {
		p.loop = newpeersendingloop(p, c, unregister)
	}
}


