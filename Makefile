.PHONY: format lint unittests recreate_logs test start_sentinel_env run_jepsen_sentinel_test jepsen_sentinel_test start_cluster_env run_jepsen_cluster_test jepsen_cluster_test clean
PROJECT=rdsync
ZK_VERSION=3.9.3

cmd/rdsync/rdsync:
	GOOS=linux GOEXPERIMENT=jsonv2 go build -tags netgo,osusergo -o ./cmd/rdsync/rdsync ./cmd/rdsync/...

format:
	gofmt -s -w `find . -name '*.go'`
	goimports -w `find . -name '*.go'`

lint:
	docker run --rm -v ${CURDIR}:/app -w /app golangci/golangci-lint:v2.4-alpine golangci-lint run -v

unittests:
	go test ./cmd/... ./internal/...
	go test ./cmd/... ./tests/testutil/matchers/

valkey/src/valkey-server:
	docker run --rm -v ${CURDIR}:/app -w /app ubuntu:noble /app/valkey_patches/build.sh

test: base_image valkey/src/valkey-server cmd/rdsync/rdsync recreate_logs
	rm -rf ./tests/images/valkey/rdsync && cp cmd/rdsync/rdsync ./tests/images/valkey/rdsync
	rm -rf ./tests/images/valkey/valkey-server && cp valkey/src/valkey-server ./tests/images/valkey/valkey-server
	rm -rf ./tests/images/valkey/valkey-senticache && cp valkey/src/valkey-senticache ./tests/images/valkey/valkey-senticache
	rm -rf ./tests/images/valkey/valkey-cli && cp valkey/src/valkey-cli ./tests/images/valkey/valkey-cli
	go build ./tests/...
	(cd tests; go test -timeout 180m)

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

start_sentinel_env: base_image valkey/src/valkey-server cmd/rdsync/rdsync recreate_logs
	rm -rf ./tests/images/valkey/rdsync && cp cmd/rdsync/rdsync ./tests/images/valkey/rdsync
	rm -rf ./tests/images/valkey/valkey-server && cp valkey/src/valkey-server ./tests/images/valkey/valkey-server
	rm -rf ./tests/images/valkey/valkey-senticache && cp valkey/src/valkey-senticache ./tests/images/valkey/valkey-senticache
	rm -rf ./tests/images/valkey/valkey-cli && cp valkey/src/valkey-cli ./tests/images/valkey/valkey-cli
	docker compose -p $(PROJECT) -f ./tests/images/jepsen-compose.yaml up -d --force-recreate --build
	timeout 600 docker exec rdsync-zoo1-1 setup_zk.sh
	timeout 600 docker exec rdsync-valkey1-1 setup_sentinel.sh
	timeout 600 docker exec rdsync-valkey2-1 setup_sentinel.sh valkey1
	timeout 600 docker exec rdsync-valkey3-1 setup_sentinel.sh valkey1

run_jepsen_sentinel_test: recreate_logs start_sentinel_env
	(docker exec rdsync-jepsen-1 /root/jepsen/run.sh >tests/logs/jepsen.log 2>&1 && tail -n 4 tests/logs/jepsen.log) || ./tests/images/jepsen/save_logs.sh

jepsen_sentinel_test: run_jepsen_sentinel_test clean

start_cluster_env: base_image valkey/src/valkey-server cmd/rdsync/rdsync recreate_logs
	rm -rf ./tests/images/valkey/rdsync && cp cmd/rdsync/rdsync ./tests/images/valkey/rdsync
	rm -rf ./tests/images/valkey/valkey-server && cp valkey/src/valkey-server ./tests/images/valkey/valkey-server
	rm -rf ./tests/images/valkey/valkey-senticache && cp valkey/src/valkey-senticache ./tests/images/valkey/valkey-senticache
	rm -rf ./tests/images/valkey/valkey-cli && cp valkey/src/valkey-cli ./tests/images/valkey/valkey-cli
	docker compose -p $(PROJECT) -f ./tests/images/jepsen-compose.yaml up -d --force-recreate --build
	timeout 600 docker exec rdsync-zoo1-1 setup_zk.sh
	timeout 600 docker exec rdsync-valkey1-1 setup_cluster.sh
	timeout 600 docker exec rdsync-valkey2-1 setup_cluster.sh valkey1
	timeout 600 docker exec rdsync-valkey3-1 setup_cluster.sh valkey1

run_jepsen_cluster_test: recreate_logs start_cluster_env
	(docker exec rdsync-jepsen-1 /root/jepsen/run.sh >tests/logs/jepsen.log 2>&1 && tail -n 4 tests/logs/jepsen.log) || ./tests/images/jepsen/save_logs.sh

jepsen_cluster_test: run_jepsen_cluster_test clean

clean:
	docker ps | grep rdsync | awk '{print $$1}' | xargs -r docker rm -f || true
	docker network ls | grep rdsync | awk '{print $$1}' | xargs -r docker network rm || true
	rm -rf ./tests/logs
