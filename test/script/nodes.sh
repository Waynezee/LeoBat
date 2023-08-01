#!/bin/bash

NUM=$1
BATCHSIZE=$2
PAYLOAD=$3
TIME=$4
SLICE=$5
SPECIAL=$6

for(( i = 0 ; i < SPECIAL ; i++)); do
{
    host1=$(jq  '.nodes['$i'].host'  nodes.json)
    host=${host1//\"/}
    port1=$(jq  '.nodes['$i'].port'  nodes.json)
    port=${port1//\"/}
    user1=$(jq  '.nodes['$i'].user' nodes.json)
    user=${user1//\"/}
    key1=$(jq  '.nodes['$i'].keypath' nodes.json)
    key=${key1//\"/}
    id1=$(jq  '.nodes['$i'].id'  nodes.json)
    id=${id1//\"/}
    node="node"$id
   
    {
	expect <<-END
	set timeout -1
	spawn ssh -oStrictHostKeyChecking=no -i $key $user@$host -p $port "cd leobat-go/script;chmod +x mod.sh;./mod.sh $node $BATCHSIZE $PAYLOAD $TIME $SLICE 1"
	expect EOF
	exit
	END
    }
} &

done

for(( i = SPECIAL ; i < NUM ; i++)); do
{
    host1=$(jq  '.nodes['$i'].host'  nodes.json)
    host=${host1//\"/}
    port1=$(jq  '.nodes['$i'].port'  nodes.json)
    port=${port1//\"/}
    user1=$(jq  '.nodes['$i'].user' nodes.json)
    user=${user1//\"/}
    key1=$(jq  '.nodes['$i'].keypath' nodes.json)
    key=${key1//\"/}
    id1=$(jq  '.nodes['$i'].id'  nodes.json)
    id=${id1//\"/}
    node="node"$id
   
    {
	expect <<-END
	set timeout -1
	spawn ssh -oStrictHostKeyChecking=no -i $key $user@$host -p $port "cd leobat-go/script;chmod +x mod.sh;./mod.sh $node $BATCHSIZE $PAYLOAD $TIME $SLICE 0"
	expect EOF
	exit
	END
    }
} &

done
sleep 500
wait
