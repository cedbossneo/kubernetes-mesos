#!/usr/bin/env bash

EXPECTED_SCRIPT_DIR=/docker/complete

set -e

script_dir=$(cd $(dirname $0) && pwd -P)

# desense against lazy refactoring
[[ "$script_dir" != *"$EXPECTED_SCRIPT_DIR" ]]  && echo "Build script has moved! Expected location: $EXPECTED_SCRIPT_DIR" && exit 1

cd ${script_dir}/../..

# build image with just the binaries
docker build -f "./$EXPECTED_SCRIPT_DIR/Dockerfile" -t mesosphere/kubernetes-mesos-complete .
