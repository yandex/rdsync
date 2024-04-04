![Unit-tests-status](https://github.com/yandex/rdsync/workflows/Unit-tests/badge.svg)
![Linters-status](https://github.com/yandex/rdsync/workflows/Linters/badge.svg)
![Func-tests-status](https://github.com/yandex/rdsync/workflows/Func-tests/badge.svg)

# rdsync

Rdsync is a redis high-availability tool.
It uses a patched redis version to make a cluster or sentinel-like setup less prone to data loss.

## Limitations and requirements

* Patched redis (patches for redis 7.2 are included in this repo)
* ZooKeeper as DCS
* Single redis instance per host
* In clustered setup each shard must have it's own DCS prefix
* Client application must use `WAITQUORUM` command to make data loss less usual (check jepsen test for example).

## Try it out

* You will need a linux vm with gnu make, docker, docker compose and go >1.22 installed.
* Use `make start_sentinel_env` to start an environment with senticache
* Or `make start_cluster_env` to start an environment with single shard of clustered setup
* Run `make clean` to drop containers and network
