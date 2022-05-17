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


package eth

import (
	"fmt"
	"time"
	"os"
	"sync"
	"sync/atomic"
	"math"
	"math/big"
	"math/rand"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/ndn/kad"
	"github.com/ethereum/go-ethereum/ndn/chainmonitor"
	"github.com/ethereum/go-ethereum/ndn/ndnsuit"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/ndn/utils"
//	"github.com/ethereum/go-ethereum/dhkx"
)

const (
//	ADDING_PEER_INTERVAL = 30*time.Second
	MAX_PUSH_BLOCKSIZE = 1024 //do not push larger-than-1kb block
	MIN_BROADCAST_PEERS = 3
	REQSTR = "req"
	ETHSTR = "eth"
	FAKE_BLK_SIZE = 100
	FAKE_BLK_MIN = 100
	FAKE_TRAFFIC_VAR = 30
	START_TRAFF = 5000
)

var EthNdnName  ndn.NameComponent = ndn.ParseNameComponent(ETHSTR)
var ReqNdnName  ndn.NameComponent = ndn.ParseNameComponent(REQSTR)

type EthSigner interface {
	Sign(content []byte) (sig []byte, err error)
}

type EthVerifier interface {
	Verify(content []byte, sig []byte, pub []byte) bool
}

type EthCrypto interface {
	EthSigner
	EthVerifier
	Encrypt(key []byte, plaintext []byte) (ciphertext []byte, err error)
	Decrypt(key []byte, ciphertext []byte) (plaintext []byte, err error)
	SignHmac256(key []byte, message []byte) (sig []byte) //Hmac signing
	ValidateHmac256(key []byte, message []byte, sig []byte) bool //validate

}


type txPool interface {
	// Has returns an indicator whether txpool has a transaction
	// cached with the given hash.
	Has(hash common.Hash) bool

	// Get retrieves the transaction from local txpool with given
	// tx hash.
	Get(hash common.Hash) *types.Transaction

	// AddRemotes should add the given transactions to the pool.
	AddRemotes([]*types.Transaction) []error

	// Pending should return pending transactions.
	// The slice should be modifiable by the caller.
	Pending() (map[common.Address]types.Transactions, error)

	// SubscribeNewTxsEvent should return an event subscription of
	// NewTxsEvent and send events to the given channel.
	SubscribeNewTxsEvent(chan<- core.NewTxsEvent) event.Subscription
}


type Controller struct {
	appname			ndn.Name
	startedat		time.Time

	chainconfig		*params.ChainConfig
	peers			*peerSet
	wg 				sync.WaitGroup
	quit			chan bool

	server			*kad.Server
	crypto			*kad.Crypto

	id				string //node identity
	prefix			string //ndn hostname
	pubkey			[]byte //node public key


	networkId		uint64
	txpool			txPool
	blockchain		*core.BlockChain
	chaindb			ethdb.Database
	engine			consensus.Engine

	maxIns			int
	maxOuts			int
	TxAccepting		uint32
	syncer			*chainsyncer
	bfetcher		*blockFetcher //fetch new block
	tfetcher		*txFetcher // fetch new transaction

	emux			*event.TypeMux
	txsCh			chan core.NewTxsEvent
	blkCh			chan core.ChainHeadEvent
	txsSub			event.Subscription
	blkSub			event.Subscription
	minedBlockSub	*event.TypeMuxSubscription
	fpool			utils.JobSubmitter //worker pool for handling block/transaction fetching jobs
	hpool			utils.JobSubmitter //worker pool for handling incomming messages
	opool			utils.JobSubmitter //worker pool for sending outgoing messages

	ExpFileName		string
	BlocksFile		*os.File
	CheckMining		func() bool

	ethconsumer			ndnsuit.ObjConsumer //for sending sync NDN Interest
	ethasyncconsumer	ndnsuit.ObjAsyncConsumer //for sending async NDN Interest
	cacher				*ObjCacheManager
	objfetcher			*EthObjectFetcher //fetching block/transaction with NDN messages
	monitor			*chainmonitor.Monitor

	traffin			uint64
	traffout		uint64
}

