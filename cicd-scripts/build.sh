#!/bin/bash
# Copyright (c) 2021 Red Hat, Inc.
# Copyright Contributors to the Open Cluster Management project.
set -e

make docker-binary

echo "Building endpoint-monitoring-operator image"
export DOCKER_IMAGE_AND_TAG=${1}
export DOCKER_FILE=Dockerfile
make docker/build