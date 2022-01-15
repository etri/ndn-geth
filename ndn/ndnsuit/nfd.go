package ndnsuit

import (
	"fmt"
	"errors"
	"math/rand"
	"time"

	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/usnistgov/ndn-dpdk/ndn/tlv"
	ndntype "github.com/usnistgov/ndn-dpdk/ndn/an"
)

const (
	TtFaceID 			= 105
	TtURI				= 114
	TtLURI				= 129
	TtOrigin			= 111
	TtCost				= 106
	TtCapacity			= 131
	TtCount				= 132
	TtBCMI				= 135
	TtDCT				= 136
	TtMtu				= 137
	TtFlags				= 108
	TtMask				= 112
	TtStrategy			= 107
	TtExpirationPeriod	= 109
	TtFacePersistency	= 133
	TtCommandResponse	= 101
	TtStatusCode		= 102
	TtStatusText		= 103
	TtParameters		= 104
)

type NfdCommand ndn.Name 

func makeNfdCommand(local,nfd,module,command string, params *Parameters) NfdCommand {
	timestamp := uint64(time.Now().UnixNano() / 1000000)
    nonce :=  uint64(rand.Uint32())
	name  := make([]ndn.NameComponent, 9)
	name[0] = ndn.MakeNameComponent(ndntype.TtGenericNameComponent,[]byte(local))
	name[1] = ndn.MakeNameComponent(ndntype.TtGenericNameComponent,[]byte(nfd))
	name[2] = ndn.MakeNameComponent(ndntype.TtGenericNameComponent,[]byte(module))
	name[3] = ndn.MakeNameComponent(ndntype.TtGenericNameComponent,[]byte(command))
	wire,_ := tlv.Encode(params) 
	name[4] = ndn.MakeNameComponent(ndntype.TtGenericNameComponent,wire)
	name[5] = ndn.NameComponent{tlv.MakeElementNNI(ndntype.TtGenericNameComponent,timestamp)}
	name[6] = ndn.NameComponent{tlv.MakeElementNNI(ndntype.TtGenericNameComponent,nonce)}
	return name
}

func (cmd NfdCommand) SignWith(signer func(ndn.Name,*ndn.SigInfo) (ndn.LLSign, error)) error {
	var si ndn.SigInfo	
	llsigner, err := signer(ndn.Name(cmd[:7]),&si)
	if err != nil {
		return err
	}
	wire, _ := tlv.Encode(cmd[:7])
	var sig []byte
	sig, err = llsigner(wire)
	if err != nil {
		return err
	}
	wire, _ = tlv.Encode(si.EncodeAs(ndntype.TtDSigInfo))
	cmd[7] = ndn.MakeNameComponent(ndntype.TtGenericNameComponent,wire)
	cmd[8] = ndn.NameComponent{tlv.MakeElement(ndntype.TtDSigValue,sig)}
	return nil
}


// Command alters forwarder state.
//
// See http://redmine.named-data.net/projects/nfd/wiki/Management.
/*
type Command struct {
	Local          string                  `tlv:"8"`
	NFD            string                  `tlv:"8"`
	Module         string                  `tlv:"8"`
	Command        string                  `tlv:"8"`
	Parameters     Parameters		       `tlv:"8"`
	Timestamp      uint64                  `tlv:"8"`
	Nonce          uint64                  `tlv:"8"`
	SignatureInfo  sigInfoComp 			   `tlv:"8"`
	SignatureValue sigValueComp 		   `tlv:"8*"`
}
type parametersComponent struct {
	Parameters Parameters `tlv:"104"`
}

type sigInfoComp struct {
	Info ndn.SigInfo `tlv:"22"`
}

type sigValueComp struct {
	Value []byte `tlv:"23"`
}
*/
// Parameters contains arguments to command.
type Parameters struct {
	Name             ndn.Name     `tlv:"7?"`
	FaceID           uint64   `tlv:"105?"`
	URI              string   `tlv:"114?"`
	LURI             string   `tlv:"129?"`
	Origin           uint64   `tlv:"111?"`
	Cost             uint64   `tlv:"106?"`
	Capacity         uint64   `tlv:"131?"`
	Count            uint64   `tlv:"132?"`
	BCMI             uint64   `tlv:"135?"`
	DCT              uint64   `tlv:"136?"`
	Mtu              uint64   `tlv:"137?"`
	Flags            uint64   `tlv:"108?"`
	Mask             uint64   `tlv:"112?"`
	Strategy         Strategy `tlv:"107?"`
	ExpirationPeriod uint64   `tlv:"109?"`
	FacePersistency  uint64   `tlv:"133?"`
}

