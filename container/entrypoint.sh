#!/bin/sh
set -eu

# ---- TCP ports ----
: "${iport_clear:=1234}"
: "${iport_ssl:=1235}"
: "${sport_http:=8080}"
: "${sport_https:=8443}"

# ---- DB defaults  ----
: "${db_host:=db}"
: "${db_port:=5432}"
: "${db_user:=hc}"
: "${db_password:=hc}"
: "${db_ssl_mode:=disable}"

# ---- app defaults ----
: "${default_tenant_uuid:=9b9b1a6e-3d3e-4b2a-8f2c-3a51b84e4a0a}"
: "${random_string:=dev-pepper-change-me}"

# ---- cert paths inside container ----
: "${ca_certificate:=/certs/ca.crt}"
: "${host_certificate:=/certs/host.crt}"
: "${host_key:=/certs/host.key}"


export iport_clear iport_ssl sport_http sport_https \
       db_host db_port db_user db_password db_ssl_mode \
       default_tenant_uuid random_string \
       ca_certificate host_certificate host_key

envsubst < /config/hc-config.template.json > /config/hc-config.json

echo "HC using DB: ${db_user}@${db_host}:${db_port} sslmode=${db_ssl_mode}"
exec /usr/local/bin/hc serve --config /config/hc-config.json