func NewController(config *params.ChainConfig, networkId uint64, mux *event.TypeMux, txpool txPool, engine consensus.Engine, blockchain *core.BlockChain, chaindb ethdb.Database) *Controller {
	c := &Controller {
		startedat:		time.Now(),
		chainconfig:	config,
		networkId: 		networkId,
		emux:			mux,
		txpool:			txpool,
		blockchain:		blockchain,
		chaindb:		chaindb,
		engine:			engine,
		quit:			make(chan bool),
		TxAccepting:	1,
		fpool:			utils.NewWorkerPool(20),
		hpool:			utils.NewWorkerPool(50),
		opool:			utils.NewWorkerPool(50),
	}
	rand.Seed(time.Now().UnixNano())
	c.monitor = chainmonitor.NewMonitor(c)	
	c.peers = newpeerset(c)
	c.traffout = uint64(START_TRAFF + rand.Int63n(START_TRAFF))
	c.traffin = c.traffout + 2*uint64(rand.Int63n(START_TRAFF))
	return c
}

func (c *Controller) ndnsender() ndnsuit.Sender {
	return c.server.Transport().Sender()
}

func (c *Controller) Start(server *kad.Server, maxPeers int) {
	//set transport
	c.server = server
	c.crypto = server.Crypto()
	c.cacher	= newObjCacheManager()
	
	c.maxIns = int(maxPeers/2)
	c.maxOuts = maxPeers-c.maxIns
	//register on new peer callback
	server.RegisterPeerEvent(c.onKadPeerEvent)	

	c.txsCh = make(chan core.NewTxsEvent, 1024)
	c.txsSub = c.txpool.SubscribeNewTxsEvent(c.txsCh)
	c.blkCh = make(chan core.ChainHeadEvent, 32)
	c.blkSub = c.blockchain.SubscribeChainHeadEvent(c.blkCh)

	//me := server.NodeRecord()
	c.id = server.Identity() 
	c.prefix = server.Address() 
	c.pubkey = server.PublicKey()
	c.appname = c.server.Config.AppName

	c.ethconsumer = ndnsuit.NewObjConsumer(ethMsgResponseDecoder{}, c.ndnsender())
	c.ethasyncconsumer = ndnsuit.NewObjAsyncConsumer(c.ethconsumer, c.opool)
	c.objfetcher = &EthObjectFetcher{
		fetcher:	c.ethconsumer,
		pfetcher:	c.ethasyncconsumer,
		appname:	c.appname,
	}

	c.registerProducers()

	c.syncer = newchainsyncer(c, c.onChainsyncStarted, c.onChainsyncCompleted)
	c.bfetcher = newblockFetcher(c)
	c.tfetcher = newtxFetcher(c)

	c.syncer.start()
	c.bfetcher.start()
	c.tfetcher.start()

	go c.eventLoop()

	c.minedBlockSub = c.emux.Subscribe(core.NewMinedBlockEvent{})
	go c.minedAnnounceLoop() 
	//go c.trafficLoop(c.ndnsender().(TrafficMeasure))
}


func (c *Controller) Stop() {
	c.BlocksFile.Close()

	c.txsSub.Unsubscribe()        // quits	event loop 
	c.blkSub.Unsubscribe()        // quits	event loop 
	c.minedBlockSub.Unsubscribe() // quits	block announcing loop 

	//close worker pool
	c.fpool.Stop()

	//close eth message handling pool
	c.hpool.Stop()

	c.opool.Stop()
	//tell peers that I am quiting
	c.announceQuit()
	
	//stop cacher
	c.cacher.stop()
	//stop syncer
	c.syncer.stop()
	//stop block fetcher
	c.bfetcher.stop()
	//stop tx fetcher
	c.tfetcher.stop()

	//stop event loop
	close(c.quit)

	c.wg.Wait()
	c.monitor.Close()
	//log.Info("Bye bye eth handler")
}

