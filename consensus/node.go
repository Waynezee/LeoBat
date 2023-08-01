package consensus

import (
	"bytes"
	"encoding/json"
	"errors"
	"leobat-go/common"
	"leobat-go/crypto"
	"leobat-go/logger"
	"leobat-go/network"
	"leobat-go/utils"
	"sort"

	"net"
	"net/http"
	"net/rpc"
	"time"

	"github.com/gogo/protobuf/proto"
)

type executionState uint8

const (
	INIT     executionState = iota
	ALL_ONE                 // decide but not excute
	ALL_HALF                // decide but not excute
	PENGING                 // decide but not excute
	LEFT                    // after coin pass or ALL_HALF pass still have block not execute
	FINISH                  // 3f+1 blocks execute
	FAST_FAST
	ODDFAST
	ODDPENGING
	EVENFAST
	EVENPENGING
)

type Node struct {
	cfg            *common.Config
	network        network.NetWork
	networkPayList []network.NetWork
	peers          map[uint32]common.Peer
	logger         logger.Logger
	stop           bool
	identity       bool // true: byzantine; false: normal
	proposeRound   uint32
	oddStuckRound  uint32
	evenStuckRound uint32

	currRound  uint32
	readyRound uint32
	roundState map[uint32]uint8 // 0: before 2f+1 prepare; 1: after 2f+1 prepare
	execState  map[uint32]executionState
	nodeState  map[uint32]map[uint8]int                  // 0: 2f+1 nodes not connect; 1: 2f+1 nodes connect
	roundConn  map[uint32]map[uint32]map[uint32]struct{} // round r+1 -> round r
	connNum    map[uint32]map[uint32]int                 // round r -> round r+1
	weakLink   map[uint32]map[uint32][]common.Vertex     // round -> source <weakLink> round -> source
	weakWait   *utils.PriorityQueue
	hasWeak    map[uint32]map[uint32]struct{}

	// round -> sender -> from -> signature
	payloads         map[uint32]map[uint32][]byte
	pps              map[uint32]map[uint32]*common.Message
	prepares         map[uint32]map[uint32]map[uint32][]byte
	readys           map[uint32]map[uint32]map[uint32][]byte
	quorumReady      map[uint32]map[uint32]struct{} // get 2f+1 readys
	quorumPowerReady map[uint32]map[uint32]struct{} // get 3f+1 readys
	quorumCoin       map[uint32]map[uint32][]byte
	coins            map[uint32]uint32
	coinFlag         map[uint32]bool

	myBlocks []string                    // my payload hash
	pendings map[uint32]map[uint32]uint8 // 0: not execute; 1: execute

	payload  string
	mergeMsg map[uint32]map[uint32]common.PayloadIds
	sliceNum uint32

	msgChan          chan *common.Message
	proposeChan      chan []byte
	startChan        chan struct{}
	startProposeChan chan struct{}
	tryProposeChan   chan struct{}

	// statistics
	startTime          map[uint32]uint64
	executionLatencies map[uint32]uint64
	fastPassStates     map[executionState]int
	badCoin            int

	// client
	currBatch   *common.Batch
	clientChan  chan *common.ClientReq
	connection  *rpc.Client
	reqNum      int
	timeflag    bool
	proposeflag bool
	interval    int
	startId     int32
	blockInfos  []*common.BlockInfo
	excNum      int
}

func NewNode(cfg *common.Config, peers map[uint32]common.Peer, logger logger.Logger, conn uint32, identity bool) *Node {
	crypto.Init()

	node := &Node{
		cfg:            cfg,
		network:        nil,
		networkPayList: nil,
		peers:          peers,
		logger:         logger,
		stop:           false,
		identity:       identity,
		proposeRound:   1,
		oddStuckRound:  0,
		evenStuckRound: 0,

		currRound:        1,
		readyRound:       0,
		roundState:       make(map[uint32]uint8),
		execState:        make(map[uint32]executionState),
		nodeState:        make(map[uint32]map[uint8]int),
		roundConn:        make(map[uint32]map[uint32]map[uint32]struct{}),
		connNum:          make(map[uint32]map[uint32]int),
		weakLink:         make(map[uint32]map[uint32][]common.Vertex),
		hasWeak:          make(map[uint32]map[uint32]struct{}),
		payloads:         make(map[uint32]map[uint32][]byte),
		pps:              make(map[uint32]map[uint32]*common.Message),
		prepares:         make(map[uint32]map[uint32]map[uint32][]byte),
		readys:           make(map[uint32]map[uint32]map[uint32][]byte),
		quorumReady:      make(map[uint32]map[uint32]struct{}),
		quorumPowerReady: make(map[uint32]map[uint32]struct{}),
		quorumCoin:       make(map[uint32]map[uint32][]byte),
		coins:            make(map[uint32]uint32),
		coinFlag:         make(map[uint32]bool),
		pendings:         make(map[uint32]map[uint32]uint8),
		myBlocks:         nil,
		payload:          "",
		mergeMsg:         make(map[uint32]map[uint32]common.PayloadIds),
		sliceNum:         conn,

		msgChan:          make(chan *common.Message, 200000),
		proposeChan:      make(chan []byte, 10),
		startChan:        make(chan struct{}, 1),
		startProposeChan: make(chan struct{}, 1),
		tryProposeChan:   make(chan struct{}, 10),
		clientChan:       make(chan *common.ClientReq, 100),

		startTime:          make(map[uint32]uint64),
		executionLatencies: make(map[uint32]uint64),
		fastPassStates:     make(map[executionState]int),
		badCoin:            0,

		currBatch:   new(common.Batch),
		timeflag:    true,
		proposeflag: true,
		blockInfos:  make([]*common.BlockInfo, 0),
		startId:     1,
		reqNum:      0,
		excNum:      0,
	}

	node.network = network.NewNoiseNetWork(node.cfg.ID, node.cfg.Addr, node.peers, node.msgChan, node.logger, false, 0)
	for i := uint32(0); i < conn; i++ {
		node.networkPayList = append(node.networkPayList, network.NewNoiseNetWork(node.cfg.ID, node.cfg.Addr, node.peers, node.msgChan, node.logger, true, i+1))
	}
	node.weakWait = utils.NewPriorityQueue()
	return node
}

