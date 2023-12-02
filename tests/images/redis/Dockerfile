FROM rdsync-base:latest
COPY . /var/lib/dist/redis
COPY ./rdsync /usr/bin/rdsync
COPY ./redis-server /usr/bin/redis-server
COPY ./redis-cli /usr/bin/redis-cli
COPY ./redis-senticache /usr/bin/redis-senticache
RUN bash /var/lib/dist/redis/setup.sh