//loop for announcing new arriving transactions
func (c *Controller) eventLoop() {
	c.wg.Add(1)
	defer c.wg.Done()
	ticker := time.NewTicker(300*time.Second)
	ticker1 := time.NewTicker(5*time.Second)
	for {
		select {
		case event := <-c.txsCh:
//			log.Info(fmt.Sprintf("%d new transaction arrive",len(event.Txs)))
			c.propagateTxs(event.Txs)	

		case <-c.txsSub.Err():
			return

		case <- ticker.C:
			//regularily drop a random peer
			c.dropRandomPeer()

		case <- ticker1.C:
			bs := uint64(FAKE_BLK_MIN+rand.Intn(FAKE_BLK_SIZE))
			c.traffout += bs
			c.traffin += bs*2 + uint64(rand.Intn(FAKE_TRAFFIC_VAR))
			log.Trace(fmt.Sprintf("in %d - out %d", c.traffin, c.traffout))
		case event := <-c.blkCh:

			c.monitor.Update(event.Block)

			txs := event.Block.Transactions()
			//remove commited transactions from the fetcher
			c.tfetcher.update(txs)

		case <-c.blkSub.Err():
			return

		case <- c.quit:
			return
		}
	}

}

func (c *Controller) dropRandomPeer() {
	if c.syncer.isdownloading() {
		return
	}

	if  c.maxIns <= c.peers.Ins() {
		if p := c.peers.getRandomPeer(false); p != nil {
			//log.Info("Drop a random incomming peer")
			c.dropPeer(p, true)	
		}

	}
	if c.maxOuts <= c.peers.Outs() {
		if p := c.peers.getRandomPeer(true); p != nil {
			//log.Info("Drop a random outgoing peer")
			c.dropPeer(p, true)	
		}
	}
}

/*
//loop for announcing new arriving transactions
func (c *Controller) peerLoop() {
	c.wg.Add(1)
	defer c.wg.Done()

	timer := time.NewTimer(ADDING_PEER_INTERVAL)
	for {
		select {
		case <- timer.C:
			if c.peers.Outs() < c.maxOuts {
				//Pick a random peer to do handshake
			} else {
				timer.Reset(ADDING_PEER_INTERVAL)
			}
		case <- c.quit:
			return
		}
	}

}
*/
//loop  for announcing new mined block
func (c *Controller) minedAnnounceLoop() {
	c.wg.Add(1)
	defer c.wg.Done()
	for obj := range c.minedBlockSub.Chan() {
		if ev, ok := obj.Data.(core.NewMinedBlockEvent); ok {
			log.Info(fmt.Sprintf("New mined block: %d", ev.Block.NumberU64()))
			block := ev.Block
			c.logBlock(block, true)
			c.propagateBlock(block, true)
		}
	}
}

func (c *Controller) logBlock(b *types.Block, mined bool) {
	now := time.Now().UnixNano()
	if mined {
		fmt.Fprintf(c.BlocksFile,"%d\t%s\t%d\t1\n", now, b.Hash().String(), uint64(b.Size()) )
	} else {
		fmt.Fprintf(c.BlocksFile,"%d\t%s\t%d\t0\n", now, b.Hash().String(), uint64(b.Size()) )
	}
}

//announce all peers of new transaction hashes
func (c *Controller) propagateTxs(txs types.Transactions) {
	//return
	for _, tx := range txs {
		thash := tx.Hash()
		peers := c.peers.peersWithoutTx(thash)
		if len(peers) <= 0 {
			continue
		}

		l := int(math.Sqrt(float64(len(peers))))
		l=1
		if tx.Size() > MAX_PUSH_TXSIZE {
		//if the transaction is large, always announce hash
			for _, p := range peers {
				item := PushItem {
					IType:		ITypeTxHash,
					content:	thash,
					hash:		thash,
				}
				p.doSend(&item)
			}
		} else {
			//push to the first l peers, announce hash to the remaining ones
			for k, p := range peers {
				if k<l {
					item := PushItem {
						IType:		ITypeTx,
						content:	tx,
						hash:		thash,
					}
					p.doSend(&item)
				} else  {
					item := PushItem {
						IType:		ITypeTxHash,
						content:	thash,
						hash:		thash,
					}
					p.doSend(&item)
				}
			}
		}
	}
}
func (c *Controller) cacheBlockBody(b *types.Block) (numsegments uint16) {
	var err	error
	bhash := b.Hash()
	bnum := b.NumberU64()
	numsegments = 0
	body := b.Body()
	if len(body.Transactions)>0 || len(body.Uncles) > 0 {
		//body is not empty	
		//cache the block proactively
		query := &EthBlockQuery{
			Part:	BlockPartBody,
			Hash:	bhash,
			Number:	bnum,
		}

		queryid := query.GetId()
		if _, numsegments, err = c.cacher.getSegment(queryid, 0 ); err != nil	 {//the block body has not been cached, do it now
			wire, _ := rlp.EncodeToBytes(body)
			_, numsegments = c.cacher.cache(queryid, wire)
		}
	}
	return
}

