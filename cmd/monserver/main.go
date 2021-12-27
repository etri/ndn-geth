package main

import (
	//"bytes"
	"fmt"
	"os"
//	"os/signal"
	"io/ioutil"
	"sync"
//	"syscall"
	"net"
	"net/http"
	"time"
	"math/rand"
	"encoding/json"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/ndn/ndnapp"
	"github.com/ethereum/go-ethereum/ndn/utils"
	ethndn "github.com/ethereum/go-ethereum/eth/ndn"
	"github.com/ethereum/go-ethereum/eth/ndn/jsonrpc2"
	"github.com/gorilla/mux"
)
const (
	REFRESH_PERIOD = 1000 //millisecond
	NODE_REQUEST_PERIOD = 5000 //millisecond
)

type Network struct {
	NodeList		map[string]*Node
	id2name			map[string]string
	mutex			sync.Mutex
}
type NetworkInfo struct {
	NumMiners	uint16
	Height		uint64
	Incoming	uint64
	Outgoing	uint64
	NodeList	[]NodeInfo
}

type NodeInfo struct {
	Name		string
	Id			string
	Peers		uint8
	NumBlocks	uint64
	LastBlock	string
}

func (info *NetworkInfo) load(nodelist map[string]*Node) {
	info.NumMiners = 0
	info.Height = 0
	info.Incoming = 0
	info.Outgoing = 0
	info.NodeList = make([]NodeInfo, len(nodelist))
	i := 0
	for _, node := range nodelist {
		if node == nil {
			continue
		}

		if node.IsMining {
			info.NumMiners++
		}
		info.Incoming += node.Incoming
		info.Outgoing += node.Outgoing
		if info.Height < node.NumBlocks {
			info.Height = node.NumBlocks
		}
		info.NodeList[i] = NodeInfo {
			Name:		node.Prefix,
			Id:			node.Id,
			Peers:		uint8(len(node.Peers)),
			NumBlocks:	node.NumBlocks,
			LastBlock:	node.Head.String(),
		}
		i++
	}
	info.NodeList = info.NodeList[:i]
}

func (net *Network) load(fn string) error {
	if f, err := os.Open(fn); err != nil {
		return err
	} else {
		if wire, err := ioutil.ReadAll(f); err != nil {
			return err
		} else {
			var nodelist []string
			if err := json.Unmarshal(wire, &nodelist); err != nil {
				return err
			} else {
				for _, n := range nodelist {
					net.NodeList[n] = nil
				}
			}
		}

	}
	return nil
}

func (net *Network) update(info *ethndn.NodeInfo) {
	net.mutex.Lock()
	defer net.mutex.Unlock()

	if node, ok := net.NodeList[info.Prefix]; ok {
		if node == nil {
			//fmt.Printf("%s is created\n", info.Prefix)
			net.NodeList[info.Prefix] = NewNode(info)
			net.id2name[info.Id] = info.Prefix
		} else {
			//fmt.Printf("%s is updated\n", info.Prefix)
			node.update(info)
		}
		for _, p := range info.Peers {
			if _, has := net.NodeList[p.Name]; !has {
				net.NodeList[p.Name] = nil
				net.id2name[p.Id] = p.Name
			}
		}
	} else {
		fmt.Printf("%s can not be found\n", info.Prefix)
	}
}

func (net *Network) NetInfo() *NetworkInfo {
	var info NetworkInfo
	info.load(net.NodeList)
	return &info	
}

func (net *Network) drop(id string) {
	if node, ok := net.NodeList[id]; ok {
		delete(net.id2name, node.Id)
		delete(net.NodeList, id)
	}
}

type Node struct {
	ethndn.NodeInfo
	expired		time.Time
	mutex		sync.Mutex
}

func NewNode(info *ethndn.NodeInfo) *Node{
	return &Node{
		NodeInfo: 	*info,
		expired:	time.Now().Add(NODE_REQUEST_PERIOD*time.Millisecond),
	}
}

func (n *Node) Recent() bool {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	return time.Now().Before(n.expired)
}
func (n *Node) update(info *ethndn.NodeInfo) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	n.expired = time.Now().Add(NODE_REQUEST_PERIOD*time.Millisecond)
	n.NodeInfo = *info
}


func main() {
	if len(os.Args) != 3 {
		return
	}

	fn := os.Args[2]
	sock := os.Args[1]
	server := NewMonServer(fn, sock)
	if server == nil {
		return
	}
	//sigs := make(chan os.Signal,1)
	//signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/netinfo", server.getnetinfo)
	router.HandleFunc("/nodeinfo/{id}", server.getnodeinfo)
	router.HandleFunc("/blocks/{id}", server.getblocks)
	router.HandleFunc("/txs/{id}", server.gettxs)

	fmt.Printf("listenting\n")
	http.ListenAndServe(":7888", router)
	//do something here
	//<- sigs
	fmt.Printf("stop monitoring server\n")
	server.Stop()
	fmt.Println("bye")
}

type MonServer struct {
	network		Network
	quit		chan bool
	done		bool
	pool		utils.WorkerPool
	client		*ethndn.RpcClient
	mutex		sync.Mutex
}

