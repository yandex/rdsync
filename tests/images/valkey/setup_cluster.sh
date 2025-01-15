#!/bin/bash

set -xe

MASTER=${1}

supervisorctl stop valkey

cat >>/etc/valkey/valkey.conf <<EOF
cluster-enabled yes
cluster-config-file "/etc/valkey/cluster.conf"
cluster-slave-no-failover yes
cluster-allow-replica-migration no
EOF

supervisorctl start valkey

if [ "${MASTER}" != "" ]
then
    valkey-cli -e -a functestpassword -p 6379 config set offline no
    master_addr=$(host ${MASTER} | awk '{print $NF}')
    valkey-cli -e -a functestpassword -p 6379 cluster meet ${master_addr} 6379
    master_id=$(valkey-cli -e -a functestpassword -h ${master_addr} -p 6379 cluster myid)
    tries=0
    ok=0
    while [ ${tries} -le 60 ]
    do
        if valkey-cli -e -a functestpassword -p 6379 cluster nodes | grep -q ${master_id}
        then
            ok=1
            break
        else
            tries=$(( tries + 1 ))
            sleep 1
        fi
    done
    if [ "${ok}" != "1" ]
    then
        echo "Cluster meet failed"
        exit 1
    fi
    valkey-cli -e -a functestpassword -p 6379 cluster replicate ${master_id}
    tries=0
    ok=0
    while [ ${tries} -le 60 ]
    do
        if valkey-cli -e -a functestpassword -p 6379 cluster nodes | grep -q myself,slave
        then
            ok=1
            break
        else
            tries=$(( tries + 1 ))
            sleep 1
        fi
    done
    if [ "${ok}" != "1" ]
    then
        echo "Cluster replication init failed"
        exit 1
    fi
else
    valkey-cli -e -a functestpassword -p 6379 config set offline no
    valkey-cli -e -a functestpassword -p 6379 cluster addslotsrange 0 16383
fi

cp /var/lib/dist/valkey/supervisor_rdsync.conf /etc/supervisor/conf.d/rdsync.conf
cp /var/lib/dist/valkey/rdsync_cluster.yaml /etc/rdsync.yaml

/var/lib/dist/base/generate_certs.sh

supervisorctl update
