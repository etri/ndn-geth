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
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

const (
	MONITOR_PREFIX = "ethrpc"
)


type Backend interface {
	Blockchain() *core.BlockChain
	GetNodeInfo() NodeInfo
}

type Monitor struct {
	backend			Backend
	rpchandler		*rpcHandler
	latest			*LatestInfo
	quit			chan struct{}
	blockch			chan *types.Block
}


func NewMonitor(b Backend) *Monitor {
	m := &Monitor{
		backend:	b,
		latest:		newLatestInfo(b.Blockchain()),
		blockch:	make(chan *types.Block, 1),
		quit:		make(chan struct{}),
	}
	m.rpchandler = newRpcHandler(m)
	go m.loop()
	return m
}

func (m *Monitor) Close() {
	m.quit <- struct{}{}
}

func (m *Monitor) Update(b *types.Block) {
	m.blockch <- b
}

func (m *Monitor) loop() {
	LOOP:
	for {
		select {
		case <- m.quit:
			break LOOP
		case b := <- m.blockch:
			m.latest.update(b)
		}
	}
}

func (m *Monitor) MakeProducer(appname ndn.Name) ndnsuit.Producer {
	prefix := ndnsuit.BuildName(appname, []ndn.NameComponent{ndn.ParseNameComponent(MONITOR_PREFIX)})

	rpcdecoder := rpcRequestDecoder{}
	return ndnsuit.NewProducer(prefix, rpcdecoder, nil, nil, m.rpchandler)
}

func (m *Monitor) getBlock(num uint64) *types.Block {
	return m.backend.Blockchain().GetBlockByNumber(num)
}

func (m *Monitor) getNodeInfo() (info NodeInfo) {
	return m.backend.GetNodeInfo()
}

