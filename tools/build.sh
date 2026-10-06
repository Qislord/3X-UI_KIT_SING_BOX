#!/usr/bin/env bash
# Сборка статических бинарников kit-portal для Linux (без CGO)
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> Сборка kit-portal для Linux (amd64 и arm64)..."
mkdir -p dist

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -ldflags="-s -w" -o dist/kit-portal-linux-amd64 ./cmd/kit-portal
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags="-s -w" -o dist/kit-portal-linux-arm64 ./cmd/kit-portal

echo "✓ Бинарники успешно собраны:"
ls -lh dist/kit-portal-linux-*
