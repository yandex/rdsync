#!/bin/bash

set -xe

mkdir -p /var/lib/redis /var/log/redis /etc/redis
touch /var/log/redis/senticache.log
cp /var/lib/dist/redis/default.conf /etc/redis/redis.conf
cp /var/lib/dist/redis/supervisor_redis.conf /etc/supervisor/conf.d

cp /var/lib/dist/redis/setup_*.sh /usr/local/bin
