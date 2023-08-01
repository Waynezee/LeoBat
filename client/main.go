package main

import (
	"context"
	"fmt"
	"leobat/common"
	"leobat/go-ycsb/pkg/client"
	"leobat/go-ycsb/pkg/measurement"
	"leobat/go-ycsb/pkg/prop"
	"leobat/go-ycsb/pkg/util"
	_ "leobat/go-ycsb/pkg/workload"
	"leobat/go-ycsb/pkg/ycsb"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/magiconair/properties"
)

type Client struct {
	publicAddr  string
	privateAddr string
	interval    int
	reqId       int32
	nodeId      uint32
	startChan   chan struct{}
	sendChan    chan struct{}
	stopChan    chan string
	zeroNum     uint32
	batch       int
	payload     int

	startTime          []uint64
	consensusLatencies []uint64
	executionLatencies []uint64
	clientLatencies    []uint64
	finishNum          uint64
	allTime            uint64
	blockNum           int
	badCoin            uint32
	exeStates          []int
	prodRound          uint32
	yscbClient         *client.Client
	rpcClient          *rpc.Client
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

	if onProperties != nil {
		onProperties()
	}

	if len(tableName) == 0 {
		tableName = globalProps.GetString(prop.TableName, prop.TableNameDefault)
	}

	workloadName := globalProps.GetString(prop.Workload, "core")
	workloadCreator := ycsb.GetWorkloadCreator(workloadName)

	var err error
	if globalWorkload, err = workloadCreator.Create(globalProps); err != nil {
		util.Fatalf("create workload %s failed %v", workloadName, err)
	}

}
func main() {
	c := &Client{
		publicAddr:         os.Args[1],
		privateAddr:        os.Args[2],
		reqId:              1,
		startChan:          make(chan struct{}, 1),
		sendChan:           make(chan struct{}, 1),
		stopChan:           make(chan string, 1),
		startTime:          make([]uint64, 0),
		consensusLatencies: make([]uint64, 0),
		executionLatencies: make([]uint64, 0),
		clientLatencies:    make([]uint64, 0),
		zeroNum:            0,
		finishNum:          0,
		allTime:            0,
		blockNum:           0,
		badCoin:            0,
		exeStates:          make([]int, 0),
		prodRound:          0,
	}
	initialGlobal("redis", func() {
		doTransFlag := "true"
		if !true {
			doTransFlag = "false"
		}
		globalProps.Set(prop.DoTransactions, doTransFlag)
		globalProps.Set(prop.Command, "load")
	})

	c.yscbClient = client.NewClient(globalProps, globalWorkload, globalDB)
	startRpcServer(c)

	cli, err := rpc.DialHTTP("tcp", c.publicAddr)
	if err != nil {
		panic(err)
	}
	c.rpcClient = cli

	<-c.startChan

	// payload := make([]byte, c.payload*c.interval)
	fmt.Printf("50ms send request number: %+v\n", c.interval)
	fmt.Printf("start send req\n")
	for {
		req := &common.ClientReq{
			StartId: c.reqId,
			ReqNum:  int32(c.interval),
			// Payload: payload,
		}
		c.reqId += int32(c.interval)
		// var resp common.ClientResp
		// go cli.Call("Node.Request", req, &resp)
		go c.SendReqest(req)
		c.startTime = append(c.startTime, uint64(time.Now().UnixNano()/1000000))
		time.Sleep(time.Millisecond * 50)
	}
}

func startRpcServer(server *Client) {
	rpc.Register(server)
	rpc.HandleHTTP()
	listener, err := net.Listen("tcp", server.privateAddr)
	if err != nil {
		panic(err)
	}
	go http.Serve(listener, nil)
}

func (cl *Client) SendReqest(req *common.ClientReq) {
	var resp common.ClientResp
	// startTime := time.Now().UnixNano()
	txs := new(common.Transactions)
	for i := 0; i < cl.interval; i++ {
		op, table, keyName, fields, values := cl.yscbClient.Generate(globalContext)
		tx := &common.Transaction{
			Op:      op,
			Table:   table,
			KeyName: keyName,
			Fields:  fields,
			Values:  values,
		}
		txs.Txs = append(txs.Txs, tx)
	}
	payload, _ := proto.Marshal(txs)
	req.Payload = payload
	// endTime := time.Now().UnixNano()
	// fmt.Println("payload size:", len(payload), " interval:", cl.interval, " time:", (endTime-startTime)/1000000)
	cl.rpcClient.Call("Node.Request", req, &resp)

}

func (cl *Client) OnStart(msg *common.CoorStart, resp *common.Response) error {
	fmt.Printf("receive coor\n")
	cl.batch = msg.Batch
	cl.payload = msg.Payload
	cl.interval = msg.Interval
	cl.startChan <- struct{}{}
	return nil
}

func (cl *Client) NodeFinish(msg *common.NodeBack, resp *common.Response) error {
	if msg.NodeID == 0 {
		cl.blockNum++
		cl.finishNum += uint64(msg.ReqNum)
		nowTime := uint64(time.Now().UnixNano() / 1000000)
		thisLatency := uint64(0)
		for i := 0; i < int(msg.ReqNum)/cl.interval; i++ {
			cl.allTime += ((nowTime - cl.startTime[msg.StartID/uint32(cl.interval)+uint32(i)]) * uint64(cl.interval))
			thisLatency += ((nowTime - cl.startTime[msg.StartID/uint32(cl.interval)+uint32(i)]) * uint64(cl.interval))
		}
		if msg.ReqNum == 0 {
			cl.clientLatencies = append(cl.clientLatencies, 0)
		} else {
			cl.clientLatencies = append(cl.clientLatencies, thisLatency/uint64(msg.ReqNum))
		}
	} else {
		cl.nodeId = msg.NodeID
		cl.exeStates = msg.States
		// cl.zeroNum = msg.Zero - uint32(cl.blockNum)
		cl.badCoin = msg.BadCoin
		cl.prodRound = msg.ReqNum
		cl.Stop(msg.Addr)
	}
	return nil
}

func (cl *Client) Stop(addr string) {
	conn, err := rpc.DialHTTP("tcp", addr)
	if err != nil {
		panic(err)
	}
	st := &common.CoorStatistics{
		// Zero:            cl.zeroNum,
		States:          cl.exeStates,
		BadCoin:         cl.badCoin,
		ConsensusNumber: uint64(cl.blockNum),
		ExecutionNumber: cl.finishNum,
		ID:              uint32(cl.nodeId),
		LatencyMap:      cl.clientLatencies,
		Zero:            cl.prodRound, // produce round num
	}
	if cl.finishNum == 0 {
		st.ExecutionLatency = 0
	} else {
		st.ExecutionLatency = cl.allTime
	}
	var resp common.Response
	conn.Call("Coordinator.Finish", st, &resp)
	fmt.Printf("call back\n")
}