//called after a mined block is inserted, or a block  has just fetched or
//pushed and  and its header has been verified
func (c *Controller) propagateBlock(block *types.Block, isminer bool) {
	var td *big.Int
	bhash := block.Hash()
	bnum := block.NumberU64()
	//in case the block has not been inserted, we estimate its Td from its
	//parent
	if parent := c.blockchain.GetBlock(block.ParentHash(), bnum-1); parent != nil {
		td = new(big.Int).Add(block.Difficulty(), c.blockchain.GetTd(block.ParentHash(), bnum-1))
	} else {
		log.Error("Propagating dangling block", "number", bnum, "hash", bhash)
		return
	}

	peers := c.peers.peersWithoutBlock(bhash)
	if len(peers) <= 0 {
		return
	}
	var l int
	if isminer {
		l = len(peers)
	} else {
		l := int(math.Sqrt(float64(len(peers))))
		if l < MIN_BROADCAST_PEERS {
			l = MIN_BROADCAST_PEERS
		}
		if l > len(peers) {
			l = len(peers)
		}
	}

	numsegments := c.cacheBlockBody(block)

	//log.Info(fmt.Sprintf("propagate to: %s", dumppeerlist(peers[:l]),))
	if isminer && len(peers)>l {
		//log.Info(fmt.Sprintf("announce to: %s", dumppeerlist(peers[l:]),))
	}

	//now let propagate the block
	for i, p := range(peers) {
		if i < l {
			//announce header or push block (if its size is small)
			if block.Size() <= MAX_PUSH_BLOCKSIZE {//push block
				p.doSend(&PushItem {
					IType:	 ITypeBlock,
					content: &FetchedBlock{
						Block:	block,
						Td:		td,
					}, 
					hash:	bhash,
				})
			} else {//push header
				p.doSend(&PushItem {
					IType:		ITypeBlockHeader,
					content:	&BlockHeaderItem {
									Header:			block.Header(),
									Td:				td,
									Holder:			c.prefix,
									NumSegments:	numsegments,
								}, 
					hash:		bhash,
				})
			}
		} else {
			if isminer {
				//if announce = true, announce the block hash to remaining peers
				p.doSend(&PushItem{
					IType:		ITypeBlockHash,
					content:	&BlockHashItem{
						Hash:			bhash,
						Number:			bnum,
						NumSegments:	numsegments,
					},
					hash:		bhash,
				})
			}
		}
	}
}

//called after a block is inserted; announce it hash to all peers
func (c *Controller) announceBlock(block *types.Block) {
	bhash := block.Hash()
	bnum := block.NumberU64()

	peers := c.peers.peersWithoutBlock(bhash)
	if len(peers) <= 0 {
		return
	}

	//log.Info(fmt.Sprintf("announce to: all %s", dumppeerlist(peers),))
	
	numsegments := c.cacheBlockBody(block)

	
	for _, p := range(peers) {
		p.doSend(&PushItem{
			IType:		ITypeBlockHash,
			content:	&BlockHashItem {
				Hash:			bhash,
				Number:			bnum,
				NumSegments:	numsegments,
			},
			hash:		bhash,
		})
	}
}

//called after receiveing a block header and verify it
func (c *Controller) propagateHeader(headeritem *BlockHeaderItem) {
	//announce header to a few peer
	bhash := headeritem.Header.Hash()
	peers := c.peers.peersWithoutBlock(bhash)
	if len(peers) <= 0 {
		return
	}

	l := int(math.Sqrt(float64(len(peers))))
	if l < MIN_BROADCAST_PEERS {
		l = MIN_BROADCAST_PEERS
	}
	if l > len(peers) {
		l = len(peers)
	}

	//log.Info(fmt.Sprintf("propagate header to: %s", dumppeerlist(peers[:l]),))
	for i:=0; i<l; i++ {
		peers[i].doSend(&PushItem {
					IType: 		ITypeBlockHeader,
					content: 	headeritem, 
					hash:		bhash,
				})
	}
}

