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

        expect -c "

        set timeout -1

        spawn scp -i $key ../../leobat  $user@$host:leobat-go/

        expect 100%

        exit

       "
        } &
done

wait

