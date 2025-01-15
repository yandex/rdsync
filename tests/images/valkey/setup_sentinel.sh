#!/bin/bash

set -xe

MASTER=${1}

if [ "${MASTER}" != "" ]
then
    valkey-cli -e -a functestpassword -p 6379 config set offline no
    master_addr=$(host ${MASTER} | awk '{print $NF}')
    valkey-cli -e -a functestpassword -p 6379 replicaof ${master_addr} 6379
    valkey-cli -e -a functestpassword -p 6379 config rewrite
    tries=0
    ok=0
    while [ ${tries} -le 60 ]
    do
        if valkey-cli -e -a functestpassword -p 6379 info replication | grep -q master_link_status:up
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
else
    valkey-cli -e -a functestpassword -p 6379 config set offline no
fi

cp /var/lib/dist/valkey/supervisor_rdsync.conf /etc/supervisor/conf.d/rdsync.conf
cp /var/lib/dist/valkey/rdsync_sentinel.yaml /etc/rdsync.yaml

cp /var/lib/dist/valkey/supervisor_senticache.conf /etc/supervisor/conf.d/senticache.conf
cp /var/lib/dist/valkey/senticache.conf /etc/valkey/senticache.conf

/var/lib/dist/base/generate_certs.sh

supervisorctl update