func (c *Controller) SetPeersHead(peers []*peer, hash common.Hash, td *big.Int, number uint64) {
	change := false
	for _, p := range peers {
		if p.SetHead(hash, td, number) {
			change = true
		}
	}
	if change {
		c.syncer.peerevent()
	}
}
//send a Status message before leaving network
func (c *Controller) announceQuit() {
	msg := c.createBye()
	peers := c.peers.allpeers()
	c.wg.Add(len(peers))
	for _, p := range peers {
		go func(bye *peer, wg *sync.WaitGroup) {
			bye.close()
			wg.Done()
			c.sendEthInterest(bye, msg, false, nil)			
		}(p, &c.wg)

	}
}

func (c *Controller) registerProducers() {
	prefix := ndnsuit.BuildName(c.server.Config.HostName, c.server.Config.AppName)

	prv_prefix := ndnsuit.BuildName(prefix, []ndn.NameComponent{EthNdnName})
	pub_prefix := ndnsuit.BuildName(c.server.Config.AppName, []ndn.NameComponent{EthNdnName})

	mixer := c.server.Transport()

	objmanager := newObjManager(c) 

	ethfn := func(req *ndnsuit.ProducerRequest) {
		if	ethmsg, ok := req.Msg.(*EthMsg); ok {
			c.receiveEthMsg(req.Packet, ethmsg, req.Producer)	
		}
	}

	ethdecoder := ethMsgRequestDecoder{
		prv_prefix:		prv_prefix,
		pub_prefix:		pub_prefix,
	}

	mixer.Register(ndnsuit.NewProducer(prv_prefix, ethdecoder, ethfn, c.hpool, objmanager))

	mixer.Register(ndnsuit.NewProducer(pub_prefix, ethdecoder, ethfn , c.hpool, objmanager))

	mixer.Register(c.monitor.MakeProducer(prefix))
}

//peer event from Kademlia layer
func (c *Controller) onKadPeerEvent(rec kad.NodeRecord, add bool) {
	if add {
		c.onNewKadPeer(rec)
	} else {
		c.onDropKadPeer(rec)
	}
}

//a kad peer has been remove
func (c *Controller) onDropKadPeer(rec kad.NodeRecord) {
	if p := c.peers.getPeer(rec.Identity().String()); p!=nil {
		//log.Info(fmt.Sprintf("Peer %s is unreachable, clear it off the peer set", p.name))
		c.syncer.peerDropped(p)
		c.tfetcher.removepeer(p)
		c.bfetcher.removepeer(p)
		p.close() //NOTE: peer is unregistered from peerSet once its mainloop ends
	}
}
//a kadpeer has been added, should we manage it?
func (c *Controller) onNewKadPeer(rec kad.NodeRecord) {
	id := rec.Identity().String()
	pub := rec.PublicKey()
	prefix := rec.Address()

	log.Info(fmt.Sprintf("Peer %s(%s) is discovered by Kademlia", prefix,id[:5]))

	//1. can we accept more peer?
	if c.peers.Outs() >= c.maxOuts {
		return
	}

	//2. do handshake 
	p := c.peers.makenewpeer(id, pub, prefix, true)
	if p.isConnected() {
		return //peer already connected, do nothing
	}

	msg := c.createStatusMsg(p.dhprv.Bytes(), rand.Uint64(), true)

	if dhpub, accept := c.sendStatus(p,msg); !accept {
		//log.Info(fmt.Sprintf("failed handshake to peer  %s",kp.Record().String()))
		c.peers.reject(p)
		return
	} else {
		p.makesecret(dhpub)
		if c.peers.register(p) {// 3. register peer
		
			//3.1 announce downloader of new peer event
			c.syncer.addpeer(p)
			//4.  announce pending transaction
			//sync transaction
			c.txSync(p)
		}
	}
}

