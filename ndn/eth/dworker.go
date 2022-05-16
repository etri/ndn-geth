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
//	"time"
	"sync"
	"math/rand"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	MAX_HEADERS_COUNT = 15
	NUM_DWORKERS 	  = 10
)

//dwork represents a downloading work
type dwork struct {
	master		*peer //the sync peer
	ancestor	uint64 //download from
	last		uint64 //down to	
	headerCh	chan *types.Header // buffering downloaded headers
	newblockCh	chan uint64 //buffering downloaded blocks 
	kmutex		sync.Mutex //
	finish		chan bool // chanel to signal task end
	dead		bool // flag indicating task dead
	fblocks		fBlocks //list of downloaded blocks
	err			error // last error
	bpool		utils.JobSubmitter //pool for downloading block
	ppool		utils.JobSubmitter //pool for downloading block parts
	fetcher		*EthObjectFetcher //interface for fetching a generic data object
}

// holds list of downloaded block
type fBlocks struct {
	blocks	map[uint64]*types.Block
	mutex	sync.Mutex
}

//put a fetched block to the list
func (fb *fBlocks) put(b *types.Block) {
	fb.mutex.Lock()
	defer fb.mutex.Unlock()
	if b == nil {
		return
	}
	if fb.blocks == nil {
		fb.blocks = make(map[uint64]*types.Block)
	}
	fb.blocks[b.NumberU64()] = b
}

//get fetched sequential blocks starting from bnum
func (fb *fBlocks) pop(bnum uint64) []*types.Block {
	fb.mutex.Lock()
	defer fb.mutex.Unlock()

	blocks := []*types.Block{}
	for {
		b, ok := fb.blocks[bnum]
		if !ok {
			break
		}
		blocks = append(blocks, b)
		delete(fb.blocks, bnum)
		bnum++
	}
	return blocks 
}

//create a new download work
func newdwork(best *peer, f *EthObjectFetcher) *dwork {
	ret := &dwork{
		master:		best,
		fetcher:	f,
		finish:		make(chan bool),
		newblockCh: make(chan uint64,64),
		dead:		false,
		bpool:  	utils.NewWorkerPool(NUM_DWORKERS),
		ppool:		utils.NewWorkerPool(10),
	}
	ret.headerCh = make(chan *types.Header) //the chanel buffer enough headers for all blockfetching-workers
	return ret
}

// get a batch of headers using the worker's worker pool
// headers are download directly from master peer using its private prefix
func (w *dwork) getHeaders(from uint64, count uint16, skip uint16) (headers []*types.Header, err error) {
	query := &EthHeadersQuery{
		From: from,
		Count:	count,
		Skip:	skip,
		Nonce:	rand.Uint64(),
	}
	done := make(chan bool)
	fn := func(job *objFetchingJob) {
		err = job.err
		if err == nil {
			err = rlp.DecodeBytes(job.obj, &headers)
		}
		close(done)
	}
	route := &PeerRouteInfo {
		peername:	w.master.prefix,
		usehint:	false,
	}

	job := newObjFetchingJob(w.fetcher, query, 0, route, fn)
	if w.ppool.Submit(job) {
		<- done
	} else { //pool was stoped
		err = ErrJobAborted
	}
	if err != nil {
		log.Trace(err.Error())
		//log.Error(err.Error())
	}
	return 
}

// get a single header from a specific peer
func (w *dwork) getHeader(p *peer, bhash common.Hash, bnum uint64) (header *types.Header,err error){
	query := &EthBlockQuery{
		Part: BlockPartHeader,
		Hash:	bhash,
		Number:	bnum,
	}
	done := make(chan bool)
	fn := func(job *objFetchingJob) {
		err = job.err
		if err == nil {
			var fh	FetchedHeader 
			if err = rlp.DecodeBytes(job.obj, &fh); err == nil {
				header = fh.Header
			}
		}
		close(done)
	}
	route := &PeerRouteInfo {
		peername:	p.prefix,
		usehint:	false,
	}

	job := newObjFetchingJob(w.fetcher, query, 0, route, fn)
	if w.ppool.Submit(job) {
		<- done
	} else { //pool was stoped
		err = ErrJobAborted
	}

	if err != nil {
		//log.Error(fmt.Sprintf("fail fetching %d - %s", len(job.obj), err.Error()))
		log.Trace(fmt.Sprintf("fail fetching %d - %s", len(job.obj), err.Error()))
	}
	return 
}

