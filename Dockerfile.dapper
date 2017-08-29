FROM golang:1.8.3
RUN go get github.com/rancher/trash
RUN go get github.com/golang/lint/golint
RUN curl -sfL https://get.docker.com/builds/Linux/x86_64/docker-1.12.6.tgz | tar xzf - -C /usr/bin --strip-components=1; 
ENV PATH /go/bin:$PATH
ENV DAPPER_SOURCE /go/src/github.com/rancher/rancher-metadata
ENV DAPPER_OUTPUT bin dist
ENV DAPPER_DOCKER_SOCKET true
ENV DAPPER_ENV TAG REPO CROSS WINDOWS_DOCKER_HOST
ENV GO15VENDOREXPERIMENT 1
ENV TRASH_CACHE ${DAPPER_SOURCE}/.trash-cache
WORKDIR ${DAPPER_SOURCE}
ENTRYPOINT ["./scripts/entry"]
CMD ["ci"]
