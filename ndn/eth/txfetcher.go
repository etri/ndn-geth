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
//	"math/big"
	"sync"
	"time"
//	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

const (
	TxFetchingLifeTime = 10*time.Second
	TX_FETCH_BUFFER_SIZE = 1024
	MAX_PUSH_TXSIZE = 256 //do not push tx larger than 256
)
type TxAnnouncement struct {
	p		*peer
	hash	common.Hash
}

type fetchedTxList struct {
	mux		sync.Mutex
	works	[]*txFetchingWork
	index	map[common.Hash]*txFetchingWork
}

func newFetchedTxList() *fetchedTxList {
	return &fetchedTxList{
		works: 	[]*txFetchingWork{},
		index:	make(map[common.Hash]*txFetchingWork),
	}
}

func (f *fetchedTxList) add(w *txFetchingWork) {
	defer f.mux.Unlock()
	f.mux.Lock()

	h := w.identity()
	if _,ok := f.index[h]; !ok {
		f.works = append(f.works, w)
		f.index[h] = w
	}
}

func (f *fetchedTxList) empty() bool {
	defer f.mux.Unlock()
	f.mux.Lock()

	return len(f.works) == 0
}

func (f *fetchedTxList) dump() []*txFetchingWork {
	defer f.mux.Unlock()
	f.mux.Lock()

	ret := f.works
	f.works = []*txFetchingWork{}
	f.index = make(map[common.Hash]*txFetchingWork)
	return ret
}

func (f *fetchedTxList) has(hash common.Hash) bool {
	defer f.mux.Unlock()
	f.mux.Lock()

	_, ok := f.index[hash]
	return ok
}

type txFetcher struct {
	fetching	*fetchingList //currently fetching works
	fetched		*fetchedTxList //done fetching works
	fetcher		*EthObjectFetcher // for data object fetching
	pool		utils.JobSubmitter // a worker pool that do the fetching job
	addtxsFn	func([]*types.Transaction) []error
	hastxFn		func(common.Hash) bool
	kmutex		sync.Mutex //protect kill signal
	dead		bool
	quit		chan bool
	wg			sync.WaitGroup
	annCh		chan *TxAnnouncement
	fetchedSig	chan struct{}
}


func newtxFetcher(c *Controller) *txFetcher {
	return &txFetcher{
		fetching: 	newFetchingList(),	
		fetched:	newFetchedTxList(),	
		fetcher:	c.objfetcher,
		addtxsFn:	c.txpool.AddRemotes,
		hastxFn:	c.txpool.Has,
		pool:		c.fpool,
		quit:		make(chan bool),
		fetchedSig:	make(chan struct{},1),
		annCh:		make(chan *TxAnnouncement,TX_FETCH_BUFFER_SIZE),
		dead:		true,
	}
}

func (f *txFetcher) start() {
	defer f.kmutex.Unlock()
	f.kmutex.Lock()
	if !f.dead {
		//not dead, already started
		return
	}
	f.dead = false
	go f.mainloop()
}

func (f *txFetcher) stop() {
	defer f.kmutex.Unlock()
	f.kmutex.Lock()

	close(f.quit)
	f.wg.Wait()
	f.dead = true
}

//a peer was closed, remove it from all announcer list
func (f *txFetcher) removepeer(p *peer) {
	f.fetching.removepeer(p)
}

func (f *txFetcher) mainloop() {
	defer f.wg.Done()
	f.wg.Add(1)
	go f.insertloop()

	for {
		select {
			case <- f.quit:
				return
			case ann := <- f.annCh:
				f.processAnnouncement(ann.p, ann.hash)
		}
	}
}

func (f *txFetcher) insertloop() {
	defer f.wg.Done()
	f.wg.Add(1)

	for {
		if !f.fetched.empty() {
			f.inserttxs()
		}
		select {
		case <- f.quit:
			return
		case <-f.fetchedSig:
		}
	}
}

func (f *txFetcher) inserttxs() {
	//get all the fetched txs then add to the transaction pool
	works := f.fetched.dump()
	var txs []*types.Transaction
	if len(works) ==0 {
		return
	}
	txs = make([]*types.Transaction, len(works))
	i := 0
	for _, w := range works {
		if w.err == nil && w.tx != nil {
			txs[i] = w.tx
			i++
		}
	}
	if i>0 {
		//log.Info(fmt.Sprintf("add %d txs to txpool", i))
		f.addtxsFn(txs[:i])
	}
}

//some txs were fetched externally, remove their currently fetching work
func (f *txFetcher) update(txs []*types.Transaction) {
	for _, tx := range txs {
		if w, ok := f.fetching.getwork(tx.Hash()); ok {
			w.(*txFetchingWork).setDone()
		}
	}
}


func (f *txFetcher) announce(ann *TxAnnouncement) bool {
	if f.isdead() {
		return false
	}
	f.annCh <- ann
	return true
}

