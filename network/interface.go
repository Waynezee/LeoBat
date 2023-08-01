package network

import (
	"leobat-go/common"
)

type NetWork interface {
	Start()
	Stop()
	BroadcastMessage(msg *common.Message)
	SendMessage(id uint32, msg *common.Message)
}
