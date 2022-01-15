package chainmonitor
import (
	"encoding/json"
	"strconv"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/ethereum/go-ethereum/ndn/chainmonitor/jsonrpc2"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

type JsonEncoding interface {
	MarshalJSON() ([]byte, error)
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

type rpcDecoder struct {
	rpcRequestDecoder
	rpcResponseDecoder
}

