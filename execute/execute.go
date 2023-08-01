package execute

import (
	"context"
	"leobat/common"
	"leobat/go-ycsb/pkg/client"
	"leobat/go-ycsb/pkg/measurement"
	"leobat/go-ycsb/pkg/prop"
	"leobat/go-ycsb/pkg/util"
	"leobat/go-ycsb/pkg/ycsb"
	"leobat/logger"
	"time"

	_ "leobat/go-ycsb/db/redis"
	_ "leobat/go-ycsb/pkg/workload"

	"github.com/gogo/protobuf/proto"
	"github.com/magiconair/properties"
)

type ExecuteInfo struct {
	Payload []byte
	Round   uint32
	Sender  uint32
}
type Executor struct {
	Pending    chan *ExecuteInfo
	Reply      chan *ExecuteInfo
	ycsbClient *client.Client
	logger     logger.Logger
}

var (
	propertyFiles  []string
	propertyValues []string
	dbName         string
	tableName      string

	globalContext context.Context
	globalCancel  context.CancelFunc

	globalDB       ycsb.DB
	globalWorkload ycsb.Workload
	globalProps    *properties.Properties
)

func initialGlobal(dbName string, onProperties func()) {
	globalContext, globalCancel = context.WithCancel(context.Background())
	globalProps = properties.NewProperties()
	propertyFiles = append(propertyFiles, "./workloadc") ///home/ubuntu/fastba-go/workloadc

	if len(propertyFiles) > 0 {
		globalProps = properties.MustLoadFiles(propertyFiles, properties.UTF8, false)
	}
	measurement.InitMeasure(globalProps)
	if len(tableName) == 0 {
		tableName = globalProps.GetString(prop.TableName, prop.TableNameDefault)
	}

	workloadName := globalProps.GetString(prop.Workload, "core")
	workloadCreator := ycsb.GetWorkloadCreator(workloadName)

	var err error
	if globalWorkload, err = workloadCreator.Create(globalProps); err != nil {
		util.Fatalf("create workload %s failed %v", workloadName, err)
	}

	dbCreator := ycsb.GetDBCreator(dbName)
	if dbCreator == nil {
		util.Fatalf("%s is not registered", dbName)
	}
	if globalDB, err = dbCreator.Create(globalProps); err != nil {
		util.Fatalf("create db %s failed %v", dbName, err)
	}
	globalDB = client.DbWrapper{globalDB}
}

func InitExecutor(reply chan *ExecuteInfo, logger logger.Logger) *Executor {
	e := &Executor{
		Pending: make(chan *ExecuteInfo, 1024),
		Reply:   reply,
		logger:  logger,
	}
	initialGlobal("redis", func() {
		doTransFlag := "true"
		globalProps.Set(prop.DoTransactions, doTransFlag)
		globalProps.Set(prop.Command, "run")
	})
	e.ycsbClient = client.NewClient(globalProps, globalWorkload, globalDB)
	go e.ExecuteLoop()
	return e
}

func (e *Executor) Execute(payload []byte, round uint32, sender uint32) {
	info := &ExecuteInfo{
		Payload: payload,
		Round:   round,
		Sender:  sender,
	}
	e.Pending <- info
	e.logger.Infof("execute pending info: %d %d\n", round, sender)
}

func (e *Executor) ExecuteLoop() {
	for {
		info := <-e.Pending
		start := time.Now().UnixNano()
		msg := new(common.Message)
		err := proto.Unmarshal(info.Payload, msg)
		if err != nil {
			e.logger.Infoln(err.Error())
		}
		batch := new(common.Batch)
		err = proto.Unmarshal(msg.Payload, batch)
		if err != nil {
			e.logger.Infoln(err.Error())
		}

		reqs := batch.Reqs
		for _, req := range reqs {
			var cmds common.Transactions
			err = proto.Unmarshal(req.Payload, &cmds)
			if err != nil {
				e.logger.Infoln(err.Error())
			}
			for _, tx := range cmds.Txs {
				e.ycsbClient.Execute(globalContext, tx.Op, tx.Table, tx.KeyName, tx.Fields, tx.Values)
			}
		}

		e.logger.Infof("execute info: %d %d %d\n", len(info.Payload), len(reqs), (time.Now().UnixNano()-start)/1000)
		e.Reply <- info
	}
}

func (e *Executor) ExecuteCore(payload []byte, round uint32, sender uint32) {
	start := time.Now().UnixNano()
	msg := new(common.Message)
	err := proto.Unmarshal(payload, msg)
	if err != nil {
		e.logger.Infoln(err.Error())
	}
	batch := new(common.Batch)
	err = proto.Unmarshal(msg.Payload, batch)
	if err != nil {
		e.logger.Infoln(err.Error())
	}

	reqs := batch.Reqs
	for _, req := range reqs {
		var cmds common.Transactions
		err = proto.Unmarshal(req.Payload, &cmds)
		if err != nil {
			e.logger.Infoln(err.Error())
		}
		for _, tx := range cmds.Txs {
			e.ycsbClient.Execute(globalContext, tx.Op, tx.Table, tx.KeyName, tx.Fields, tx.Values)
		}
	}

	e.logger.Infof("execute info: %d %d %d\n", len(payload), len(reqs), (time.Now().UnixNano()-start)/1000)
}
