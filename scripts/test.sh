#!/usr/bin/env bash
# path: scripts/test.sh
set -euo pipefail

echo "=================================================="
echo "   Forge CI — Automated Test & Build Suite        "
echo "=================================================="

# 1. Go Verification
echo -e "\n[1/3] Running Go Vet across all modules..."
go vet ./...

echo -e "\n[2/3] Compiling and running Go test suites..."
go test -v ./...

# 3. Web UI Verification
echo -e "\n[3/3] Type-checking and building React 18 Web UI..."
cd web
npm run build
cd ..

echo -e "\n=================================================="
echo "   All tests and builds passed successfully!     "
echo "=================================================="

