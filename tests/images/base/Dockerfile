FROM ubuntu:noble
ENV container docker
ENV DEBIAN_FRONTEND noninteractive
COPY . /var/lib/dist/base
RUN bash /var/lib/dist/base/setup.sh
RUN chmod 755 /var/lib/dist/base/generate_certs.sh
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]
