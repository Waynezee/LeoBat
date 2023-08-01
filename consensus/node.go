package consensus

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"leobat-go/common"
	"leobat-go/crypto"
	"leobat-go/logger"
	"leobat-go/network"
	"leobat-go/utils"
	"sort"
	"strconv"

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

	currRound  uint32
	readyRound uint32
	roundState map[uint32]uint8 // 0: before 2f+1 bval; 1: after 2f+1 bval
	execState  map[uint32]executionState
	nodeState  map[uint32]map[uint8]int                  // 0: 2f+1 nodes not connect; 1: 2f+1 nodes connect
	roundConn  map[uint32]map[uint32]map[uint32]struct{} // round r+1 -> round r
	connNum    map[uint32]map[uint32]int                 // round r -> round r+1
	weakLink   map[uint32]map[uint32][]common.Vertex     // round -> source <weakLink> round -> source
	weakWait   *utils.PriorityQueue
	hasWeak    map[uint32]map[uint32]struct{}

	// round -> sender -> from -> signature
	payloads        map[uint32]map[uint32][]byte
	vals            map[uint32]map[uint32]*common.Message
	bvals           map[uint32]map[uint32]map[uint32][]byte
	proms           map[uint32]map[uint32]map[uint32][]byte
	quorumProm      map[uint32]map[uint32]struct{} // get 2f+1 proms
	quorumPowerProm map[uint32]map[uint32]struct{} // get 3f+1 proms
	quorumCoin      map[uint32]map[uint32][]byte
	coins           map[uint32]uint32
	coinFlag        map[uint32]bool

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

		currRound:       1,
		readyRound:      0,
		roundState:      make(map[uint32]uint8),
		execState:       make(map[uint32]executionState),
		nodeState:       make(map[uint32]map[uint8]int),
		roundConn:       make(map[uint32]map[uint32]map[uint32]struct{}),
		connNum:         make(map[uint32]map[uint32]int),
		weakLink:        make(map[uint32]map[uint32][]common.Vertex),
		hasWeak:         make(map[uint32]map[uint32]struct{}),
		payloads:        make(map[uint32]map[uint32][]byte),
		vals:            make(map[uint32]map[uint32]*common.Message),
		bvals:           make(map[uint32]map[uint32]map[uint32][]byte),
		proms:           make(map[uint32]map[uint32]map[uint32][]byte),
		quorumProm:      make(map[uint32]map[uint32]struct{}),
		quorumPowerProm: make(map[uint32]map[uint32]struct{}),
		quorumCoin:      make(map[uint32]map[uint32][]byte),
		coins:           make(map[uint32]uint32),
		coinFlag:        make(map[uint32]bool),
		pendings:        make(map[uint32]map[uint32]uint8),
		myBlocks:        nil,
		payload:         "",
		mergeMsg:        make(map[uint32]map[uint32]common.PayloadIds),
		sliceNum:        conn,

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

	// node.payload = strings.Repeat("a", cfg.MaxBatchSize*cfg.PayloadSize)
	// node.logger.Infof("payload size: %v", len(node.payload))

	node.network = network.NewNoiseNetWork(node.cfg.ID, node.cfg.Addr, node.peers, node.msgChan, node.logger, false, 0)
	for i := uint32(0); i < conn; i++ {
		node.networkPayList = append(node.networkPayList, network.NewNoiseNetWork(node.cfg.ID, node.cfg.Addr, node.peers, node.msgChan, node.logger, true, i+1))
	}
	node.weakWait = utils.NewPriorityQueue()
	return node
}

