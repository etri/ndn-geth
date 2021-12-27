package main

import (
	"time"
	"fmt"
//	"net"
	"io/ioutil"
//	"sync"
	mrand "math/rand"
	//"encoding/base64"
	"encoding/json"
	"encoding/hex"
//	"crypto/elliptic"
//	"crypto/x509"
	"crypto/ecdsa"
//	"crypto/sha256"
//	"crypto/rand"
	"github.com/ethereum/go-ethereum/crypto"
//	ndn "github.com/usnistgov/ndn-dpdk/ndn"
	p2p "github.com/ethereum/go-ethereum/ndn/kad"
)

type Peer struct {
	Prefix string
	key	*ecdsa.PrivateKey
	KeyString	string
	Id	string
}
/*
func (p *Peer) GetNdnKey() ndn.Signer {
	key := &ndn.ECDSAKey{ndn.NewName(p.Prefix), p.key}
	return key
}
*/
func Generatepeers(n int, start int, subdomains []string, domain string) []Peer {
	mrand.Seed(time.Now().Unix())

	peers := make([]Peer, n)
	for i:=0; i<n; i++ {
		ri := mrand.Intn(len(subdomains))
		prk,_ := crypto.GenerateKey() 
		kb := crypto.FromECDSA(prk)
		ks := hex.EncodeToString(kb)
		id := p2p.IdFromPublicKey(&prk.PublicKey).String()
		//name := fmt.Sprintf("%s/%s-peer%d", prefix, hexid[:8], i+start)
		name := fmt.Sprintf("/%s/%s/node%d-%s", domain, subdomains[ri], i+start, id[:4])
		peers[i] = Peer{
			Prefix: name,
			key:	prk,
			KeyString: ks,
			Id: id, 
		}
	}
	return peers
}

func Writepeers(peers []Peer, filename string) {
	b, _ := json.MarshalIndent(peers,"","  ")
	ioutil.WriteFile(filename, b ,0644)
}
func Readpeers(filename string) []*Peer {
	b,_ := ioutil.ReadFile(filename)
	var peers []*Peer
	fmt.Println("Reading peers from ", filename)
	if err := json.Unmarshal(b, &peers); err != nil {
		return []*Peer{}
	}
	for _,p := range peers {
		kb, _ := hex.DecodeString(p.KeyString)
		k, err := crypto.ToECDSA(kb) 
		if err != nil {
			fmt.Println(p.Prefix, "key error: ", err.Error())
			return []*Peer{}
		}
		p.key = k
	}

//	fmt.Println(peers[0])
	return peers
}

