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
	"fmt"
	"encoding/hex"
	"crypto/sha256"
	"strconv"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/ndn/ndnapp"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/usnistgov/ndn-dpdk/ndn/tlv"
)
const (
	TtRpcReq			= 100
	TtSegReq 			= 101
	TtSegment			= 102

	TtKadRpcPing		= 1
	TtKadRpcFindnode 	= 2
)
type Peer interface {
	Prefix() ndn.Name
	UpdateNodeRecord(NdnNodeRecordInfo) error
}
type KadRpcClient interface {
	Findnode(s Peer, target string, k uint8, abort chan bool) (*FindnodeResponse, error)
	Ping(s Peer, abort chan bool) (*PingResponse, error) 
}

type KadSigner interface {
	Sign(content []byte) (sig []byte, err error)
}

type KadVerifier interface {
	Verify(content []byte, sig []byte, pub []byte) bool
}

type KadCrypto interface {
	KadSigner
	KadVerifier
}

//Wrapper for Kademlia message
type KadRpcMsg struct {
	request		*KadRpcRequest
	segment		*ndnapp.SimpleSegment	
	segreq		*RpcSegmentReq
	msgtype 	uint32 //message type
}

//Kademlia Rpc request
type KadRpcRequest struct {
	Method		uint16
	Sender		NdnNodeRecordInfo
	ParamsW		[]byte
	params		interface{}
	Nonce		uint64
	Sig			[]byte
	wire		[]byte
	id			string
}

//Kademlia Rpc response
type KadRpcResponse struct {
	Method		uint16
	Sender		NdnNodeRecordInfo
	ResultsW	[]byte
	results		interface{}
	Err			string
	Sig			[]byte
}
	
func (req *KadRpcRequest) WriteRequest(segId uint16) ndnapp.Request {
	if segId == 0 {
		return &KadRpcMsg {
			msgtype: 	TtRpcReq,	
			request:	req,
		}
	} else {
		query := &RpcSegmentReq{
			Id:		req.getId(),
		}
		return query.WriteRequest(segId)
	}
}

func (req *KadRpcRequest) getId() string {
	if len(req.id) > 0 {
		return req.id
	}

	if len(req.wire) == 0 {
		req.wire, _ = rlp.EncodeToBytes(req)
	}
	digest := sha256.Sum256(req.wire)
	req.id = hex.EncodeToString(digest[:])
	return req.id
}

func (req *KadRpcRequest) encodeParams() (err error) {
	if len(req.ParamsW) == 0 {
		req.ParamsW, err = rlp.EncodeToBytes(req.params)
	}
	return
}

func (req *KadRpcRequest) decodeParams() (err error) {
	switch req.Method {
	case TtKadRpcPing:
		var params PingParams
		err =  rlp.DecodeBytes(req.ParamsW, &params)
		req.params = &params
	case TtKadRpcFindnode:
		var params FindnodeParams
		err =  rlp.DecodeBytes(req.ParamsW, &params)
		req.params = &params
	default:
		err = ErrUnexpected
	}
	return
}

func (req *KadRpcRequest) Sign(crypto KadCrypto) (err error) {
	if err = req.encodeParams(); err == nil {
		var wire []byte
		if wire,  err =  rlp.EncodeToBytes([]interface{}{req.Method, req.Sender, req.ParamsW}); err == nil {
			req.Sig, err = crypto.Sign(wire)
		}
	}
	return
}

func (req *KadRpcRequest) Verify(crypto KadCrypto) bool {
	if err := req.encodeParams(); err == nil {
		if wire,  err :=  rlp.EncodeToBytes([]interface{}{req.Method, req.Sender, req.ParamsW}); err == nil {
			return crypto.Verify(wire, req.Sig, req.Sender.Pub)
		}
	}
	return false 
}

func (resp *KadRpcResponse) encodeResults() (err error) {
	if len(resp.ResultsW) == 0 {
		resp.ResultsW, err = rlp.EncodeToBytes(resp.results)
	}
	return
}

func (resp *KadRpcResponse) decodeResults() (err error) {
	switch resp.Method {
	case TtKadRpcPing:
		var results PingResponse
		err =  rlp.DecodeBytes(resp.ResultsW, &results)
		resp.results = &results
	case TtKadRpcFindnode:
		var results FindnodeResponse
		err =  rlp.DecodeBytes(resp.ResultsW, &results)
		resp.results = &results
	default:
		err = ErrUnexpected
	}
	return
}



func (resp *KadRpcResponse) Sign(crypto KadCrypto) (err error) {
	if err = resp.encodeResults(); err == nil {
		var wire []byte
		if wire, err = rlp.EncodeToBytes([]interface{}{resp.Method, resp.Sender, resp.ResultsW, resp.Err}); err == nil {
			resp.Sig, err = crypto.Sign(wire)
		}
	}
	return 
}

func (resp *KadRpcResponse) Verify(crypto KadCrypto) bool {
	if err := resp.encodeResults(); err == nil {
		if wire, err := rlp.EncodeToBytes([]interface{}{resp.Method, resp.Sender, resp.ResultsW, resp.Err}); err == nil {
			return crypto.Verify(wire, resp.Sig, resp.Sender.Pub)
		}
	}
	return false
}