func (p *Parameters) MarshalTlv() (t uint32,v []byte,e error) {
	fields := []interface{}{}
	if len(p.Name) >0 {
		fields = append(fields, p.Name)
	}
	if len(p.URI) > 0 {
		fields = append(fields, tlv.MakeElement(TtURI, []byte(p.URI)))
	}

	if p.ExpirationPeriod >0 {
		fields = append(fields, tlv.MakeElementNNI(TtExpirationPeriod,p.ExpirationPeriod))
	}
	//TODO: add other fields if they are needed
	v, e = tlv.Encode(fields...)
	return TtParameters,v,e
}

func (p *Parameters) UnmarshalBinary(wire []byte) (err error) {
	d := tlv.Decoder(wire)
	for _,field := range d.Elements() {
		switch field.Type {
		case ndntype.TtName:
		case TtFaceID:
			err = field.UnmarshalNNI(&p.FaceID)
		case TtURI:
			p.URI = string(field.Wire)
		case TtLURI:
			p.LURI = string(field.Wire)
		case TtOrigin:
			err = field.UnmarshalNNI(&p.Origin)
		case TtCost:
			err = field.UnmarshalNNI(&p.Cost)
		case TtCapacity:
			err = field.UnmarshalNNI(&p.Capacity)
		case TtCount:
			err = field.UnmarshalNNI(&p.Count)
		case TtBCMI:
			err = field.UnmarshalNNI(&p.BCMI)
		case TtDCT:
			err = field.UnmarshalNNI(&p.DCT)
		case TtMtu:
			err = field.UnmarshalNNI(&p.Mtu)
		case TtFlags:
			err = field.UnmarshalNNI(&p.Flags)
		case TtMask:
			err = field.UnmarshalNNI(&p.Mask)
		case TtStrategy:
			err = field.UnmarshalValue(&p.Strategy)
		case TtExpirationPeriod:
			err = field.UnmarshalNNI(&p.ExpirationPeriod)
		case TtFacePersistency:
			err = field.UnmarshalNNI(&p.FacePersistency)
		default:
			err = errors.New("Unknown field in Parameters")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

type Strategy struct {
	Name ndn.Name `tlv:"7"`
}

func (s *Strategy) UnmarshalBinary(wire []byte) error {
	d := tlv.Decoder(wire)
	field, err := d.Element()
	if err != nil {
		return err
	}
	return field.UnmarshalValue(&s.Name)
}

// CommandResponse contains status code and text.
//
// StatusCode generally follows HTTP convention [RFC2616].
type CommandResponse struct {
	StatusCode uint64     `tlv:"102"`
	StatusText string     `tlv:"103"`
	Parameters Parameters `tlv:"104?"`
}

func (cr *CommandResponse) UnmarshalTlv(typ uint32, wire []byte) error {
	if typ != TtCommandResponse {
		return errors.New("Unexpected nfd command response")
	}
	return cr.UnmarshalBinary(wire)
}

func (cr *CommandResponse) UnmarshalBinary(wire []byte) (err error) {
	d := tlv.Decoder(wire)
	for _, field := range d.Elements() {
		switch field.Type {
			case TtStatusCode:
				err = field.UnmarshalNNI(&cr.StatusCode)
			case TtStatusText:
				cr.StatusText = string(field.Wire)
			case TtParameters:
				err = field.UnmarshalValue(&cr.Parameters)
			default:
				err = errors.New("Unknown field in CommandResponse")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// SendControl sends command and waits for its response.
//
// ErrResponseStatus is returned if the status code is not 200.
func SendControl(w Sender, module, command string, params *Parameters, signer ndn.Signer, abort chan bool) (err error) {
	cmd := makeNfdCommand("localhost", "nfd", module, command, params)
	if err = signer.Sign(cmd); err != nil {
		return
	}

	i := ndn.MakeInterest(ndn.Name(cmd)) 
	var d *ndn.Data
	ch := w.AsyncSendInterest(&i)
	select {
	case <- abort:
		err = ErrInteruption
	case outcome := <- ch:
		d = outcome.D
		err = outcome.Err
	}

	if err != nil {
		return 
	}

	var resp CommandResponse
	if err = tlv.Decode(d.Content, &resp); err != nil {
		return 
	}
	if resp.StatusCode != 200 {
		fmt.Println(resp.StatusText)
		err = ErrResponseStatus
	}
	return 
}
