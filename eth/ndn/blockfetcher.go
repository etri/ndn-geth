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
	"fmt"
	"math/big"
	"sync"
	"time"
	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/ndn/ndnapp"
)

const (
	MAX_UNCLE_DIST = 7
	MAX_QUEUE_DIST = 256
	FetchedLifeTime = 30*time.Second
	BLK_FETCH_BUFFER_SIZE = 1024
)
type FetchingWork interface {
	addAnnouncer(*peer)	
	dropAnnouncer(*peer)
	identity()	common.Hash
}

type BlockAnnouncement struct {
	p			*peer //announcer
	headeritem	*BlockHeaderItem //announcement is a header item
	hashitem	*BlockHashItem //announcment is a hash item
}

func (ann *BlockAnnouncement) BlockInfo() (bhash common.Hash, bnum uint64, numsegments uint16) {
	if ann.headeritem != nil {
		bhash =  ann.headeritem.Header.Hash()
		bnum = ann.headeritem.Header.Number.Uint64()
		numsegments = ann.headeritem.NumSegments
	} else {
		bhash =  ann.hashitem.Hash
		bnum = ann.hashitem.Number
		numsegments = ann.hashitem.NumSegments
	}
	return
}
type fetchingList struct {
	mutex		sync.Mutex
	works		map[common.Hash]FetchingWork
}

func newFetchingList() *fetchingList {
	return &fetchingList{
		works: make(map[common.Hash]FetchingWork),
	}
}

func (fl *fetchingList) getwork(hash common.Hash) (FetchingWork, bool) {
	fl.mutex.Lock()
	defer fl.mutex.Unlock()
	w, ok := fl.works[hash]
	return w,ok
}

func (fl *fetchingList) removework(hash common.Hash) {
	fl.mutex.Lock()
	defer fl.mutex.Unlock()
	delete(fl.works, hash)
}

func (fl *fetchingList) addwork(w FetchingWork) {
	fl.mutex.Lock()
	defer fl.mutex.Unlock()
	fl.works[w.identity()] = w
}

func (fl *fetchingList) removepeer(p *peer) {
	fl.mutex.Lock()
	defer fl.mutex.Unlock()
	for _, w := range fl.works {
		w.dropAnnouncer(p)
	}
}

type fetchedList struct {
	mutex		sync.Mutex
	blocks		map[common.Hash]*FetchedBlock
	queue		*prque.Prque
}

func newFetchedList() *fetchedList {
	return &fetchedList {
		blocks:	make(map[common.Hash]*FetchedBlock),
		queue:	prque.New(nil),
	}
}

func (fl *fetchedList) push(b *FetchedBlock) bool {
	fl.mutex.Lock()
	defer fl.mutex.Unlock()
	if _,ok := fl.blocks[b.Block.Hash()]; ok {
		return false //
	}
	fl.blocks[b.Block.Hash()] = b
	fl.queue.Push(b, -int64(b.Block.NumberU64()))
	return true
}

func (fl *fetchedList) pop() *FetchedBlock {
	fl.mutex.Lock()
	defer fl.mutex.Unlock()
	if len(fl.blocks) == 0 {
		return nil
	}
	ret := fl.queue.PopItem().(*FetchedBlock)
	delete(fl.blocks, ret.Block.Hash())
	return ret
}

func (fl *fetchedList) empty() bool {
	fl.mutex.Lock()
	defer fl.mutex.Unlock()
	return len(fl.blocks) == 0
}

func (fl *fetchedList) get(hash common.Hash) (*FetchedBlock, bool) {
	fl.mutex.Lock()
	defer fl.mutex.Unlock()
	b, ok := fl.blocks[hash]
	return b, ok
}

type blockFetcher struct {
	fetching	*fetchingList //currently fetching works
	fetched		*fetchedList //done fetching works
	//fetcher		dataFetcher // interface for data object fetching
	fetcher		*EthObjectFetcher
	pool		utils.WorkerPool // a worker pool that do the fetching job
	kmutex		sync.Mutex //protect kill signal
	dead		bool
	quit		chan struct{}
	blockchain	*core.BlockChain
	controller	*Controller
	annCh		chan *BlockAnnouncement
	fetchedSig	chan struct{}
}


