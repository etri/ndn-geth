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


package rpc

import (
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/ethereum/go-ethereum/rlp"
//	"github.com/ethereum/go-ethereum/log"
)

type server struct {
	api			Api
	cacher		utils.SimpleCacher
	crypto		KadCrypto
}

func NewServer(backend ApiBackend, crypto KadCrypto) ndnsuit.ObjManager {
	return &server{api: Api{backend: backend}, crypto: crypto, cacher: utils.NewSimpleCacher(1000,100)}
}

func (srv *server) GetSegment(query ndnsuit.Query, segId uint16) (reply ndnsuit.ResponseWithDataSegment, err error) {
	var response *KadRpcResponse
	var queryid string
	switch query.(type) {
		case *KadRpcRequest:
			req, _ := query.(*KadRpcRequest)
			queryid = req.getId()
			if segId > 0 { //make sure segId must be 0
				break
			}

			response = new(KadRpcResponse)
			response.Sender = srv.api.backend.Record()
			response.Method = req.Method
			if req.Method == TtKadRpcPing {
				//handle Ping request
				params, _ := req.params.(*PingParams)
				var results PingResponse
				response.results = &results 
				if err = srv.api.Ping(req.Sender, params, &results); err != nil {
					response.Err = err.Error()
				}

			} else if req.Method == TtKadRpcFindnode {
				//handle Findnode request
				params, _ := req.params.(*FindnodeParams)
				var results FindnodeResponse 
				response.results = &results 
				if err = srv.api.Findnode(req.Sender, params, &results); err != nil {
					response.Err = err.Error()
				}
			} else { //unexpected case
				err = ErrUnexpected
			}
		case *RpcSegmentReq:
			req, _ := query.(*RpcSegmentReq)
			queryid = req.Id
	default:
		err = ErrUnexpected
	}
	if err == nil {
		var num uint16
		var segment []byte
		if response != nil {
			response.Sign(srv.crypto) //results was encoded too
			if wire, err := rlp.EncodeToBytes(response); err == nil {
				segment, num = srv.cacher.Cache(queryid, wire)
			}
		} else {
			segment, num, err = srv.cacher.GetSegment(queryid, segId)
		}
		if err == nil {
			reply = &KadRpcMsg {
				msgtype:	TtSegment,
				segment:	ndnsuit.NewSimpleSegment(segment, segId, num),
			}
		}
	}

	return
}

