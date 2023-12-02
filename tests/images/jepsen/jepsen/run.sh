#!/bin/bash

set -xe

ssh-keygen -p -f /root/.ssh/id_rsa -m pem -P "" -N ""
touch /root/.ssh/known_hosts
cd "$(dirname "$0")"
lein test
