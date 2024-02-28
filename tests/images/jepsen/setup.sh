#!/bin/bash

set -xe

apt update
apt install openjdk-17-jre-headless libjna-java gnuplot wget
chmod 600 /root/.ssh/id_rsa
wget https://raw.githubusercontent.com/technomancy/leiningen/stable/bin/lein -O /usr/bin/lein
chmod +x /usr/bin/lein
cp /var/lib/dist/jepsen/ssh_config /etc/ssh/ssh_config
cp -r /var/lib/dist/jepsen/jepsen /root/
cd /root/jepsen
lein install
lein deps