// A blocked call to get block from a specific peer
// Error returns immediately if the worker pool is stopped
// ispublic is a flag indicating whether public or private prefix will be used
// for requesting the data
func (w *dwork) getFullBlock1(p *peer, bhash common.Hash, bnum uint64, ispublic bool) (fullblock *types.Block, err error) {
	query := &EthBlockQuery{
		Part: BlockPartFull,
		Hash:	bhash,
		Number:	bnum,
	}
	done := make(chan bool)

	fn := func(job *objFetchingJob) {
		var fblock FetchedBlock
		err = job.err
		if err == nil {
			if err = rlp.DecodeBytes(job.obj, &fblock); err == nil {
				fullblock = fblock.Block
			}
		}
		close(done)
	}
	route := &PeerRouteInfo {
		peername:	p.prefix,
		usehint:	ispublic,
	}
	job := newObjFetchingJob(w.fetcher, query, 0, route, fn)
	if w.ppool.Submit(job) {
		<- done
	} else { //pool was stoped
		err = ErrJobAborted
	}

	if err != nil {
		log.Trace(err.Error())
		//log.Error(err.Error())
	}

	return 
}
func (w *dwork) getFullBlock(p *peer, header *types.Header, ispublic bool) (fullblock *types.Block, err error) {
	bhash := header.Hash()
	bnum := header.Number.Uint64()
	query := &EthBlockQuery{
		Part: BlockPartBody,
		Hash:	bhash,
		Number:	bnum,
	}
	done := make(chan bool)

	fn := func(job *objFetchingJob) {
		var body types.Body
		err = job.err
		if err == nil {
			if err = rlp.DecodeBytes(job.obj, &body); err == nil {
				fullblock = types.NewBlockWithHeader(header).WithBody(body.Transactions, body.Uncles)
			}
		}
		close(done)
	}
	route := &PeerRouteInfo {
		peername:	p.prefix,
		usehint:	ispublic,
	}
	job := newObjFetchingJob(w.fetcher, query, 0, route, fn)
	if w.ppool.Submit(job) {
		<- done
	} else { //pool was stoped
		err = ErrJobAborted
	}

	if err != nil {
		log.Trace(err.Error())
		//log.Error(err.Error())
	}

	return 
}


// kill the downloading work
// it is ok to call it multiple time
func (w *dwork) kill(err error) {
	w.kmutex.Lock()
	if w.dead {
		//already set to be killed, do nothing
		w.kmutex.Unlock()
		return
	}
	//set to be killed
	w.dead = true
	w.err = err
	w.kmutex.Unlock()

	//NOTE: kill the worker pools in a separate routine to be sure that this function is
	//not blocked so that its caller can exit. Otherwise deadlock can happen if
	//the caller is executing inside one of the workers
	go func(ww *dwork) {
		log.Trace("Stop chain sync job")

		//do the killing
		ww.ppool.Stop()
		log.Trace("ppool is closed")

		ww.bpool.Stop()
		log.Trace("bpool is closed")
		close(ww.finish)
	}(w)

}

