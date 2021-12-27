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

package ndn

type PeerInfo struct {
	Name 	string  `json:"name"`
	Id		string	`json:"id"`
}

func (c *Controller) PeerList() []PeerInfo {
	return c.peers.PeerList()
}

func (ps *peerSet) PeerList() (peers []PeerInfo) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	peers = make([]PeerInfo, len(ps.peers))
	i := 0
	for _, p := range ps.peers {
		peers[i] = PeerInfo{Name: p.name, Id: p.id}
		i++
	}
	return peers
}


func (c *Controller) StartMeasuring() {
	if c.tm == nil {
		c.tm = newtmeasurer(c)
	}
}

func (c *Controller) StopMeasuring() {
	if c.tm != nil {
		c.tm.stop()
		c.tm=nil
	}
}


