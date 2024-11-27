#!/bin/bash

set -xe

cat <<EOF > /etc/apt/apt.conf.d/01buildconfig
Acquire::AllowInsecureRepositories "true";
Acquire::AllowDowngradeToInsecureRepositories "true";
APT::Install-Recommends "0";
APT::Get::Assume-Yes "true";
APT::Get::AllowUnauthenticated "true";
APT::Install-Suggests "0";
EOF

apt update

apt install less \
    bind9-host \
    net-tools \
    iputils-ping \
    sudo \
    telnet \
    git \
    supervisor \
    openssh-server \
    faketime \
    iptables \
    openssl \
    netcat-traditional

rm -rf /var/run
ln -s /dev/shm /var/run

ln -sf /usr/sbin/ip6tables-legacy /usr/sbin/ip6tables
ln -sf /usr/sbin/iptables-legacy /usr/sbin/iptables

mkdir -p /run/sshd
cp /var/lib/dist/base/sshd_config /etc/ssh/sshd_config
mkdir /root/.ssh
chmod 0700 /root/.ssh
yes | ssh-keygen -t rsa -N '' -f /root/.ssh/id_rsa
cp /root/.ssh/id_rsa.pub /root/.ssh/authorized_keys
chmod 0600 /root/.ssh/*

mkdir -p /etc/supervisor/conf.d
cp /var/lib/dist/base/supervisor.conf /etc/supervisor/supervisord.conf
cp /var/lib/dist/base/supervisor_ssh.conf /etc/supervisor/conf.d/ssh.conf