func (msg *KadRpcMsg) MarshalTlv() (typ uint32, value []byte, err error) {
	if msg.request != nil {
		if err = msg.request.encodeParams(); err == nil {
			if value, err = rlp.EncodeToBytes(msg.request); err == nil {
				msg.request.wire = value
			}
		}
	} else if msg.segreq != nil {
		value, err = rlp.EncodeToBytes(msg.segreq)
	} else {
		value, err = rlp.EncodeToBytes(msg.segment)
	}
	
	typ = msg.msgtype
	return
}

func (msg *KadRpcMsg) UnmarshalTlv(typ uint32, value []byte) (err error) {
	switch typ {
	case TtRpcReq:
		msg.request = new(KadRpcRequest)
		if err = rlp.DecodeBytes(value, &msg.request); err == nil {
			err =msg.request.decodeParams()
		}
	case TtSegReq:
		var req RpcSegmentReq
		//msg.segreq = new(RpcSegmentReq)
		err = rlp.DecodeBytes(value, &req)
		msg.segreq = &req
	case TtSegment:
		var seg ndnapp.SimpleSegment
		//msg.segment = new(ndnapp.SimpleSegment)
		err = rlp.DecodeBytes(value, &seg)
		msg.segment = &seg
	default:
		err =  ErrTlvUnmarshalFailed
	}
	
	msg.msgtype = typ	
	return
}

func (msg *KadRpcMsg) ExtractSegment() (seg ndnapp.ObjSegment, err error) {
	if msg.msgtype != TtSegment {
		err = ErrUnexpected
	} else {
		seg = msg.segment
	}
	return
}

func (msg *KadRpcMsg) ExtractQuery() (query ndnapp.Query, segId uint16,  err error) {
	switch msg.msgtype {
	case TtRpcReq:
		query = msg.request
		segId = 0
	case TtSegReq:
		query = msg.segreq
		segId = msg.segreq.SegId
	default:
		err = ErrUnexpected
	}
	return
}

type RouteInfo struct {
	prefix 		ndn.Name
	producer	ndn.Name
}

func (msg *KadRpcMsg) WriteData(i *ndn.Interest) (d ndn.Data) {
	if wire, err := tlv.Encode(msg); err == nil {
		d = ndn.MakeData(i, wire)
	}
	return
}

func (msg *KadRpcMsg) WriteInterest(route interface{}) (i ndn.Interest) {
	routeinfo,_ := route.(*RouteInfo)
	if msg.request != nil { //original request
		if wire, err := tlv.Encode(msg); err == nil {
			i = ndn.MakeInterest(ndnapp.BuildName(routeinfo.prefix,routeinfo.producer), wire)
			//i.MustBeFresh = true
			i.UpdateParamsDigest()
		} else {
			log.Info(err.Error())
		}
	} else if msg.segreq != nil { //subsequent segment request
		i = ndn.MakeInterest(msg.segreq.ToName(ndnapp.BuildName(routeinfo.prefix, routeinfo.producer)))
	}
	return
}


type rpcMsgDecoder struct {
}

func NewMsgDecoder() ndnapp.Decoder {
	return rpcMsgDecoder{}
}
func (decoder rpcMsgDecoder) DecodeInterest(i *ndn.Interest) (msg ndnapp.Request, err error) {
	if len(i.AppParameters)>0 {
		var rpcmsg	KadRpcMsg
		if err = tlv.Decode(i.AppParameters, &rpcmsg); err == nil {
			msg = &rpcmsg
		} else {
			log.Info(err.Error())
		}
	} else {
		//a segment request
		l := len(i.Name)
		dataId := string(i.Name[l-2].Value)
		var segId int64
		if segId, err = strconv.ParseInt(string(i.Name[l-1].Value),10,16); err == nil {
			req := &RpcSegmentReq{Id: dataId, SegId: uint16(segId)}
			msg = &KadRpcMsg{msgtype: TtSegReq, segreq: req}
		}
	}
	return
}

func (decoder rpcMsgDecoder) DecodeData(d *ndn.Data) (msg ndnapp.Response, err error) {
	var rpcmsg	KadRpcMsg
	if err = tlv.Decode(d.Content, &rpcmsg); err == nil {
		msg = &rpcmsg
	}
	return
}


type RpcSegmentReq struct {
	Id		string //identity of result
	SegId	uint16 //identity of requested segment
}

func (req *RpcSegmentReq) String() string {
	return req.Id
}

func (req *RpcSegmentReq) WriteRequest(segId uint16) ndnapp.Request {
	req.SegId = segId
	return &KadRpcMsg{
		msgtype:	TtSegReq,	
		segreq:		req,
	}
}

func (req *RpcSegmentReq) ToName(prefix ndn.Name) (iname ndn.Name) {
	posfix := fmt.Sprintf("/%s/%d", req.Id, req.SegId)
	iname = ndnapp.BuildName(prefix, ndn.ParseName(posfix))
	return
}