func (n *Node) Run() {
	n.startRpcServer()
	n.network.Start()
	for _, networkPay := range n.networkPayList {
		networkPay.Start()
	}
	go n.proposeLoop()
	n.mainLoop()
}

func (n *Node) startRpcServer() {
	rpc.Register(n)
	rpc.HandleHTTP()
	listener, err := net.Listen("tcp", n.cfg.RpcServer)
	if err != nil {
		panic(err)
	}
	go http.Serve(listener, nil)
}

func (n *Node) OnStart(msg *common.CoorStart, resp *common.Response) error {
	n.logger.Infoln("start")
	n.startChan <- struct{}{}
	return nil
}

func (n *Node) Request(req *common.ClientReq, resp *common.ClientResp) error {
	if req.StartId == 1 {
		n.interval = int(req.ReqNum)
		n.logger.Infoln("start test")
		n.startChan <- struct{}{}
		n.startProposeChan <- struct{}{}
	}
	n.clientChan <- req
	return nil
}

func (n *Node) mainLoop() {
	select {
	case <-n.startChan:
	}

	conn, err := rpc.DialHTTP("tcp", n.cfg.ClientServer)
	if err != nil {
		panic(err)
	}
	n.connection = conn

	timer := time.NewTimer(time.Second * time.Duration(n.cfg.Time))
	for {
		select {
		case msg := <-n.msgChan:
			n.handleMessage(msg)
		case payload := <-n.proposeChan:
			n.propose(payload)
		case <-timer.C:
			n.StopClient()
		}
	}
}

func (n *Node) proposeLoop() {
	select {
	case <-n.startProposeChan:
	}
	timer := time.NewTimer(time.Millisecond * time.Duration(n.cfg.MaxWaitTime))
	for {
		select {
		case <-timer.C:
			if n.proposeflag {
				n.logger.Debugf("time propose")
				if !n.identity {
					n.getBatch()
				}
				n.proposeflag = false
				n.timeflag = false
			} else {
				n.timeflag = true
			}
		case req := <-n.clientChan:
			n.currBatch.Reqs = append(n.currBatch.Reqs, req)
			n.reqNum += n.interval
		case <-n.tryProposeChan:
			n.getBatch()
			n.proposeflag = false
		}
	}
}

func (n *Node) getBatch() {
	if n.reqNum <= n.cfg.MaxBatchSize {
		payloadBytes, _ := proto.Marshal(n.currBatch)
		n.logger.Debugf("propose req num: %v", n.reqNum)
		n.currBatch.Reset()
		currBlock := &common.BlockInfo{
			StartID: n.startId,
			ReqNum:  int32(n.reqNum),
		}
		n.startId += int32(n.reqNum)
		n.reqNum = 0
		n.blockInfos = append(n.blockInfos, currBlock)
		n.proposeChan <- payloadBytes
	} else {
		reqs := n.currBatch.Reqs[0 : n.cfg.MaxBatchSize/n.interval]
		n.currBatch.Reqs = n.currBatch.Reqs[n.cfg.MaxBatchSize/n.interval:]
		clientreqs := new(common.Batch)
		clientreqs.Reqs = reqs
		payloadBytes, _ := proto.Marshal(clientreqs)
		currBlock := &common.BlockInfo{
			StartID: n.startId,
			ReqNum:  int32(n.cfg.MaxBatchSize),
		}
		n.blockInfos = append(n.blockInfos, currBlock)
		n.startId += int32(n.cfg.MaxBatchSize)
		n.reqNum -= n.cfg.MaxBatchSize
		n.proposeChan <- payloadBytes
		n.logger.Debugf("out of batch propose req num: %v", len(clientreqs.Reqs)*n.interval)
		n.logger.Debugf("remain num: %v", len(n.currBatch.Reqs)*n.interval)
	}
}

func (n *Node) StopClient() {
	var exeStates []int
	exeStates = append(exeStates, n.fastPassStates[ODDFAST])
	exeStates = append(exeStates, n.fastPassStates[EVENFAST])
	exeStates = append(exeStates, n.fastPassStates[ODDPENGING])
	exeStates = append(exeStates, n.fastPassStates[EVENPENGING])
	st := &common.NodeBack{
		NodeID:  n.cfg.ID,
		Addr:    n.cfg.Coordinator,
		States:  exeStates,
		Zero:    uint32(n.excNum), // finidhed block num
		BadCoin: uint32(n.badCoin),
		ReqNum:  n.currRound, // produce round num
	}
	var resp common.Response
	n.logger.Debugln("call back")
	n.connection.Call("Client.NodeFinish", st, &resp)
}

func (n *Node) propose(payload []byte) {
	n.broadcastPayload(payload)

	round := n.proposeRound
	proposal := &common.Message{
		Round:  round,
		Sender: n.cfg.ID,
		Type:   common.Message_PREPARE,
		Hash:   n.myBlocks[len(n.myBlocks)-1],
	}
	msgBytes, err := proto.Marshal(proposal)
	if err != nil {
		n.logger.Error("marshal prepare failed", err)
		return
	}
	proposal.From = n.cfg.ID
	proposal.Signature = crypto.Sign(n.cfg.PrivKey, msgBytes)
	n.addPrepare(round, n.cfg.ID, n.cfg.ID, proposal.Signature, proposal.Hash)

	proposal.Type = common.Message_PP
	if round > 1 {
		proposal.Payload = n.getPPQC(round)
	}
	n.network.BroadcastMessage(proposal)
	n.logger.Infof("propose pp in round %v", round)
	if n.pps[round] == nil {
		n.pps[round] = make(map[uint32]*common.Message)
	}
	n.pps[round][n.cfg.ID] = proposal
	if n.pendings[round] == nil {
		n.pendings[round] = make(map[uint32]uint8)
	}
	n.pendings[round][n.cfg.ID] = 0
}

