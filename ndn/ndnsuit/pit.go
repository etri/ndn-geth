package ndnsuit

import (
	"sync"
	"time"
	"github.com/usnistgov/ndn-dpdk/ndn"
)
type pInterest struct {
	i 			*ndn.Interest
	expired		int64 //expiring time
	from		int64 //sending time
	ch			chan InterestData //chanel to receive a Data packet
	satisfied 	bool //mark the interest as a goner
	mutex		sync.Mutex
}


func newpInterest(i *ndn.Interest, ch chan InterestData) *pInterest {
	i.ApplyDefaultLifetime()

	now := time.Now().UnixNano()
	expired := now + int64(i.Lifetime)
	if ch == nil {
		ch = make(chan InterestData,1)
	}
	return &pInterest {
		i: i,
		expired: expired,
		from:	now,
		ch:ch,
		satisfied: false,
	}
}


func (pi *pInterest) satisfy(data *ndn.Data, err error) {
	pi.ch <- InterestData{data, err}
	pi.gone()
}

func (pi *pInterest) gone() {
	pi.mutex.Lock()
	defer pi.mutex.Unlock()
	pi.satisfied = true
}

func (pi *pInterest) isgone() bool {
	pi.mutex.Lock()
	defer pi.mutex.Unlock()
	return pi.satisfied
}

func (pi *pInterest) dump() {
	pi.mutex.Lock()
	defer pi.mutex.Unlock()
//	log.Info(fmt.Sprintf("%s at %d, s=%t\n", PrettyName(pi.i.Name), pi.expired, pi.satisfied))
}

type Pit interface {
	satisfy(ndn.Name, *ndn.Data, error) //satisfy a pending interest
	insert(*pInterest) bool	//insert a pending interest
	refresh()	//flush expired interests
}

type pitImpl struct {
	ts			*timersort
	pilist		map[string][]*pInterest
	mutex		sync.Mutex
}

func newPit() Pit {
	return &pitImpl {
		ts:		newtimersort(),
		pilist:	make(map[string][]*pInterest),
	}
}

func (p *pitImpl) satisfy(n ndn.Name, d *ndn.Data, err error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	iname := n.String()

	if pil, ok := p.pilist[iname]; ok {
		for _, pi := range pil {
			if err != nil {
				pi.satisfy(d, err)
			} else {
				pi.satisfy(d, nil)
			}
		}
	}
	delete(p.pilist, iname)
}

func (p *pitImpl) insert(pi *pInterest) bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	iname := pi.i.Name.String()
	if pil, ok := p.pilist[iname]; ok {
		p.pilist[iname] = append(pil, pi)
	} else {
		p.pilist[iname] = []*pInterest{pi}
	}
	//add timer to the priority queue	
	p.ts.push(pi)
	return true
}

func (p *pitImpl) refresh() {

	expired := p.ts.checkexpiring()

	for _, epi := range expired {
		epi.satisfy(nil, ErrTimeout)
		
		//try remove the pending interest
		iname := epi.i.Name.String()
		pil := p.pilist[iname]
		newpil := []*pInterest{}
		for _, pi := range pil {
			if !pi.isgone() {
				newpil = append(newpil, pi)
			}
		}
		if len(newpil) == 0 {
			//no more pending interest for iname, just delete it
			delete(p.pilist, iname)
		} else {
			// retain none-expired pending interests
			p.pilist[iname] = newpil
		}
	}
}

