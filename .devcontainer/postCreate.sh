#!/usr/bin/env bash

set -e

# Download dependencies if go.mod exists
if [ -f go.mod ]; then
  go mod download
fi
