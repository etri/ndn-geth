package ndnsuit
import(
	"fmt"
	"io"
	"encoding/binary"
	"sync"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/usnistgov/ndn-dpdk/ndn/tlv"
	"github.com/ethereum/go-ethereum/log"
)

const (
	//MaxSize = 8800 //MTU = 9000 (jumbo frame)
	MaxSize = 1300 //MTU=1500
)

type tlvwriter struct {
	io.Writer
	traffic		uint64
	wm			sync.Mutex // writer mutex
}

// NewWriter creates a new buffered Writer.
func newtlvwriter(w io.Writer) *tlvwriter {
	return &tlvwriter{
		Writer: w,
		traffic:	0,
	}
}

func (w *tlvwriter) Traffic() uint64 {
	return w.traffic
}

func (w *tlvwriter) Reset() {
	w.traffic = 0
}

func (w *tlvwriter) Write(p ndn.L3Packet) (e error) {
	var wire []byte
	var l int
	if wire, e = tlv.Encode(p); e !=nil {
		return
	}
	if len(wire) > MaxSize {
		log.Info(fmt.Sprintf("oversize packet %d", len(wire)))
		e = ErrOversize
		return
	}
	w.wm.Lock()
	l, e = w.Writer.Write(wire)
	w.traffic += uint64(l)
	//log.Info(fmt.Sprintf("%d is written ", len(wire)))
	w.wm.Unlock()
	return
}

type tlvreader struct {
	io.Reader
	b     []byte
	traffic	uint64
}

// NewReader creates a new buffered Reader.
func newtlvreader(r io.Reader) *tlvreader {
	return &tlvreader{
		Reader: r,
		traffic: 0,
	}
}

func (r *tlvreader) Traffic() uint64 {
	return r.traffic
}

func (r *tlvreader) Reset() {
	r.traffic = 0
}

func (r *tlvreader) Read() (*ndn.Packet, error) {
	var err error
	r.b = make([]byte,MaxSize)
	var l int
	if l, err = r.fill(); err != nil {
		return nil, err
	}
	var p ndn.Packet
	err = tlv.Decode(r.b[:l], &p)
	//e = elem.Unmarshal(&p)
	if err!=nil {
	//	log.Info(fmt.Sprintf("TLV DECODE ERROR: %s", err.Error()))
	}
	r.traffic += uint64(l)
	return &p, err
}

// fill fills b with the next tlv-encoded block.
func (r *tlvreader) fill() (int, error) {
	var n int
	nn, err := fillVarNum(r.Reader, r.b[n:])
	if err != nil {
		return n, err
	}
	n += nn

	nn, err = fillVarNum(r.Reader, r.b[n:])
	if err != nil {
		return n,err
	}
	_, l := readVarNum(r.b[n:])
	n += nn

	if l > uint64(len(r.b[n:])) {
		return n,ErrOversize
	}
	nn, err = io.ReadFull(r.Reader, r.b[n:n+int(l)])
	if err != nil {
		return n,err
	}
	return n+nn, nil
}

// fillVarNum fills b with the next non-negative integer in variable-length encoding.
func fillVarNum(r io.Reader, b []byte) (n int, err error) {
	_, err = io.ReadFull(r, b[:1])
	if err != nil {
		return
	}
	switch b[0] {
	case 0xFF:
		n = 9
	case 0xFE:
		n = 5
	case 0xFD:
		n = 3
	default:
		n = 1
	}
	_, err = io.ReadFull(r, b[1:n])
	return
}

func readVarNum(b []byte) (n int, v uint64) {
	switch b[0] {
	case 0xFF:
		v = binary.BigEndian.Uint64(b[1:])
		n = 9
	case 0xFE:
		v = uint64(binary.BigEndian.Uint32(b[1:]))
		n = 5
	case 0xFD:
		v = uint64(binary.BigEndian.Uint16(b[1:]))
		n = 3
	default:
		v = uint64(b[0])
		n = 1
	}
	return
}

