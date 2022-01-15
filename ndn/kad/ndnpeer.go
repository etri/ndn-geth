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
	"errors"
	"fmt"
	"crypto/ecdsa"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/ethereum/go-ethereum/ndn/kad/rpc"
	"github.com/ethereum/go-ethereum/crypto"
//	"github.com/ethereum/go-ethereum/log"
)


type ndnNodeRecord struct {
	id 			ID
	prefix		ndn.Name
	pubkey		[]byte
}



func NdnNodeRecordFromPrivateKey(hostname ndn.Name, key *ecdsa.PrivateKey) (NodeRecord, error) {
	rec := &ndnNodeRecord {
		prefix:		hostname,
		pubkey:		crypto.FromECDSAPub(&key.PublicKey),
	}
	if len(rec.prefix) == 0 {
		return rec, fmt.Errorf("Invalid NDN name")
	}
	rec.makeId()
	return rec, nil
}

func NdnNodeRecordUnmarshaling(info rpc.NdnNodeRecordInfo) (NodeRecord, error) {
	var rec ndnNodeRecord
	var err error
	rec.prefix = ndn.ParseName(info.Prefix)
	rec.pubkey = make([]byte,len(info.Pub))
	copy(rec.pubkey, info.Pub)
	rec.makeId()
	return &rec, err
}

func NdnNodeRecordMarshaling(rec NodeRecord) (info rpc.NdnNodeRecordInfo, err error) {
	if ndnrec, ok := rec.(*ndnNodeRecord); !ok {
		err = errors.New("not a NDN-based node record")
	} else {
		info.Prefix = ndnsuit.PrettyName(ndnrec.prefix)
		info.Pub = make([]byte,len(ndnrec.pubkey))
		copy(info.Pub, ndnrec.pubkey)
	}
	return
}

//a bootnode record
func CreateUntrustedNdnNodeRecord(id string, prefix string) (NodeRecord, error) {
	var err error
	r := &ndnNodeRecord{}
	r.id, err = IdFromHexString(id)
	if err != nil {
		return r, err
	}
	r.prefix = ndn.ParseName(prefix)
	if len(r.prefix) == 0 {
		return r, fmt.Errorf("Invalid NDN name")
	}
	return r, nil
}

func (r *ndnNodeRecord) Prefix() ndn.Name {
	return r.prefix
}


func (r *ndnNodeRecord) makeId() {
	r.id = MakeID(r.pubkey, ndnsuit.PrettyName(r.prefix))
}

func (r *ndnNodeRecord) Identity() ID {
	return r.id 
}

func (r *ndnNodeRecord) String() string {
	return ndnsuit.PrettyName(r.prefix)
}

func (r *ndnNodeRecord) Address() string {
	return ndnsuit.PrettyName(r.prefix)
}

func (r *ndnNodeRecord) untrusted() bool {
	return len(r.pubkey)==0 
}

func (r *ndnNodeRecord) PublicKey() []byte {
	return r.pubkey
}

func (p *kadpeer) Prefix() ndn.Name {
	return p.record.(*ndnNodeRecord).Prefix()
}

func (p *kadpeer) UpdateNodeRecord(rec rpc.NdnNodeRecordInfo) (err error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	//update record for peer 
	var newrec NodeRecord
	if newrec, err = NdnNodeRecordUnmarshaling(rec); err == nil {
		if newrec.Identity() != p.record.Identity() {
			err = ErrInvalidIdentity
		} else if p.record.(*ndnNodeRecord).untrusted() { //update only if the node record has no public key
			p.record = newrec
		}
	}
	return
}

//called whenever the rpc server learn of a new peer
func (node *kadnode) AddSeenRecord(info rpc.NdnNodeRecordInfo) {
	if node.isDead() {
		return
	}

	node.smutex.Lock()
	defer node.smutex.Unlock()
	rec,_ := NdnNodeRecordUnmarshaling(info)
	node.seenlist = append(node.seenlist, rec)
	if !node.seensig {
		node.seensig = true
		node.seench <- struct{}{}
	}
}


//get k-nearest peers from the routing table - backend for Findnode RPC
func (node *kadnode) Find(target string, k uint8) (peerlist []rpc.NdnNodeRecordInfo) {
	if node.isDead() {
		return
	}

	if id,err := IdFromHexString(target); err == nil {
		peers := node.rtable.closest(id, int(k)) 
		peerlist = make([]rpc.NdnNodeRecordInfo, len(peers))	
		for i, p := range peers {
			peerlist[i],_ = NdnNodeRecordMarshaling(p.record)
		}
	}
	return
}

func (node *kadnode) Record() rpc.NdnNodeRecordInfo {
	info,_ := NdnNodeRecordMarshaling(node.self.Record())
	return info
}
