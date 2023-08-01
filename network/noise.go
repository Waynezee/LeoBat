package network

import (
	"context"
	"leobat-go/common"
	"leobat-go/crypto"
	"leobat-go/logger"
	"net"
	"strconv"
	"strings"

	"github.com/perlin-network/noise"
)

type NoiseMessage struct {
	Msg *common.Message
}

func (m NoiseMessage) Marshal() []byte {
	data, _ := m.Msg.Marshal()
	return data
}

func UnmarshalNoiseMessage(buf []byte) (NoiseMessage, error) {
	m := NoiseMessage{Msg: new(common.Message)}
	err := m.Msg.Unmarshal(buf)
	if err != nil {
		return NoiseMessage{}, err
	}
	return m, nil
}

type NoiseNetWork struct {
	node    *noise.Node
	peers   map[uint32]common.Peer
	msgChan chan *common.Message
	logger  logger.Logger
	id      uint32
}

func NewNoiseNetWork(id uint32, addr string, peers map[uint32]common.Peer, msgChan chan *common.Message,
	logger logger.Logger, change bool, multi uint32) *NoiseNetWork {
	ip := strings.Split(addr, ":")
	port, _ := strconv.ParseUint(ip[1], 10, 64)
	myPeers := make(map[uint32]common.Peer)
	if change {
		port += 20 + uint64(multi)
		for id, p := range peers {
			peerIP := strings.Split(p.Addr, ":")
			peerPort, _ := strconv.ParseUint(peerIP[1], 10, 64)
			peerPort += 20 + uint64(multi)
			newAddr := peerIP[0] + ":" + strconv.FormatInt(int64(peerPort), 10)
			myPeers[id] = common.Peer{
				ID:              id,
				Addr:            newAddr,
				PublicKey:       p.PublicKey,
				ThresholdPubKey: p.ThresholdPubKey,
			}
		}
	} else {
		for id, p := range peers {
			myPeers[id] = common.Peer{
				ID:              id,
				Addr:            p.Addr,
				PublicKey:       p.PublicKey,
				ThresholdPubKey: p.ThresholdPubKey,
			}
		}
	}

	node, _ := noise.NewNode(noise.WithNodeBindHost(net.ParseIP(ip[0])),
		noise.WithNodeBindPort(uint16(port)), noise.WithNodeMaxRecvMessageSize(128*1024*1024))
	n := &NoiseNetWork{
		id:      id,
		node:    node,
		peers:   myPeers,
		msgChan: msgChan,
		logger:  logger,
	}
	n.node.RegisterMessage(NoiseMessage{}, UnmarshalNoiseMessage)
	n.node.Handle(n.Handler)
	err := n.node.Listen()
	if err != nil {
		panic(err)
	}
	n.logger.Debugf("listening on %v", n.node.Addr())
	return n
}

func (n *NoiseNetWork) Start() {
}

func (n *NoiseNetWork) Stop() {
	n.node.Close()
}

func (n *NoiseNetWork) BroadcastMessage(msg *common.Message) {
	m := NoiseMessage{Msg: msg}
	for _, p := range n.peers {
		if p.ID == n.id {
			continue
		}
		go n.node.SendMessage(context.TODO(), p.Addr, m)
		//for {
		//	if err == nil {
		//		break
		//	}
		//	err = n.node.SendMessage(context.TODO(), p.Addr, m)
		//}
	}
}

func (n *NoiseNetWork) SendMessage(id uint32, msg *common.Message) {
	m := NoiseMessage{Msg: msg}
	err := n.node.SendMessage(context.TODO(), n.peers[id].Addr, m)
	for {
		if err == nil {
			return
		}
		err = n.node.SendMessage(context.TODO(), n.peers[id].Addr, m)
	}
}

func (n *NoiseNetWork) Handler(ctx noise.HandlerContext) error {
	obj, err := ctx.DecodeMessage()
	if err != nil {
		n.logger.Error("decode msg failed", err)
		return err
	}
	msg, ok := obj.(NoiseMessage)
	if !ok {
		n.logger.Error("cast msg failed", err)
		return nil
	}
	n.OnReceiveMessage(msg.Msg)
	return nil
}

func (n *NoiseNetWork) OnReceiveMessage(msg *common.Message) {
	if msg.Type == common.Message_BVAL || msg.Type == common.Message_PROM {
		tmp := &common.Message{
			Round:  msg.Round,
			Sender: msg.Sender,
			Type:   msg.Type,
			Hash:   msg.Hash,
		}
		data, err := tmp.Marshal()
		if err != nil {
			n.logger.Error("marshal msg in network recv failed", err)
			return
		}
		if !crypto.Verify(n.peers[msg.From].PublicKey, data, msg.Signature) {
			n.logger.Debugf("invalid %v signature in network recv", msg.Type)
			return
		}
	}
	n.msgChan <- msg
}
