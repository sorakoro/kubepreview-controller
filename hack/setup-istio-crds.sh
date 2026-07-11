#!/usr/bin/env bash
set -euo pipefail

# Download Istio networking CRDs from the go module cache for use in envtest.

OUTPUT_DIR="${1:-config/crd/istio}"

# Skip if already downloaded
if [ -f "${OUTPUT_DIR}/virtualservices.networking.istio.io.yaml" ]; then
  exit 0
fi

# Resolve istio.io/api version and path from go module cache
ISTIO_API_VERSION="$(go list -m -f '{{.Version}}' istio.io/api 2>/dev/null)"
ISTIO_API_DIR="$(go env GOMODCACHE)/istio.io/api@${ISTIO_API_VERSION}"
CRD_SOURCE="${ISTIO_API_DIR}/kubernetes/customresourcedefinitions.gen.yaml"

if [ ! -f "${CRD_SOURCE}" ]; then
  echo "Error: CRD source not found at ${CRD_SOURCE}" >&2
  echo "Run 'go mod download istio.io/api' first." >&2
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"

# Extract networking CRDs (VirtualService, Gateway, etc.) from the bundled YAML.
# Each CRD is separated by "---".
echo "Extracting Istio networking CRDs from ${CRD_SOURCE}"
awk '
  /^---$/ { flush(); next }
  { buf = buf $0 "\n" }
  /^  name:.*networking\.istio\.io$/ { name = $2 }
  END { flush() }
  function flush() {
    if (name != "") {
      outfile = "'"${OUTPUT_DIR}"'/" name ".yaml"
      printf "%s", buf > outfile
      print "  -> " outfile
      name = ""
    }
    buf = ""
  }
' "${CRD_SOURCE}"