// goroutine to download headers from the master peer
// it exits due to any following reasons: 1) worker pool has stopped, 2)
// failure to download a batch of headers; 3) all neccessary headers have been
// downloaded
func (d *Downloader) fetchHeaders(w *dwork) error {
	abort := false
	current := w.ancestor+1
	cnt := uint16(MAX_HEADERS_COUNT) //number of headers to retrieve at each request
	var err error
	var headers []*types.Header
	LOOP:
	for {
		if current + uint64(cnt) > w.last+1 {
			cnt = uint16(w.last-current + 1)
		}
		select {
		case <- d.quit:
			w.kill(ErrJobAborted)
			break LOOP

		default:
			if abort {
				//abort request from other goroutines
				break LOOP
			}

			if current > w.last {
				//fetch no more header
				//don't worry, all headers has been pushed to the block
				//fetcher for fetching
				break LOOP
			}

			//1. get next batch of headers from best peer
			if headers, err = w.getHeaders(current, cnt, 0); err != nil {
				log.Trace("Failed to get a batch of header from the master peer, sync must be aborted now")
				//log.Error("Failed to get a batch of header from the master peer, sync must be aborted now")
				w.kill(err)
				break LOOP
			}

			//2. verify the batch

			//3. add to queue
			HLOOP:
			for _, h := range headers {
				select {
				case <- w.finish: //task was canceled
					//log.Info("abort is requested")
					abort = true
					break HLOOP
				default:
					w.headerCh <- h
				}
			}
			current = current + uint64(len(headers))
		}
	}

	<- w.finish //make sure that someone ignite the ending
	close(w.headerCh) //close header channel so that the fetchFullBlocks goroutine knows to terminate
	//log.Info("finish fetching headers")
	return err
}

// gorouting to download blocks
// it receives downloaded headers from the fetchHeaders goroutine then download
// corresponding blocks.
// it exits the input header chanel is closed (signaled by the fetchHeaders
// goroutine)
// any detected failure will trigger a kill signal to the downloading work, as
// a consequence the worker pool will be stopped that in turn aborts all
// in-progressing fetching tasks.

func (d *Downloader) fetchFullBlocks(w *dwork) error {

	var err error
	onfetched := func(job *downloadBlockJob) {
		if job.err != nil {//failed to fetch a block, quit fetching
			//log.Error(fmt.Sprintf("fetching block %d failed", job.header.Number.Uint64()))
			log.Trace(fmt.Sprintf("fetching block %d failed", job.header.Number.Uint64()))
			w.kill(job.err)
			return
		}
		
		if job.block != nil {
	//		log.Info(fmt.Sprintf("ok, got the block %d", job.header.Number.Uint64()))
			//else, add block to fetched list
			w.fblocks.put(job.block)
			//trigger block processing
			w.newblockCh <- job.block.NumberU64() 
			//log.Info("done processing the block")
		} else {
			//log.Error("should never happens (dworker)")
			w.newblockCh <- 0 
		}
	}


	abort := false
	for h := range w.headerCh {
		if abort {
			//the worker pool was stopped, just ignoring all remaining headers
			continue
		}

		if !w.bpool.Submit(newdownloadBlockJob(d, w, h, onfetched)) {
			//worker pool is already stopped
			abort = true
		}
	}

	//close the newblockCh so that the processBlocks goroutine know to
	//terminate
	close(w.newblockCh)
	return err
}

// goroutine to insert downloaded block to current local chain
// it exit when the last block has been inserted; or any failure occurs

func (d *Downloader) processBlocks(w *dwork) error {
	var err error
	abort := false
	bnum := w.ancestor+1

	if bnum > w.last {
		d.w.kill(nil)
		return nil
	}

	for snum := range w.newblockCh {
		if abort {
			//log.Info("processing block job was aborted, ignore new block signal")
			continue
		}
		if snum != bnum {
			//not the number we are expecting
			log.Trace(fmt.Sprintf("waiting %d to process but got %d", bnum, snum))
			continue
		}
		blocks := w.fblocks.pop(bnum)
		if len(blocks) > 0 {
			log.Trace(fmt.Sprintf("try inserting %d blocks", len(blocks)))
			err = d.insertBlocks(blocks)
			if err != nil {
				//failed to insert blocks to the chain, need to abort
				//synchronization
				abort = true //do not process any anyother block
				w.kill(err) //signaling of job killing
				continue
			}
			if blocks[len(blocks)-1].NumberU64() == w.last {
				//log.Info("reach to the last block, completing sync now...")
				abort = true
				w.kill(nil) //signaling of job killing with no error
				continue
			}
			bnum += uint64(len(blocks))
		}
	}

	//TODO: rollback if it is neccessary in case of error
	return err
}

