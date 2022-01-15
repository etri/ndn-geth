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
	"time"
	"errors"
	"encoding/json"
	"encoding/hex"
	"crypto/sha256"
	"strconv"
//	"math/big"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/ndn/eth/jsonrpc2"
	"github.com/ethereum/go-ethereum/ndn/utils"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/usnistgov/ndn-dpdk/ndn"
)
const (
	RMT_Command 	= 0
	RMT_SegmentReq 	= 1
	RMT_Segment 	= 2

	MAX_RPC_CACHE	= 10
)
var EthRpcNdnName  ndn.NameComponent = ndn.ParseNameComponent("ethrpc")

func min(x,y int) int {
	if x<y {
		return x
	}
	return y
}

type JsonEncoding interface {
	MarshalJSON() ([]byte, error)
}

type RpcMsg struct {
	MsgType		byte
	Content		interface{}
}

//Extract query embeded in a RpcMsg which was decoded from an Interest packet
func (msg *RpcMsg) ExtractQuery() (query ndnsuit.Query, segId uint16, err error) {
	switch msg.MsgType {
		case RMT_Command:
			segId = 0
			query, _ = msg.Content.(*RpcCommand)
		case RMT_SegmentReq:
			req,_ := msg.Content.(*RpcSegmentReq)
			segId = req.SegId
			query	= req
		default:
			err = ErrUnexpected
	}
	return
}

//Inject Rpc message into a Data packet
func (msg *RpcMsg) WriteData(i *ndn.Interest) (d ndn.Data) {
	if objsegment, ok := msg.Content.(*ndnsuit.SimpleSegment); ok {
		if wire, err := json.Marshal(objsegment); err == nil {
			d = ndn.MakeData(i.Name, wire)
		}
	} else {
		//response message must have Data
		panic("wrong RpcMsg")
	}
	return	
}

func (msg *RpcMsg) ExtractSegment() (seg ndnsuit.ObjSegment, err error) {
	if msg.MsgType == RMT_Segment { 
		seg, _ = msg.Content.(ndnsuit.ObjSegment)
	} else {
		err = ErrUnexpected
	}
	return
}

func (msg *RpcMsg) WriteInterest(route interface{}) (i ndn.Interest) {
	prefix,_ := route.(ndn.Name)
	if cmd, ok := msg.Content.(*RpcCommand); ok { //rpc command
		enc, _ := cmd.Message.(JsonEncoding)
		if wire, err := enc.MarshalJSON();err == nil {
			i = ndn.MakeInterest(prefix, wire)
			i.UpdateParamsDigest()
		}
	} else { //RpcSegmentReq
		req, _ := msg.Content.(*RpcSegmentReq)
		i = ndn.MakeInterest(req.ToName(prefix))	
	}
	return
}

type RpcCommand	struct {
	jsonrpc2.Message
	id		string
}
func (req *RpcCommand) String() string {
	return req.id
}

func (req *RpcCommand) MakeId() {
	enc, _ := req.Message.(JsonEncoding)
	wire, _ := enc.MarshalJSON()
	digest := sha256.Sum256(wire)
	req.id = hex.EncodeToString(digest[:])
}

func (req *RpcCommand) WriteRequest(segId uint16) ndnsuit.Request {
	if segId == 0 {
		return &RpcMsg {
			MsgType:	RMT_Command,
			Content:	req,
		}
	} else {
		if len(req.id) == 0  {
			req.MakeId()
		}
		query := &RpcSegmentReq{Id:	req.id }
		return query.WriteRequest(segId)
	}
}

type RpcSegmentReq struct {
	Id		string //identity of result
	SegId	uint16 //identity of requested segment
}

func (req *RpcSegmentReq) String() string {
	return req.Id
}

func (req *RpcSegmentReq) WriteRequest(segId uint16) ndnsuit.Request {
	req.SegId = segId
	return &RpcMsg{
		MsgType:	RMT_SegmentReq,
		Content:	req,
	}
}

func (req *RpcSegmentReq) ToName(prefix ndn.Name) (iname ndn.Name) {
	posfix := fmt.Sprintf("/%s/%d", req.Id, req.SegId)
	iname = append(prefix, ndn.ParseName(posfix)...)
	return
}


type rpcRequestDecoder struct{
}

