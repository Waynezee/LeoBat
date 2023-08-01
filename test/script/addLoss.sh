#!/bin/bash
# 增加丢包率 ./addloss.sh [loss rate] [server id] [server num] [protocl name]
# node1 增加5%的丢包率 ./addLoss.sh 5 1 10 addHot
loss=$1
svrId=$2
svrNum=$3
name=$4

addport=0

for(( i = 0 ; i < svrNum ; i++));
{

    id1=$(jq  '.nodes['$i'].id'  nodes.json)
    id=${id1//\"/}

    if [ $id == $svrId ]
    then 
        continue
    fi

    host1=$(jq  '.nodes['$i'].host'  nodes.json)
    host=${host1//\"/}

    if [[ $name = "addHot" && $addport == 0 ]]
    then
        sudo tcset ens5 --add --loss $loss% --dst-network $host --exclude-dst-port 6000
        addport=1
        echo "except port 6000"
    else
        sudo tcset ens5 --add --loss $loss% --dst-network $host
    fi
      
    echo "add node$id $loss% loss"    
}



 

