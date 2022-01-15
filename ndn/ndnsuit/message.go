package ndnsuit

import (
//	"fmt"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

//Request represents an application message embeded in an Interest
type Request interface {
	WriteInterest(interface{}) ndn.Interest
}

//Response represents an application message embeded in a Data
type Response interface {
	WriteData(*ndn.Interest) ndn.Data
}

//Decoder reads an application request from an Interest
type RequestDecoder interface {
	DecodeInterest(*ndn.Interest) (Request, error)
}

//Decoder reads an application response from a Data
type ResponseDecoder interface {
	DecodeData(*ndn.Data) (Response, error)
}


type Decoder interface {
	RequestDecoder
	ResponseDecoder
}