//extract Rpc message from an Interest packet
func (e rpcRequestDecoder) DecodeInterest(i *ndn.Interest) (msg ndnsuit.Request, err error) {
	if i.AppParameters != nil { //a command message
		var jmsg jsonrpc2.Message
		if jmsg, err = jsonrpc2.DecodeMessage(i.AppParameters); err == nil {
			cmd := &RpcCommand{Message: jmsg}
			cmd.MakeId()
			msg = &RpcMsg{MsgType: RMT_Command, Content: cmd}
		}
	} else {
		//a result pull request
		l := len(i.Name)
		dataId := string(i.Name[l-2].Value)
		var segId int64
		if segId, err = strconv.ParseInt(string(i.Name[l-1].Value),10,16); err == nil {
			req := &RpcSegmentReq{Id: dataId, SegId: uint16(segId)}
			msg = &RpcMsg{MsgType: RMT_SegmentReq, Content: req}
		}
	}
	return
}

type rpcResponseDecoder struct {
}

func (e rpcResponseDecoder) DecodeData(d *ndn.Data) (msg ndnsuit.Response, err error) {
	var segment ndnsuit.SimpleSegment
	if err = json.Unmarshal(d.Content, &segment); err == nil {
		msg = &RpcMsg{MsgType: RMT_Segment, Content: &segment}
	}

	return
}

type RpcDecoder struct {
	rpcRequestDecoder
	rpcResponseDecoder
}

func (c *Controller) receiveRpcMsg(i *ndn.Interest, msg *RpcMsg, producer ndnsuit.Producer) {
}

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

type ApiFn	func([]interface{}) (interface{}, error)
type RpcHandler struct {
	backend	*Controller
	cacher	utils.SimpleCacher	
	apis	map[string]ApiFn
}

func newRpcHandler(c *Controller) *RpcHandler {
	rpc := &RpcHandler{backend: c, cacher: utils.NewSimpleCacher(800,100)}
	rpc.init()
	return rpc
}

func (rpc *RpcHandler) init() {
	rpc.apis = make(map[string]ApiFn)
	rpc.apis["getnodeinfo"] = rpc.getNodeInfo
	rpc.apis["getblock"] = rpc.getBlock
	rpc.apis["getlatestblocks"] = rpc.getLatestBlocks
	rpc.apis["getlatesttxs"] = rpc.getLatestTxs
}

func (rpc *RpcHandler) getBlock(params []interface{}) (interface{}, error) {
	if len(params) != 1 {
		return nil, errors.New("getblock expects 1 parameter")
	}
	if n, ok := params[0].(float64); !ok {
		return nil, errors.New("getblock expect a number input")
	} else {
		block := rpc.backend.blockchain.GetBlockByNumber(uint64(n))
		if block == nil {
			return nil, errors.New("block not found")
		}

		info := make(map[string]interface{})
		info["header"] = block.Header()
		info["body"] = block.Body()
		return info, nil
	}
}

func (rpc *RpcHandler) getNodeInfo(params []interface{}) (info interface{}, err error) {
	return rpc.backend.GetNodeInfo(), nil
}

func (rpc *RpcHandler) getLatestTxs(params []interface{}) (info interface{},err error) {
	info = rpc.backend.latest.getTxs()
	return
}

func (rpc *RpcHandler) getLatestBlocks(params []interface{}) (info interface{},err error) {
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
	info = rpc.backend.latest.getBlocks()
	return
}

func (rpc *RpcHandler) GetSegment(query ndnsuit.Query, segId uint16) (msg ndnsuit.ResponseWithDataSegment, err error) {
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

type NodeInfo struct {
	StartedAt		time.Time
	Id				string
	Prefix			string
	Peers			[]PeerInfo
	TxPending		uint64
	NumBlocks		uint64
	Head			common.Hash
	Incoming		uint64
	Outgoing		uint64
	IsMining		bool
}

type BlockInfo	struct {
	Hash			common.Hash
	Number			uint64
	Coinbase		common.Address
	Size			common.StorageSize
	Created			uint64
	GasUsed			uint64
	NumTxs			int
}

func (c *Controller) GetNodeInfo() (info NodeInfo) {
	if c.server == nil {
		return
	}
	info.TxPending = 0
	pendings,_ := c.txpool.Pending()
	for _,txs := range pendings {
		info.TxPending += uint64(len(txs))
	}

	info.StartedAt = c.startedat
	info.Id = c.server.Identity() 
	info.Prefix	= c.server.Address() 
	info.Peers = c.peers.Info()
	block := c.blockchain.CurrentBlock()
	info.NumBlocks = block.NumberU64()
	info.Head = block.Hash()
	info.Outgoing, info.Incoming = c.getTraffic()
	info.IsMining = c.CheckMining()
	
	return
}

func (c *Controller) getTraffic() (uint64, uint64) {
	//return c.ndnsender().Traffic()
	return 0,0
}
