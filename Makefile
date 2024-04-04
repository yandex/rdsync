.PHONY: format lint unittests recreate_logs test start_sentinel_env run_jepsen_sentinel_test jepsen_sentinel_test start_cluster_env run_jepsen_cluster_test jepsen_cluster_test clean
PROJECT=rdsync
ZK_VERSION=3.9.1

cmd/rdsync/rdsync:
	GOOS=linux go build -tags netgo,osusergo -o ./cmd/rdsync/rdsync ./cmd/rdsync/...

format:
	gofmt -s -w `find . -name '*.go'`
	goimports -w `find . -name '*.go'`

lint:
	docker run --rm -v ${CURDIR}:/app -w /app golangci/golangci-lint:v1.55-alpine golangci-lint run -v

unittests:
	go test ./cmd/... ./internal/...
	go test ./cmd/... ./tests/testutil/matchers/

redis/src/redis-server:
	docker run --rm -v ${CURDIR}:/app -w /app ubuntu:jammy /app/redis_patches/build.sh

test: base_image redis/src/redis-server cmd/rdsync/rdsync recreate_logs
	rm -rf ./tests/images/redis/rdsync && cp cmd/rdsync/rdsync ./tests/images/redis/rdsync
	rm -rf ./tests/images/redis/redis-server && cp redis/src/redis-server ./tests/images/redis/redis-server
	rm -rf ./tests/images/redis/redis-senticache && cp redis/src/redis-senticache ./tests/images/redis/redis-senticache
	rm -rf ./tests/images/redis/redis-cli && cp redis/src/redis-cli ./tests/images/redis/redis-cli
	go build ./tests/...
	(cd tests; go test -timeout 150m)

recreate_logs:
	@if [ "$(shell ls tests/logs 2>/dev/null | wc -l)" != "0" ]; then\
		rm -rf ./tests/logs;\
	fi
	mkdir -p ./tests/logs

tests/images/zookeeper/zookeeper.tar.gz:
	wget https://archive.apache.org/dist/zookeeper/zookeeper-$(ZK_VERSION)/apache-zookeeper-$(ZK_VERSION)-bin.tar.gz -nc -O tests/images/zookeeper/zookeeper.tar.gz

base_image: tests/images/zookeeper/zookeeper.tar.gz
	@if [ "$(shell docker images | grep -c rdsync-base)" != "1" ]; then\
		docker build tests/images/base -t rdsync-base:latest;\
	fi

start_sentinel_env: base_image redis/src/redis-server cmd/rdsync/rdsync recreate_logs
	rm -rf ./tests/images/redis/rdsync && cp cmd/rdsync/rdsync ./tests/images/redis/rdsync
	rm -rf ./tests/images/redis/redis-server && cp redis/src/redis-server ./tests/images/redis/redis-server
	rm -rf ./tests/images/redis/redis-senticache && cp redis/src/redis-senticache ./tests/images/redis/redis-senticache
	rm -rf ./tests/images/redis/redis-cli && cp redis/src/redis-cli ./tests/images/redis/redis-cli
	docker compose -p $(PROJECT) -f ./tests/images/jepsen-compose.yaml up -d --force-recreate --build
	timeout 600 docker exec rdsync_zoo1_1 setup_zk.sh
	timeout 600 docker exec rdsync_redis1_1 setup_sentinel.sh
	timeout 600 docker exec rdsync_redis2_1 setup_sentinel.sh redis1
	timeout 600 docker exec rdsync_redis3_1 setup_sentinel.sh redis1

run_jepsen_sentinel_test: recreate_logs start_sentinel_env
	(docker exec rdsync_jepsen_1 /root/jepsen/run.sh >tests/logs/jepsen.log 2>&1 && tail -n 4 tests/logs/jepsen.log) || ./tests/images/jepsen/save_logs.sh

jepsen_sentinel_test: run_jepsen_sentinel_test clean

start_cluster_env: base_image redis/src/redis-server cmd/rdsync/rdsync recreate_logs
	rm -rf ./tests/images/redis/rdsync && cp cmd/rdsync/rdsync ./tests/images/redis/rdsync
	rm -rf ./tests/images/redis/redis-server && cp redis/src/redis-server ./tests/images/redis/redis-server
	rm -rf ./tests/images/redis/redis-senticache && cp redis/src/redis-senticache ./tests/images/redis/redis-senticache
	rm -rf ./tests/images/redis/redis-cli && cp redis/src/redis-cli ./tests/images/redis/redis-cli
	docker compose -p $(PROJECT) -f ./tests/images/jepsen-compose.yaml up -d --force-recreate --build
	timeout 600 docker exec rdsync_zoo1_1 setup_zk.sh
	timeout 600 docker exec rdsync_redis1_1 setup_cluster.sh
	timeout 600 docker exec rdsync_redis2_1 setup_cluster.sh redis1
	timeout 600 docker exec rdsync_redis3_1 setup_cluster.sh redis1

run_jepsen_cluster_test: recreate_logs start_cluster_env
	(docker exec rdsync_jepsen_1 /root/jepsen/run.sh >tests/logs/jepsen.log 2>&1 && tail -n 4 tests/logs/jepsen.log) || ./tests/images/jepsen/save_logs.sh

jepsen_cluster_test: run_jepsen_cluster_test clean

clean:
	docker ps | grep rdsync | awk '{print $$1}' | xargs -r docker rm -f || true
	docker network ls | grep rdsync | awk '{print $$1}' | xargs -r docker network rm || true
	rm -rf ./tests/logs
