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


package utils
import (
	"errors"
)
var (
	ErrObjNotFound = errors.New("object segment is not found")
)

func min(x,y int) int {
	if x<y {
		return x
	}
	return y
}

type cacheobj struct {
	wire	[]byte
	id		string
}

type SimpleCacher interface {
	Cache(id string, wire []byte) (segment []byte, num uint16)
	GetSegment(id string, segId uint16) (seg []byte, num uint16, err error) }

//simple cacher, keep only 100 object
type simpleCacher struct {
	ssize		int //segment size
	imax		int //max number of items
	objlist		[]*cacheobj
	objmap		map[string]*cacheobj
	next		int
}

func NewSimpleCacher(segmentsize int, maxitems int) *simpleCacher {
	return &simpleCacher{ 
		ssize:		segmentsize,
		objmap:		make(map[string]*cacheobj),
		objlist:	make([]*cacheobj, maxitems),
		imax:		maxitems,
		next:		0,
	}
}

func (c *simpleCacher) Cache(id string, wire []byte) (segment []byte, num uint16) {
	if len(wire) < c.ssize {
		return wire, 1
	}
	if _, ok := c.objmap[id]; !ok {
		obj := &cacheobj{wire: wire, id: id}
		c.objmap[id] = obj
		if c.objlist[c.next] != nil {
			delete(c.objmap, c.objlist[c.next].id)
		}
		c.objlist[c.next] = obj
		c.next++
		if c.next >= c.imax {
			c.next = 0
		}
	}

	segment, num, _ =  c.segment(wire, 0)
	return
}

func (c *simpleCacher) GetSegment(id string, segId uint16) (seg []byte, num uint16, err error) {
	if obj, ok := c.objmap[id]; ok {
		return c.segment(obj.wire, segId)
	}
	err = ErrObjNotFound	
	return
}

func (c *simpleCacher) segment(wire []byte, segId uint16) (segment []byte, num uint16, err error) {
	start := 0
	num = 0
	for start < len(wire) {
		start += c.ssize
		num++
	}
	if segId < num {
		segment = wire[int(segId)*c.ssize: min(len(wire), (int(segId)+1)*c.ssize)]
	} else {
		err = ErrObjNotFound
	}
	return
}