func (n *Node) Run() {
	//time.Sleep(10 * time.Second)
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
		n.logger.Infoln("start")
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
			// n.Stop()
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
			// if n.reqNum >= n.cfg.MaxBatchSize {
			// 	if n.proposeflag {
			// 		n.getBatch()
			// 		n.proposeflag = false
			// 		n.timeflag = false
			// 	} else {
			// 		n.timeflag = true
			// 	}
			// }
		case <-n.tryProposeChan:
			// if n.timeflag {
			n.getBatch()
			n.proposeflag = false
			// } else {
			// 	n.proposeflag = true
			// }
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

func (n *Node) Stop() {
	conn, err := rpc.DialHTTP("tcp", n.cfg.Coordinator)
	if err != nil {
		panic(err)
	}
	totalExecutionLatency := uint64(0)
	for _, l := range n.executionLatencies {
		totalExecutionLatency += l
	}
	st := &common.CoorStatistics{
		ExecutionNumber: uint64(len(n.executionLatencies)),
		ID:              n.cfg.ID,
	}
	if len(n.executionLatencies) == 0 {
		st.ExecutionLatency = 0
	} else {
		st.ExecutionLatency = totalExecutionLatency / uint64(len(n.executionLatencies))
	}
	var resp common.Response
	conn.Call("Coordinator.Finish", st, &resp)
	n.logger.Debugln("call back")
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
		Type:   common.Message_BVAL,
		Hash:   n.myBlocks[len(n.myBlocks)-1],
	}
	msgBytes, err := proto.Marshal(proposal)
	if err != nil {
		n.logger.Error("marshal bval failed", err)
		return
	}
	proposal.From = n.cfg.ID
	proposal.Signature = crypto.Sign(n.cfg.PrivKey, msgBytes)
	n.addBval(round, n.cfg.ID, n.cfg.ID, proposal.Signature, proposal.Hash)

	proposal.Type = common.Message_VAL
	if round > 1 {
		proposal.Payload = n.getValQC(round)
	}
	n.network.BroadcastMessage(proposal)
	n.logger.Infof("propose val in round %v", round)
	if n.vals[round] == nil {
		n.vals[round] = make(map[uint32]*common.Message)
	}
	n.vals[round][n.cfg.ID] = proposal
	if n.pendings[round] == nil {
		n.pendings[round] = make(map[uint32]uint8)
	}
	n.pendings[round][n.cfg.ID] = 0
}

func (n *Node) getValQC(round uint32) []byte {
	connBytes, err := json.Marshal(n.roundConn[round][n.cfg.ID])
	if err != nil {
		panic(err)
	}
	valqc := &common.ValQC{
		StrongConnections: connBytes,
		StrongQCs:         make([]*common.QC, len(n.roundConn[round][n.cfg.ID])),
		WeakQCs:           make([]*common.QC, 0),
	}
	i := uint32(0)
	for node := range n.roundConn[round][n.cfg.ID] {
		var hash string
		if _, ok := n.vals[round-1][node]; !ok {
			n.logger.Debugf("not receive val of %v-%v but used", round-1, node)
			hash = "badhash"
		} else {
			hash = n.vals[round-1][node].Hash
		}
		bvalqc := &common.QC{
			Hash:   hash,
			Sigs:   make([]*common.Signature, n.cfg.N-n.cfg.F),
			Sender: node,
		}
		j := uint32(0)
		for id, sig := range n.bvals[round-1][node] {
			bvalqc.Sigs[j] = new(common.Signature)
			bvalqc.Sigs[j].Id = id
			bvalqc.Sigs[j].Sig = sig
			j++
			if j == n.cfg.N-n.cfg.F {
				break
			}
		}
		valqc.StrongQCs[i] = bvalqc
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
			if _, ok := n.vals[r][s]; !ok {
				n.logger.Debugf("not receive val of %v-%v but weak used", r, s)
				hash = "badhash"
			} else {
				hash = n.vals[r][s].Hash
			}
			bvalqc := &common.QC{
				Hash:   hash,
				Sigs:   make([]*common.Signature, n.cfg.N-n.cfg.F),
				Sender: s,
			}
			j := uint32(0)
			for id, sig := range n.bvals[r][s] {
				bvalqc.Sigs[j] = new(common.Signature)
				bvalqc.Sigs[j].Id = id
				bvalqc.Sigs[j].Sig = sig
				j++
				if j == n.cfg.N-n.cfg.F {
					break
				}
			}
			valqc.WeakQCs = append(valqc.WeakQCs, bvalqc)
		}
		weakConnBytes, err := json.Marshal(n.weakLink[round][n.cfg.ID])
		if err != nil {
			panic(err)
		}
		valqc.WeakConnections = weakConnBytes
	}
	valqcBytes, err := valqc.Marshal()
	if err != nil {
		panic(err)
	}
	return valqcBytes
}

