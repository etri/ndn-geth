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
	"errors"
	"time"
	"math/rand"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/ethereum/go-ethereum/rlp"
//	"github.com/ethereum/go-ethereum/log"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

type Client struct {
	kadprefix		ndn.Name //appname + p2p name
	consumer		ndnsuit.ObjConsumer //sending interest
	me				NdnNodeRecordInfo  //sender info for kad messages
	crypto			KadCrypto //sign/verify kad messages
}

func NewClient(me NdnNodeRecordInfo, kadname ndn.Name, consumer ndnsuit.ObjConsumer, crypto KadCrypto) KadRpcClient {
	c := &Client{
		me:			me,
		consumer:	consumer,
		kadprefix:	kadname,
		crypto:		crypto,
	}
	return c
}

func (cli *Client) Findnode(peer Peer, target string, k uint8, abort chan bool) (response *FindnodeResponse, err error) {
	msg := &KadRpcRequest{
			Method:		TtKadRpcFindnode,
			Sender:		cli.me,
			Nonce:	 	rand.Uint64(),	
			params:		&FindnodeParams {
							Target: target,
							K: 		k,
						},
			}
	var reply interface{} 
	reply, err = cli.request(msg, peer, abort)
	if err == nil {
		response, _ = reply.(*FindnodeResponse)	
	}
	return
}

func (cli *Client) Ping(peer Peer, abort chan bool) (response *PingResponse, err error) {
	msg := &KadRpcRequest {
			Method:		TtKadRpcPing,
			Sender:		cli.me,
			Nonce:	 	rand.Uint64(),	
			params: 	&PingParams {
							Time: 	uint64(time.Now().UnixNano()),
						},
			}
	var reply interface{} 
	reply, err = cli.request(msg, peer, abort)
	if err == nil {
		response, _ = reply.(*PingResponse)	
	}
	return
}

func (cli *Client) request(req *KadRpcRequest, peer Peer, abort chan bool) (response interface{}, err error) {
	prefix := peer.Prefix() 
	routeinfo := &RouteInfo{
		prefix:		prefix,
		producer:	cli.kadprefix,
	}
	req.Sign(cli.crypto)
	var obj []byte
	if obj, err = cli.consumer.FetchObject(req, routeinfo, abort); err == nil {
		var rpcresponse	KadRpcResponse
		if err = rlp.DecodeBytes(obj, &rpcresponse); err == nil {
			if err = rpcresponse.decodeResults(); err == nil {
				if rpcresponse.Verify(cli.crypto) {
					if len(rpcresponse.Err)>0{
						err = errors.New(rpcresponse.Err)
					} else {
						response = rpcresponse.results
						peer.UpdateNodeRecord(rpcresponse.Sender)
					}
				} else 
				{
					err = ErrInvalidSignature
				}
			}
		}
	}
	return
}

