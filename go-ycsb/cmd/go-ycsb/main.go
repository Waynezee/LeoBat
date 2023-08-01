// package main

// import (
// 	"leobat/common"
// 	"leobat/consensus"
// 	"leobat/logger"
// 	"encoding/json"
// 	"flag"
// 	"io/ioutil"
// 	"os"
// 	"strconv"
// )

// func main() {
// 	configFile := flag.String("c", "", "config file")
// 	// payload := flag.Int("payload", 1000, "payload size")
// 	payloadCon := flag.Int("n", 1, "payload connection num")
// 	flag.Parse()
// 	jsonFile, err := os.Open(*configFile)
// 	if err != nil {
// 		panic(err)
// 	}
// 	defer jsonFile.Close()
// 	byteValue, _ := ioutil.ReadAll(jsonFile)
// 	c := new(common.ConfigFile)
// 	json.Unmarshal(byteValue, c)

// 	peers := make(map[uint32]common.Peer)
// 	for _, p := range c.Peers {
// 		peers[p.ID] = p
// 		//fmt.Println(p.Addr)
// 	}
// 	node := consensus.NewNode(&c.Cfg, peers, logger.NewZeroLogger("./log/node"+strconv.FormatUint(uint64(c.Cfg.ID), 10)+".log"), uint32(*payloadCon))
// 	node.Run()
// }

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/magiconair/properties"

	"leobat/go-ycsb/pkg/client"

	"leobat/go-ycsb/pkg/prop"
	"leobat/go-ycsb/pkg/util"

	_ "leobat/go-ycsb/db/redis"
	"leobat/go-ycsb/pkg/measurement"
	_ "leobat/go-ycsb/pkg/workload"
	"leobat/go-ycsb/pkg/ycsb"
)

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
	propertyFiles = append(propertyFiles, "/home/ubuntu/Leobat-test/Leobat-ycsb/go-ycsb/workloads/workloadc") ///home/ubuntu/leobat-go/workloadc

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

type Transaction struct {
	Op      int64
	Table   string
	KeyName string
	Fields  []string
	Values  map[string][]byte
}

func Execute(doTransactions bool, command string, tran Transaction) {
	dbName = "redis"

	initialGlobal(dbName, func() {
		doTransFlag := "true"
		if !doTransactions {
			doTransFlag = "false"
		}
		globalProps.Set(prop.DoTransactions, doTransFlag)
		globalProps.Set(prop.Command, command)
	})

	// fmt.Println("***************** properties *****************")
	// for key, value := range globalProps.Map() {
	// 	fmt.Printf("\"%s\"=\"%s\"\n", key, value)
	// }
	// fmt.Println("**********************************************")

	c := client.NewClient(globalProps, globalWorkload, globalDB)
	before := time.Now()
	for i := 0; i < num; i++ {
		c.Execute(globalContext, cmd[i].Op, cmd[i].Table, cmd[i].KeyName, cmd[i].Fields, cmd[i].Values)
	}
	after := time.Now()
	fmt.Printf("Round took %v\n", after.Sub(before))
}

var cmd = []Transaction{}
var num = 7000

func Generate(doTransactions bool, command string) Transaction {
	dbName = "redis"

	initialGlobal(dbName, func() {
		doTransFlag := "true"
		if !doTransactions {
			doTransFlag = "false"
		}
		globalProps.Set(prop.DoTransactions, doTransFlag)
		globalProps.Set(prop.Command, command)
	})

	fmt.Println("***************** properties *****************")
	for key, value := range globalProps.Map() {
		fmt.Printf("\"%s\"=\"%s\"\n", key, value)
	}
	fmt.Println("**********************************************")

	c := client.NewClient(globalProps, globalWorkload, globalDB)
	for i := 0; i < num; i++ {
		op, table, keyName, fileds, values := c.Generate(globalContext)
		a := Transaction{
			Op:      op,
			Table:   table,
			KeyName: keyName,
			Fields:  fileds,
			Values:  values,
		}
		cmd = append(cmd, a)
	}
	fmt.Println(cmd[0])

	for k, v := range cmd[0].Values {
		fmt.Println("HGET "+cmd[0].Table+"/"+cmd[0].KeyName, k, string(v))
	}
	return cmd[0]
}

func main() {
	a := Generate(false, "load")
	fmt.Println("ssss")
	Execute(true, "run", a)
}
