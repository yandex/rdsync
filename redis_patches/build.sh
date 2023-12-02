#!/bin/bash

set -xe

apt update
DEBIAN_FRONTEND=noninteractive TZ=Etc/UTC apt -y install build-essential git
cd /app
git clone https://github.com/redis/redis.git
cd redis
git checkout 7.2.3

for i in ../redis_patches/*.patch
do
    git apply "${i}"
done

make -j
