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
	"strconv"
	"encoding/hex"
	"math/bits"
	"math/rand"
	"crypto/ecdsa"
	"crypto/sha256"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

const (
	KADEMLIA_ALPHA = 3
	ADDRBYTES = 20
	ADDRBITS = ADDRBYTES * 8
	BUCKETSIZE = 10 //number of etries in a bucket
	REPSIZE = 5 //replacement size
	NUMBUCKETS = ADDRBITS/15
	BUCKETMINDISTANCE = ADDRBITS - NUMBUCKETS
)

var KadNameComponent = ndn.ParseNameComponent("p2p")
var AppName ndn.Name

type ID [ADDRBYTES]byte
type Value interface {}

func (id ID) String() string {
	return hex.EncodeToString(id[:])	
}

func IdFromPublicKey(k *ecdsa.PublicKey) (id ID) {
	kb := crypto.FromECDSAPub(k) 
	h := crypto.Keccak256(kb)
	copy(id[:],h[:])
	return id
}

func IdFromPrivateKey(prik *ecdsa.PrivateKey) (id ID) {
	id = IdFromPublicKey(&prik.PublicKey)
	return id
}


func IdFromHexString(id string) (ID, error) {
	b, err := hex.DecodeString(id)
	var ret ID
	if err==nil {
		copy(ret[:],b)	
	}
	return ret,err
}
func MakeID(pub []byte, prefix string) (id ID) {
	hash := crypto.Keccak256(pub, []byte(prefix))
	copy(id[:],hash[:])
	return
}
//0.75 times do self-lookup, 0.25 times do random lookup
func RandomId(seed ID) (id ID) {
	lottery := rand.Intn(100)
	if lottery <75 {
		id = seed
		return id
	}

	b := []byte(strconv.FormatUint(rand.Uint64(),10))
	b = append(b, seed[:]...)
	h := sha256.Sum256(b)
	copy(id[:], h[:ADDRBYTES])
	return id
}
//compare distance from a,b to a target by comparing byte vs byte from left to right
func DistCmp(target, a, b ID) int {
	for i:= range target {
		da := a[i] ^ target[i]
		db := b[i] ^ target[i]
		if da > db {
			return 1
		} else if da < db {
			return -1
		}
	}
	return 0
}


//log2(a^b) = length of the key minus the number of identical leading bits
func LogDist(a, b ID) int {
	lz :=0
	for i:= range a {
		x := a[i] ^ b[i] //xor byte
		if x == 0 { //identical byte
			lz += 8
		} else { //different -> count the identical bits
			lz += bits.LeadingZeros8(x)
			break
		}
	}
	return len(a)*8 - lz
}

func min(x,y int) int {
	if x<y {
		return x
	}
	return y
}
/*
func idcompare(id1, id2 ID) bool {
	for i:=0; i< ADDRBYTES; i++ {
		if id1[i] != id2[i] {
			return false
		}
	}
	return true
}
*/
func dumppeers(peers []*kadpeer) string {
	if peers == nil || len(peers) == 0 {
		return ""
	}
	s := "["
	for i,p := range peers {
		if i == len(peers)-1 {
			s = fmt.Sprintf("%s%s]", s, p.record.String())
		} else {
			s = fmt.Sprintf("%s%s,", s, p.record.String())
		}
	}
	return s
}