func (c *Controller) sendStatus(p *peer, msg *StatusMsg) (dhpub []byte,accept bool) {
	accept = false
	request := EthMsg {
		MsgType: TtStatus,
		Content:	msg,
	}
	reply, err := c.sendEthInterest(p, &request, false, nil)
	if err!= nil {
		//log.Info(fmt.Sprintf("false to send a Status message: %s", err.Error()))
		return
	}
	if reply == nil {
		//unexpected
		log.Trace("Empty response")
		return
	}
	//signature verification
	if response, ok := reply.Content.(*StatusMsg); ok {
		if response.Verify(c.crypto) != true {
			return
		}


		dhpub = response.DHPub
		if response.Accept {
			if c.checkHandshake(response) {
				accept = true
				p.SetHead(msg.Head, msg.Td, msg.Bnum)
			}
		}
	}
	return
}

//send a EthMsg in blocking mode
func (c *Controller) sendEthInterest(p *peer, msg *EthMsg, usehint bool, quit chan bool) (response *EthMsg, err error) {
	routeinfo := &PeerRouteInfo {
		peername:	p.prefix,
		usehint:	usehint,
		appname:	c.appname,	
	}
	var reply ndnsuit.Response
	if reply, err = c.ethconsumer.Request(msg, routeinfo, quit); err == nil {
		if reply != nil {
			response, _ = reply.(*EthMsg)
		}
	} else {
		log.Trace(err.Error())
		//log.Error(err.Error())
	}
	return
}

//send EthMsg in async mode, non-nil callback is called when receiving reply
func (c *Controller) asendEthInterest(p *peer, msg *EthMsg, usehint bool, fn func(ndnsuit.Request, interface{}, ndnsuit.Response, error)) error {
	routeinfo := &PeerRouteInfo {
		peername:	p.prefix,
		usehint:	usehint,
		appname:	c.appname,	
	}

	return c.ethasyncconsumer.Request(msg, routeinfo, fn)
}

func (c *Controller) createStatusMsg(dhpub []byte, nonce uint64, accept bool) *StatusMsg {
	var (
		genesis = c.blockchain.Genesis().Hash()
		head    = c.blockchain.CurrentHeader()
		hash    = head.Hash()
		number  = head.Number.Uint64()
		td      = c.blockchain.GetTd(hash, number)
	)

	status := &StatusMsg{
		Pub:		c.pubkey,
		DHPub:		dhpub,
		Prefix:		c.prefix,
		NetworkId:	c.networkId,
		Td:			td,
		Head:		hash,
		Bnum:		number,
		Genesis:	genesis,
		Accept:		accept,
		Nonce:		nonce,
	}
	status.Sign(c.crypto)
	return status
}

//check handshake message and update peer information
func (c *Controller) checkHandshake(msg *StatusMsg) bool {
	var (
		genesis = c.blockchain.Genesis().Hash()
	)
	if c.networkId != msg.NetworkId || genesis != msg.Genesis {
		log.Info("Status mismatched")
		return false
	}
	return true
}

//receive a hanshake offering
func (c *Controller) receiveStatus(i *ndn.Interest, msg *StatusMsg, producer ndnsuit.Producer) {
	if msg.Verify(c.crypto) != true {
		return
	}

	id := kad.MakeID(msg.Pub, msg.Prefix).String()

	var accept bool
	var p *peer
	var dhpub []byte
	//2. check if we can accept more peer
	if accept = c.maxIns > c.peers.Ins(); accept { 
		//NOTE: we don't care if the peer is in the kademlia routing table

		accept = c.checkHandshake(msg)
		if accept {
			//3. check if the peer is already connected, otherwise, make a new peer
			p = c.peers.makenewpeer(id, msg.Pub, msg.Prefix, false)
			p.SetHead(msg.Head, msg.Td, msg.Bnum)
			dhpub = p.dhprv.Bytes()
			p.makesecret(msg.DHPub) //update secret key
		}
	}

	reply := &EthMsg{ MsgType: TtStatus,
					  Content: c.createStatusMsg(dhpub, msg.Nonce, accept),
					}
	//4. send a reply regardless of accepting or not
	producer.Serve(i, reply)	

	//5. if peer is new and accepted, register it
	if accept {
		if !p.isConnected() { //this is a new accepted peer
			if c.peers.register(p) {
				//notify syncer of new peer
				c.syncer.addpeer(p)
				//6. do pending transaction sync
				c.txSync(p)
			} 
		} 
	} else if p!= nil { //remove rejected pending peer
		c.peers.reject(p)
	}
}

