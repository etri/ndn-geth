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
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

type OnObjFetchedHandler func(*objFetchingJob)

type ObjManager struct {
	txpool		txPool	
	blockchain	*core.BlockChain
	cacher		*ObjCacheManager
}

func newObjManager(c *Controller) ObjManager {
	return ObjManager{
		txpool:		c.txpool,
		blockchain:	c.blockchain,
		cacher:		c.cacher,
	}
}

func (m ObjManager) GetSegment(query ndnsuit.Query, segId uint16) (msg ndnsuit.ResponseWithDataSegment, err error) {
	var segment []byte //segment
	var num uint16 //number of segments
	ethquery,ok := query.(ndnsuit.HasId)
	if !ok {
		err = ErrUnexpected
		return
	}
	queryid := ethquery.GetId()
	if segId > 0 {
		segment, num, err = m.cacher.getSegment(queryid, segId)
		if err == nil {
			//log.Info(fmt.Sprintf("A segment %d for %s from cache", segId, queryid[:8]))
			msg = &EthMsg{
				MsgType:	TtDataSegment,
				Content:	ndnsuit.NewSimpleSegment(segment, segId, num),
			}
		}
		return
	}
	//get the first segment, need query for the object
	var obj interface{}
	switch query.(type) {
		case *EthHeadersQuery: 
			obj, err = m.queryHeaders(query.(*EthHeadersQuery))
		case *EthTxQuery:
			obj, err = m.queryTx(query.(*EthTxQuery))
		case *EthBlockQuery:
			var block *types.Block
			bquery,_ := query.(*EthBlockQuery)
			if block, err = m.queryBlock(bquery); err != nil {
				break
			}
			switch bquery.Part {
				case BlockPartHeader:
					if td := m.blockchain.GetTd(block.Hash(), block.NumberU64()); td != nil {
						obj = &FetchedHeader{ Header: block.Header(), Td: td}
					} else {
						err = ErrUnexpected
					}

				case BlockPartBody:
					obj = block.Body()
				case BlockPartFull:
					if td := m.blockchain.GetTd(block.Hash(), block.NumberU64()); td != nil {
						obj = &FetchedBlock{ Block: block, Td: td}
					} else {
						err = ErrUnexpected
					}
	
				default:
					err = ErrUnexpected
			}
		default:
			err = ErrUnexpected
	}

	if err != nil {

		//log.Error(err.Error())
		log.Trace(err.Error())
		return
	}


	wire, _ := rlp.EncodeToBytes(obj)

	segment, num = m.cacher.cache(queryid, wire)
		
	msg = &EthMsg {
		MsgType:	TtDataSegment,
		Content:	ndnsuit.NewSimpleSegment(segment, segId, num),
	}
	return
}

func (m ObjManager) queryHeaders(query *EthHeadersQuery) ([]*types.Header, error) {
	log.Trace("handle headers request")
	if query.Count < 1 {
		return nil, ErrInvalidRequest 
	}
	headers := make([]*types.Header, query.Count)
	var bnum uint64
	var numheaders int
	for i:=0; i< int(query.Count); i++ {
		bnum = query.From + uint64(uint16(i)*(query.Skip+1))
		if block := m.blockchain.GetBlockByNumber(bnum); block !=nil {
			headers[i] = block.Header()
			numheaders = i+1
		} else {
			break
		}
	}
	if numheaders < 1 {
		return nil, ErrNoHeaderFound 
	}

	return headers,nil
}

func (m ObjManager) queryTx(query *EthTxQuery) (*types.Transaction, error) {
	var tx *types.Transaction

	//get pending transaction or tx in recent blocks
	tx = m.txpool.Get(common.Hash(*query))
	if tx == nil {
		//TODO: try to get the transaction from recent blocks
		return nil, ErrNoTxFound
	}
	//now that we have the requested transaction

	return tx,nil
}

func (m ObjManager) queryBlock(query *EthBlockQuery) (*types.Block, error) {
	block := m.blockchain.GetBlock(query.Hash, query.Number)

	log.Trace(fmt.Sprintf("block %d is requested", query.Number))

	if block == nil {
		return nil, ErrNoBlockFound 
	}

	return block, nil 
}

type EthObjectFetcher struct {
	fetcher		ndnsuit.ObjConsumer
	pfetcher	ndnsuit.ObjAsyncConsumer
	appname		ndn.Name
}

func (f *EthObjectFetcher) fetch(query ndnsuit.Query, route *PeerRouteInfo, abort chan bool) ([]byte, error) {
	route.appname = f.appname
	return f.fetcher.FetchObject(query, route, abort)
}

func (f *EthObjectFetcher) pfetch(query ndnsuit.Query, route *PeerRouteInfo, numsegments uint16, abort chan bool) ([]byte, error) {
	route.appname = f.appname
	return f.pfetcher.FetchObject(query, route, abort, numsegments)
}

//Job for fetching an object
type objFetchingJob struct {
	fetcher 	*EthObjectFetcher //object fetcher
	route		*PeerRouteInfo
	query		ndnsuit.Query	 //object query
	numsegments	uint16
	abort		chan bool //signal to abort job
	fn			OnObjFetchedHandler //callback when object is fetched
	obj			[]byte //fetched object wire
	err			error //error while fetching
}

func newObjFetchingJob(fetcher *EthObjectFetcher, query ndnsuit.Query, numsegments uint16, route *PeerRouteInfo, fn OnObjFetchedHandler) *objFetchingJob {
	return &objFetchingJob {
		fetcher:		fetcher,
		route:			route,
		numsegments:	numsegments,
		query:			query,
		fn:				fn,
		abort:			make(chan bool),
	}
}

//get hash of the fetching query
func (j *objFetchingJob) queryId() string {
	if q, ok := j.query.(ndnsuit.HasId); ok {
		return q.GetId()
	}
	return "" 
}
//call fetcher to fetch the object
func (j *objFetchingJob) Execute() {
	j.obj, j.err = j.fetcher.pfetch(j.query,j.route, j.numsegments, j.abort); 	
	/*
	if j.numsegments <= 1 {//if the number of segment is unknown or 1, use sequential fetcher
		j.obj, j.err = j.fetcher.fetch(j.query,j.route, j.abort); 	
	} else { //otherwise user the parallel fetcher
		j.obj, j.err = j.fetcher.pfetch(j.query,j.route, j.numsegments, j.abort); 	
	}
	*/
	j.fn(j)
}

//abort the fetching job (as worker pool stop)
func (j *objFetchingJob) Kill() {
	close(j.abort)
}
