# syntax=docker/dockerfile:1.6

FROM golang:1.22-alpine AS build
WORKDIR /src

RUN apk add --no-cache git make bash

RUN git clone https://github.com/alessandrocarminati/hc .

RUN set -eux; \
    make

RUN set -eux; \
    mkdir -p /out; \
    cp -L hc.app /out/hc

FROM alpine:3.20
WORKDIR /app

RUN apk add --no-cache gettext

RUN mkdir -p /certs

COPY --from=build /out/hc /usr/local/bin/hc

COPY --from=build /src/container/hc-config.template.json /config/hc-config.template.json

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 1234 1235 8080 8443
ENTRYPOINT ["/entrypoint.sh"]
