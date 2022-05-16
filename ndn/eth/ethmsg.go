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
	"time"
	"encoding/hex"
	"crypto/sha256"
	"strconv"
	"io"
	"math/big"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	//"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/log"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/usnistgov/ndn-dpdk/ndn/tlv"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
)


//EthMsg types
const (
	TtEmpty			= 100
	TtStatus 		= 200
	TtBye			= 201
	TtPush			= 202
	TtDataReq		= 203
	TtDataSegment	= 204

	QtHeadersQuery	= 1
	QtTxQuery		= 2
	QtBlockQuery	= 3
	QtGenericQuery  = 4

	BlockPartHeader = 0
	BlockPartBody	= 1
	BlockPartFull	= 2

	DATA_SEGMENT_FRESHNESS = 10000 //milliseconds
)


//Peer status message
type StatusMsg struct {
	Pub			[]byte
	DHPub		[]byte
	Prefix		string
	NetworkId	uint64
	Td			*big.Int
	Head		common.Hash
	Bnum		uint64
	Genesis		common.Hash
	Accept		bool
	Nonce		uint64
	Sig			[]byte
}

func (msg *StatusMsg) signedpart() []byte {
	wire, _ := rlp.EncodeToBytes([]interface{}{msg.Pub, msg.DHPub, msg.Prefix, msg.NetworkId, msg.Td, msg.Head, msg.Bnum, msg.Genesis, msg.Accept, msg.Nonce})
	return wire
}

func (msg *StatusMsg) Sign(crypto EthCrypto) (err error) {
	wire := msg.signedpart()
	msg.Sig, err = crypto.Sign(wire)
	return
}

func (msg *StatusMsg) Verify(crypto EthCrypto) bool {
	wire := msg.signedpart()
	return crypto.Verify(wire, msg.Sig, msg.Pub)
}


//Goodbye message
type ByeMsg struct {
	Pub			[]byte
	Prefix		string
	Nonce		uint64
	Sig			[]byte
}
func (msg *ByeMsg) Sign(crypto EthCrypto) (err error) {
	wire, _ := rlp.EncodeToBytes([]interface{}{msg.Pub, msg.Prefix, msg.Nonce})
	msg.Sig, err = crypto.Sign(wire)
	return
}

func (msg *ByeMsg) Verify(crypto EthCrypto) bool {
	wire, _ := rlp.EncodeToBytes([]interface{}{msg.Pub, msg.Prefix, msg.Nonce})
	return crypto.Verify(wire, msg.Sig, msg.Pub)
}

//Message to query a segment of object
type EthDataReqMsg struct {
	QType		byte
	Segment		uint16
	Query		ndnsuit.Query
}


func (msg *EthDataReqMsg) EncodeRLP(w io.Writer) error {
	if wire, err := rlp.EncodeToBytes(msg.Query); err != nil {
		return err
	} else {
		return rlp.Encode(w, []interface{}{msg.QType, msg.Segment, wire})
	}
}

func (msg *EthDataReqMsg) DecodeRLP(s *rlp.Stream) (err error) {
	var obj struct{
		QType		byte
		Segment		uint16
		Query		[]byte
	}

	if err = s.Decode(&obj); err != nil {
		return
	} 
	msg.QType = obj.QType
	msg.Segment = obj.Segment
	switch obj.QType {
	case QtHeadersQuery:
		var query EthHeadersQuery
		err = rlp.DecodeBytes(obj.Query, &query)
		msg.Query = &query
	case QtTxQuery:
		var query EthTxQuery
		err = rlp.DecodeBytes(obj.Query, &query)
		msg.Query = &query
	case QtBlockQuery:
		var query EthBlockQuery
		err = rlp.DecodeBytes(obj.Query, &query)
		msg.Query = &query
	default:
		err = ErrUnexpected
	}
	return
}

type EthGenericQuery struct {
	Id		string
}
func (q *EthGenericQuery) GetId() string {
	return q.Id 
}

func (q *EthGenericQuery) WriteRequest(segId uint16) ndnsuit.Request {
	return &EthMsg{
		MsgType:	TtDataReq,
		Content:	&EthDataReqMsg {
			QType: 		QtGenericQuery,
			Segment:	segId,
			Query:		q,
		},
	}
}

//Get Tx query
type EthTxQuery	common.Hash

func (q *EthTxQuery) GetId() string {
	return common.Hash(*q).String()
}
func (q *EthTxQuery) String() string {
	return q.GetId()[:8]
}

func (q *EthTxQuery) WriteRequest(segId uint16) ndnsuit.Request {
	return writeRequest(q, QtTxQuery, segId)
}

