#!/bin/bash

for i in 1 2 3
do
    mkdir -p tests/logs/valkey${i}
    mkdir -p tests/logs/zookeeper${i}

    for logfile in /var/log/rdsync.log /var/log/valkey/server.log /var/log/valkey/senticache.log /var/log/supervisor.log
    do
        logname=$(echo "${logfile}" | rev | cut -d/ -f1 | rev)
        docker exec rdsync-valkey${i}-1 cat "${logfile}" > "tests/logs/valkey${i}/${logname}"
    done

    docker exec rdsync-zoo${i}-1 cat /var/log/zookeeper/zookeeper.log > tests/logs/zookeeper${i}/zookeeper.log 2>&1
done

tail -n 18 tests/logs/jepsen.log
# Explicitly fail here
exit 1
