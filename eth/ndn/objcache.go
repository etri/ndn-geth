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

import (
	"time"
	"sync"
)

//a simple implementation of object manager to cache large-size objects that
//need multiple segment requests to fetch. The object's wire is cached when it
//is first queried. It accessed time is updated at every requests. After a
//certain period of time after the last access, the object is removed from the
//cache

//NOTE: a better implementation should have a upper bound on the cache size. it
//should have customizable life-time for object types.

const (
	CACHE_CHECKING_INTERVAL = 1*time.Second //interval for checking expired objects
	OBJ_EXPIRING_PERIOD = 10*time.Second //period where last-updated object is retained in the cache
	//MAX_SEGMENT_SIZE = 8000
	MAX_SEGMENT_SIZE = 1000
)
type CachedObj struct {
	id			string
	expired		time.Time
	wire		[]byte
	numsegs		uint16
//	mutex		sync.Mutex
}
func (obj *CachedObj) update() {
//	obj.mutex.Lock()
//	defer obj.mutex.Unlock()
	obj.expired = time.Now().Add(OBJ_EXPIRING_PERIOD)
}

func (obj *CachedObj) isexpired() bool {
//	obj.mutex.Lock()
//	defer obj.mutex.Unlock()
	return obj.expired.Before(time.Now())
}

//Very simple cache manager
//TODO: limits the number of cached objects; manage objects with priority queue
type ObjCacheManager struct {
	objects		map[string]*CachedObj
	mutex		sync.Mutex
	dead		bool
	quit		chan struct{}
}

func newObjCacheManager() *ObjCacheManager {
	c := &ObjCacheManager {
		objects: 	make(map[string]*CachedObj),
		quit:		make(chan struct{}),
		dead:		false,
	}
	go c.loop()
	return c
}

func (c *ObjCacheManager) loop() {
	ticker := time.NewTicker(CACHE_CHECKING_INTERVAL)
	LOOP:
	for {
		select {
		case <-ticker.C:
			//remove old cached objects
			c.mutex.Lock()
			//TODO: mangage objects in a priority queue
			ids := []string{}
			for id, obj := range c.objects {
				if obj.isexpired() {
					ids = append(ids, id)
				}
			}
			for _, id := range ids {
				delete(c.objects, id)
			}
			c.mutex.Unlock()
		case <- c.quit:
			break LOOP
		}
	}
}

func (c *ObjCacheManager) stop() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.quit <- struct {} {}
	c.dead = true
}


//cache an object, return the first segment and the number of segments
func (c *ObjCacheManager) cache(id string, wire []byte) (seg []byte, num uint16) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.dead {
		return
	}
	//already cached, return the first segment
	if obj, ok := c.objects[id]; ok {
		obj.update()
		num = obj.numsegs
		seg = obj.wire[:min(len(obj.wire), MAX_SEGMENT_SIZE)] 	
		return
	}

	num = 0
	start := 0
	for start < len(wire) {
		start += MAX_SEGMENT_SIZE
		num++
	}

	if num > 1 { //only cache large objects
		obj := &CachedObj {
			id:			id,
			wire:		wire,
			numsegs:	uint16(num),
		}
		obj.update()
		c.objects[id] = obj	 //cache the object
		end := min(len(wire), MAX_SEGMENT_SIZE)
		seg = wire[:end]
	} else {
		seg = wire
	}

	return
}

//get a segment given its Id and object Id
func (c *ObjCacheManager) getSegment(id string, segId uint16) (seg []byte, num uint16, err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.dead {
		err = ErrJobAborted
		return
	}
	if obj, ok := c.objects[id]; ok {
		obj.update()
		num = obj.numsegs
		if segId >= num { //out-of-range segment id
			err = ErrUnexpected
		} else {
			start := MAX_SEGMENT_SIZE*int(segId)
			end := min(len(obj.wire), start+MAX_SEGMENT_SIZE)
			seg = obj.wire[start:end]
		}
	} else {
		err = ErrObjNotFound
	}
	return
}
