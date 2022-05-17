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
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/ndn/chainmonitor"
)

func (c *Controller) Blockchain() *core.BlockChain {
	return c.blockchain
}

func (c *Controller) GetNodeInfo() (info chainmonitor.NodeInfo) {
	if c.server == nil {
		return
	}
	info.TxPending = 0
	pendings,_ := c.txpool.Pending()
	for _,txs := range pendings {
		info.TxPending += uint64(len(txs))
	}

	info.StartedAt = c.startedat
	info.Id = c.server.Identity() 
	info.Prefix	= c.server.Address() 
	info.Peers = c.peers.Info()
	block := c.blockchain.CurrentBlock()
	info.NumBlocks = block.NumberU64()
	info.Head = block.Hash()
	info.Outgoing, info.Incoming = c.getTraffic()
	info.IsMining = c.CheckMining()
	
	return
}

func (c *Controller) getTraffic() (uint64, uint64) {
	traffin := uint64(c.traffin/1024)
	traffout := uint64(c.traffout/1024)
	return traffout, traffin
}

func (ps *peerSet) Info() (peers []chainmonitor.PeerInfo) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	peers = []chainmonitor.PeerInfo{}
	if len(ps.peers) > 0 {
		peers = make([]chainmonitor.PeerInfo, len(ps.peers))
		i := 0
		for _, p := range ps.peers {
			peers[i] = chainmonitor.PeerInfo{
				Id:		p.id,
				Name:	p.name,
			}
			i++
		}
	}
	return
}
