#!/bin/bash

set -xe

apt update

apt install openjdk-17-jre-headless

tar -xzf /var/lib/dist/zookeeper/zookeeper.tar.gz -C /opt
mv /opt/apache-zookeeper* /opt/zookeeper

mkdir /var/log/zookeeper
cp /var/lib/dist/zookeeper/supervisor_zookeeper.conf /etc/supervisor/conf.d/zookeeper.conf
cp /var/lib/dist/zookeeper/retriable_path_create.sh /usr/local/bin/retriable_path_create.sh
cp /var/lib/dist/zookeeper/setup_zk.sh /usr/local/bin/setup_zk.sh