func (f *txFetcher) processAnnouncement(p *peer, h common.Hash) {
	if w, ok := f.fetching.getwork(h); ok {
		w.addAnnouncer(p)
		return //tx is being fetched
	}
		
	//check if the tx was fetched (but not inserted yet)
	if f.fetched.has(h) {
		return
	}

	//check if the tx was already inserted to txpool
	if f.hastxFn(h) {
		return
	}

	//prepare a query to fetch the tx
	query := EthTxQuery(h)

	//create work
	work := newTxFetchingWork(p,h)
	//create a callback handler for the fetching job
	fn := f.createHandler(work) 
	//add fetching job
	route := &PeerRouteInfo{
		peername:	p.prefix,
		usehint:	true,
	}
	work.running = newObjFetchingJob(f.fetcher, &query, 0, route, fn) 
	//mark the tx as being fetched
	f.fetching.addwork(work)
	f.pool.Submit(work.running)
}

func (f *txFetcher) isdead() bool {
	defer f.kmutex.Unlock()
	f.kmutex.Lock()
	return f.dead
}
func (f *txFetcher) onWorkDone(w *txFetchingWork) {
	defer f.fetching.removework(w.hash)
	
	if f.isdead() {
		return
	}

	if w.err == nil  && w.tx != nil {
		//log.Info(fmt.Sprintf("Got %d", w.tx.Nonce()))
		w.ftime = time.Now()
		//add the done work to the list
		f.fetched.add(w)
		f.fetchedSig <- struct {}{}
	} else {
		if w.err != nil {
			log.Info(fmt.Sprintf("fail to get tx %s: %s", w.hash.String()[:8], w.err.Error()))
		}
	}
}

func (f *txFetcher) createHandler(work *txFetchingWork) OnObjFetchedHandler {

	//create a callback handler for a fetching job
	//if the tx was successfully fetched, we call onWorkDone
	//otherwise try with private prefix or a new peer
	//try until either the tx is fetched or there is no more peer
	return func(job *objFetchingJob) {
		done := work.isDone() 

		if !done && time.Now().After(work.created.Add(TxFetchingLifeTime)) {
			//fetching for too long, ignore the tx
			work.err = ErrFetchForTooLong
			//log.Info("fetching for too long")
			done = true
		}

		if !done {
			if job.err == ndnsuit.ErrNoObject || job.err == ErrJobAborted || job.err == ndnsuit.ErrNack {
				//tx is gone, may be it is no longer a pending one
				//log.Info("Tx is gone!!!")
				work.err = job.err
				done = true
			}
		}

		if done {
			//do nothing, job was aborted
			f.onWorkDone(work)
			return
		}
		
		//peer to fetch from in the next try
		peername := job.route.peername
		if job.err == nil {
			if work.err = rlp.DecodeBytes(job.obj, &work.tx); work.err == nil {
				//nothing wrong, process the fetched block
				f.onWorkDone(work)
				return
			}
		}

		log.Info(fmt.Sprintf("Fetch tx failed: %s", job.err.Error()))
		//there was an error, retry is needed
		if job.route.usehint == false { //previous fetching was using private prefix, thus try with a new peer
			if peername = work.nextPrefix(); len(peername) <= 0 {
				//no more peer, nothing we can do
				work.err = ErrNoPeer
				f.onWorkDone(work)
				return
			} 
		}

		
		//create new job and try again (always use private prefix)
		route := &PeerRouteInfo{
			peername:	peername,	
			usehint:	false,
		}

		work.running = newObjFetchingJob(job.fetcher, job.query, 0, route, job.fn)
		f.pool.Submit(work.running)
	}
}



type txFetchingWork struct {
	hash		common.Hash //block hash
	announcers	map[string]*announcingPeer
	tx			*types.Transaction //fetched transaction
	ftime		time.Time		//fetched time
	err			error
	running		*objFetchingJob // current fetching job
	done		bool	//tx was fetched by another source
	mutex		sync.Mutex
	created		time.Time
}

func newTxFetchingWork(p *peer, h common.Hash) *txFetchingWork {
	ret := &txFetchingWork{
		hash:	h,
		announcers: make(map[string]*announcingPeer),
		created:	time.Now(),
		done:	false,
	}
	ret.announcers[p.id] = &announcingPeer{p: p, flag: true}
	return ret
}
//get the next peer prefix for fetching the transaction
func (w *txFetchingWork) nextPrefix() (newname ndn.Name) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	for name, ap := range w.announcers {
		if ap.flag == false {
			newname = ndn.ParseName(name)
			ap.flag = true
			return
		}
	}
	return
}

func (w *txFetchingWork) getAnnouncers() (peers []*peer) {
	defer w.mutex.Unlock()
	w.mutex.Lock()
	if len(w.announcers) > 0 {
		peers = make([]*peer, len(w.announcers))
		i := 0
		for _, a := range w.announcers {
			peers[i] = a.p
			i++
		}
	}
	return

}
func (w *txFetchingWork) setDone() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.done = true
}
func (w *txFetchingWork) isDone() bool {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.done
}
//announce that a peer has announce the same transaction hash, add it to the annoucer
//list

func (w *txFetchingWork) addAnnouncer(p *peer) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if _, ok := w.announcers[p.id]; !ok {
		w.announcers[p.id] = &announcingPeer{p:p, flag:false}
	}
}

func (w *txFetchingWork) dropAnnouncer(p *peer) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	delete(w.announcers, p.id)
}

func (w *txFetchingWork) identity() common.Hash {
	return w.hash
}