//Get block headers query
type EthHeadersQuery struct {
	From	uint64
	Count	uint16
	Skip	uint16
	Nonce	uint64
}
func (q *EthHeadersQuery) String() string {
	return fmt.Sprintf("headers from %d count %d (skip=%d)", q.From, q.Count, q.Skip)
}

func (q *EthHeadersQuery) GetId() string {
	wire, _ := rlp.EncodeToBytes(q)
	digest := sha256.Sum256(wire)
	return hex.EncodeToString(digest[:])
}

func (q *EthHeadersQuery) WriteRequest(segId uint16) ndnsuit.Request {
	return writeRequest(q, QtHeadersQuery, segId)
}

//get block part query
type EthBlockQuery struct {
	Part	byte //header, body or full block
	Hash	common.Hash //block hash
	Number	uint64 //block number
}

func (q *EthBlockQuery) GetId() string {
	wire, _ := rlp.EncodeToBytes(q)
	digest := sha256.Sum256(wire)
	return hex.EncodeToString(digest[:])
}

func (q EthBlockQuery) String() string {
	return fmt.Sprintf("block %s", q.Hash.String()[:8])
}
func (q *EthBlockQuery) WriteRequest(segId uint16) ndnsuit.Request {
	return writeRequest(q, QtBlockQuery, segId)
}

//Prepare a Request from a query.
//the  query is injected for the first segment
//otherwise, it is replaced by a generic query to be injected 
func writeRequest(q ndnsuit.Query, qtype byte, segId uint16) ndnsuit.Request {
	if segId == 0 { 
		return &EthMsg{
			MsgType:	TtDataReq,
			Content:	&EthDataReqMsg {
				QType: 		qtype,
				Segment:	0,
				Query:		q,
			},
		}
	} else {
		newq := &EthGenericQuery{
			Id: q.(ndnsuit.HasId).GetId(),
		}
		return newq.WriteRequest(segId)
	}
}

type PeerRouteInfo struct {
	peername	ndn.Name
	usehint		bool
	appname		ndn.Name
}

//Eth protocol message. Encoded in Interest/Data packets
type EthMsg struct {
	MsgType		uint32
	Content		interface{}
}

//try to get a querry from the message content
//only TtDataReq has a query embedded
func (msg *EthMsg) ExtractQuery() (query ndnsuit.Query,segId uint16,err error) {
	if msg.MsgType != TtDataReq {
		err = ErrUnexpected
		return
	}
	if req, ok := msg.Content.(*EthDataReqMsg); !ok {
		err = ErrUnexpected
		return
	} else {
		query = req.Query
		segId = req.Segment
	}
	return
}


func (msg *EthMsg) MarshalTlv() (typ uint32, value []byte, err error) {
	if msg.MsgType == TtDataSegment {
		seg, _ := msg.Content.(*ndnsuit.SimpleSegment)
		value, err = rlp.EncodeToBytes(seg)
	} else {
		value, err = rlp.EncodeToBytes(msg.Content)
	}

	if err != nil {
		//log.Error(err.Error())
		log.Trace(err.Error())
	}
	typ = msg.MsgType
	return
}

func (msg *EthMsg) UnmarshalTlv(typ uint32, value []byte) (err error) {
	switch typ {
	case TtStatus:
		var content StatusMsg
		err = rlp.DecodeBytes(value, &content)
		msg.Content = &content
	case TtBye:
		var content ByeMsg
		err = rlp.DecodeBytes(value, &content)
		msg.Content = &content
	case TtPush: //announcement
		var content PushMsg
		err = rlp.DecodeBytes(value, &content)
		msg.Content = &content
	case TtDataReq:
		var content EthDataReqMsg
		err = rlp.DecodeBytes(value, &content)
		msg.Content = &content
	case TtDataSegment:
		var content ndnsuit.SimpleSegment
		err = rlp.DecodeBytes(value, &content)
		msg.Content = &content 

	default:
		err = ErrUnexpected
	}
	msg.MsgType = typ
	return
}

func (msg *EthMsg) ExtractSegment() (seg ndnsuit.ObjSegment, err error) {
	if msg.MsgType != TtDataSegment {
		err = ErrUnexpected
	} else {
		seg, _ = msg.Content.(ndnsuit.ObjSegment)
	}
	return
}

//Encode EthMsg to a Data packet
func (msg *EthMsg) WriteData(i *ndn.Interest) (d ndn.Data) {
	if msg.MsgType != TtEmpty {
		wire, _ := tlv.Encode(msg)
		d = ndn.MakeData(i, wire)
	} else {
		d = ndn.MakeData(i)
		if msg.MsgType == TtDataSegment { //cached the segment at CS for 10 seconds
			d.Freshness = DATA_SEGMENT_FRESHNESS*time.Millisecond
		}
	}
	return
}