func (n *Node) getPPQC(round uint32) []byte {
	connBytes, err := json.Marshal(n.roundConn[round][n.cfg.ID])
	if err != nil {
		panic(err)
	}
	ppqc := &common.PPQC{
		StrongConnections: connBytes,
		StrongQCs:         make([]*common.QC, len(n.roundConn[round][n.cfg.ID])),
		WeakQCs:           make([]*common.QC, 0),
	}
	i := uint32(0)
	for node := range n.roundConn[round][n.cfg.ID] {
		var hash string
		if _, ok := n.pps[round-1][node]; !ok {
			n.logger.Debugf("not receive PP of %v-%v but used", round-1, node)
			hash = "badhash"
		} else {
			hash = n.pps[round-1][node].Hash
		}
		prepareqc := &common.QC{
			Hash:   hash,
			Sigs:   make([]*common.Signature, n.cfg.N-n.cfg.F),
			Sender: node,
		}
		j := uint32(0)
		for id, sig := range n.prepares[round-1][node] {
			prepareqc.Sigs[j] = new(common.Signature)
			prepareqc.Sigs[j].Id = id
			prepareqc.Sigs[j].Sig = sig
			j++
			if j == n.cfg.N-n.cfg.F {
				break
			}
		}
		ppqc.StrongQCs[i] = prepareqc
		i++
	}
	if i < n.cfg.N {
		for ; i < n.cfg.N && n.weakWait.Len() != 0; i++ {
			v := n.weakWait.Pop()
			if n.weakLink[round] == nil {
				n.weakLink[round] = make(map[uint32][]common.Vertex)
			}
			if n.weakLink[round][n.cfg.ID] == nil {
				n.weakLink[round][n.cfg.ID] = make([]common.Vertex, 0)
			}
			n.weakLink[round][n.cfg.ID] = append(n.weakLink[round][n.cfg.ID], v.(common.Vertex))
			n.logger.Debugf("weak link %v-%v link %v-%v", round, n.cfg.ID, v.(common.Vertex).Round, v.(common.Vertex).Sender)

			r := v.(common.Vertex).Round
			s := v.(common.Vertex).Sender

			var hash string
			if _, ok := n.pps[r][s]; !ok {
				n.logger.Debugf("not receive pp of %v-%v but weak used", r, s)
				hash = "badhash"
			} else {
				hash = n.pps[r][s].Hash
			}
			prepareqc := &common.QC{
				Hash:   hash,
				Sigs:   make([]*common.Signature, n.cfg.N-n.cfg.F),
				Sender: s,
			}
			j := uint32(0)
			for id, sig := range n.prepares[r][s] {
				prepareqc.Sigs[j] = new(common.Signature)
				prepareqc.Sigs[j].Id = id
				prepareqc.Sigs[j].Sig = sig
				j++
				if j == n.cfg.N-n.cfg.F {
					break
				}
			}
			ppqc.WeakQCs = append(ppqc.WeakQCs, prepareqc)
		}
		weakConnBytes, err := json.Marshal(n.weakLink[round][n.cfg.ID])
		if err != nil {
			panic(err)
		}
		ppqc.WeakConnections = weakConnBytes
	}
	ppqcBytes, err := ppqc.Marshal()
	if err != nil {
		panic(err)
	}
	return ppqcBytes
}

func (n *Node) getSimplePPQC(round uint32) []byte {
	connBytes, err := json.Marshal(n.roundConn[round][n.cfg.ID])
	if err != nil {
		panic(err)
	}
	ppqc := &common.PPQC{
		StrongConnections: connBytes,
	}
	i := uint32(len(n.roundConn[round][n.cfg.ID]))
	if i < n.cfg.N {
		for ; i < n.cfg.N && n.weakWait.Len() != 0; i++ {
			v := n.weakWait.Pop()
			if n.weakLink[round] == nil {
				n.weakLink[round] = make(map[uint32][]common.Vertex)
			}
			if n.weakLink[round][n.cfg.ID] == nil {
				n.weakLink[round][n.cfg.ID] = make([]common.Vertex, 0)
			}
			n.weakLink[round][n.cfg.ID] = append(n.weakLink[round][n.cfg.ID], v.(common.Vertex))
			n.logger.Debugf("weak link %v-%v link %v-%v", round, n.cfg.ID, v.(common.Vertex).Round, v.(common.Vertex).Sender)
		}
		weakConnBytes, err := json.Marshal(n.weakLink[round][n.cfg.ID])
		if err != nil {
			panic(err)
		}
		ppqc.WeakConnections = weakConnBytes
	}
	ppqcBytes, err := ppqc.Marshal()
	if err != nil {
		panic(err)
	}
	return ppqcBytes
}

func (n *Node) broadcastPayload(payload []byte) {
	hash := crypto.Hash(payload)
	n.myBlocks = append(n.myBlocks, hash)

	round := n.proposeRound
	sliceLength := len(payload) / int(n.sliceNum)
	for i := uint32(0); i < n.sliceNum; i++ {
		msgSlice := &common.Message{
			From:            n.cfg.ID,
			Round:           round,
			Sender:          n.cfg.ID,
			Type:            common.Message_PAYLOAD,
			Hash:            hash,
			TotalPayloadNum: n.sliceNum,
			PayloadSlice:    i + 1,
		}
		if i < (n.sliceNum - 1) {
			msgSlice.Payload = payload[i*uint32(sliceLength) : (i+1)*uint32(sliceLength)]
		} else {
			msgSlice.Payload = payload[i*uint32(sliceLength):]
		}
		n.networkPayList[i].BroadcastMessage(msgSlice)
	}

	msg := &common.Message{
		From:    n.cfg.ID,
		Round:   round,
		Sender:  n.cfg.ID,
		Type:    common.Message_PAYLOAD,
		Hash:    hash,
		Payload: payload,
	}
	n.startTime[round] = uint64(time.Now().UnixNano() / 1000000)
	msgBytes, _ := proto.Marshal(msg)
	if n.payloads[round] == nil {
		n.payloads[round] = make(map[uint32][]byte)
	}
	n.payloads[round][n.cfg.ID] = msgBytes
}

