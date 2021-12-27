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
	//"fmt"
	"io"
	"time"
	"math/big"

	"github.com/ethereum/go-ethereum/rlp"
//	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
//	"github.com/ethereum/go-ethereum/ndn/kad"
)

const (
	ITypeTx				= 1
	ITypeTxHash			= 2
	ITypeBlock			= 3
	ITypeBlockHeader	= 4
	ITypeBlockHash		= 5
)

type PushItem struct {
	IType		byte
	Wire		[]byte
	hash		common.Hash
	content		interface{}
}

func (item *PushItem) Size() int {
	return len(item.Wire)+1
}

func (item *PushItem) EncodeWire()  {
	item.Wire, _ = rlp.EncodeToBytes(item.content)
}

func (item *PushItem) ForBlock() bool {
	if item.IType == ITypeBlock || item.IType == ITypeBlockHash || item.IType == ITypeBlockHeader {
		return true
	}
	return false
}


func (item *PushItem) Hash() common.Hash {
	return item.hash
}

func (item *PushItem) EncodeRLP(w io.Writer) (err error) {
	if item.Wire, err = rlp.EncodeToBytes(item.content); err == nil {
		err = rlp.Encode(w, []interface{}{item.IType, item.Wire})
	}
	return
}

func (item *PushItem) DecodeRLP(s *rlp.Stream) (err error) {
	var obj struct {
		IType	byte
		Wire	[]byte
	}

	if err = s.Decode(&obj); err != nil {
		return
	} 

	switch obj.IType {
		case ITypeTx:
			var tx types.Transaction
			if err = rlp.DecodeBytes(obj.Wire, &tx); err == nil {
				item.content = &tx
				item.hash = tx.Hash()
			}
		case ITypeTxHash:
			var h common.Hash
			if err = rlp.DecodeBytes(obj.Wire, &h); err == nil {
				item.content = h 
				item.hash = h 
			}

		case ITypeBlock:
			var fblock FetchedBlock 
			if err = rlp.DecodeBytes(obj.Wire, &fblock); err == nil {
				item.content = &fblock 
				item.hash = fblock.Block.Hash() 
			}
		case ITypeBlockHeader:
			var headeritem BlockHeaderItem 
			if err = rlp.DecodeBytes(obj.Wire, &headeritem); err == nil {
				item.content = &headeritem
				item.hash = headeritem.Header.Hash() 
			}
		case ITypeBlockHash:
			var ann BlockHashItem
			if err = rlp.DecodeBytes(obj.Wire, &ann); err == nil {
				item.content = &ann 
				item.hash = ann.Hash 
			}
		default:
			err = ErrUnexpected
	}

	if err == nil  {
		item.IType = obj.IType
		item.Wire = obj.Wire
	}
	return
}

type  PushMsg struct {
	NodeId		string
	Items 		[]*PushItem	
	Nonce		uint64
	Sig			[]byte
}
//use Hmac for signing
func (msg *PushMsg) signedpart() []byte {
	wire, _ := rlp.EncodeToBytes([]interface{}{msg.NodeId, msg.Items, msg.Nonce})
	return wire
}

func (msg *PushMsg) Sign(crypto EthCrypto, key []byte) {
	wire := msg.signedpart()
	msg.Sig = crypto.SignHmac256(key, wire)
	return
}

func (msg *PushMsg) Verify(crypto EthCrypto, key []byte) bool {
	wire := msg.signedpart()
	return crypto.ValidateHmac256(key, wire, msg.Sig)
}

/*
//use Aes De/Encryption in data announcing
type  PushMsg struct {
	NodeId		string
	items 		[]*PushItem	
	nonce		uint64
	Wire		[]byte
}

func (msg *PushMsg) Encrypt(crypto EthCrypto, key []byte) (err error) {
	var wire []byte
	content := struct {
			Items	[]*PushItem
			Nonce	uint64
		}  {
			Items:	msg.items,
			Nonce:	msg.nonce,
		}
	if wire, err = rlp.EncodeToBytes(&content); err == nil {
		msg.Wire, err = crypto.Encrypt(key, wire[:])
	}

	if err != nil {
	//	log.Info(fmt.Sprintf("encrypt err: %s", err.Error()))
	}
	return
}

func (msg *PushMsg) Decrypt(crypto EthCrypto, key []byte) (err error) {
	var wire []byte
	if wire, err = crypto.Decrypt(key, msg.Wire[:]); err == nil {
		var content struct {
			Items	[]*PushItem
			Nonce	uint64
		} 
		if err = rlp.DecodeBytes(wire, &content); err == nil {
			msg.items = content.Items
			msg.nonce = content.Nonce
		}
	} 
	if err != nil {
	//	log.Info(fmt.Sprintf("decrypt err: %s", err.Error()))
	}
	return
}
*/

/*
//use public signature for data announcing
func (msg *PushMsg) signedpart() []byte {
	wire, _ := rlp.EncodeToBytes([]interface{}{msg.NodeId, msg.Items, msg.Nonce})
	return wire
}

func (msg *PushMsg) Sign(crypto EthCrypto) (err error) {
	wire := msg.signedpart()
	msg.Sig, err = crypto.Sign(wire)
	return
}

func (msg *PushMsg) Verify(crypto EthCrypto, pub []byte) bool {
	wire := msg.signedpart()
	return crypto.Verify(wire, msg.Sig, pub)
}
*/

//a fetched block
type FetchedBlock struct {
	Block	*types.Block
	Td		*big.Int  //chain difficulty at remote peer
	ftime	time.Time //fetched time
	w		*blockFetchingWork //nil if it is a pushed block
	pushed	bool //received by pushing?
}

type FetchedHeader struct {
	Header	*types.Header
	Td		*big.Int  //chain difficulty at remote peer
}

// block hash announcement
type BlockHashItem struct {
	Hash		common.Hash
	Number		uint64
	NumSegments	uint16
}

type BlockHeaderItem struct {
	Header		*types.Header
	Td			*big.Int
	Holder		string
	NumSegments	uint16 //number of segments in block body
}

