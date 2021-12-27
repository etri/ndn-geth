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


package ndnapp

import (
	"fmt"
	"time"
	"sync"
	"net"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/log"
)

const (
	PREFIX_EXPIRATION_PERIOD = 3600000
	PREFIX_RECOVER_PERIOD = 500
	INTEREST_BUFFER_SIZE = 1024
)

type Mixer struct {
	face			Face
	announcer		PrefixAnnouncer
	recv			chan *ndn.Interest //Interest receiving chanel
	quit			chan bool 
	wg				sync.WaitGroup //for closing cleanly
	mutex			sync.Mutex
	producers		map[string]AppProducer
}

func NewMixer(c net.Conn, prefix ndn.Name, signer ndn.Signer) *Mixer {
	m := &Mixer{
		announcer:	newPrefixAnnouncer(prefix,signer),
		recv: 		make(chan *ndn.Interest, INTEREST_BUFFER_SIZE),
		quit: 		make(chan bool),
		producers:	make(map[string]AppProducer),
	}
	m.face = NewFace(c, m.recv)

	go m.run()
	return m
}

func (m *Mixer) run() {
	m.wg.Add(1)
	defer m.wg.Done()

	period := PREFIX_EXPIRATION_PERIOD-1000
	if m.announcer.Announce(m.face,m.quit) != nil {
		//keep runing without receiving 
		period = PREFIX_RECOVER_PERIOD
	}

	timer := time.NewTimer(time.Duration(period)*time.Millisecond)

	LOOP:
	for {
		select {
		case <- m.quit:
			break LOOP

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
				p := m.getProducer(i)

				if p != nil {
					p.handler()(i)
				} else {
					log.Info(fmt.Sprintf("Interest %s has no destination",PrettyName(i.Name) ))
				}
			}
		}
	}
	//stop all producers
	m.mutex.Lock()
	for _, p := range m.producers {
		p.stop()
	}
	m.mutex.Unlock()
	//close face
	m.face.Close()
}

func (m *Mixer) Sender() Sender {
	return m.face
}

func (m *Mixer) getProducer(i *ndn.Interest) AppProducer {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, p := range m.producers {
		if p.canprocess(i) {
			return p
		}
	}
	return nil
}

func (m *Mixer) Register(app AppProducer) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	id:= app.identity()
	if _, ok := m.producers[id]; ok {
		return false
	}
	m.producers[id] = app
	app.start()
	return true
}

func (m *Mixer) Unregister(appid string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if running, ok := m.producers[appid]; ok {
		running.stop()
		delete(m.producers, appid)
	}
}

func (m *Mixer) Stop() {
	m.mutex.Lock()
	close(m.quit)
	m.mutex.Unlock()
	//wait for all producers have stop and face was closed
	m.wg.Wait()
}

