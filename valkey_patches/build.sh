#!/bin/bash

set -xe

apt update
DEBIAN_FRONTEND=noninteractive TZ=Etc/UTC apt -y install build-essential git
cd /app
git clone https://github.com/valkey-io/valkey.git
cd valkey
git checkout 8.0.2

for i in ../valkey_patches/*.patch
do
    git apply "${i}"
done

make -j
