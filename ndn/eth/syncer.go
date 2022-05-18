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
	"fmt"
	"math/big"
	"time"
	"sync"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/core"
)

const (
	CHAINSYNC_INTERVAL = 60*time.Second
)

type chainsyncer struct {
	quit	chan struct {}
	kmutex	sync.Mutex
	wg		sync.WaitGroup
	dead	bool
	doneCh	chan error
	peerEventCh		chan struct{}
	peerQuitCh		chan *peer
	peers	*peerSet
	chain	*core.BlockChain
	downloader	*Downloader
	oncompleted		func(error) //sync completion callback function
	onstarted		func() //sync completion callback function
}

//sync operation information
type syncop struct {
	p 		*peer //master peer
	num		uint64 //blocknumber
	head	common.Hash //chain head
	td		*big.Int //chain difficulty
}

func newchainsyncer(c *Controller, ons func(), onc func(error)) *chainsyncer {
	drop := func(p *peer) {
		c.dropPeer(p, true)
	}
	return  &chainsyncer{
		peerEventCh: make(chan struct{},1),
		peerQuitCh: make(chan *peer, 1),
		quit:	make(chan struct{}),
		chain:	c.blockchain,
		peers: c.peers,
		onstarted: ons,
		oncompleted: onc,
		downloader: newDownloader(c.objfetcher, c.blockchain, drop),
		dead:	true,
	}
}

func (cs *chainsyncer) start() {
	cs.kmutex.Lock()
	defer cs.kmutex.Unlock()
	if cs.dead {
		cs.dead = false
		go cs.loop()
	}
}

func (cs *chainsyncer) stop() {
	cs.kmutex.Lock()
	defer cs.kmutex.Unlock()
	if !cs.dead {
		cs.quit <- struct{}{}
		cs.dead = true
		cs.wg.Wait()
	}
}

func (cs *chainsyncer) isdownloading() bool {
	return cs.downloader.issyncing()
}

func (cs *chainsyncer) addpeer(p *peer) {
	cs.kmutex.Lock()
	defer cs.kmutex.Unlock()

	if !cs.dead {
		cs.downloader.registerPeer(p)
		cs.peerEventCh <- struct{}{}
	}
}
func (cs *chainsyncer) peerevent() {
	cs.kmutex.Lock()
	defer cs.kmutex.Unlock()
	if !cs.dead {
		cs.peerEventCh <- struct{}{}
	}
}

//a peer is dropped, check if it is the master peer of a currently running sync
func (cs *chainsyncer) peerDropped(p *peer) {
	cs.downloader.onPeerQuit(p)
}

func (cs *chainsyncer) loop() {
	cs.wg.Add(1)
	defer cs.wg.Done()

	forced := false
	killed := false
	synctimer := time.NewTimer(CHAINSYNC_INTERVAL)
	for {
		if op := cs.nextop(forced); op != nil {
			cs.startsync(op)
		}

		select {
		case <- cs.peerEventCh:
		case p := <- cs.peerQuitCh:
			if cs.doneCh != nil {
				cs.downloader.onPeerQuit(p)
			}

		case <- cs.doneCh:
			if killed {
				return
			}
			log.Info("Sync is completed")
			cs.doneCh = nil
			synctimer.Reset(CHAINSYNC_INTERVAL)
			forced = false
		case <- synctimer.C:
			log.Trace("Forced sync")
			synctimer.Reset(CHAINSYNC_INTERVAL)
			forced = true
		case <- cs.quit:
			//stop chain insert
			cs.chain.StopInsert()
			//terminate downloader
		//	log.Info("syncer received kill signal")
			cs.downloader.terminate()
			if cs.doneCh != nil { //there a sync running
				killed = true //set killed then wait for done signal to quit
			} else {
		//		log.Info("No syncing, just quit")
				return
			}
		}
	}
}

func (cs *chainsyncer) nextop(forced bool) *syncop {
	if cs.doneCh != nil {
		log.Trace("syncing in-progress")
		return nil
	}
	log.Trace("Get next sync operation")
	required := 1 //need only one peer for starting if sync is forced by timer
	if !forced {
		required = 3
	}
	if cs.peers.Len() < required {
		log.Trace("not enough peer for sync")
		return nil
	}

	best := cs.peers.bestPeer()
	if best == nil {
		log.Trace("No best peer to sync")
		return nil
	}
	head := cs.chain.CurrentHeader()
	td := cs.chain.GetTd(head.Hash(), head.Number.Uint64())
	phead, pnum, ptd := best.Head()
	if ptd.Cmp(td) <= 0 {
		log.Trace(fmt.Sprintf("I am already synced %d:%d at %s", head.Number.Uint64(), pnum, best.name))
		return nil
	}

	return &syncop{
		p:	best,
		head:	phead,
		num:	pnum,
		td:		ptd,
	}
}

func  (cs *chainsyncer) startsync(op *syncop) {
	cs.doneCh = make(chan error, 1)
	go func() {
		cs.doneCh <- cs.dosync(op)
	}()
}


func (cs *chainsyncer) dosync(op *syncop) error {
	if cs.onstarted != nil {
		cs.onstarted()
	}
	err := cs.downloader.synchronise(op.p, op.head, op.num, op.td)
	if cs.oncompleted != nil {
		cs.oncompleted(err)
	}
	return err
}
