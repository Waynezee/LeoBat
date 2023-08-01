#!/bin/bash

NUM=$1

for(( i = 0 ; i < NUM ; i++)); do
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
    private1=$(jq  '.config.client_server'  ../../conf/multi/$node.json)
    private=${private1//\"/}
    public1=$(jq  '.config.rpc_server'  ../../conf/multi/$node.json)
    public=${public1//\"/}
    {
	expect <<-END
	set timeout -1
	spawn ssh -oStrictHostKeyChecking=no -i $key $user@$host -p $port "cd leobat-go;./client $public $private"
	expect EOF
	exit
	END
    }
} &

done
sleep 500
wait