func (n *Node) handleMessage(msg *common.Message) {
	// n.logger.Debugf("receive %v of %v-%v from %v", msg.Type, msg.Round, msg.Sender, msg.From)
	switch msg.Type {
	case common.Message_PAYLOAD:
		n.onReceivePayload(msg)
	case common.Message_PP:
		n.onReceivePP(msg)
	case common.Message_PREPARE:
		n.onReceivePrepare(msg)
	case common.Message_READY:
		n.onReceiveReady(msg)
	case common.Message_PAYLOADREQ:
		n.onReceivePayloadReq(msg)
	case common.Message_PAYLOADRESP:
		n.onReceivePayloadResp(msg)
	default:
		n.logger.Error("invalid msg type", errors.New("bug in msg dispatch"))
	}
}

func (n *Node) onReceivePP(msg *common.Message) {
	if n.pps[msg.Round] == nil {
		n.pps[msg.Round] = make(map[uint32]*common.Message)
	}
	n.pps[msg.Round][msg.Sender] = msg
	if n.identity && len(n.pps[msg.Round]) >= int(n.cfg.N-n.cfg.F) {
		if _, ok := n.pps[msg.Round][n.cfg.ID]; !ok {
			if n.proposeRound < msg.Round {
				n.logger.Debugf("round %v 2f+1 pps, bad guy propose", msg.Round)
				n.proposeRound = msg.Round
				n.tryProposeChan <- struct{}{}
			}
		}
	}
	if n.pendings[msg.Round] == nil {
		n.pendings[msg.Round] = make(map[uint32]uint8)
	}
	n.pendings[msg.Round][msg.Sender] = 0
	n.addPrepare(msg.Round, msg.Sender, msg.From, msg.Signature, msg.Hash)
	if msg.Round > 1 {
		n.onReceivePPQC(msg.Payload, msg.Sender, msg.Round)
	}

	// if msg.Round >= n.currRound {
	if _, ok := n.payloads[msg.Round][msg.Sender]; ok {
		if _, ok := n.prepares[msg.Round][msg.Sender][n.cfg.ID]; !ok {
			votePrepare := &common.Message{
				Round:  msg.Round,
				Sender: msg.Sender,
				Type:   common.Message_PREPARE,
				Hash:   msg.Hash,
			}
			voteBytes, err := proto.Marshal(votePrepare)
			if err != nil {
				n.logger.Error("marshal votePrepare failed", err)
				return
			}
			votePrepare.From = n.cfg.ID
			votePrepare.Signature = crypto.Sign(n.cfg.PrivKey, voteBytes)
			n.network.BroadcastMessage(votePrepare)
			n.addPrepare(msg.Round, msg.Sender, n.cfg.ID, votePrepare.Signature, msg.Hash)
		}
	}
	// }

	if (msg.Round >= n.currRound && n.roundState[msg.Round] == 1) || msg.Round < n.currRound {
		n.logger.Debugf("check %v-%v", msg.Round, msg.From)
		n.checkConn(msg.Round-1, msg.From)
	}
}

func (n *Node) onReceivePayload(msgSlice *common.Message) {
	if n.mergeMsg[msgSlice.Round] == nil {
		n.mergeMsg[msgSlice.Round] = make(map[uint32]common.PayloadIds)
	}
	n.mergeMsg[msgSlice.Round][msgSlice.From] = append(n.mergeMsg[msgSlice.Round][msgSlice.From], common.PayloadId{Id: msgSlice.PayloadSlice, Payload: msgSlice.Payload})
	if len(n.mergeMsg[msgSlice.Round][msgSlice.From]) == int(n.sliceNum) {
		sort.Sort(n.mergeMsg[msgSlice.Round][msgSlice.From])
		var buffer bytes.Buffer
		for _, ps := range n.mergeMsg[msgSlice.Round][msgSlice.From] {
			buffer.Write(ps.Payload)
		}
		msg := &common.Message{
			From:    msgSlice.From,
			Round:   msgSlice.Round,
			Sender:  msgSlice.Sender,
			Type:    msgSlice.Type,
			Hash:    msgSlice.Hash,
			Payload: buffer.Bytes(),
		}
		if _, ok := n.payloads[msg.Round][msg.Sender]; ok {
			return
		}
		msgBytes, _ := proto.Marshal(msg)
		if n.payloads[msg.Round] == nil {
			n.payloads[msg.Round] = make(map[uint32][]byte)
		}
		n.payloads[msg.Round][msg.Sender] = msgBytes

		if _, ok := n.pps[msg.Round][msg.Sender]; ok {
			// if msg.Round >= n.currRound {
			if _, ok := n.prepares[msg.Round][msg.Sender][n.cfg.ID]; !ok {
				votePrepare := &common.Message{
					Round:  msg.Round,
					Sender: msg.Sender,
					Type:   common.Message_PREPARE,
					Hash:   msg.Hash,
				}
				voteBytes, err := proto.Marshal(votePrepare)
				if err != nil {
					panic(err)
				}
				votePrepare.From = n.cfg.ID
				votePrepare.Signature = crypto.Sign(n.cfg.PrivKey, voteBytes)
				n.network.BroadcastMessage(votePrepare)
				n.addPrepare(msg.Round, msg.Sender, n.cfg.ID, votePrepare.Signature, msg.Hash)
			}
			// }
		}
	}
}

