FROM rdsync-base:latest
COPY . /var/lib/dist/valkey
COPY ./rdsync /usr/bin/rdsync
COPY ./valkey-server /usr/bin/valkey-server
COPY ./valkey-cli /usr/bin/valkey-cli
COPY ./valkey-senticache /usr/bin/valkey-senticache
RUN bash /var/lib/dist/valkey/setup.sh
