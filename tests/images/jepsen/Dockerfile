FROM rdsync-base:latest
ENV LEIN_ROOT=1
COPY . /var/lib/dist/jepsen
RUN bash /var/lib/dist/jepsen/setup.sh