// make a new block from downloaded header and body
func makeBlock(h *types.Header, body *types.Body) *types.Block {
	
	return types.BlockAssemble(h, body)
}

// insert a list of block to the local chain
func (d *Downloader) insertBlocks(blocks []*types.Block) error {
	var err error
	if _, err = d.blockchain.InsertChain(blocks); err != nil {
		log.Trace(fmt.Sprintf("Failed to insert blocks", err.Error()))
		//log.Error(fmt.Sprintf("Failed to insert blocks", err.Error()))
	}
	return err
}

type peerManager interface {
	workingPeer(bnum uint64) *peer // get a random peer to download block body
	dropPeer(*peer) //drop a bad peer
}

type fullblockgetter interface {
	//getFullBlock(*peer, common.Hash, uint64, bool) (*types.Block, error)
	getFullBlock(*peer, *types.Header, bool) (*types.Block, error)
}

// a block downloading job.
// given a header, it tries to download its body from a random peer then build
// the block.
// it tries multiple times util the download succeeds or no peer is left
type downloadBlockJob struct {
	header		*types.Header
	abort		chan bool
	manager		peerManager
	getter		fullblockgetter
	block		*types.Block
	err			error
	//resCh		chan *downloadBlockJob
	fn			func(*downloadBlockJob)
}

// create a new block-downloading job
func newdownloadBlockJob(pm peerManager, bg fullblockgetter, h *types.Header, fn func(*downloadBlockJob)) *downloadBlockJob {
	return &downloadBlockJob {
		abort:		make(chan bool,1),
		header:		h,
		manager:	pm,
		getter:		bg,
		fn:			fn,
	}
}

func (j *downloadBlockJob) validate() bool {
	//TODO: validate the received block against the requesting header
	return true
}
// main logic of block downloading job
func (j *downloadBlockJob) Execute() {
	bnum := j.header.Number.Uint64()
	//hash := j.header.Hash()


	var p *peer
	ispublic := true
	numtries := 0
	LOOP:
	for {
		numtries++
		if numtries >2 {
			j.err = ErrTooManyTries
			break LOOP
		}

		log.Trace(fmt.Sprintf("fetching block %d attemps %d", bnum, numtries ))

		if ispublic { //need to get a new peer
			p = j.manager.workingPeer(bnum)
			if p == nil {
				j.err = ErrNoPeer
				break LOOP
			}
		}

		select {
		case <- j.abort:
			//log.Info(fmt.Sprintf("downloading job %d is requested to be terminated", bnum))
			j.err = ErrJobAborted
			break LOOP
		default:
			//if j.block, j.err = j.getter.getFullBlock(p, hash, bnum, ispublic); j.err == nil {
			if j.block, j.err = j.getter.getFullBlock(p, j.header, ispublic); j.err == nil {
				if j.validate() {
					// we've got the block that we need, time to quit
					break LOOP
				} else {
					//log.Info(fmt.Sprintf("Bad block 1: %d", bnum))
					//received body is not match to the header
					//poisioning attack?
					j.err = ErrInvalidBlock
					//we got a bad block in private fetching mode, the peer is
					//bad
					if !ispublic {
						//failed to fetch from the peer directly, need to drop
						//it
						j.manager.dropPeer(p)
					}
				}
			} 

			if j.err == ErrJobAborted {
				break LOOP
			}

			if ispublic {
				ispublic = false
			}
		}
	}

	log.Trace(fmt.Sprintf("fetching job for block %d is done!",bnum))
	j.fn(j)
}

//implementation fo the interface to abort the downloading job
func (j *downloadBlockJob) Kill() {
	close(j.abort)
}

