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


package kad

import (
	"github.com/ethereum/go-ethereum/ndn/utils"
)


type lookupCallbackHandler func([]*kadpeer, error) 

type lookupJob struct {
	algo	*kadlookupalgo
	target	ID
	k		int
	abort	chan bool
	fn		lookupCallbackHandler
}

func newlookupJob(algo *kadlookupalgo, target ID, k int, fn lookupCallbackHandler) *lookupJob {
	j := &lookupJob{
		algo:	algo,
		target:	target,
		k:		k,
		fn:		fn,
		abort:	make(chan bool),
	}
	return j
}


func (j *lookupJob) Execute() {
	peers, err := j.algo.lookup(j.target, j.k, j.abort)
	j.fn(peers, err)
}

func (j *lookupJob) Kill() {
	close(j.abort)
}

type lookupservice struct {
	algo		*kadlookupalgo
	pool		utils.WorkerPool
}

func newlookupservice(algo *kadlookupalgo, pool utils.WorkerPool) *lookupservice {
	return &lookupservice{algo: algo, pool: pool}
}


func (ls *lookupservice) lookup(target ID, k int, fn lookupCallbackHandler ) {
	job := newlookupJob(ls.algo, target, k, fn)	
	if !ls.pool.Do(job) {
		fn([]*kadpeer{}, ErrJobAborted)
	}
}


