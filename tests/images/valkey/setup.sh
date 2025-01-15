#!/bin/bash

set -xe

mkdir -p /var/lib/valkey /var/log/valkey /etc/valkey
touch /var/log/valkey/senticache.log
cp /var/lib/dist/valkey/default.conf /etc/valkey/valkey.conf
cp /var/lib/dist/valkey/supervisor_valkey.conf /etc/supervisor/conf.d

cp /var/lib/dist/valkey/setup_*.sh /usr/local/bin
