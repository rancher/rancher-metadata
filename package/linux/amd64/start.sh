#!/bin/bash

ip addr add 169.254.169.250/32 dev eth0
exec "$@"