func newblockFetcher(c *Controller) *blockFetcher {
	//header verification function
	return &blockFetcher{
		fetching: 	newFetchingList(),	
		fetched:	newFetchedList(),	
		fetcher:	c.objfetcher,
		blockchain:	c.blockchain,
		pool:		c.fpool,
		controller:	c,
		quit:		make(chan struct{}),
		dead:		true,
		annCh:		make(chan *BlockAnnouncement,BLK_FETCH_BUFFER_SIZE),
		fetchedSig:	make(chan struct{},128),
	}
}

func (f *blockFetcher) isdead() bool {
	f.kmutex.Lock()
	defer f.kmutex.Unlock()
	return f.dead
}
func (f *blockFetcher) verifyHeader(header *types.Header) bool {
	return f.controller.engine.VerifyHeader(f.controller.blockchain, header, true) == nil
}
func (f *blockFetcher) start() {
	f.kmutex.Lock()
	defer f.kmutex.Unlock()
	if !f.dead {
		//not dead, already started
		return
	}
	go f.loop()
}

func (f *blockFetcher) stop() {
	f.kmutex.Lock()
	defer f.kmutex.Unlock()
	f.quit <- struct{}{}
}

//a peer is closed, remove it from all announcer list
func (f *blockFetcher) removepeer(p *peer) {
	f.fetching.removepeer(p)
}

//a block is pushed directly from a remote peer
func (f *blockFetcher) update(p *peer, block *FetchedBlock) {
	announcers := []*peer{p}
	if w, ok := f.fetching.getwork(block.Block.Hash()); ok {
		bw,_ := w.(*blockFetchingWork)
		announcers = append(bw.getAnnouncers(), p)
	}
	f.handleFetchedBlock(block, announcers)
}

func (f *blockFetcher) loop() {
	f.dead = false
	LOOP:
	for {
		//try to insert some fetched blocks
		//stop when we reach a future block
		//fetched blocks are sorted by blocknumber
		for !f.fetched.empty() {
			if f.insertnext() {
				break
			}
		}

		select {
		case <- f.quit:
			break LOOP
		case ann := <- f.annCh:
			f.processAnnouncement(ann)
		case <- f.fetchedSig://a block was fetched
		}
	}
	f.dead = true
}

//insert the next block to the local chain, assuming that the fetched list is
//not empty return true if we should stop advancing the list

func (f *blockFetcher) insertnext() bool {
	fblock := f.fetched.pop()
	if fblock == nil {
		return true
	}
	
	hash := fblock.Block.Hash()
	number := fblock.Block.NumberU64()
	height := f.blockchain.CurrentBlock().NumberU64()
	if number > height + 1 { //future block
		if time.Now().After(fblock.ftime.Add(FetchedLifeTime)) {
			//log.Info("Block was fetched for too long, remove")
			return false
		} else {
			//log.Info("Future block, push it back!")
			f.fetched.push(fblock)
			return true //must stop inserting now because remaining blocks are for future
		}
	}
	if number+MAX_UNCLE_DIST < height || f.blockchain.GetBlockByHash(hash) != nil {
		//log.Info("Too old or known block, ignore it")
		return false
	}
	propagating := false //should block be propagated before inserting?
	if fblock.pushed {
		//a pushed block, we need to propagate it (block/header) now
		//log.Info("a pushed block, push it to other peers now")
		propagating = true
	} else { //a fetched block indeeed
		if fblock.w.isheadersent() == false {
			//block header has not been propagated, we propagate the block or its header now
			propagating = true
			fblock.w.markheadersent()
		} else {
		//	log.Info("a fetched block with propagated header")
			//simple validation (may be not need?)
			if f.blockchain.GetBlockByHash(fblock.Block.ParentHash()) == nil {
				//log.Info("unknown parent block, ignore it!")
				return false
			}
		}
	}
	if propagating && f.verifyHeader(fblock.Block.Header()) {
		f.controller.propagateBlock(fblock.Block, false)
	}
	// now insert the block
	if  _, err := f.blockchain.InsertChain(types.Blocks{fblock.Block}); err == nil {
		//announce block hash to remaining peers (not knowning it)
		//log.Info("announce to the remaining ones")
		f.controller.announceBlock(fblock.Block)
	}
	return false
}

