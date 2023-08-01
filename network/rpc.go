package network

import (
	"leobat/common"
	"leobat/logger"
	"net"
	"net/http"
	"net/rpc"
)

type RpcNetWork struct {
	ID           uint32
	Addr         string
	Peers        map[uint32]common.Peer
	Clients      map[uint32]*rpc.Client
	MsgChan      chan *common.Message
	DispatchChan chan *common.Message
	logger       logger.Logger
}

func NewRpcNetWork(msgChan chan *common.Message, dispatchChan chan *common.Message, id uint32, addr string,
	peers map[uint32]common.Peer, logger logger.Logger) *RpcNetWork {
	network := &RpcNetWork{
		ID:           id,
		Addr:         addr,
		Peers:        peers,
		Clients:      make(map[uint32]*rpc.Client),
		MsgChan:      msgChan,
		DispatchChan: dispatchChan,
		logger:       logger,
	}
	rpc.Register(network)
	rpc.HandleHTTP()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	go http.Serve(listener, nil)

	//for _, peer := range peers {
	//	if id == peer.ID {
	//		continue
	//	}
	//	conn, err := net.DialTimeout("tcp", peer.Addr, time.Minute)
	//	if err != nil {
	//		panic(err)
	//	}
	//	network.Clients[peer.ID] = rpc.NewClient(conn)
	//}

	return network
}

func (network *RpcNetWork) Start() {
	for _, peer := range network.Peers {
		if network.ID == peer.ID {
			continue
		}
		conn, err := rpc.DialHTTP("tcp", peer.Addr)
		if err != nil {
			panic(err)
		}
		network.Clients[peer.ID] = conn
	}
	network.logger.Infoln("network start successfully")
}

func (network *RpcNetWork) Stop() {

}

func (network *RpcNetWork) SendMessage(id uint32, msg *common.Message) {
	var resp common.Response
	//if network.Clients[id] == nil {
	//	conn, err := rpc.DialHTTP("tcp", network.Peers[id].Addr)
	//	if err != nil {
	//		common.Error("connect failed", err)
	//		return
	//	}
	//	network.Clients[id] = conn
	//}
	network.Clients[id].Call("RpcNetWork.OnMessage", msg, &resp)
}

func (network *RpcNetWork) BroadcastMessage(msg *common.Message) {
	for _, peer := range network.Peers {
		//var resp common.Response
		if peer.ID == network.ID {
			continue
		}
		go network.SendMessage(peer.ID, msg)
		//go conn.Call("RpcNetWork.OnMessage", msg, &resp)
	}
}

func (network *RpcNetWork) OnMessage(req *common.Message, resp *common.Response) error {
	if req.Type != common.Message_VAL && req.Type != common.Message_PAYLOADREQ &&
		req.Type != common.Message_PAYLOADRESP &&
		req.Type != common.Message_PAYLOAD {
		//TODO: verify signature
	}
	if req.Type == common.Message_VAL || req.Type == common.Message_PAYLOADREQ ||
		req.Type == common.Message_PAYLOADRESP || req.Type == common.Message_PAYLOAD {
		network.MsgChan <- req
	} else {
		network.DispatchChan <- req
	}
	return nil
}
