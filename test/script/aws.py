import boto3
import json
import sys

# regions = ["us-east-2","us-west-1","ap-east-1","ap-southeast-1","ap-southeast-2","ap-northeast-1","ca-central-1","eu-central-1","eu-west-2","eu-north-1"]
# regions = ["us-east-2","ap-southeast-1","ap-northeast-1","ca-central-1","eu-central-1"]
# regions = ["us-east-2","ap-southeast-1","ap-northeast-1","eu-central-1"]
regions = ["us-east-1","us-east-2","us-west-1","ap-southeast-2"]
access_key = ""
secret_key = ""

total = {}
total["nodes"] = []
clients = {}
clients["nodes"] = []
coordinator = {}
server_id = 1
client_id = 1
num = int(sys.argv[1])
for region in regions:
    if num <= 0:
        break
    print("region:",region)
    ec2 = boto3.client('ec2',aws_access_key_id=access_key, aws_secret_access_key=secret_key,region_name=region)
    Filter = [
        {
            'Name': 'tag:Name',
            'Values': ["cf",]  # test instances name
        },
        {
            'Name': 'instance-state-name',
            'Values': ['running',]
        }
    ]
    response = ec2.describe_instances(Filters=Filter)
    instances = []
    for i in range(len(response['Reservations'])):
        instances += response['Reservations'][i]['Instances']
    print(len(instances))

    for i in range(len(instances)):
        if num <= 0:
            break
        status = instances[i]['State']['Name']
        if status != "running":
            continue
        instance = {}
        instance['id'] = server_id
        server_id += 1
        instance['host'] = instances[i]['PublicIpAddress']
        instance['privaddr'] = instances[i]['PrivateIpAddress']
        instance['port'] = "22"
        instance['user'] = "ubuntu"
        instance['keypath'] = "/root/.ssh/aws"
        instance['region'] = region
        total['nodes'].append(instance)
        num -= 1

print("----- begin to load -----")
file = "nodes.json"
with open(file,"w") as f:
    json.dump(total,f,indent=2)

coorEc2 = boto3.client('ec2',aws_access_key_id=access_key, aws_secret_access_key=secret_key,region_name="us-east-2")
coorResponse = coorEc2.describe_instances(Filters=[{'Name':'tag:Name','Values': ["leobat",]}])  # develop instance name
coordinator['private'] = coorResponse['Reservations'][0]['Instances'][0]['PrivateIpAddress']
coordinator['public'] = coorResponse['Reservations'][0]['Instances'][0]['PublicIpAddress']
file = "devip.json"
with open(file,"w") as f:
    json.dump(coordinator,f)

file = "../../conf/nodes.txt"
with open(file,"w") as f:
    index = 1
    for instance in total['nodes']:
        if index == len(total['nodes']):
            f.write(instance['host'] + ":" + str(6000+index))
        else:
            f.write(instance['host'] + ":" + str(6000+index) + "\n")
        index += 1

print("----- load success -----")