//Encode a EthMsgWapper to Interest packet
func (msg *EthMsg) WriteInterest(route interface{}) (i ndn.Interest) {
	routeinfo,_ := route.(*PeerRouteInfo) 

	var querystr string
	var queryname ndn.Name
	var wire []byte
	if msg.MsgType == TtDataReq {
		req, _ := msg.Content.(*EthDataReqMsg)
		if req.QType == QtGenericQuery {
			//generic query is encoded in the Interest's name
			querystr = fmt.Sprintf("/%s/%s/%d",REQSTR,req.Query.(ndnsuit.HasId).GetId(),req.Segment)
			//query name is /req/queryid/segid
			queryname = ndn.ParseName(querystr)
		}
	}
	// routename is /blockchain/eth
	routename := ndnsuit.BuildName(routeinfo.appname, []ndn.NameComponent{EthNdnName})

	//not a generic query, build AppParameters, no segment number
	if len(queryname) <= 0 {
		wire, _ = tlv.Encode(msg)
	}

	if routeinfo.usehint { //set forwardinghint
		// hints is /peer-prefix/blockchain
		hintname := ndnsuit.BuildName(routeinfo.peername, routeinfo.appname)
		delegation := ndn.MakeFHDelegation(0, hintname)

		if len(queryname) > 0 {
			iname := ndnsuit.BuildName(routename, queryname)
			i = ndn.MakeInterest(iname, delegation)
		} else {
			i = ndn.MakeInterest(routename, wire, delegation)
		}
	} else {
		if len(queryname) >0 {
			i = ndn.MakeInterest(ndnsuit.BuildName(routeinfo.peername, routename, queryname))
		} else {
			i = ndn.MakeInterest(ndnsuit.BuildName(routeinfo.peername, routename), wire)
		}
	}
	//if msg.MsgType != TtDataReq {
	//	i.MustBeFresh = true //no need to cache announcements
	//}
	if len(wire)>0 {
		i.UpdateParamsDigest()
	}
	return
}

//Encode/decode Interest and Data packet for consumer
type ethMsgResponseDecoder struct {
}

//Decode Data packet to EthMsg
func (enc ethMsgResponseDecoder) DecodeData(d *ndn.Data) (msg ndnsuit.Response, err error) {
	var ethmsg EthMsg
	if len(d.Content) > 0 {
		err = tlv.Decode(d.Content, &ethmsg)
		msg = &ethmsg
	}
	return
}

//Encode/Decode Ndn packets for producer
type ethMsgRequestDecoder struct {
	prv_prefix		ndn.Name
	pub_prefix		ndn.Name
}

//Decode an Interest packet to EthMsg
func (enc ethMsgRequestDecoder) DecodeInterest(i *ndn.Interest) (msg ndnsuit.Request, err error) {
	var ethmsg EthMsg
	//check if the Interest includes a data query
	var c ndn.NameComponent
	var isprv bool
	l1 := len(enc.prv_prefix)
	l2 := len(enc.pub_prefix)
	l := len(i.Name)
	if enc.prv_prefix.IsPrefixOf(i.Name) && l>l1 {
		c = i.Name[l1]
		isprv = true
	} else if enc.pub_prefix.IsPrefixOf(i.Name) && l>l2 {
		isprv = false
		c = i.Name[l2]
	} else {
		//Interest name does not match to any prefix
		err = ErrUnexpected
		return
	}

	if c.Equal(ReqNdnName) { //the interest name includes query id and data segment number, the AppParameters element is nil
		var queryid,segstr string
		var segment int64
		//check if length of the name is correct
		if isprv && l != l1+3 {
			err = ErrUnexpected
			return
		}
		if !isprv && l != l2+3 {
			err = ErrUnexpected
			return
		}
		queryid = string(i.Name[l-2].Value)	//query id
		segstr = string(i.Name[l-1].Value) //segment number string
		if segment, err = strconv.ParseInt(segstr,10,16); err != nil {
			return
		}
		msg = &EthMsg{
			MsgType:	TtDataReq,
			Content:	&EthDataReqMsg{
							QType:	QtGenericQuery,
							Query:	&EthGenericQuery{
										Id:		queryid,
									},
							Segment:	uint16(segment),
						},
		}
		return
	}	
	//not a generic query, decode message from AppParameters element
	err = tlv.Decode(i.AppParameters, &ethmsg)
	msg = &ethmsg
	return
}