func (n *Node) onReceivePrepare(msg *common.Message) {
	n.addPrepare(msg.Round, msg.Sender, msg.From, msg.Signature, msg.Hash)
	if len(n.prepares[msg.Round][msg.Sender]) == int(n.cfg.F+1) {
		if _, ok := n.prepares[msg.Round][msg.Sender][n.cfg.ID]; !ok {
			// if msg.Round >= n.currRound {
			votePrepare := &common.Message{
				Round:  msg.Round,
				Sender: msg.Sender,
				Type:   common.Message_PREPARE,
				Hash:   msg.Hash,
			}
			voteBytes, err := proto.Marshal(votePrepare)
			if err != nil {
				panic(err)
			}
			votePrepare.From = n.cfg.ID
			votePrepare.Signature = crypto.Sign(n.cfg.PrivKey, voteBytes)
			n.network.BroadcastMessage(votePrepare)
			n.addPrepare(msg.Round, msg.Sender, n.cfg.ID, votePrepare.Signature, msg.Hash)
			// }
		}
	}
	// if len(n.prepares[msg.Sender][msg.Timestamp][msg.Hash]) == int(n.cfg.F+1) {
	// 	if _, ok := n.hashToPayloads[msg.Hash]; !ok {
	// 		req := &common.Message{
	// 			From: n.cfg.ID,
	// 			Type: common.Message_PAYLOADREQ,
	// 			Hash: msg.Hash,
	// 		}
	// 		n.network.SendMessage(msg.From, req)
	// 		n.logger.Debugf("send pull request to %v", msg.From)
	// 	}
}

func (n *Node) onReceiveReady(msg *common.Message) {
	n.addReady(msg.Round, msg.Sender, msg.From, msg.Signature)
	// if msg.Round >= n.currRound {
	// 	if _, ok := n.readys[msg.Round][msg.Sender][n.cfg.ID]; !ok {
	// 		n.onReceiveprepareqc(msg)
	// 		// onReceiveprepareqc -> addPrepare -> sendReady
	// 	}
	// }
}

func (n *Node) onReceivePPQC(payload []byte, sender uint32, round uint32) {
	ppqc := new(common.PPQC)
	err := ppqc.Unmarshal(payload)
	if err != nil {
		panic(err)
	}
	StrongConn := make(map[uint32]struct{})
	err = json.Unmarshal(ppqc.StrongConnections, &StrongConn)
	if err != nil {
		n.logger.Error("unmarshal conn in ppqc failed", err)
		return
	}
	for _, qc := range ppqc.StrongQCs {
		if len(n.prepares[round-1][qc.Sender]) < int(n.cfg.N-n.cfg.F) {
			var hash string
			if qc.Hash == "badhash" {
				if _, ok := n.pps[round-1][qc.Sender]; !ok {
					n.logger.Debugf("badhash of %v-%v", round-1, qc.Sender)
					continue
				} else {
					hash = n.pps[round-1][qc.Sender].Hash
				}
			}
			tmp := &common.Message{
				Round:  round - 1,
				Sender: qc.Sender,
				Type:   common.Message_PREPARE,
				Hash:   hash,
			}
			data, err := tmp.Marshal()
			if err != nil {
				panic(err)
			}
			for _, sig := range qc.Sigs {
				if !crypto.Verify(n.peers[sig.Id].PublicKey, data, sig.Sig) {
					n.logger.Error("invalid prepare signature in pp qc", err)
					continue
				}
				n.addPrepare(round-1, qc.Sender, sig.Id, sig.Sig, hash)
				if len(n.prepares[round-1][qc.Sender]) >= int(n.cfg.N-n.cfg.F) {
					break
				}
			}
		}
	}
	if n.roundConn[round] == nil {
		n.roundConn[round] = make(map[uint32]map[uint32]struct{})
	}
	n.roundConn[round][sender] = StrongConn
	if len(ppqc.WeakQCs) != 0 {
		weakConn := make([]common.Vertex, 0)
		err = json.Unmarshal(ppqc.WeakConnections, &weakConn)
		if err != nil {
			n.logger.Error("unmarshal conn in ppqc failed", err)
			return
		}
		if n.weakLink[round] == nil {
			n.weakLink[round] = make(map[uint32][]common.Vertex)
		}
		if n.weakLink[round][sender] == nil {
			n.weakLink[round][sender] = make([]common.Vertex, 0)
		}
		n.weakLink[round][sender] = weakConn
	}
}

func (n *Node) onReceiveprepareqc(msg *common.Message) {
	qc := new(common.QC)
	err := qc.Unmarshal(msg.Payload)
	if err != nil {
		panic(err)
	}
	tmp := &common.Message{
		Round:  msg.Round,
		Sender: msg.Sender,
		Type:   common.Message_PREPARE,
		Hash:   qc.Hash,
	}
	data, err := tmp.Marshal()
	if err != nil {
		panic(err)
	}
	for _, sig := range qc.Sigs {
		if !crypto.Verify(n.peers[sig.Id].PublicKey, data, sig.Sig) {
			n.logger.Error("invalid prepare signature in prepare qc", err)
			return
		}
		n.addPrepare(msg.Round, msg.Sender, sig.Id, sig.Sig, qc.Hash)
		if _, ok := n.readys[msg.Round][msg.Sender][n.cfg.ID]; ok {
			break
		}
	}
}

func (n *Node) onReceivePayloadReq(req *common.Message) {
	//if _, ok := n.hashToPayloads[req.Hash]; !ok {
	//	return
	//}
	//resp := &common.Message{
	//	From:    n.cfg.ID,
	//	Type:    common.Message_PAYLOADRESP,
	//	Hash:    req.Hash,
	//	Payload: n.hashToPayloads[req.Hash],
	//}
	//n.network.SendMessage(req.From, resp)
	//n.logger.Debugf("send pull response to %v", req.From)
}

