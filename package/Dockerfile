FROM ubuntu:16.04
RUN apt-get update && \
    apt-get install -y --no-install-recommends iptables iproute2 ca-certificates
COPY rancher-metadata start.sh /usr/bin/
CMD ["rancher-metadata"]
