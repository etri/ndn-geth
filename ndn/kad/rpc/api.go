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


package rpc 

import (
	"time"
//	"github.com/ethereum/go-ethereum/log"
)

//node record marshaling structure
type NdnNodeRecordInfo struct {
	Prefix	string
	Pub		[]byte
}

type PingParams struct {
	Time	uint64
}

type PingResponse struct {
	Time	uint64
}

type FindnodeParams struct {
	Target	string
	K		uint8
}

type FindnodeResponse struct {
	Peers	[]NdnNodeRecordInfo
}


type ApiBackend interface {
	Find(target string, k uint8) []NdnNodeRecordInfo //find peers from routing table
	Record() NdnNodeRecordInfo //get node record of the server
	AddSeenRecord(NdnNodeRecordInfo) //notify of seen peer
}

type Api struct {
	backend ApiBackend
}


func (a Api) Ping(sender NdnNodeRecordInfo, params *PingParams, results *PingResponse) error {
	a.backend.AddSeenRecord(sender)
	results.Time = uint64(time.Now().UnixNano())
	return nil
}

func (a Api) Findnode(sender NdnNodeRecordInfo, params *FindnodeParams, results *FindnodeResponse) error {
	a.backend.AddSeenRecord(sender)
	results.Peers = a.backend.Find(params.Target, params.K)
	return nil
}