func (n *Node) onReceivePayloadResp(resp *common.Message) {
	//if _, ok := n.hashToPayloads[resp.Hash]; ok {
	//	return
	//}
	//n.hashToPayloads[resp.Hash] = resp.Payload
	//n.logger.Debugf("receive pull response from %v", resp.From)
	//n.execute()
}

func (n *Node) newRound(round uint32) {
	n.currRound = round
	n.execState[n.currRound] = INIT
	n.logger.Debugf("start new round %v", n.currRound)
	if !n.identity {
		n.proposeRound = round
		n.tryProposeChan <- struct{}{}
	}
}

func (n *Node) weakExc(round uint32, sender uint32) {
	for _, weakNode := range n.weakLink[round][sender] {
		if state, ok2 := n.pendings[weakNode.Round][weakNode.Sender]; ok2 {
			if state == 0 {
				n.pendings[weakNode.Round][weakNode.Sender] = 1
				n.logger.Debugf("%v-%v weak execute", weakNode.Round, weakNode.Sender)
				if weakNode.Sender == n.cfg.ID {
					n.blockBack(weakNode.Round)
				}
			}
		}
	}
}

func (n *Node) allRoundExc(round uint32) {
	if n.execState[round] == FINISH || n.execState[round] == LEFT {
		return
	}
	if n.readyRound != round-1 {
		if n.readyRound%2 == round%2 {
			n.logger.Debugf("round %v allExc but readyRound is %v, just wait", round, n.readyRound)
			if round%2 == 0 {
				n.evenStuckRound = round
			} else {
				n.oddStuckRound = round
			}
			return
		} else {
			var nodes []uint32
			for i := uint32(1); i <= n.cfg.N; i++ {
				nodes = append(nodes, i)
			}
			n.logger.Debugf("round %v allExc and excuteBack", round)
			n.excuteBackFastPath(round-2, nodes)
		}
	}

	if n.readyRound != round-1 || n.execState[round] == FINISH || n.execState[round] == LEFT {
		return
	}

	n.logger.Debugf("all execute round %v", round)
	n.readyRound = round
	n.execState[round] = FINISH
	for node := range n.pendings[round] {
		n.pendings[round][node] = 1
		if _, ok1 := n.weakLink[round][node]; ok1 {
			n.weakExc(round, node)
		}
		if node == n.cfg.ID {
			n.blockBack(round)
		}
	}
	if round-1 > 0 && n.execState[round-1] == LEFT {
		var nodes []uint32
		n.execState[round-1] = FINISH
		for node, state := range n.pendings[round-1] {
			if state == 0 {
				if n.connNum[round-1][node] > 0 {
					n.pendings[round-1][node] = 1
					if _, ok1 := n.weakLink[round-1][node]; ok1 {
						n.weakExc(round-1, node)
					}
					nodes = append(nodes, node)
					if node == n.cfg.ID {
						n.blockBack(round - 1)
					}
				} else {
					n.execState[round-1] = LEFT
				}
			}
		}
		if len(nodes) > 0 {
			n.excuteBackLeft(round-2, nodes)
		}
	}
	if n.execState[round+1] == ALL_ONE {
		n.allRoundExc(round + 1)
	}
	if n.execState[round+1] == ALL_HALF {
		n.partRoundExc(round + 1)
	}
}

func (n *Node) partRoundExc(round uint32) {
	if n.execState[round] == FINISH || n.execState[round] == LEFT {
		return
	}
	if n.readyRound != round-1 {
		if n.readyRound%2 == round%2 {
			n.logger.Debugf("round %v partExc but readyRound is %v, just wait", round, n.readyRound)
			if round%2 == 0 {
				n.evenStuckRound = round
			} else {
				n.oddStuckRound = round
			}
			return
		} else {
			var nodes []uint32
			for i := uint32(1); i <= n.cfg.N; i++ {
				if n.connNum[round][i] >= int(n.cfg.N-n.cfg.F) {
					nodes = append(nodes, i)
				}
			}
			n.logger.Debugf("round %v partExc and excuteBack", round)
			n.excuteBackFastPath(round-2, nodes)
		}
	}

	if n.readyRound != round-1 || n.execState[round] == FINISH || n.execState[round] == LEFT {
		return
	}

	n.logger.Debugf("part execute round %v", round)
	n.readyRound = round
	var nodes []uint32
	n.execState[round] = FINISH
	for node, state := range n.pendings[round] {
		if state == 0 {
			if n.connNum[round][node] >= int(n.cfg.N-n.cfg.F) {
				// n.logger.Infof("%v-%v execute in round %v", round, node, n.currRound)
				n.pendings[round][node] = 1
				if _, ok1 := n.weakLink[round][node]; ok1 {
					n.weakExc(round, node)
				}
				nodes = append(nodes, node)
				if node == n.cfg.ID {
					n.blockBack(round)
				}
			} else {
				n.execState[round] = LEFT
			}
		}
	}
	if len(nodes) > 0 {
		n.excuteBackLeft(round-1, nodes)
	}

	if n.execState[round+1] == ALL_ONE {
		n.allRoundExc(round + 1)
	}
	if n.execState[round+1] == ALL_HALF {
		n.partRoundExc(round + 1)
	}
}

func (n *Node) fastPass(round uint32) {
	if round <= 0 || n.execState[round] == FINISH || n.execState[round] == LEFT {
		return
	}
	if n.nodeState[round][1] == int(n.cfg.N) {
		n.logger.Debugf("round %v ALL_ONE", round)
		n.fastPassStates[ALL_ONE]++
		n.execState[round] = ALL_ONE
		n.allRoundExc(round)
	} else {
		n.logger.Debugf("round %v ALL_HALF", round)
		n.fastPassStates[ALL_HALF]++
		n.execState[round] = ALL_HALF
		n.partRoundExc(round)
	}
}

