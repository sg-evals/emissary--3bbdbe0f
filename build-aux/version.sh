#!/bin/bash
set -euE -o pipefail

# capture Go psuedo version
version="$(\"$(dirname \"$0\")\"/../tools/bin/goversion)"

# output the tag version so that it can be used by CI for things such as container scanning
echo "${version:1}"