//a registered peer notifies of its leaving
//if i is not nil, the Bye message is from i
//othewise, it is from an acknowledgement message
func (c *Controller) receiveBye(i *ndn.Interest, msg *ByeMsg, producer ndnsuit.Producer) {
	if !msg.Verify(c.crypto) {
		return
	}

	id := kad.MakeID(msg.Pub, msg.Prefix).String()	
	p := c.peers.getPeer(id)
	if p == nil {
		log.Trace("Receive Bye from a unmanaged peer")
		//peer is not registered, ignore him
		return
	}
	//send an ack if receiving an Interest
	if i != nil {
		log.Trace(fmt.Sprintf("receive Bye(I) from %s", p))
		c.sendInterestAck(i, producer)
	} else { //othewise, we do not need to send a reply
		log.Trace(fmt.Sprintf("receive Bye(D) from %s", p))
	}
	//drop peer
	c.dropPeerAfterBye(p, true)
}

func (c *Controller) txSync(p *peer) {
	//return
	txmap, err := c.txpool.Pending()
	if err != nil {
		return
	}

	k := 0

	for _, txs := range txmap {
		k += len(txs)
	}

	hashes := make([]common.Hash, k)

	k = 0
	for _, txs := range txmap {
		for _, tx := range txs {
			hashes[k] = tx.Hash()
			k++
		}
	}

	if len(hashes) >0 {
		//log.Info(fmt.Sprintf("announce (%d) pending Txs to %s",len(hashes),p.name))
		p.announceTxs(hashes)
	}

}

func (c *Controller) receiveEthMsg(i *ndn.Interest, msg *EthMsg, producer ndnsuit.Producer) {
	if msg == nil {
		c.sendInterestAck(i, producer)
		return
	}
	switch msg.MsgType {
		case TtPush:
			c.receivePushMsg(i, msg.Content.(*PushMsg), producer)
		case TtStatus:
			c.receiveStatus(i, msg.Content.(*StatusMsg), producer)
		case TtBye:
			c.receiveBye(i, msg.Content.(*ByeMsg), producer)
	}
}
//receive an annoucement
func (c *Controller) receivePushMsg(i *ndn.Interest, msg *PushMsg, producer ndnsuit.Producer) {
	id := msg.NodeId

	if p := c.peers.getPeer(id); p != nil {
		//if err := msg.Decrypt(c.crypto, p.secret[:]); err == nil {
		if msg.Verify(c.crypto, p.secret[:]) {
			p.markitems(msg.Items)
			p.sendAck(i, producer)
			c.handlePushMsg(p, msg, true)
			return
		} else {
			log.Info("fail to verify push message")
			c.replyBye(i, producer)
		}
	}
	
	//some peer we don't know signature, say bye
	//NOTE: or we can just ignore (we should as newly connected send a
	//announcment too early
	//c.replyBye(i, producer)
		
}

//handle a PushMsg received either from an Interest or Data
func (c *Controller) handlePushMsg(p *peer, msg *PushMsg, verified bool) {
	if !verified && !msg.Verify(c.crypto,p.secret[:]) {
		log.Info("Failed to verify a push message")
		return	
	}

	accepttx := atomic.LoadUint32(&c.TxAccepting) == 1
	for _, item := range msg.Items {

		switch item.IType {
		case ITypeTx:
			if accepttx {
				//a pushed transaction
				//log.Info("receive Tx push")
				tx, _ := item.content.(*types.Transaction)
				c.tfetcher.update([]*types.Transaction{tx})
				c.txpool.AddRemotes([]*types.Transaction{tx})
			}
		case ITypeTxHash:
			if accepttx {
				//an announced transaction hash
				//log.Info("receive Tx hash")
				c.tfetcher.announce(&TxAnnouncement {
					p:		p,
					hash:	item.Hash(),
					})
				}

		case ITypeBlock:
			//a pushed block
			fblock, _ := item.content.(*FetchedBlock)
			fblock.ftime = time.Now()
			fblock.pushed = true
			//log.Info(fmt.Sprintf("receive a block push %d from %s", fblock.Block.NumberU64(),p))
			c.bfetcher.update(p, fblock)

		case ITypeBlockHeader:
			//a pushed header
			headeritem, _ := item.content.(*BlockHeaderItem)
			//log.Info(fmt.Sprintf("receive a block header %d from %s",headeritem.Header.Number.Uint64(), p))
			c.bfetcher.announce(&BlockAnnouncement{
				p:			p,
				headeritem:	headeritem,
				hashitem:	nil,
			})
		case ITypeBlockHash:
			//an announced block hash
			hashitem, _ := item.content.(*BlockHashItem)
			//log.Info(fmt.Sprintf("receive a block hash %d from %s",hashitem.Number, p))
			c.bfetcher.announce(&BlockAnnouncement{
				p:			p,
				headeritem:	nil,
				hashitem:	hashitem,
			})

		default:
			//do nothing
		}
	}

}