func (n *Node) blockBack(round uint32) {
	latency := uint64(time.Now().UnixNano()/1000000) - n.startTime[round]
	n.executionLatencies[round] = latency
	n.logger.Infof("my round %v execute using %v ms", round, latency)
	st := &common.NodeBack{
		NodeID:  0,
		StartID: uint32(n.blockInfos[n.excNum].StartID),
		ReqNum:  uint32(n.blockInfos[n.excNum].ReqNum),
	}
	n.excNum++
	var resp common.Response
	n.connection.Call("Client.NodeFinish", st, &resp)
}

func (n *Node) excuteBackFastPath(round uint32, fatherNodes []uint32) {
	if round <= 0 || round <= n.readyRound || n.execState[round] == FINISH || n.execState[round] == LEFT {
		return
	}
	witness := make(map[uint32]int)
	for _, father := range fatherNodes {
		for child := range n.roundConn[round+2][father] {
			witness[child]++
		}
	}
	quorumVote := make(map[uint32]int)
	for w := range witness {
		for candidate := range n.roundConn[round+1][w] {
			quorumVote[candidate]++
		}
	}

	if n.readyRound != round-1 {
		var nodes []uint32
		for node, votes := range quorumVote {
			if votes >= int(n.cfg.F+1) {
				nodes = append(nodes, node)
			}
		}
		n.excuteBackFastPath(round-2, nodes)
	}

	if n.readyRound != round-1 || n.execState[round] == FINISH || n.execState[round] == LEFT {
		return
	}

	var nodes []uint32
	n.execState[round] = FINISH
	n.readyRound = round
	n.logger.Debugf("round %v back execute", round)
	for node, state := range n.pendings[round] {
		if state == 0 {
			if quorumVote[node] >= int(n.cfg.F+1) {
				n.pendings[round][node] = 1
				if _, ok1 := n.weakLink[round][node]; ok1 {
					n.weakExc(round, node)
				}
				nodes = append(nodes, node)
				if node == n.cfg.ID {
					n.blockBack(round)
				}
			} else {
				n.execState[round] = LEFT
			}
		}
	}
	if len(nodes) > 0 {
		n.excuteBackLeft(round-1, nodes)
	}
	if n.execState[round+1] == ALL_ONE {
		n.allRoundExc(round + 1)
	}
	if n.execState[round+1] == ALL_HALF {
		n.partRoundExc(round + 1)
	}
	if n.execState[round+1] == PENGING {
		if (round+1)%2 != 0 && round+1 < n.oddStuckRound {
			if n.execState[n.oddStuckRound] == ALL_ONE {
				n.allRoundExc(n.oddStuckRound)
			}
			if n.execState[round+1] == ALL_HALF {
				n.partRoundExc(n.oddStuckRound)
			}
		}
		if (round+1)%2 == 0 && round+1 < n.evenStuckRound {
			if n.execState[n.evenStuckRound] == ALL_ONE {
				n.allRoundExc(n.evenStuckRound)
			}
			if n.execState[n.evenStuckRound] == ALL_HALF {
				n.partRoundExc(n.evenStuckRound)
			}
		}
	}
}

func (n *Node) excuteBackLeft(round uint32, fatherNodes []uint32) {
	if round <= 0 || n.execState[round] != LEFT {
		return
	}
	quorumVote := make(map[uint32]int)
	for _, father := range fatherNodes {
		for child := range n.roundConn[round+1][father] {
			quorumVote[child]++
		}
	}
	var nodes []uint32
	n.execState[round] = FINISH
	for node, state := range n.pendings[round] {
		if state == 0 {
			if quorumVote[node] > 0 {
				n.pendings[round][node] = 1
				if _, ok1 := n.weakLink[round][node]; ok1 {
					n.weakExc(round, node)
				}
				nodes = append(nodes, node)
				if node == n.cfg.ID {
					n.blockBack(round)
				}
			} else {
				n.execState[round] = LEFT
			}
		}
	}
	if len(nodes) > 0 {
		n.logger.Debugf("round %v excuteBack %v blocks", round, len(nodes))
		n.excuteBackLeft(round-1, nodes)
	}
}

func (n *Node) checkConn(round uint32, sender uint32) {
	if round <= 0 {
		return
	}
	if _, ok := n.connNum[round]; !ok {
		n.logger.Debugf("early checkConn in round %v by receive node %v", round, sender)
		return
	}
	for i := uint32(1); i <= n.cfg.N; i++ {
		if _, ok := n.roundConn[round+1][sender][i]; ok {
			n.connNum[round][i]++
			if n.connNum[round][i] == int(n.cfg.N-n.cfg.F) {
				n.nodeState[round][1]++
			}
		} else {
			if (len(n.pps[round+1]) - n.connNum[round][i]) == int(n.cfg.N-n.cfg.F) {
				n.nodeState[round][0]++
			}
		}
	}
	if n.nodeState[round][1]+n.nodeState[round][0] == int(n.cfg.N) && n.execState[round] == PENGING {
		n.logger.Debugf("checkConn and fastPass")
		if round%2 == 0 {
			n.fastPassStates[EVENFAST]++
			n.fastPassStates[EVENPENGING]--
		} else {
			n.fastPassStates[ODDFAST]++
			n.fastPassStates[ODDPENGING]--
		}
		n.fastPass(round)
	}
}

