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


package chainmonitor

import (
	"encoding/json"
	"time"
	"sync"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/common"
)

const (
	LATEST_BLOCKS_MAX = 20
	LATEST_TXS_MAX = 20
)

func min(x,y int) int {
	if x<y {
		return x
	}
	return y
}

type PeerInfo struct {
	Name 	string  `json:"name"`
	Id		string	`json:"id"`
}


type NodeInfo struct {
	StartedAt		time.Time
	Id				string
	Prefix			string
	Peers			[]PeerInfo
	TxPending		uint64
	NumBlocks		uint64
	Head			common.Hash
	Incoming		uint64
	Outgoing		uint64
	IsMining		bool
}

type BlockInfo	struct {
	Hash			common.Hash
	Number			uint64
	Coinbase		common.Address
	Size			common.StorageSize
	Created			uint64
	GasUsed			uint64
	NumTxs			int
}


type TxInfo map[string]interface{}

type LatestInfo struct {
	blocks 		[]BlockInfo
	txs			[]TxInfo
	mutex		sync.Mutex
	prevhash	common.Hash
	blockchain	*core.BlockChain
}

func newLatestInfo(chain *core.BlockChain) (l *LatestInfo) {
	l = &LatestInfo{
		prevhash:	common.Hash{},
		blockchain:	chain,
	}
	l.update(nil)
	return l
}

func (l *LatestInfo) reset(block *types.Block) {
	var info BlockInfo

	curblock := block
	if block == nil {
		curblock = l.blockchain.CurrentBlock()
	}
	if curblock == nil {
		return
	}

	l.prevhash = curblock.Hash() 
	blocks := []BlockInfo{}
	txs := []TxInfo{}

	for len(blocks) < LATEST_BLOCKS_MAX {
		if curblock == nil {
			break
		}

		info = BlockInfo{
			Hash:		curblock.Hash(),
			Number:		curblock.NumberU64(),
			Coinbase:	curblock.Coinbase(),
			Size:		curblock.Size(),
			Created:	curblock.Time(),
			GasUsed:	curblock.GasUsed(),
			NumTxs:		len(curblock.Transactions()),
		}
		blocks = append([]BlockInfo{info}, blocks...)
		curtxs := make([]TxInfo, len(curblock.Transactions()))
		for i, tx := range curblock.Transactions() {
			curtxs[i] = make(map[string]interface{})
			wire, _ := json.Marshal(tx)
			json.Unmarshal(wire, &curtxs[i])
			curtxs[i]["index"] = uint16(i)
			curtxs[i]["block"] = info.Number
		}
		if len(curtxs) >0 && len(txs)<LATEST_TXS_MAX {
			txs = append(curtxs, txs...)
		}
		curblock = l.blockchain.GetBlock(curblock.ParentHash(), curblock.NumberU64()-1)
	}

	l.blocks = blocks[:min(len(blocks), LATEST_BLOCKS_MAX)]
	l.txs = txs[:min(len(txs),LATEST_TXS_MAX)]
}

func (l *LatestInfo) update(block *types.Block) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if block == nil {
		//first update
		l.reset(nil)
		return
	}

	info := BlockInfo{
		Hash:		block.Hash(),
		Number:		block.NumberU64(),
		Coinbase:	block.Coinbase(),
		Size:		block.Size(),
		Created:	block.Time(),
		GasUsed:	block.GasUsed(),
		NumTxs:		len(block.Transactions()),
	}
	emptyHash := common.Hash{}
	if l.prevhash != emptyHash && l.prevhash != info.Hash {
		//chain was reorganized
		//we just simple discard all previous accumulated information
		l.reset(block)
		return
	}

	//add new block to the latest list
	bl := len(l.blocks)
	if bl < LATEST_BLOCKS_MAX {
		l.blocks = append(l.blocks, info)
	} else {
		copy(l.blocks,l.blocks[1:])
		l.blocks = append(l.blocks, info)
	}

	//add new transactions to the latest list
	txs := make([]TxInfo, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		txs[i] = make(map[string]interface{})
		wire, _ := json.Marshal(tx)
		json.Unmarshal(wire, &txs[i])
		txs[i]["index"] = 	uint16(i)
		txs[i]["block"] =	info.Number
	}
	newlen := len(txs) 
	existinglen := len(l.txs)
	dellen := newlen+existinglen - LATEST_TXS_MAX //num txs to be deleted
	if newlen >0 {
		if dellen <= 0 {
			l.txs = append(l.txs,txs...)
		} else if dellen >= existinglen { //delete all
			end := min(newlen, LATEST_TXS_MAX)
			l.txs = txs[:end]
	 	} else { //delete few
			k := copy(l.txs, l.txs[dellen:])	
			l.txs = append(l.txs[:k], txs...)
		}
	}
	l.prevhash = info.Hash
}

func (l *LatestInfo) getBlocks() []BlockInfo {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.blocks
}

func (l *LatestInfo) getTxs() []TxInfo {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.txs
}