func (f *blockFetcher) onWorkDone(w *blockFetchingWork) {
	defer f.fetching.removework(w.hash)

	//do nothing if the fetcher was stopped
	if f.isdead() {
		return
	}

	if w.err ==nil  && w.fblock != nil {
		f.handleFetchedBlock(w.fblock, w.getAnnouncers())
	}
	//do nothing for a failed fetching work
}

func (f *blockFetcher) handleFetchedBlock(fblock *FetchedBlock, announcers []*peer) {

		f.controller.logBlock(fblock.Block, false)

		hash := fblock.Block.ParentHash()
		number := fblock.Block.NumberU64()
		td := new(big.Int).Sub(fblock.Td, fblock.Block.Difficulty())
		f.controller.SetPeersHead(announcers, hash, td,number-1)
		
		if dist := int64(number) - int64(f.blockchain.CurrentBlock().NumberU64()); dist < -MAX_UNCLE_DIST ||  dist > MAX_QUEUE_DIST {
			log.Info("Strange block, ignore it")
			return // strange block, do nothing
		}
		//add the done work to the list
		f.fetched.push(fblock)
		f.fetchedSig <- struct{}{}
}

//fetch the block, return true if the fetching job has been queued
func (f *blockFetcher) announce(ann *BlockAnnouncement) bool {
	if f.isdead() {
		return false
	}

	f.annCh <- ann

	return true
}

//process an announcement, return true if a fetching work is created.
func (f *blockFetcher) processAnnouncement(ann *BlockAnnouncement) bool {
	bhash, bnum, numsegments := ann.BlockInfo()
	p := ann.p
	//1. check if the block is being fetched
	if w, ok := f.fetching.getwork(bhash); ok {
		//log.Info(fmt.Sprintf("block %d is being fetched", bnum))
		
		//if the header is valid and we have not propagate it, let propagate it
		//add its holder to the holder list, don't add the announcer because he may not has the block yet

		bw, _ := w.(*blockFetchingWork)
		if ann.headeritem != nil { //a header announcement
			if bw.isheadersent() == false {
				//verify header first, if it is future header we will not propagate now, wait util the block is fetched.
				if f.verifyHeader(ann.headeritem.Header) {
					//log.Info("propagate header for a fetching block")
					f.controller.propagateHeader(ann.headeritem)
				}
				bw.setheader(ann.headeritem.Header, ann.headeritem.Td)
			}
			bw.addHolder(ann.headeritem.Holder)
			if _, ok:= bw.buildblock(); ok {//try to build the block (if body has been fetched)
				//TODO: should we handle the newly assembled block?
			}
		} else  { //a hash announcement
			bw.addAnnouncer(p)
		} 
		return false //no processing
	}

	//2. check if the block was already inserted
	if block := f.blockchain.GetBlock(bhash, bnum); block != nil {
		//log.Info(fmt.Sprintf("block %d was kwown",bnum))
		//if the block was inserted, set head for the peer 
		//the honest announcer either already has the block, or its verified header
		//thus the parent of this block must be on its chain.
		phash := block.ParentHash()
		if ltd := f.blockchain.GetTd(phash, bnum-1); ltd != nil {
			f.controller.SetPeersHead([]*peer{p}, phash, ltd, bnum-1)
		}
		//known block
		return false //no processing
	}

	//3. check if the block was fetched (but not inserted yet)
	if fblock,ok := f.fetched.get(bhash); ok {
		//similarly, the honest announcer either already has the block, or its verified header
		//thus the parent of this block must be on its chain.
		thash := fblock.Block.ParentHash()
		ttd := new(big.Int).Sub(fblock.Td, fblock.Block.Difficulty())
		f.controller.SetPeersHead([]*peer{p}, thash, ttd, fblock.Block.NumberU64()-1)
		//log.Info(fmt.Sprintf("%d block was already fetched, no more fetching", bnum))
		return false //no processing
	}


	//4. make sure the block is not too old or too far
	cblock := f.blockchain.CurrentBlock()

	if dist := int64(bnum) - int64(cblock.NumberU64()); dist < -MAX_UNCLE_DIST ||  dist > MAX_QUEUE_DIST {
		//log.Info(fmt.Sprintf("%d block is ignored", bnum))
		return false //strange block, just ignore its announcement
	}

	//5. a new announcement that need fetching of its block	

	
	//5.2 create work
	var route *PeerRouteInfo
	work := &blockFetchingWork {
			hash:			bhash,
			number:			bnum,
			numsegments:	numsegments,
			announcers: 	make(map[string]*announcingPeer),	
			holders:		make(map[string]bool),
		}
	
	if ann.hashitem != nil {
		work.announcers[p.id] = &announcingPeer{p:p, flag: true}
		route = &PeerRouteInfo{
			peername:	p.prefix,
			usehint:	true,
		}
	} else {
		work.holders[ann.headeritem.Holder] = true
		route = &PeerRouteInfo{
			peername:	ndn.ParseName(ann.headeritem.Holder),
			usehint:	true,
		}
		work.header = ann.headeritem.Header
		work.td	=ann.headeritem.Td
	}

	//5.2 start fetching job
	
	//create a callback handler for the fetching job
	fn := f.createHandler(work) 
	ret := work.fetch(f, route, fn)
	if ret {
		//mark the block as being fetched
		f.fetching.addwork(work)
	}
	//5.3 if the annoucement include a block header, we need to propagate it since it is the first header announcement for the block
	if ann.headeritem != nil {
		//verify header first, if it is future header we will not propagate now, wait util the block is fetched.
		if f.verifyHeader(ann.headeritem.Header) {
			f.controller.propagateHeader(ann.headeritem)
			work.markheadersent()
			//log.Info("proppagate header after starting a fetching job")
		}
	}

	return ret
}

