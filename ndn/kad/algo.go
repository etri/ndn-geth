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
//	"fmt"
//	"github.com/ethereum/go-ethereum/log"
)
const (
	MAX_CHECKLIVE_ROUNDS=3 //stop after 3 rounds of lookup result consolidation
)

type findnoderesult struct {
	queried	*PqPeer
	peers 	[]*kadpeer
	err		error
}

type findnodeCallbackHandler func([]*kadpeer, error)

//type kadquerier interface {
//	asyncfindnode(*kadpeer, ID, int, findnodeCallbackHandler)
//}

type kadlookupalgo struct {
	closest func(ID, int) []*kadpeer
	find func(*kadpeer, ID, int, findnodeCallbackHandler)
}


func newkadlookupalgo(closest func(ID, int) []*kadpeer, find func(*kadpeer, ID, int, findnodeCallbackHandler)) *kadlookupalgo {
	return &kadlookupalgo{closest: closest, find: find}
}

//the kademlia lookup algorithm
func (a *kadlookupalgo) lookup(target ID, k int, abort <- chan bool) (peers []*kadpeer, err error) {
	//1. get k neighbors from the routing table
	solution := a.closest(target, k) //first solution
	if len(solution) == 0 {
		//unexpected
		return []*kadpeer{}, ErrNoPeer 
	}
	//2. add them to the neighbor buffer
	sortedpeers :=  newpeersort(target, solution) //neighbors buffer

	//pop closest peers for querying
	queriedpeers := sortedpeers.closest(KADEMLIA_ALPHA)

	var newsolution []*kadpeer
	newlookupround := make(chan bool, 1)
	newcheckliveround := make(chan bool, 1)

	newlookupround <- true //start lookup
	lookuproundnum := 1
	checkliverounds := 1
	LOOP:
	for {	

		select {
		case <- abort:
			err = ErrJobAborted
			peers = []*kadpeer{}
			break LOOP

		case <- newlookupround:
			//3. get alpha neighbors for querying
			if len(queriedpeers) == 0 {
				//nothing to query, just return
				break LOOP
			}
//			log.Info(fmt.Sprintf("current solution %s", dumppeers(solution)))
			resultCh := make(chan findnoderesult,len(queriedpeers))

			for _, qp := range queriedpeers {
				if !qp.recentlyqueried() {
					a.find(qp.p, target, k, func(peers []*kadpeer, err error) {
						resultCh <- findnoderesult{queried: qp, peers: peers, err: err}	
					})
				//	log.Info(fmt.Sprintf("%s is queried", qp.p.node.String()))
				} else {
				//	log.Info(fmt.Sprintf("%s is recently queried %d", qp.p.node.String(), lookuproundnum))
					resultCh <- findnoderesult{queried: qp, peers: []*kadpeer{}, err: nil}
				}
			}

			for i:=0; i< len(queriedpeers); i++ {
				select {
				case result := <- resultCh:
					if result.err != nil || len(result.peers) == 0 {
						//do nothing as the peer may already be marked as being
					//dead or it is recently queried
					} else { //the queried peer has replied
						sortedpeers.repush(result.queried)
						result.queried.mark()
						sortedpeers.update(result.peers)
					}
				case <- abort:
					err = ErrJobAborted
					peers = []*kadpeer{}
					break LOOP
				}
			}

			//5. extract the new solution
			// make new querying list
			closest := sortedpeers.closest(k)
			newsolution = []*kadpeer{}
			queriedpeers = []*PqPeer{}
			for i, qp := range closest {
				newsolution = append(newsolution, qp.p)
				if i<KADEMLIA_ALPHA {
					queriedpeers = append(queriedpeers,qp)
				} else {
					sortedpeers.repush(qp)
				}
			}
			

			if compare(solution, newsolution) {
				//log.Info(fmt.Sprintf("solution found on %d: %s",
				//lookuproundnum, dumppeers(newsolution)))
				solution = newsolution
				newcheckliveround <- true
			} else {
				//log.Info(fmt.Sprintf("solution %d: %s", lookuproundnum,
				//dumppeers(newsolution)))
				solution = newsolution
				lookuproundnum++
				newlookupround <- true
			}
		case <- newcheckliveround:
			//TODO: consolidate the solution
			//check if all peers in the solution is live
			//we should stop consolidation after a few rounds
			if checkliverounds > MAX_CHECKLIVE_ROUNDS {
				break LOOP
			}

			//TODO: ping all peers in the solution
			//TODO: wait for result and check if all reply
			all_reply := true
			//if all reply, we are good
			if all_reply {
				peers = newsolution
				break LOOP
			} else {
				checkliverounds++
				newcheckliveround <- true
			}
		}
	}
	return
}

//check if two peer lists are identical
func compare(l1 []*kadpeer, l2 []*kadpeer) bool {
//	log.Info(fmt.Sprintf("Comparing %s - %s\n", dumppeers(l1), dumppeers(l2)))

	if len(l1) != len(l2) {
		return false
	}
	for i, p := range l1 {
		if p.id != l2[i].id {
			return false
		}
	}
	return true
}