func (n *Node) addPrepare(round uint32, sender uint32, from uint32, signature []byte, hash string) {
	if n.prepares[round] == nil {
		n.prepares[round] = make(map[uint32]map[uint32][]byte)
	}
	if n.prepares[round][sender] == nil {
		n.prepares[round][sender] = map[uint32][]byte{}
	}
	n.prepares[round][sender][from] = signature

	if len(n.prepares[round][sender]) >= int(n.cfg.N-n.cfg.F) {
		if _, ok := n.readys[round][sender][n.cfg.ID]; !ok {
			if round >= n.currRound {
				// payload := n.getprepareqc(round, sender, hash)
				voteReady := &common.Message{
					Round:  round,
					Sender: sender,
					Type:   common.Message_READY,
					Hash:   hash,
				}
				voteBytes, err := proto.Marshal(voteReady)
				if err != nil {
					panic(err)
				}
				voteReady.Signature = crypto.Sign(n.cfg.PrivKey, voteBytes)
				voteReady.From = n.cfg.ID
				// voteReady.Payload = payload
				n.network.BroadcastMessage(voteReady)
				n.addReady(round, sender, n.cfg.ID, voteReady.Signature)
				if sender == n.cfg.ID && round == n.currRound && len(n.quorumReady[round]) >= int(n.cfg.N-n.cfg.F) {
					n.newRound(round + 1)
				}
			} else {
				if _, ok := n.hasWeak[round][sender]; !ok {
					v := common.Vertex{
						Round:  round,
						Sender: sender,
					}
					n.weakWait.Push(v)
					if n.hasWeak[round] == nil {
						n.hasWeak[round] = make(map[uint32]struct{})
					}
					n.hasWeak[round][sender] = struct{}{}
				}

			}
		}
	}
}

func (n *Node) addReady(round uint32, sender uint32, from uint32, signature []byte) {
	if n.readys[round] == nil {
		n.readys[round] = make(map[uint32]map[uint32][]byte)
	}
	if n.readys[round][sender] == nil {
		n.readys[round][sender] = map[uint32][]byte{}
	}
	n.readys[round][sender][from] = signature

	if len(n.readys[round][sender]) >= int(n.cfg.N-n.cfg.F) {
		if n.quorumReady[round] == nil {
			n.quorumReady[round] = make(map[uint32]struct{})
		}
		n.quorumReady[round][sender] = struct{}{}
		if n.roundConn[round+1] == nil {
			n.roundConn[round+1] = make(map[uint32]map[uint32]struct{})
		}
		if n.roundConn[round+1][n.cfg.ID] == nil {
			n.roundConn[round+1][n.cfg.ID] = make(map[uint32]struct{})
		}
		n.roundConn[round+1][n.cfg.ID][sender] = struct{}{}
		if round >= n.currRound && len(n.quorumReady[round]) >= int(n.cfg.N-n.cfg.F) && len(n.prepares[n.currRound][n.cfg.ID]) >= int(n.cfg.N-n.cfg.F) {
			if round > n.currRound {
				v := common.Vertex{
					Round:  n.currRound,
					Sender: n.cfg.ID,
				}
				n.weakWait.Push(v)
			}
			n.newRound(round + 1)
		}
	}

	if len(n.readys[round][sender]) == int(n.cfg.N) {
		if n.quorumPowerReady[round] == nil {
			n.quorumPowerReady[round] = make(map[uint32]struct{})
		}
		n.quorumPowerReady[round][sender] = struct{}{}
		if len(n.quorumPowerReady[round]) == int(n.cfg.N) && n.execState[round] != FINISH {
			n.logger.Debugf("round %v FAST_FAST", round)
			n.fastPassStates[FAST_FAST]++
			n.execState[round] = ALL_ONE
			n.allRoundExc(round)
		}
	}

	if from == n.cfg.ID && round >= n.currRound {
		if n.roundConn[round+1] == nil {
			n.roundConn[round+1] = make(map[uint32]map[uint32]struct{})
		}
		if n.roundConn[round+1][n.cfg.ID] == nil {
			n.roundConn[round+1][n.cfg.ID] = make(map[uint32]struct{})
		}
		n.roundConn[round+1][n.cfg.ID][sender] = struct{}{}
		if n.roundState[round] == 0 && len(n.roundConn[round+1][n.cfg.ID]) >= int(n.cfg.N-n.cfg.F) {
			n.logger.Debugf("round %v turn state", round)
			n.roundState[round] = 1
			n.getConnNum(round - 1)
		}
	}
}

func (n *Node) getprepareqc(round uint32, sender uint32, hash string) []byte {
	qc := &common.QC{
		Hash: hash,
		Sigs: make([]*common.Signature, n.cfg.N-n.cfg.F),
	}
	i := uint32(0)
	for id, sig := range n.prepares[round][sender] {
		qc.Sigs[i] = new(common.Signature)
		qc.Sigs[i].Id = id
		qc.Sigs[i].Sig = sig
		i++
		if i == n.cfg.N-n.cfg.F {
			break
		}
	}
	qcBytes, err := qc.Marshal()
	if err != nil {
		panic(err)
	}
	return qcBytes
}

func (n *Node) getConnNum(round uint32) {
	if round <= 0 {
		return
	}
	n.connNum[round] = make(map[uint32]int)
	for _, conn := range n.roundConn[round+1] {
		for node := range conn {
			n.connNum[round][node]++
		}
	}
	n.nodeState[round] = make(map[uint8]int)
	for i := uint32(1); i <= n.cfg.N; i++ {
		if n.connNum[round][i] >= int(n.cfg.N-n.cfg.F) {
			n.nodeState[round][1]++
		} else if (len(n.pps[round+1]) - n.connNum[round][i]) >= int(n.cfg.N-n.cfg.F) {
			n.nodeState[round][0]++
		}
	}
	if n.nodeState[round][1]+n.nodeState[round][0] == int(n.cfg.N) {
		if round%2 == 0 {
			n.fastPassStates[EVENFAST]++
		} else {
			n.fastPassStates[ODDFAST]++
		}
		n.fastPass(round)
	} else {
		n.execState[round] = PENGING
		n.fastPassStates[PENGING]++
		if round%2 == 0 {
			n.fastPassStates[EVENPENGING]++
		} else {
			n.fastPassStates[ODDPENGING]++
		}
		n.logger.Debugf("round %v PENGING", round)
	}
}
