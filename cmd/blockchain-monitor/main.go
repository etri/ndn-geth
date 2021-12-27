package main

import (
	"bytes"
	"fmt"
	"net"
	"encoding/json"
	"os"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/ndn/ndnapp"
	ethndn "github.com/ethereum/go-ethereum/eth/ndn"
	"github.com/ethereum/go-ethereum/eth/ndn/jsonrpc2"
)

func main() {

}
func test() {

	//rand.Seed(time.Now().UnixNano())

	if len(os.Args) < 3 {
		cmdprint()
		os.Exit(1)
	}
	//1. parse node prefix
	nodeprefix := ndn.ParseName(os.Args[1])

	if len(nodeprefix) == 0 {
		fmt.Fprintf(os.Stderr, "error: %s is not a valid ndn prefix", os.Args[1])	
		os.Exit(1)
	}

	//2. parse json command
	msg, err := jsonrpc2.DecodeMessage([]byte(os.Args[2]))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		os.Exit(1)
	}
	cmd, ok := msg.(*jsonrpc2.Call)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: not a rpc call, ID field is missing\n")
		os.Exit(1)
	}


	//4. create NDN face
	conn, err := net.Dial("unix", "/run/nfd.sock")
	if err!= nil {
		fmt.Fprintf(os.Stderr, "NFD is not running\n")
		os.Exit(1)
		return
	}

	rcvCh := make(chan *ndn.Interest)
	f := ndnapp.NewFace(conn, rcvCh)
	client := ethndn.NewRpcClient(f)

	//3. make RPC call
	reply, err := client.Request(cmd, nodeprefix)
	
	//5. Print outcomes
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		os.Exit(1)
	}
	replywire,_ := reply.MarshalJSON()
	var pretty bytes.Buffer
	json.Indent(&pretty, replywire, "", "  ")
	fmt.Fprintf(os.Stdout, "%s\n", string(pretty.Bytes()))
}

func cmdprint() {
	fmt.Fprintf(os.Stderr, "%s node-prefix jsonrpc-command\n", os.Args[0])
}


