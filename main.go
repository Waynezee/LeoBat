package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"leobat-go/common"
	"leobat-go/consensus"
	"leobat-go/logger"
	"os"
	"strconv"
)

func main() {
	configFile := flag.String("c", "", "config file")
	payloadCon := flag.Int("n", 2, "payload connection num")
	identity := flag.Int("b", 0, "if byzantine node")
	flag.Parse()
	jsonFile, err := os.Open(*configFile)
	if err != nil {
		panic(err)
	}
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)
	c := new(common.ConfigFile)
	json.Unmarshal(byteValue, c)
	peers := make(map[uint32]common.Peer)
	for _, p := range c.Peers {
		peers[p.ID] = p
	}
	var iden bool
	if uint32(*identity) == 0 {
		iden = false
	} else {
		iden = true
	}
	node := consensus.NewNode(&c.Cfg, peers, logger.NewZeroLogger("./node"+strconv.FormatUint(uint64(c.Cfg.ID), 10)+".log"), uint32(*payloadCon), iden)
	node.Run()
}
