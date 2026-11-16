#!/bin/sh
# amh retrieval benchmark (deterministic). exit 1 on failure.
set -eu
cd "$(dirname "$0")/.."
if [ ! -x ./amh ] && command -v go >/dev/null 2>&1; then
  go build -o amh ./cmd/amh
fi
./amh eval
