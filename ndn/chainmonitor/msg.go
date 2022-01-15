package chainmonitor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/ethereum/go-ethereum/ndn/chainmonitor/jsonrpc2"
	"github.com/usnistgov/ndn-dpdk/ndn"
)


const (
	RMT_Command 	= 0
	RMT_SegmentReq 	= 1
	RMT_Segment 	= 2

	MAX_RPC_CACHE	= 10
)

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


