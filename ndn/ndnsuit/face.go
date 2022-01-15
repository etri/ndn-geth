package ndnsuit

import (
	"net"
//	"fmt"
	"sync"
	"time"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

const (
	INTEREST_TIMEOUT_GRANULARITY = 500*time.Millisecond //check for interest timeout every 500ms
	INTEREST_LIFE_TIME = 4*time.Second//ndn.DefaultInterestLifetime 
)


// Sender sends interest and data packets.
// This is the minimum abstraction for NDN nodes.
type Sender interface {
	SendInterest(*ndn.Interest) (*ndn.Data, error)
	AsyncSendInterest(i *ndn.Interest) chan InterestData
	SendData(*ndn.Data) error
	SendNack(*ndn.Interest, uint8) error
}


// Face implements Sender.
type Face interface {
	Sender
//	LocalAddr() net.Addr
//	RemoteAddr() net.Addr
	Close() error
}


type face struct {
	conn 	net.Conn
	writer 	*tlvwriter
	reader	*tlvreader
	pit		Pit
	recv 	chan<- *ndn.Interest
	wg 		sync.WaitGroup
	quit 	chan bool
}

type InterestData struct {
	D *ndn.Data
	Err	error
}

// NewFace creates a face from net.Conn.
//
// recv is the incoming interest queue.
// If it is nil, incoming interests will be ignored.
// Otherwise, this queue must be handled before it is full.
func NewFace(transport net.Conn, recv chan<- *ndn.Interest) Face {
	f := &face{
		conn:   transport,
		writer: newtlvwriter(transport),
		reader: newtlvreader(transport),
		recv:   recv,
		quit:	make(chan bool),
		pit:	newPit(),
	}

	go f.run()

	go f.tickerloop()

	return f
}

func (f *face) Close() error {
	close(f.quit)
	err := f.conn.Close()
	f.wg.Wait()
	return err
}

//mainloop read NDN packet from the network connection
func (f *face) run() {
	f.wg.Add(1)
	defer f.wg.Done()
	LOOP:
	for {
		select {
		case <- f.quit:
			break LOOP

		default:
			pack, err := f.reader.Read()
			if err != nil {
			//	log.Info(err.Error())
				break LOOP
			}
			if pack.Interest != nil {
				f.recvInterest(pack.Interest)
			} else if pack.Data != nil {
				f.recvData(pack.Data)
			} else if pack.Nack != nil {
				//log.Info(fmt.Sprintf("NACK (%d) for %s\n",pack.Nack.Reason, PrettyName(pack.Nack.Interest.Name)))
				f.pit.satisfy(pack.Nack.Interest.Name, nil, ErrNack)
			} else {
				//log.Info("Something else")
			}
		}
	}
	close(f.recv)
}

//ticker loop regularly check for expired pending interests and remove them from the pending interest table
func (f *face) tickerloop() {
	f.wg.Add(1)
	defer f.wg.Done()

	ticker := time.NewTicker(INTEREST_TIMEOUT_GRANULARITY)
	//dumpticker := time.NewTicker(10*time.Second)
	LOOP:
	for {
		select {
		case <- ticker.C:
			f.pit.refresh()
		//case <- dumpticker.C:
		//	log.Info(fmt.Sprintf("Dump pending interests at %d",time.Now().UnixNano()))
		//	f.ts.dump()
		case <- f.quit:
			break LOOP
		}
	}
}


func (f *face) recvData(d *ndn.Data) {
	f.pit.satisfy(d.Name, d, nil)
}

func (f *face) recvInterest(i *ndn.Interest) {
	if f.recv != nil {
		f.recv <- i
	}
}

func (f *face) SendData(d *ndn.Data) error {
	return f.writer.Write(d)
}

func (f *face) SendNack(i *ndn.Interest, reason uint8) error {
	nack := ndn.MakeNack(i, reason)
	return f.writer.Write(nack.ToPacket())
}

func (f *face) AsyncSendInterest(i *ndn.Interest) chan InterestData  {
	i.Lifetime = INTEREST_LIFE_TIME
	//log.Info(fmt.Sprintf("asend Interest %s", PrettyName(i.Name)))
	ret := make(chan InterestData, 1)
	pi := newpInterest(i, ret)
	f.pit.insert(pi)

	if err := f.writer.Write(i); err != nil {
		f.pit.satisfy(i.Name, nil, err)
	}

	return ret
}


func (f *face) SendInterest(i *ndn.Interest) (*ndn.Data, error) {
	result := <- f.AsyncSendInterest(i)
	return result.D, result.Err
}