func NewMonServer(fn string, sock string) *MonServer {
	ret := &MonServer{
		quit:		make(chan bool),
		done:		false,
		pool:		utils.NewWorkerPool(10),
		network:	Network{
			NodeList:	make(map[string]*Node),
			id2name:	make(map[string]string),
		},
	}
	if err := ret.network.load(fn); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load node list: %s\n", err.Error())
		return nil
	}
	//1. create NDN face
	conn, err := net.Dial("unix", sock)
	if err!= nil {
		fmt.Fprintf(os.Stderr, "NFD is not running\n")
		return nil
	}

	rcvCh := make(chan *ndn.Interest)
	f := ndnapp.NewFace(conn, rcvCh)
	ret.client = ethndn.NewRpcClient(f)

	go ret.mainloop()
	return ret
}

func (mon *MonServer) getRpcUrl(id string) string {
	if node, ok := mon.network.id2name[id]; ok {
		return fmt.Sprintf("%s/blockchain/ethrpc", node)
	}
	return ""
}

func (mon *MonServer) getnetinfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	info := make(map[string]interface{})
	info["result"] = mon.network.NetInfo()
	info["jsonrpc"] = "2.0"
	info["id"] = rand.Uint32()
	wire, _ := json.Marshal(info)
	w.Write(wire)
}

func (mon *MonServer) rpcrequest(call *jsonrpc2.Call, w http.ResponseWriter, r * http.Request) []byte {
	vars := mux.Vars(r)
	key := vars["id"]
	nodename := mon.getRpcUrl(key)
	if len(nodename) > 0 {
		if rep, err := mon.client.Request(call, ndn.ParseName(nodename)); err == nil {
			if err := rep.Err(); err == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				wire, _ := rep.MarshalJSON()
				w.Write(wire)
				return rep.Result()
			}
		} else {
			//drop node
			mon.network.drop(key)
		}
	}
	w.WriteHeader(http.StatusNotFound)
	return nil
}

func (mon *MonServer) getnodeinfo(w http.ResponseWriter, r *http.Request) {
	call,_ := jsonrpc2.NewCall(jsonrpc2.NewIntID(int64(rand.Uint32())), "getnodeinfo", []interface{}{})
	if wire := mon.rpcrequest(call, w, r); len(wire) >0 {
		var info ethndn.NodeInfo	
		if err := json.Unmarshal(wire, &info); err == nil {
			mon.network.update(&info)
		} 	
	}
}


func (mon *MonServer) gettxs(w http.ResponseWriter, r *http.Request) {
	call,_ := jsonrpc2.NewCall(jsonrpc2.NewIntID(int64(rand.Uint32())), "getlatesttxs", []interface{}{})
	mon.rpcrequest(call, w, r)
}

func (mon *MonServer) getblocks(w http.ResponseWriter, r *http.Request) {
	call,_ := jsonrpc2.NewCall(jsonrpc2.NewIntID(int64(rand.Uint32())), "getlatestblocks", []interface{}{})
	mon.rpcrequest(call, w, r)
}

func (mon *MonServer) mainloop() {
	rand.Seed(time.Now().UnixNano())

	timer  := time.NewTimer(REFRESH_PERIOD*time.Millisecond)
	LOOP:
	for {
		select {
		case <-timer.C:
			for id, n := range mon.network.NodeList {
				if n == nil || (n != nil  && !n.Recent()) {
					//make a request
					call,_ := jsonrpc2.NewCall(jsonrpc2.NewIntID(int64(rand.Uint64())), "getnodeinfo", []interface{}{})
					job := NewRequestingJob(call, id, mon.onRequestDone, mon.client)
					if mon.pool.Do(job) != true {
						break
					}
				}
			}
			timer.Reset(REFRESH_PERIOD*time.Millisecond)	
		case <- mon.quit:
			break LOOP
		}
	}

	mon.mutex.Lock()
	mon.done = true
	mon.mutex.Unlock()

	mon.pool.Stop()
}

func (mon *MonServer) Stop() {
	close(mon.quit)
}

func (mon *MonServer) onRequestDone(job *requestingJob) {
	mon.mutex.Lock()
	defer mon.mutex.Unlock()
	if mon.done {
		return
	}

	if job.err == nil &&  job.rep.Err() == nil {
		var info ethndn.NodeInfo	
		if err := json.Unmarshal(job.rep.Result(), &info); err == nil {
			mon.network.update(&info)
			return
		}
	}
	//mon.network.drop(job.node)
}


type requestingJob struct {
	node	string
	client	*ethndn.RpcClient
	call	*jsonrpc2.Call
	rep		*jsonrpc2.Response
	f		func(*requestingJob)
	err		error
}

func NewRequestingJob(call *jsonrpc2.Call, node string, f func(*requestingJob), c *ethndn.RpcClient) *requestingJob {
	return &requestingJob{
		call:	call,
		node:	node,
		f:		f,
		client:	c,
	}
}

func (job *requestingJob) Execute() {
	//request
	//fmt.Printf("request %s\n", job.node)
	name := fmt.Sprintf("%s/blockchain/ethrpc", job.node)
	job.rep, job.err= job.client.Request(job.call, ndn.ParseName(name))
	job.f(job)
}

