package main

import (
	"time"
	"fmt"
	"net"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"os"
//	"os/signal"
//	"syscall"
//	"sync"
//	"strconv"
	"math/rand"
	p2p "github.com/ethereum/go-ethereum/ndn/kad"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/crypto"
	ndn "github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/ndn/ndnapp"
	ethndn "github.com/ethereum/go-ethereum/eth/ndn"
	"github.com/ethereum/go-ethereum/eth/ndn/jsonrpc2"
)
type PingParams struct {
	Id	string
}

func main() {
	setuplog()
//	p2pnet()
//	genpeers()
//	genesis()
//	nodekeys(20)
//    testndn()
//	testmixer()
	testrpc()
}

func testrpc() {
	rand.Seed(time.Now().UnixNano())
	conn, err := net.Dial("unix", "/run/nfd.sock")
	if err!= nil {
		log.Info("Run nfd please")
		return
	}
	rcvCh := make(chan *ndn.Interest)
	f := ndnapp.NewFace(conn, rcvCh)
	client := ethndn.NewRpcClient(f)
	prefix := ndn.ParseName("/etri/node1/blockchain/ethrpc")
	params := PingParams{ Id: "test"}
	msg,_ := jsonrpc2.NewCall(jsonrpc2.NewIntID(rand.Int63()), "ping", &params)
	if reply, err := client.Request(msg,prefix); err == nil {
		raw := reply.Result()
		log.Info(string(raw))
	}
}

func setuplog() {
	glogger := log.NewGlogHandler(log.StreamHandler(os.Stderr, log.TerminalFormat(false)))
	glogger.Verbosity(log.LvlInfo)
	//glogger.Vmodule(*vmodule)
	log.Root().SetHandler(glogger)
}

func testndn() {
	conn, err := net.Dial("unix", "/run/nfd.sock")
	if err!= nil {
		log.Info("Run nfd please")
		return
	}
	rcvCh := make(chan *ndn.Interest)
	f := ndnapp.NewFace(conn, rcvCh)
	i := ndn.MakeInterest("/ndn/testprefix")
	var d *ndn.Data
	log.Info(fmt.Sprintf("Interest %s",i.Name.String()))
	if d, err = f.SendInterest(&i); err != nil {
		log.Info(err.Error())
	} else {
		log.Info(string(d.Content))

	}
}
func testmixer() {
	/*
	conn, err := net.Dial("unix", "/run/nfd.sock")
	if err!= nil {
		log.Info("Run nfd please")
		return
	}

	key, _ := crypto.GenerateKey()
	signer := ndnapp.EcdsaSigningFromPrivateKey(key)
	mixer := ndnapp.NewMixer(conn, ndn.ParseName("/x1carbon"),signer)
	mixer.PRegister(ndnapp.NewProducer("/x1carbon/test", func(i *ndn.Interest) {
		 d := ndn.MakeData(i.Name, []byte("response message"))	
		 mixer.Sender().SendData(&d)
	}))
	timer := time.NewTimer(10*time.Second)
	<- timer.C
	mixer.Stop()
	log.Info("Bye")
	*/
}

func p2pnet() {
	/*
	log.Info("Hello!")
	num,_ := strconv.ParseInt(os.Args[1],10,0)
	mrand.Seed(time.Now().Unix())
	done := make(chan bool,1)
	sigs := make(chan os.Signal,1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	peers := Readpeers("peers.json")
	bpeers := Readpeers("bootnodes.json")
	bootnodes := make([]p2p.NodeRecord, len(bpeers))
	i := 0
	for _, bn := range bpeers {
		if r, err := p2p.CreateUntrustedNdnNodeRecord(bn.Id, bn.Prefix); err == nil {
			bootnodes[i] = r
			i++
		}
	}
	bootnodes = bootnodes[:i]
	if len(bootnodes) == 0 {
		log.Info("failed bootnodes")
	}
	go func(peers []*Peer) {
		nodes := make([]*p2p.Server, num)
		for i:=0; i<int(num); i++ {
			conn, _ := net.Dial("unix", "/run/nfd.sock")
			config := p2p.Config{
				PrivateKey:	peers[i].key,
				HostName:ndn.ParseName(peers[i].Prefix),
				AppName: ndn.ParseName("/kad"),
				Bootnodes:	bootnodes,
			}

			nodes[i] = p2p.NewServer(config,conn)
			nodes[i].Start()
		}

		<- sigs
		var wg sync.WaitGroup
		for _, a := range nodes {
			go func(p *p2p.Server, iwg *sync.WaitGroup) {
				iwg.Add(1)
				defer iwg.Done()
				p.Stop()
			}(a, &wg)
//			a.Stop()
		}
		wg.Wait()
		done <- true
	}(peers)

	 <- done
	 timer := time.NewTimer(5*time.Second)
	 <- timer.C
	 log.Info("Bye")
	 */
}


func genpeers() {
	divisions := map[string][]string {
		"etri": []string{"iot", "network","bigdata", "blockchain", "optical", "ai"},
		"ucla": []string{"cs", "ee", "biology", "chemistry","nuclear"},
		"kisti": []string{"supercomp", "bigdata", "datascience", "information"},
		"kaist":[]string{"ee", "cs", "id", "chemistry", "biology","me"},
	}

	k :=0
	for domain, subdomains := range divisions {
		filename := fmt.Sprintf("%speers.json", domain)
		Writepeers(Generatepeers(100,k,subdomains, domain), filename)
		k = k+100
	}
}

   
func genesis() {
	genesis := map[string]interface{} {
		"coinbase"   : "0x0000000000000000000000000000000000000000",
  		"difficulty" : "0x1000",
  		"extraData"  : "",
  		"gasLimit"   : "0x1406F40",
  		"nonce"      : "0x0000000000000042",
  		"mixhash"    : "0x0000000000000000000000000000000000000000000000000000000000000000",
  		"parentHash" : "0x0000000000000000000000000000000000000000000000000000000000000000",
  		"timestamp"  : "0x00",
	}
	genesis["config"] = map[string]interface{} {
		"chainId": 2020,
    	"homesteadBlock": 0,
    	"eip155Block": 0,
   	 	"eip158Block": 0,
    	"eip150Block": 0,
    	"eip150Hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

	accounts := make([]interface{},10)
	allocs := make(map[string]interface{})
	for i:=0; i<10; i++ {
		k, _ := crypto.GenerateKey()
		ks := hex.EncodeToString(crypto.FromECDSA(k))
		a := crypto.PubkeyToAddress(k.PublicKey).String()
		accounts[i] = map[string]interface{} {
			"key":	ks,
			"address":	a,
		}
		allocs[a] = map[string]string {
			"balance" : "99999999999999999999",
		}
	}
	genesis["alloc"] = allocs
	gb, _ := json.MarshalIndent(genesis,"","  ")
	ioutil.WriteFile("genesis.json", gb ,0644)

	ab, _ := json.MarshalIndent(accounts,"","  ")
	ioutil.WriteFile("accounts.json", ab ,0644)
}

func nodekeys(num int) {
	nodekeys := make([]interface{},num)
	for i:=0; i<num; i++ {
		k, _ := crypto.GenerateKey()
		ks := hex.EncodeToString(crypto.FromECDSA(k))
		a := p2p.IdFromPrivateKey(k).String()
		nodekeys[i] = map[string]interface{} {
			"key":	ks,
			"address":	a,
		}
	}

	ab, _ := json.MarshalIndent(nodekeys,"","  ")
	ioutil.WriteFile("nodekeys.json", ab ,0644)

}
