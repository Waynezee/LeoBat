#!/bin/bash
# 运行设置丢包率脚本 ./runLoss.sh [server number] [scp:传文件 addmy:设置leobat各个server的丢包率] [loss rate]
# 开发机需要runLoss.sh 各个Server需要addLoss.sh 和 nodes.json 以及 sudo pip3 install tcconfig
# 10节点传文件举例：./runLoss.sh 10 scp
# 10节点 leobat 5%丢包率举例:  ./runLoss.sh 10 addMy 5 
# 10节点 hotstuff 5%丢包率举例:  ./runLoss.sh 10 addHot 5 
# 10清除丢包 ./runLoss.sh 10 del
svr_num=$1
will_do=$2
loss_rate=$3
fun_scpToSvr(){
    svrNum=$1
    for idx in $(seq 0 $((svrNum - 1)))
    do {
        host1=$(jq  '.nodes['$idx'].host'  nodes.json)
        host=${host1//\"/}
        

        expect -c "
        set timeout -1
        spawn scp -oStrictHostKeyChecking=no -i /root/.ssh/aws addLoss.sh ubuntu@$host:~/
        expect 100%
        exit
       "

        expect -c "
        set timeout -1
        spawn scp -oStrictHostKeyChecking=no -i /root/.ssh/aws nodes.json ubuntu@$host:~/
        expect 100%
        exit
       "

    } &
    done
    wait
}

fun_del(){
    svrNum=$1
    for idx in $(seq 0 $((svrNum - 1)))
    do {
        host1=$(jq  '.nodes['$idx'].host'  nodes.json)
        host=${host1//\"/}
        #echo "$host"

    expect <<-END
    set timeout -1
    spawn ssh -oStrictHostKeyChecking=no -i /root/.ssh/aws ubuntu@$host -p 22 "cd ~/;sudo tcdel ens5 --all"                
    expect EOF       
	END

    } &
    done
    wait

}

fun_addMy(){
    svrNum=$1
    loss=$2
    for idx in $(seq 0 $((svrNum - 1)))
    do {
       
        host1=$(jq  '.nodes['$idx'].host'  nodes.json)
        host=${host1//\"/}

        id1=$(jq  '.nodes['$idx'].id'  nodes.json)
        id=${id1//\"/}

        #echo $id

	expect <<-END
    spawn ssh -oStrictHostKeyChecking=no -i /root/.ssh/aws ubuntu@$host -p 22 "cd ~/;chmod +x addLoss.sh;sudo ./addLoss.sh $loss $id $svrNum"  
    expect EOF
    exit       
	END

    } &
    done
    wait

}

func_addHot(){
    svrNum=$1
    loss=$2
    name=$3
    for idx in $(seq 0 $((svrNum - 1)))
    do {
       
        host1=$(jq  '.nodes['$idx'].host'  nodes.json)
        host=${host1//\"/}

        id1=$(jq  '.nodes['$idx'].id'  nodes.json)
        id=${id1//\"/}

        #echo $id

	expect <<-END
    spawn ssh -oStrictHostKeyChecking=no -i /root/.ssh/aws ubuntu@$host -p 22 "cd ~/;chmod +x addLoss.sh;sudo ./addLoss.sh $loss $id $svrNum $name"  
    expect EOF
    exit       
	END

    } &
    done
    wait

}


case "$will_do" in
   "scp") fun_scpToSvr $svr_num
   ;;
   "addMy") fun_addMy $svr_num $loss_rate
   ;;
   "addHot") func_addHot $svr_num $loss_rate $will_do
   ;;
   "addTusk") echo "TODO tusk"
   ;;
   "del") fun_del $svr_num
   ;;
esac