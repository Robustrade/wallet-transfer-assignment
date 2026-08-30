#!/usr/bin/env bash
set -euo pipefail
out="$(gofmt -l .)"
if [[ -n "${out}" ]]; then
  echo "${out}"
  exit 1
fi