//generated handler will be called by the worker that fetches body or header
func (f *blockFetcher) createHandler(work *blockFetchingWork) OnObjFetchedHandler {

	//create a callback handler for a fetching job
	//if the block was successfully fetched, we call onWorkDone
	//otherwise try with private prefix or a new peer
	//try until either the block is fetched or there is no more peer
	return func(job *objFetchingJob) {
		if job.err == ErrJobAborted {
			work.seterror(job.err)
			//do nothing, job was aborted
			f.onWorkDone(work)
			return
		}
		query, _ := job.query.(*EthBlockQuery)
		isheader := query.Part == BlockPartHeader 
		//peer to fetch from in the previous try
		if job.err == nil {
			var err error
			if isheader {
				var fheader FetchedHeader
				if err = rlp.DecodeBytes(job.obj, &fheader); err == nil {
					//log.Info(fmt.Sprintf("header for block %d is fetched", query.Number))
					if !work.isheadersent() { //propagate the header if it has not been sent
						if f.verifyHeader(fheader.Header) {
							//log.Info("propagate header for a fetching block")
							f.controller.propagateHeader(&BlockHeaderItem{
								Header:			fheader.Header,
								Td:				fheader.Td,
								NumSegments:	work.numsegments,
								Holder:			ndnapp.PrettyName(job.route.peername), //the peer from whom the header came

							})
							work.markheadersent()
						}
					}

					work.setheader(fheader.Header, fheader.Td)
								}
			} else {
				var body types.Body
				if err = rlp.DecodeBytes(job.obj, &body); err == nil {
					work.setbody(&body)
					//log.Info(fmt.Sprintf("body for block %d is fetched", query.Number))
				}
			}
			if err == nil {
				if _, ok := work.buildblock(); ok {
					//call onWorkDone if the block was just assembled
					f.onWorkDone(work)
				}
				return
			}
			if err != nil {//fail to decode the fetched part
				job.err = err
			}
		}

		log.Info(fmt.Sprintf("Fetch block %d failed: %s", query.Number, job.err.Error()))
		if !work.refetch(f, job, isheader) {
			f.onWorkDone(work)
		}
	}
}

type announcingPeer struct {
	p		*peer
	flag	bool //the peer has been queried?
}
type blockFetchingWork struct {
	hash		common.Hash //block hash
	number		uint64	//block number
	numsegments	uint16

	header		*types.Header
	body		*types.Body
	td			*big.Int
	fblock		*FetchedBlock //fetched block

	announcers	map[string]*announcingPeer //list of announcers, key = peer id
	holders		map[string]bool //list of block holders, key = peer ndn prefix
	headersent	bool

	err			error
	hfetch		*objFetchingJob //header fetching job
	bfetch		*objFetchingJob //body fetching job

	mutex		sync.Mutex
}

