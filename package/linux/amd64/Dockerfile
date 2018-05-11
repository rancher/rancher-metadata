FROM rancher/agent-base:v0.3.0
RUN curl -sLf https://github.com/leodotcloud/loglevel/releases/download/v0.1/loglevel-amd64-v0.1.tar.gz | tar xvzf - -C /usr/bin
COPY rancher-metadata start.sh /usr/bin/
CMD ["rancher-metadata"]
