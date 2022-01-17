package ndnsuit

import (
	"time"
	"sync"
	"net"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

const (
	PREFIX_EXPIRATION_PERIOD = 3600000
	PREFIX_RECOVER_PERIOD = 500
	INTEREST_BUFFER_SIZE = 1024
)

type ProducerID string


type Mixer interface {
	Sender() Sender
	Register(Producer) bool
	Stop()
	handle(*ndn.Interest) //Interest handler
}

type mixerImpl struct {
	face			Face
	announcer		PrefixAnnouncer
	recv			chan *ndn.Interest //Interest receiving chanel
	quit			chan bool 
	stopped			bool
	wg				sync.WaitGroup //for closing cleanly
	mutex			sync.Mutex
	producers		map[ProducerID]Producer
}

func NewMixer(c net.Conn, prefix ndn.Name, signer ndn.Signer/*, fn GetProducerFn*/) Mixer {
	m := &mixerImpl{
		announcer:	newPrefixAnnouncer(prefix,signer),
		recv: 		make(chan *ndn.Interest, INTEREST_BUFFER_SIZE),
		quit: 		make(chan bool),
		producers:	make(map[ProducerID]Producer),
	}
	m.face = NewFace(c, m.recv)

	go m.run()
	m.stopped = false
	return m
}

func (m *mixerImpl) run() {
	m.wg.Add(1)
	defer m.wg.Done()

	period := PREFIX_EXPIRATION_PERIOD-1000
	if m.announcer.Announce(m.face, m.quit) != nil {
		//keep runing without receiving 
		period = PREFIX_RECOVER_PERIOD
	}

	timer := time.NewTimer(time.Duration(period)*time.Millisecond)

	LOOP:
	for {
		select {
		case <- m.quit:
			//Face will close the recv chanel that helps break the loop
			m.face.Close()

			//avoiding entering this place multiple time
			m.quit = nil

		case <- timer.C:
			//prefix announcement does not occur frequently, so we allow its
			//blocking

			if m.announcer.Announce(m.face, m.quit) == nil {
				period = PREFIX_EXPIRATION_PERIOD-1000
			} else {
				period = PREFIX_RECOVER_PERIOD
			}
			timer.Reset(time.Duration(period)*time.Millisecond)

		case i, ok := <- m.recv:
			if !ok {
				// check if face was close due to connection error
				break LOOP
			} else {
				m.handle(i)
			}
		}
	}
	//stop all producers
	m.mutex.Lock()
	for _, p := range m.producers {
		p.clean()
	}
	m.stopped = true
	m.mutex.Unlock()
}

//handle an incomming Interest
func (m *mixerImpl) handle(i *ndn.Interest) {
	var p Producer
	m.mutex.Lock()
	for _, p = range m.producers {
		if p.Prefix().IsPrefixOf(i.Name) {
			break
		}
	}
	m.mutex.Unlock()

	if p != nil {	
		p.handler()(i)
	}
}

//add a producer, false if the producer is already registered
func (m *mixerImpl) Register(p Producer) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.stopped {
		return false
	}

	id := ProducerID(PrettyName(p.Prefix()))

	if len(id) == 0 {
		//invalid producer identity
		return false
	}

	if _, ok := m.producers[id]; ok {
		//producer was registered
		return false
	}

	p.setSender(m.face)
	m.producers[id] = p

	return true
}

func (m *mixerImpl) Unregister(id ProducerID) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if running, ok := m.producers[id]; ok {
		running.clean()
		delete(m.producers, id)
	}
}

func (m *mixerImpl) Sender() Sender {
	return m.face
}

func (m *mixerImpl) Stop() {
	close(m.quit)
	m.wg.Wait()
}
