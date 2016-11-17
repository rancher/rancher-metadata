FROM ubuntu:16.04
RUN apt-get update && \
    apt-get install -y iptables iproute2 ca-certificates
COPY rancher-metadata router /usr/bin/
CMD ["rancher-metadata"]
