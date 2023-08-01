# LeoBat
This repo provides an implementation of LeoBat-coin, one of an asynchronous and leaderless Byzantine fault tolerance protocol based on the directed acyclic graph (DAG) structure. LeoBat-coin is a member of LeoBat protocol family, which uses Common Coin to guarantee liveness and can achieve fast termination when the fast path is satisfied.
## local test

```
cd leobat-go
cat conf/single/nodes > conf/nodes.txt
mkdir -p log
go build -o leobat
./leobat -c conf/single/node1.json -n 1 > log/node1.log 2>&1
./leobat -c conf/single/node2.json -n 1 > log/node2.log 2>&1
./leobat -c conf/single/node3.json -n 1 > log/node3.log 2>&1
./leobat -c conf/single/node4.json -n 1 > log/node4.log 112>&1
cd client
go build -o client
./client 127.0.0.1:7001 127.0.0.1:6001
./client 127.0.0.1:7002 127.0.0.1:6002
./client 127.0.0.1:7003 127.0.0.1:6003
./client 127.0.0.1:7004 127.0.0.1:6004
cd coordinator
go build -o coor
./coor -b 1000 -p 1 -t 10 -i 20 | tee -a result.txt
```

## WAN test

```
cd leobat-go
mkdir -p conf/multi
mkdir -p log
go build -o leobat
cd client
go build -o client
cd ../test/script
go build -o create
python3 aws.py [instance name] [dev name] [good num]
./create -n [node num] -f [tolerant fault num] (-w) (if create crash config files)
./deliverAll.sh [node num]
./nodes.sh [node num] [batch size] [payload] [test time] [conn num] [byzantine node num]
./clients.sh [node num]
./stop.sh [node num]

cd coordinator
go build -o coor
./coor -b [batch size] -p [payload] -t [test time] -i [client rate: req num per 50ms]| tee -a result.txt
```

## network port

coordinator --> client 6000+  
client --> node 7000+  
node --> node 5000+  
node --> coordinator 9000  


## crash && byzantine test mode

#### crash nodes 
locate in end regions  
do not create corresponding config files  
do not start node and client engine  

#### byzantine nodes 
locate in front regions  
create normal config files  
start node and client engine normally  