func (n *Node) getValQCSimple(round uint32) []byte {
	connBytes, err := json.Marshal(n.roundConn[round][n.cfg.ID])
	if err != nil {
		panic(err)
	}
	valqc := &common.ValQC{
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
		valqc.WeakConnections = weakConnBytes
	}
	valqcBytes, err := valqc.Marshal()
	if err != nil {
		panic(err)
	}
	return valqcBytes
}

func (n *Node) broadcastPayload(payload []byte) {
	// payloadStr := n.payload + strconv.Itoa(int(n.currRound)) + "-" + strconv.Itoa(int(n.cfg.ID))
	// payloadBytes := utils.Str2Bytes(payloadStr)
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
	// n.logger.Debugf("broadcast payload in round %v", round)
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
	case common.Message_VAL:
		n.onReceiveVal(msg)
	case common.Message_BVAL:
		n.onReceiveBval(msg)
	case common.Message_PROM:
		n.onReceiveProm(msg)
	case common.Message_COIN:
		n.onReceiveCoin(msg)
	case common.Message_PAYLOADREQ:
		n.onReceivePayloadReq(msg)
	case common.Message_PAYLOADRESP:
		n.onReceivePayloadResp(msg)
	default:
		n.logger.Error("invalid msg type", errors.New("bug in msg dispatch"))
	}
}

