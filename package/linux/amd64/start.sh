#!/bin/bash

METADATA_IP=${RANCHER_METADATA_ADDRESS:-169.254.169.250}
ip addr add ${METADATA_IP}/32 dev eth0
exec "$@"
