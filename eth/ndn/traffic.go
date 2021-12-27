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
	"time"
	"os"
	"fmt"
	"sync"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/core"
)

type TrafficMeasure interface {
	ResetTraffic()
	Traffic() (uint64, uint64)
}

type ReportSlot struct {
	intraffic		uint64
	outtraffic		uint64
	time			uint64	
	npeers			int
	bnumber			uint64
	chainsize		uint64
	txcnt			uint64
}


type tmeasurer struct {
	blockchain	*core.BlockChain
	peers		*peerSet
	m			TrafficMeasure
	quit		chan bool
	closed		bool
	filename	string
}

func newtmeasurer(c *Controller) *tmeasurer {
	ret := &tmeasurer{
		blockchain: c.blockchain,
		peers:		c.peers,
		closed: 	false,
		m:			c.ndnsender().(TrafficMeasure),
		quit:		make(chan bool),
		filename:	c.ExpFileName,
	}
	go ret.loop(&c.wg)
	return ret
}


func (tm *tmeasurer) loop(wg *sync.WaitGroup) {
	defer wg.Done()
	wg.Add(1)

	log.Info("Start measuring loop")

	ticker := time.NewTicker(5*time.Second)


	dataslots :=[]ReportSlot{ ReportSlot{
		intraffic:		0,
		outtraffic:		0,
		time:	uint64(time.Now().Unix()),
	} }

	tm.m.ResetTraffic()

	LOOP:
	for {
		select {
		case <- ticker.C:
			//collect data
			i,o := tm.m.Traffic()
			dataslots = append(dataslots, ReportSlot{
				intraffic:		i,
				outtraffic:		o,
				npeers:			tm.peers.Len(),
				time:			uint64(time.Now().Unix()),
			})

		case <- tm.quit:
			tm.closed = true
			break LOOP
		}
	}

	i,o := tm.m.Traffic()
	dataslots = append(dataslots, ReportSlot{
				intraffic:		i,
				outtraffic:		o,
				npeers:			tm.peers.Len(),
				time:			uint64(time.Now().Unix()),
			})

	//dump data
	log.Info("Dumping data")
	f, _ := os.Create(tm.filename)
	defer f.Close()
	tblock := tm.blockchain.CurrentBlock()
	ctxcnt := uint64(0)
	cchainsize := uint64(0)
	for i:=len(dataslots)-1; i >=0 ; i-- {
		txcnt := 0
		chainsize := uint64(0)
		for ;tblock.Time() > dataslots[i].time; tblock = tm.blockchain.GetBlock(tblock.ParentHash(), tblock.NumberU64()-1) {
			txcnt += len(tblock.Transactions())
			chainsize += uint64(tblock.Size())
		}
		ctxcnt += uint64(txcnt)
		cchainsize += chainsize
		dataslots[i].txcnt = ctxcnt
		dataslots[i].chainsize = cchainsize
		dataslots[i].bnumber = tblock.NumberU64()
	}

	for i:=0; i<len(dataslots); i++ {
		dataslots[i].txcnt = ctxcnt-dataslots[i].txcnt
		dataslots[i].chainsize = cchainsize - dataslots[i].chainsize
		fmt.Fprintf(f,"%d\t%d\t%d\t%d\t%d\t%d\t%d\n",dataslots[i].bnumber, dataslots[i].time, dataslots[i].intraffic, dataslots[i].outtraffic, dataslots[i].npeers, dataslots[i].txcnt,dataslots[i].chainsize)
	}
	log.Info("done dumping")
}

func (tm *tmeasurer) stop() {
	if !tm.closed {
		close(tm.quit)
	}
}