func (n *Node) onReceiveVal(msg *common.Message) {
	if n.vals[msg.Round] == nil {
		n.vals[msg.Round] = make(map[uint32]*common.Message)
	}
	n.vals[msg.Round][msg.Sender] = msg
	if n.identity && len(n.vals[msg.Round]) >= int(n.cfg.N-n.cfg.F) {
		if _, ok := n.vals[msg.Round][n.cfg.ID]; !ok {
			if n.proposeRound < msg.Round {
				n.logger.Debugf("round %v 2f+1 vals, bad guy propose", msg.Round)
				n.proposeRound = msg.Round
				n.tryProposeChan <- struct{}{}
			}
		}

	}
	if n.pendings[msg.Round] == nil {
		n.pendings[msg.Round] = make(map[uint32]uint8)
	}
	n.pendings[msg.Round][msg.Sender] = 0
	n.addBval(msg.Round, msg.Sender, msg.From, msg.Signature, msg.Hash)
	if msg.Round > 1 {
		n.onReceiveValQC(msg.Payload, msg.Sender, msg.Round)
	}

	// if msg.Round >= n.currRound {
	if _, ok := n.payloads[msg.Round][msg.Sender]; ok {
		if _, ok := n.bvals[msg.Round][msg.Sender][n.cfg.ID]; !ok {
			voteBval := &common.Message{
				Round:  msg.Round,
				Sender: msg.Sender,
				Type:   common.Message_BVAL,
				Hash:   msg.Hash,
			}
			voteBytes, err := proto.Marshal(voteBval)
			if err != nil {
				n.logger.Error("marshal votebval failed", err)
				return
			}
			voteBval.From = n.cfg.ID
			voteBval.Signature = crypto.Sign(n.cfg.PrivKey, voteBytes)
			n.network.BroadcastMessage(voteBval)
			n.addBval(msg.Round, msg.Sender, n.cfg.ID, voteBval.Signature, msg.Hash)
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
		// n.logger.Debugf("receive all payload from %v in round %v", msg.Sender, msg.Round)

		if _, ok := n.payloads[msg.Round][msg.Sender]; ok {
			return
		}
		msgBytes, _ := proto.Marshal(msg)
		if n.payloads[msg.Round] == nil {
			n.payloads[msg.Round] = make(map[uint32][]byte)
		}
		n.payloads[msg.Round][msg.Sender] = msgBytes

		if _, ok := n.vals[msg.Round][msg.Sender]; ok {
			// if msg.Round >= n.currRound {
			if _, ok := n.bvals[msg.Round][msg.Sender][n.cfg.ID]; !ok {
				voteBval := &common.Message{
					Round:  msg.Round,
					Sender: msg.Sender,
					Type:   common.Message_BVAL,
					Hash:   msg.Hash,
				}
				voteBytes, err := proto.Marshal(voteBval)
				if err != nil {
					panic(err)
				}
				voteBval.From = n.cfg.ID
				voteBval.Signature = crypto.Sign(n.cfg.PrivKey, voteBytes)
				n.network.BroadcastMessage(voteBval)
				n.addBval(msg.Round, msg.Sender, n.cfg.ID, voteBval.Signature, msg.Hash)
			}
			// }
		}
	}
}

func (n *Node) onReceiveBval(msg *common.Message) {
	n.addBval(msg.Round, msg.Sender, msg.From, msg.Signature, msg.Hash)
	if len(n.bvals[msg.Round][msg.Sender]) == int(n.cfg.F+1) {
		if _, ok := n.bvals[msg.Round][msg.Sender][n.cfg.ID]; !ok {
			// if msg.Round >= n.currRound {
			voteBval := &common.Message{
				Round:  msg.Round,
				Sender: msg.Sender,
				Type:   common.Message_BVAL,
				Hash:   msg.Hash,
			}
			voteBytes, err := proto.Marshal(voteBval)
			if err != nil {
				panic(err)
			}
			voteBval.From = n.cfg.ID
			voteBval.Signature = crypto.Sign(n.cfg.PrivKey, voteBytes)
			n.network.BroadcastMessage(voteBval)
			n.addBval(msg.Round, msg.Sender, n.cfg.ID, voteBval.Signature, msg.Hash)
			// }
		}
	}

	// if len(n.bvals[msg.Sender][msg.Timestamp][msg.Hash]) == int(n.cfg.F+1) {
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

func (n *Node) onReceiveProm(msg *common.Message) {
	n.addProm(msg.Round, msg.Sender, msg.From, msg.Signature)
	// if msg.Round >= n.currRound {
	// 	if _, ok := n.proms[msg.Round][msg.Sender][n.cfg.ID]; !ok {
	// 		n.onReceiveBvalQC(msg)
	// 		// onReceiveBvalQC -> addBval -> sendProm
	// 	}
	// }
}

func (n *Node) onReceiveValQC(payload []byte, sender uint32, round uint32) {
	valqc := new(common.ValQC)
	err := valqc.Unmarshal(payload)
	if err != nil {
		panic(err)
	}
	StrongConn := make(map[uint32]struct{})
	err = json.Unmarshal(valqc.StrongConnections, &StrongConn)
	if err != nil {
		n.logger.Error("unmarshal conn in valqc failed", err)
		return
	}
	for _, qc := range valqc.StrongQCs {
		if len(n.bvals[round-1][qc.Sender]) < int(n.cfg.N-n.cfg.F) {
			var hash string
			if qc.Hash == "badhash" {
				if _, ok := n.vals[round-1][qc.Sender]; !ok {
					n.logger.Debugf("badhash of %v-%v", round-1, qc.Sender)
					continue
				} else {
					hash = n.vals[round-1][qc.Sender].Hash
				}
			}
			tmp := &common.Message{
				Round:  round - 1,
				Sender: qc.Sender,
				Type:   common.Message_BVAL,
				Hash:   hash,
			}
			data, err := tmp.Marshal()
			if err != nil {
				panic(err)
			}
			for _, sig := range qc.Sigs {
				if !crypto.Verify(n.peers[sig.Id].PublicKey, data, sig.Sig) {
					n.logger.Error("invalid bval signature in val qc", err)
					continue
				}
				n.addBval(round-1, qc.Sender, sig.Id, sig.Sig, hash)
				if len(n.bvals[round-1][qc.Sender]) >= int(n.cfg.N-n.cfg.F) {
					break
				}
			}
		}
	}
	if n.roundConn[round] == nil {
		n.roundConn[round] = make(map[uint32]map[uint32]struct{})
	}
	n.roundConn[round][sender] = StrongConn
	if len(valqc.WeakQCs) != 0 {
		weakConn := make([]common.Vertex, 0)
		err = json.Unmarshal(valqc.WeakConnections, &weakConn)
		if err != nil {
			n.logger.Error("unmarshal conn in valqc failed", err)
			return
		}
		if n.weakLink[round] == nil {
			n.weakLink[round] = make(map[uint32][]common.Vertex)
		}
		if n.weakLink[round][sender] == nil {
			n.weakLink[round][sender] = make([]common.Vertex, 0)
		}
		n.weakLink[round][sender] = weakConn
		// n.logger.Debugf("%v-%v has %v weak link", round, sender, len(valqc.WeakQCs))
	}
	// n.logger.Debugf("%v-%v connect %v", round, sender, conn)
}

func (n *Node) onReceiveBvalQC(msg *common.Message) {
	// n.logger.Debugf("receiveBvalQC %v-%v from %v", msg.Round, msg.Sender, msg.From)
	qc := new(common.QC)
	err := qc.Unmarshal(msg.Payload)
	if err != nil {
		panic(err)
	}
	tmp := &common.Message{
		Round:  msg.Round,
		Sender: msg.Sender,
		Type:   common.Message_BVAL,
		Hash:   qc.Hash,
	}
	data, err := tmp.Marshal()
	if err != nil {
		panic(err)
	}
	for _, sig := range qc.Sigs {
		if !crypto.Verify(n.peers[sig.Id].PublicKey, data, sig.Sig) {
			n.logger.Error("invalid bval signature in bval qc", err)
			return
		}
		n.addBval(msg.Round, msg.Sender, sig.Id, sig.Sig, qc.Hash)
		if _, ok := n.proms[msg.Round][msg.Sender][n.cfg.ID]; ok {
			break
		}
	}
}

func (n *Node) onReceiveCoin(msg *common.Message) {
	if n.quorumCoin[msg.Round] == nil {
		n.quorumCoin[msg.Round] = make(map[uint32][]byte)
	}
	n.quorumCoin[msg.Round][msg.From] = msg.Payload
	if len(n.quorumCoin[msg.Round]) >= int(n.cfg.N-n.cfg.F) {
		if _, ok := n.coins[msg.Round]; !ok {
			coinBytes := crypto.Recover(n.quorumCoin[msg.Round])
			leader := binary.LittleEndian.Uint32(coinBytes)%uint32(n.cfg.N) + 1
			n.coins[msg.Round] = leader
			// n.logger.Debugf("round %v coin is %v", msg.Round, leader)
			if n.connNum[msg.Round][leader] >= int(n.cfg.F+1) {
				n.logger.Debugf("good coin %v in round %v", leader, msg.Round)
				n.coinFlag[msg.Round] = true
				n.tryCoinPass(msg.Round)
			} else {
				n.logger.Debugf("bad coin %v in round %v", leader, msg.Round)
				n.badCoin++
			}
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
	//TODO: should check payload
	if n.execState[round] == FINISH || n.execState[round] == LEFT {
		return
	}
	if n.readyRound != round-1 {
		n.logger.Debugf("round %v allExc but readyRound is %v", round, n.readyRound)
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
					// n.logger.Infof("%v-%v execute in round %v", round, node, n.currRound)
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
			n.excuteBack(round-2, nodes)
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
		n.logger.Debugf("round %v partExc but readyRound is %v", round, n.readyRound)
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
		n.excuteBack(round-1, nodes)
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

func (n *Node) coinPass(coinRound uint32) {
	if coinRound <= 2 {
		return
	}
	if n.execState[coinRound-2] == FINISH || n.execState[coinRound-2] == LEFT || !n.coinFlag[coinRound] {
		return
	}
	n.logger.Debugf("round %v coin pass", coinRound-2)
	n.readyRound = coinRound - 2

	leader := n.coins[coinRound]
	quorumVote := make(map[uint32]int)
	if len(n.roundConn[coinRound][leader]) == int(n.cfg.N) {
		quorumVote = n.connNum[coinRound-2]
	} else {
		for father := range n.roundConn[coinRound][leader] {
			for child := range n.roundConn[coinRound-1][father] {
				quorumVote[child]++
			}
		}
	}
	var nodes []uint32
	n.execState[coinRound-2] = FINISH
	for node, state := range n.pendings[coinRound-2] {
		if state == 0 {
			if quorumVote[node] >= int(n.cfg.F+1) {
				// n.logger.Infof("%v-%v execute in round %v", coinRound-2, node, n.currRound)
				n.pendings[coinRound-2][node] = 1
				if _, ok1 := n.weakLink[coinRound-2][node]; ok1 {
					n.weakExc(coinRound-2, node)
				}
				nodes = append(nodes, node)
				if node == n.cfg.ID {
					n.blockBack(coinRound - 2)
				}
			} else {
				n.execState[coinRound-2] = LEFT
			}
		}
	}
	if len(nodes) > 0 {
		n.excuteBack(coinRound-3, nodes)
	}
	if n.execState[coinRound-1] == ALL_ONE {
		n.allRoundExc(coinRound - 1)
	}
	if n.execState[coinRound-1] == ALL_HALF {
		n.partRoundExc(coinRound - 1)
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

func (n *Node) excuteBack(round uint32, fatherNodes []uint32) {
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
				// n.logger.Infof("%v-%v execute in round %v", coinRound-2, node, n.currRound)
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
		n.excuteBack(round-1, nodes)
	}
}

func (n *Node) tryCoinPass(round uint32) {
	if round <= 2 {
		return
	}
	preRound := round - 2
	for {
		if preRound <= 2 || n.coinFlag[preRound] {
			break
		}
		candidate := n.coins[preRound]
		for father := range n.roundConn[preRound+2][n.coins[preRound+2]] {
			if _, ok := n.roundConn[preRound+1][father][candidate]; ok {
				n.logger.Debugf("round %v leader %v saved by round %v leader %v", preRound, candidate, preRound+2, n.coins[preRound+2])
				n.coinFlag[preRound] = true
				break
			}
		}
		preRound -= 2
	}
	if n.readyRound >= round-2 {
		return
	}
	if n.readyRound == round-3 {
		n.coinPass(round)
	} else {
		if n.readyRound%2 == round%2 {
			// brother round coin stuck
			n.logger.Debugf("now coin is round %v, ready execute stuck in round %v", round, n.readyRound)
			return
		} else {
			for {
				executeRound := n.readyRound + 1
				if executeRound > round-2 {
					break
				}
				if n.coinFlag[executeRound+2] {
					n.coinPass(executeRound + 2)
				} else {
					// futureLeader := executeRound + 4
					// for {
					// 	if n.coinFlag[futureLeader] {
					// 		break
					// 	}
					// 	futureLeader += 2
					// }
					//TODO: get vertices in executeRound+1 that has path to futureLeader
					for i := uint32(1); i <= n.cfg.N; i++ {
						if n.connNum[executeRound+2][i] >= int(n.cfg.F+1) {
							n.logger.Debugf("virtual coin %v in round %v", i, executeRound+2)
							n.coinFlag[executeRound+2] = true
							n.coins[executeRound+2] = i
							n.coinPass(executeRound + 2)
							break
						}
					}
				}
			}
		}
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
			if (len(n.vals[round+1]) - n.connNum[round][i]) == int(n.cfg.N-n.cfg.F) {
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
	if !n.coinFlag[round] {
		n.checkCoin(round)
	}
}

func (n *Node) checkCoin(round uint32) {
	if round <= 2 {
		return
	}
	if n.connNum[round][n.coins[round]] >= int(n.cfg.F+1) {
		n.logger.Debugf("round %v coin turn good", round)
		n.coinFlag[round] = true
		n.tryCoinPass(round)
	}
}

func (n *Node) addBval(round uint32, sender uint32, from uint32, signature []byte, hash string) {
	if n.bvals[round] == nil {
		n.bvals[round] = make(map[uint32]map[uint32][]byte)
	}
	if n.bvals[round][sender] == nil {
		n.bvals[round][sender] = map[uint32][]byte{}
	}
	n.bvals[round][sender][from] = signature

	if len(n.bvals[round][sender]) >= int(n.cfg.N-n.cfg.F) {
		if _, ok := n.proms[round][sender][n.cfg.ID]; !ok {
			// n.logger.Debugf("%v-%v quorumBval", round, sender)
			if round >= n.currRound {
				// payload := n.getBvalQC(round, sender, hash)
				voteProm := &common.Message{
					Round:  round,
					Sender: sender,
					Type:   common.Message_PROM,
					Hash:   hash,
				}
				voteBytes, err := proto.Marshal(voteProm)
				if err != nil {
					panic(err)
				}
				voteProm.Signature = crypto.Sign(n.cfg.PrivKey, voteBytes)
				voteProm.From = n.cfg.ID
				// voteProm.Payload = payload
				n.network.BroadcastMessage(voteProm)
				n.addProm(round, sender, n.cfg.ID, voteProm.Signature)
				if sender == n.cfg.ID && round == n.currRound && len(n.quorumProm[round]) >= int(n.cfg.N-n.cfg.F) {
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

func (n *Node) addProm(round uint32, sender uint32, from uint32, signature []byte) {
	if n.proms[round] == nil {
		n.proms[round] = make(map[uint32]map[uint32][]byte)
	}
	if n.proms[round][sender] == nil {
		n.proms[round][sender] = map[uint32][]byte{}
	}
	n.proms[round][sender][from] = signature

	if len(n.proms[round][sender]) >= int(n.cfg.N-n.cfg.F) {
		if n.quorumProm[round] == nil {
			n.quorumProm[round] = make(map[uint32]struct{})
		}
		n.quorumProm[round][sender] = struct{}{}
		if n.roundConn[round+1] == nil {
			n.roundConn[round+1] = make(map[uint32]map[uint32]struct{})
		}
		if n.roundConn[round+1][n.cfg.ID] == nil {
			n.roundConn[round+1][n.cfg.ID] = make(map[uint32]struct{})
		}
		n.roundConn[round+1][n.cfg.ID][sender] = struct{}{}
		if round >= n.currRound && len(n.quorumProm[round]) >= int(n.cfg.N-n.cfg.F) && len(n.bvals[n.currRound][n.cfg.ID]) >= int(n.cfg.N-n.cfg.F) {
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

	if len(n.proms[round][sender]) == int(n.cfg.N) {
		if n.quorumPowerProm[round] == nil {
			n.quorumPowerProm[round] = make(map[uint32]struct{})
		}
		n.quorumPowerProm[round][sender] = struct{}{}
		if len(n.quorumPowerProm[round]) == int(n.cfg.N) && n.execState[round] != FINISH {
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
			n.getCoin(round - 1)
		}
	}
}

func (n *Node) getBvalQC(round uint32, sender uint32, hash string) []byte {
	qc := &common.QC{
		Hash: hash,
		Sigs: make([]*common.Signature, n.cfg.N-n.cfg.F),
	}
	i := uint32(0)
	for id, sig := range n.bvals[round][sender] {
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

func (n *Node) getCoin(round uint32) {
	if round <= 2 {
		return
	}
	data := n.getCoinData(round)
	sigShare := crypto.BlsSign(data, n.cfg.ThresholdSK)
	msg := &common.Message{
		Round:   round,
		From:    n.cfg.ID,
		Type:    common.Message_COIN,
		Payload: sigShare,
	}
	n.network.BroadcastMessage(msg)
	n.onReceiveCoin(msg)
}

func (n *Node) getCoinData(round uint32) []byte {
	if _, ok := n.quorumCoin[round][n.cfg.ID]; ok {
		return n.quorumCoin[round][n.cfg.ID]
	}
	var buffer bytes.Buffer
	buffer.WriteString(strconv.FormatUint(uint64(round), 10))
	buffer.WriteString("-")
	buffer.WriteString(string(n.cfg.MasterPK))
	buffer.WriteString("-")
	buffer.WriteString(strconv.FormatUint(uint64(time.Now().Day()), 10))
	if n.quorumCoin[round] == nil {
		n.quorumCoin[round] = make(map[uint32][]byte)
	}
	n.quorumCoin[round][n.cfg.ID] = buffer.Bytes()
	return n.quorumCoin[round][n.cfg.ID]
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
		} else if (len(n.vals[round+1]) - n.connNum[round][i]) >= int(n.cfg.N-n.cfg.F) {
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
