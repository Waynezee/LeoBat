#!/bin/bash 
$1
echo $1
rm *-*.log
python3 searchlog.py $1

# ./debug.sh execute //寻找node日志里 带有execute的行，并且输出到文件， $1 代表字符串