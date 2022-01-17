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


package kad

import (
	"fmt"
	"net"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/ethereum/go-ethereum/ndn/kad/rpc"
	"github.com/ethereum/go-ethereum/log"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

type PeerEventCallbackFn func(NodeRecord,bool)


type Server struct {
	Config		Config
	crypto		Crypto
	transport	ndnsuit.Mixer
	node		*kadnode
}

type KadEvent struct {
	Action	uint8
	Record	NodeRecord	
}


func nfdconnect(faceaddr string) (conn net.Conn, err error) {
	//TODO: support other types of connection (UDP, TCP)
	conn, err = net.Dial("unix", faceaddr )
	return	
}

func NewServer(conf Config) (*Server, error) {
	//create a face connection to NFD
	conn, err := nfdconnect(conf.Face)
	if err != nil {
		return nil, err
	}
	//TODO: create a signer for prefix announcement

	//create the mixer to communicate with the NFD
	mixer := ndnsuit.NewMixer(conn, append(conf.HostName, conf.AppName...), nil)

	s := &Server{
		Config: 	conf,
		transport:	mixer,
		crypto:	Crypto{
			prv:	conf.PrivateKey,
		},
	}
	
	me, err := NdnNodeRecordFromPrivateKey(conf.HostName, conf.PrivateKey)
	if err != nil {
		log.Error(fmt.Sprintf("Failed to make a node record: %s. Check the NDN host name option", err.Error()))
		return nil, err
	}
	rec, _ := NdnNodeRecordMarshaling(me)

	producername := ndnsuit.BuildName(conf.AppName, []ndn.NameComponent{KadNameComponent})
	rpcprefix := ndnsuit.BuildName(conf.HostName, producername)

	//a decoder for decoding RpcMsg from Ndn packets
	decoder := rpc.NewMsgDecoder()
	//Kad rpc client
	rpcclient := rpc.NewClient(rec, producername, ndnsuit.NewObjConsumer(decoder, mixer.Sender()), &s.crypto)
	s.node = newkadnode(me, rpcclient)


	//Kad rpc server
	rpcserver := rpc.NewServer(s.node, &s.crypto)

	//Register kad producer to the mixer
	mixer.Register(ndnsuit.NewProducer(rpcprefix, decoder, nil, nil, rpcserver))

	return s, nil
}

//server pretty name (ndn host name)
func (s *Server) String() string {
	return s.node.String()
}

func (s *Server) Address() string {
	return s.node.self.record.Address()
}

func (s *Server) Identity() string {
	return s.node.self.record.Identity().String()
}
func (s *Server) PublicKey() []byte {
	return s.node.self.record.PublicKey()
}

func (s *Server) Crypto() *Crypto {
	return &s.crypto
}

func (s *Server) Transport() ndnsuit.Mixer {
	return s.transport
}

func (s *Server) Start() error {
	s.node.start(s.Config.Bootnodes)
	return nil
}

func (s *Server) Stop() {
	s.node.stop()
	log.Info("Node stop")
	s.transport.Stop()
	log.Info("Mixer stop")
}

//register a callback function that will be called whenever a new peer is added
//to the routing table
func (s *Server) RegisterPeerEvent(fn PeerEventCallbackFn) {
	s.node.peereventfn = fn
}

//drop a peer with given identity
//TODO: has a blacklist
func (s *Server) DropPeer(ids string) {
	id, err := IdFromHexString(ids)
	if err == nil {
		s.node.droppeer(id)
	}
}

