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
	"math/rand"
	"sync"
//	"time"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)


type Downloader struct {
	blockchain *core.BlockChain
	fetcher		*EthObjectFetcher
	w		*dwork //currently syncing work
	quit	chan bool 
	wg		sync.WaitGroup
	peers	map[string]*peer
	pmutex	sync.Mutex
	syncing	bool
	smutex	sync.Mutex
	drop	func (*peer)
}


func newDownloader(f *EthObjectFetcher, chain *core.BlockChain, drop func(*peer) ) *Downloader {
	d := &Downloader{
		fetcher: 	f,
		blockchain: chain,
		quit:		make(chan bool),
		peers:		make(map[string]*peer),
		syncing:	false,
		drop:		drop,
	}
	return d
}

//controller notify of new peer
func (d *Downloader) registerPeer(p *peer) {
	d.pmutex.Lock()
	defer d.pmutex.Unlock()
	d.peers[p.id] = p
}

//controller notify of lost peer
func (d *Downloader) unregisterPeer(id string) {
	d.pmutex.Lock()
	defer d.pmutex.Unlock()
	delete(d.peers, id)
}


//drop a bad peer
func (d *Downloader) dropPeer(p *peer) {
	//Controller will ask Downloader to unregister the peer
	d.drop(p)
}
//get a random peer for handling next fetching task
func (d *Downloader) workingPeer(num uint64) *peer {
	d.pmutex.Lock()
	defer d.pmutex.Unlock()

	peers := []*peer{}
	for _, p := range d.peers {
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

func (d *Downloader) setsync(syncing bool) {
	d.smutex.Lock()
	defer d.smutex.Unlock()
	d.syncing = syncing
}

func (d *Downloader) issyncing() bool {
	d.smutex.Lock()
	defer d.smutex.Unlock()
	return d.syncing
}
func (d *Downloader) synchronise(best *peer, hash common.Hash, num uint64, td *big.Int) error {
	d.wg.Add(1)
	d.setsync(true)
	defer d.wg.Done()
	defer d.setsync(false)

	var err error
	var head	*types.Header
	log.Info(fmt.Sprintf("Start synchronization with best peer %s", best.name))
	d.w = newdwork(best, d.fetcher)

	//1. reset all data structures
	//2. fetch master's peer head header
	head, err = d.fetchHeight(d.w, hash, num)
	if err != nil {
		d.dropPeer(best)
		//log.Info("failed to fetch master peer's header, kill downloading worker")
		//d.w.kill(err)
		return err
	}
	d.w.last = num //set the last block
	//3. find the common ancester
	err = d.findAncestor(d.w, head)
	if err != nil {
		d.w.kill(err)
		return err
	}


	//4. spawn sync: //fetch headers, fetch block bodies
	//the logic is as follows:
	//1. headers are fetch from the common ancestor toward the current head
	//2. as soon as a new header is fetched, its entire block will be fetched.
	//the peer to which the block is requested is randomly selected

	errCh := make([]chan error,3)
	for i:=0; i<3; i++ {
		errCh[i] = make(chan error, 1)
	}

	go func(ec chan error) {
		ec <- d.fetchHeaders(d.w)
		//log.Info("done fetching headers")
	}(errCh[0])

	go func(ec chan error) {

		ec <- d.fetchFullBlocks(d.w)
		//log.Info("done fetching bodies")
	}(errCh[1])

	go func(ec chan error) {
		ec <- d.processBlocks(d.w)
		//log.Info("done processing blocks")
	}(errCh[2])

	//if any error occurs, one of the goroutines will announce the error
	for _, ec := range errCh {
		e := <- ec
		if e != nil {
			err = e
		} else {
		}
	}
	// return the last error
	//log.Info("Synchronozation done!")
	return d.w.err
}

func (d *Downloader) terminate() {
	//log.Info("downloader received kill signal")
	close(d.quit)
	d.wg.Wait()
}

func (d *Downloader) onPeerQuit(p *peer) {
	d.unregisterPeer(p.id)
	if d.w != nil && d.w.master.id == p.id {
		d.w.kill(fmt.Errorf("Master peer is dead"))
	}
}


//get the last header of the peer
func (d *Downloader) fetchHeight(w *dwork, hash common.Hash, num uint64) (*types.Header, error) {
	log.Info("get header from master")
	header, err := w.getHeader(w.master, hash, num) 
	if err == nil && header.Hash() != hash {
		err = ErrInvalidHeader
	}
	//TODO: validate the header
	return header, err
}


func (d *Downloader) findAncestor(w *dwork, head *types.Header) error {
	w.ancestor = 0
	log.Info("find ancestor")
	localb := d.blockchain.CurrentBlock()
	localh := localb.NumberU64()	
	if localh == 0 { //genesis block
		return nil
	}

	startbnum := uint64(1)
	endbnum := localh
	var from uint64
	var skip, count uint16
	lastround := false
	var headers []*types.Header
	var err error
	for {
		total := endbnum - startbnum + 1
		if total <= MAX_HEADERS_COUNT {
			from = startbnum
			count = uint16(total)
			skip = 0
			lastround = true
		} else {
			skip = uint16(total/MAX_HEADERS_COUNT-1)
			count = MAX_HEADERS_COUNT
			from = endbnum - uint64(skip+1)*uint64(MAX_HEADERS_COUNT-1)
		}
		//log.Info(fmt.Sprintf("local:%d,start:%d,end:%d,from:%d,count:%d,skip:%d",localh,startbnum,endbnum,from,count,skip))	
		if headers, err = d.w.getHeaders(from, count, skip); err != nil {
			log.Info("failed to get a chunk of headers")
			return err
		}

		//TODO: check the order of returning headers (should be ascending order)
		var commonheader *types.Header
		for i := len(headers)-1; i>=0; i-- {
			if d.blockchain.HasHeader(headers[i].Hash(), headers[i].Number.Uint64()) {
				commonheader = headers[i]
				break
			}
		}
		if lastround {
			if commonheader == nil {
				return nil  //genesis block is the common ancestor
			} else {
				//log.Info(fmt.Sprintf("Ancestor found: %d", commonheader.Number.Uint64()))

				w.ancestor = commonheader.Number.Uint64()
				return nil
			}
		} else {
			if commonheader == nil { //next round will be the last
				endbnum = from - 1
			} else {
				startbnum = commonheader.Number.Uint64()
				if startbnum == endbnum {

					//log.Info(fmt.Sprintf("Ancestor found: %d", commonheader.Number.Uint64()))
					//set the ancestor block number
					w.ancestor = commonheader.Number.Uint64()
					return nil
				}

				if endbnum > startbnum + uint64(skip) {
					endbnum =  startbnum + uint64(skip)
				}
			}
		}
	}

}