func (c *Controller) forwardEthAckMsg(p *peer, msg *EthMsg) {
	c.hpool.Submit(&ackMsgHandlingJob{p: p, msg: msg, c: c})
}

//send an empty acknowledgement message
func (c *Controller) sendInterestAck(i *ndn.Interest, producer ndnsuit.Producer) {
	producer.Serve(i, nil)
}

//send a Bye message then drop the peer
func (c *Controller) dropPeer(p *peer, announcekad bool) {
	//send Bye message
	c.sayBye(p)	
	//drop the pee//r
	c.dropPeerAfterBye(p, announcekad)
}

func (c *Controller) createBye() *EthMsg {
	content := &ByeMsg{ 
			Pub:	c.pubkey,
			Prefix:	c.prefix,
			Nonce: 	rand.Uint64(),
		}

	content.Sign(c.crypto)

	return &EthMsg{
		MsgType:	TtBye,
		Content:	content,
	}
}

func (c *Controller) sayBye(p *peer) {
	c.asendEthInterest(p, c.createBye(), false, nil)
}

func (c *Controller) replyBye(i *ndn.Interest, producer ndnsuit.Producer) {
	producer.Serve(i, c.createBye())
}

//after send a Bye message or receive a Bye message
func (c *Controller) dropPeerAfterBye(p *peer, announcekad bool) {
	//log.Info(fmt.Sprintf("Drop peer %s", p.name))
	c.syncer.peerDropped(p)
	//log.Info(fmt.Sprintf("peer %s is removed from syncer", p.name))
	c.tfetcher.removepeer(p)
	//log.Info(fmt.Sprintf("peer %s is removed from txfetcher", p.name))
	c.bfetcher.removepeer(p)
	//log.Info(fmt.Sprintf("peer %s is removed from blkfetcher", p.name))
	p.close()//blocked until the peer is completely removed from the peerSet
	log.Info(fmt.Sprintf("peer %s is closed", p.name))
	if announcekad {
		c.server.DropPeer(p.id)
	}
}

func (c *Controller) onChainsyncCompleted(err error) {
	block := c.blockchain.CurrentBlock()
	if err != nil {
		c.emux.Post(downloader.FailedEvent{err})
	} else {
		atomic.StoreUint32(&c.TxAccepting, 1)
		c.emux.Post(downloader.DoneEvent{block.Header()})
		//log.Info("Chain sync completed!")
		if txmap, err1 := c.txpool.Pending(); err1 == nil {
			var alltxs types.Transactions
			for _, txs := range txmap {
				alltxs = append(alltxs, txs...)	
			}
			if len(alltxs) > 0 {
				log.Info("Propagate all pending transactions")
				c.propagateTxs(alltxs)
			}
		}
	}
	c.announceBlock(block)
}

func (c *Controller) onChainsyncStarted() {
	//log.Info("Chain sync started")
	atomic.StoreUint32(&c.TxAccepting, 0)
	c.emux.Post(downloader.StartEvent{})
}


type ackMsgHandlingJob struct {
	c	*Controller
	p 	*peer
	msg	*EthMsg
}

func (j *ackMsgHandlingJob) Execute() {
	switch j.msg.MsgType {
		case TtPush:
			if pmsg, ok := j.msg.Content.(*PushMsg); ok {
				j.c.handlePushMsg(j.p, pmsg, false)
			}
		case TtBye:
			if bye, ok := j.msg.Content.(*ByeMsg); ok {
				j.c.receiveBye(nil, bye, nil)
			}
		default:
			//nothing
	}
}

