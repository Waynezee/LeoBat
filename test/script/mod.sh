#!/bin/bash

NODE=$1

BATCHSIZE=$2

PAYLOAD=$3

TIME=$4

SLICE=$5

IDENTITY=$6
    
cd ..

rm *.log

./mod --size=$BATCHSIZE --payload=$PAYLOAD --node=$NODE --time=$TIME

./leobat -c ./conf/$NODE.json -n $SLICE -b $IDENTITY $ >$NODE.log 2>&1

sleep 100

