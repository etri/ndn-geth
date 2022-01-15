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


package chainmonitor

import (
	"errors"
	"encoding/json"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/ndn/chainmonitor/jsonrpc2"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/usnistgov/ndn-dpdk/ndn"
)



//chain monitoring RPC client

type RpcClient struct {
	fetcher		ndnsuit.ObjConsumer
}



func NewRpcClient(sender ndnsuit.Sender) *RpcClient {
	decoder := rpcResponseDecoder{}
	consumer := ndnsuit.NewObjConsumer(decoder, sender)
	return &RpcClient {
		fetcher: consumer,
	}
}

func (c *RpcClient) Request(command jsonrpc2.Message, prefix ndn.Name) (response *jsonrpc2.Response, err error) {
	req := &RpcCommand{	Message:	command }

	var obj []byte
	var reply	jsonrpc2.Message
	if obj, err = c.fetcher.FetchObject(req, prefix,  nil); err == nil {
		log.Info(string(obj))
		if reply, err = jsonrpc2.DecodeMessage(obj); err == nil {
			response, _ = reply.(*jsonrpc2.Response)
		}
	}
	return
}


// chain monitoring RPC server

type ApiFn	func([]interface{}) (interface{}, error)

type rpcHandler struct {
	m			*Monitor
	cacher		utils.SimpleCacher	
	apis		map[string]ApiFn
}

func newRpcHandler(m *Monitor) *rpcHandler {
	rpc := &rpcHandler{m: m, cacher: utils.NewSimpleCacher(800,100)}
	rpc.init()
	return rpc
}

func (rpc *rpcHandler) init() {
	rpc.apis = make(map[string]ApiFn)
	rpc.apis["getnodeinfo"] = rpc.getNodeInfo
	rpc.apis["getblock"] = rpc.getBlock
	rpc.apis["getlatestblocks"] = rpc.getLatestBlocks
	rpc.apis["getlatesttxs"] = rpc.getLatestTxs
}

func (rpc *rpcHandler) getBlock(params []interface{}) (interface{}, error) {
	if len(params) != 1 {
		return nil, errors.New("getblock expects 1 parameter")
	}
	if n, ok := params[0].(float64); !ok {
		return nil, errors.New("getblock expect a number input")
	} else {
		block := rpc.m.getBlock(uint64(n))
		if block == nil {
			return nil, errors.New("block not found")
		}

		info := make(map[string]interface{})
		info["header"] = block.Header()
		info["body"] = block.Body()
		return info, nil
	}
}

func (rpc *rpcHandler) getNodeInfo(params []interface{}) (info interface{}, err error) {
	return rpc.m.getNodeInfo(), nil
}

func (rpc *rpcHandler) getLatestTxs(params []interface{}) (info interface{},err error) {
	info = rpc.m.latest.getTxs()
	return
}

func (rpc *rpcHandler) getLatestBlocks(params []interface{}) (info interface{},err error) {
	/*
	if len(params) != 2 {
		return nil, errors.New("getlastedblocks expects 2 parameter")
	}
	var n1,n2 float64
	var ok bool
	if n1, ok = params[0].(float64); !ok {
		return nil, errors.New("first parameter must be a number")
	}
	if n2, ok = params[0].(float64); !ok {
		return nil, errors.New("second parameter must be a number")
	}

	min := int(n1)
	total := int(n2)
	if total > 100 || total <=0 || min < 0 {
		return nil, errors.New("failed to parse parameters")
	}
	*/
	info = rpc.m.latest.getBlocks()
	return
}

func (rpc *rpcHandler) GetSegment(query ndnsuit.Query, segId uint16) (msg ndnsuit.ResponseWithDataSegment, err error) {
	var response *jsonrpc2.Response
	var queryid	string
	var segment []byte //segment wire
	var num	uint16 //number of segments

	switch query.(type) {
		case *RpcCommand:
			cmd, _ := query.(*RpcCommand)
			queryid = cmd.id
			req, ok := cmd.Message.(*jsonrpc2.Call)
			if !ok {
				response, err = jsonrpc2.NewResponse(jsonrpc2.NewIntID(100), nil, ErrRpcNoID)
				break
			}
			
			id := req.ID()
			method := req.Method()
			var params []interface{}
			
			if err1 := json.Unmarshal(req.Params(), &params); err1 != nil {
				response, err = jsonrpc2.NewResponse(id, nil, ErrRpcNoArray)
				break
			}
			
			if fn, ok := rpc.apis[method]; !ok {
				response, err = jsonrpc2.NewResponse(id, nil, ErrRpcNoMethod)
				break
			} else {
				if result, err1 := fn(params) ; err1 == nil {
					response, err = jsonrpc2.NewResponse(id,result,nil)
				} else {
					response, err = jsonrpc2.NewResponse(id,nil,err1)
				}
			}

		
		case *RpcSegmentReq:
		default:
			err = ErrUnexpected
	}

	if err == nil {
		if response != nil { //first segment
			wire, _ := response.MarshalJSON()
				segment, num = rpc.cacher.Cache(queryid, wire)
				msg = &RpcMsg{
					MsgType:	RMT_Segment,
					Content:	ndnsuit.NewSimpleSegment(segment, 0, num),
				}
		} else { // subsequent segments
			if req, ok := query.(*RpcSegmentReq); ok {
				if segment, num, err = rpc.cacher.GetSegment(req.Id, req.SegId); err == nil {

					msg = &RpcMsg{
						MsgType:	RMT_Segment,
						Content:	ndnsuit.NewSimpleSegment(segment, req.SegId,num),
					}

				}  else {
					log.Info(err.Error())
				}
			} else {
				err = ErrUnexpected
			}
		}
	} else {
		log.Info(err.Error())
	}
	return
}


