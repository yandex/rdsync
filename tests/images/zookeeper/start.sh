#!/bin/bash

mkdir -p /tmp/zookeeper

cp /var/lib/dist/zookeeper/zoo.cfg /opt/zookeeper/conf/zoo.cfg

echo $ZK_MYID > /tmp/zookeeper/myid

/var/lib/dist/base/generate_certs.sh;

/opt/zookeeper/bin/zkServer.sh start-foreground