//get the next peer for fetching the block
func (w *blockFetchingWork) nextPrefix() (newname ndn.Name) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	for name, flag := range w.holders {
		if flag == false {
			newname = ndn.ParseName(name)
			w.holders[name] = true
			return
		}
	}
	for _, ap := range w.announcers {
		if ap.flag == false {
			newname = ap.p.prefix 
			ap.flag = true
			return
		}
	}
	return
}

func (w *blockFetchingWork) getAnnouncers() (peers []*peer) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
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
func (w *blockFetchingWork) identity() common.Hash {
	return w.hash
}

func (w *blockFetchingWork) addHolder(holder string) {
	if _, ok := w.holders[holder]; !ok {
		w.holders[holder] = false
	}
}

//announce that a peer has announced the same block hash, add it to the annoucer
//list
func (w *blockFetchingWork) addAnnouncer(p *peer) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if _, ok := w.announcers[p.id]; !ok {
		w.announcers[p.id] = &announcingPeer{p:p, flag: false}
	}
}

func (w *blockFetchingWork) dropAnnouncer(p *peer) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	delete(w.announcers, p.id)
}

func (w *blockFetchingWork) markheadersent() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.headersent = true
}
func (w *blockFetchingWork) isheadersent() bool {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.headersent
}

func (w *blockFetchingWork) setheader(header *types.Header, td *big.Int) {
	w.header = header
	w.td = td
	w.headersent = true
}

func (w *blockFetchingWork) setbody(body *types.Body) {
	w.body = body
}
func (w *blockFetchingWork) seterror(err error) {
	w.mutex.Lock()
	w.mutex.Unlock()
	w.err = err
}

//build the block from fetched parts, return the block and a boolean value indicating if the block is created in this function
func (w *blockFetchingWork) buildblock() (*FetchedBlock, bool) {
	w.mutex.Lock()
	w.mutex.Unlock()
	if w.fblock != nil {
		return w.fblock, false //block was assembled already
	}
	if w.header != nil && w.body != nil {
		block := types.NewBlockWithHeader(w.header)
		w.fblock = &FetchedBlock{
			Block: 	block.WithBody(w.body.Transactions, w.body.Uncles),
			Td:		w.td,
			w:		w,
			pushed:	false,
			ftime:	time.Now(),
		}
		//log.Info(fmt.Sprintf("block %d is built",w.number))
		return w.fblock, true //block is newly assembled
	}
	return nil, false //not assembled yet
}

//fetch block, return false if no job was created
func (w *blockFetchingWork) fetch(f *blockFetcher, route *PeerRouteInfo, fn OnObjFetchedHandler) (ret bool) {
	ret = false
	if w.header == nil { //create header fetching task
		query := &EthBlockQuery{
			Part:	BlockPartHeader,
			Hash:	w.hash,
			Number:	w.number,
		}
		w.hfetch = newObjFetchingJob(f.fetcher, query, 0, route, fn) 
		if ret = f.pool.Do(w.hfetch); !ret { //pool is dead
			return
		}
	}
	if w.bfetch == nil {
		query := &EthBlockQuery{
			Part:	BlockPartBody,
			Hash:	w.hash,
			Number:	w.number,
		}
		w.hfetch = newObjFetchingJob(f.fetcher, query, w.numsegments, route, fn) 
		ret = f.pool.Do(w.hfetch)
	}
	return
}

func (w *blockFetchingWork) refetch(f *blockFetcher, job *objFetchingJob, isheader bool) bool {
	peername := job.route.peername
	if job.route.usehint == false { //previous fetching was using private prefix, thus try with a new peer
		if peername = w.nextPrefix(); len(peername) <= 0  {
			//no more peer, nothing we can do
			w.seterror(ErrNoPeer)
			return false
		} 
	}

	//create new job and try again (always use private prefix)
	route := &PeerRouteInfo{
		peername:	peername,
		usehint:	false,
	}
	if isheader {
		w.hfetch = newObjFetchingJob(job.fetcher, job.query, 0, route, job.fn)
		return f.pool.Do(w.hfetch)
	} else {
		w.bfetch = newObjFetchingJob(job.fetcher, job.query, w.numsegments, route, job.fn)
		return f.pool.Do(w.bfetch)
	}
}
