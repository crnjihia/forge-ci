# path: scripts/build_release.sh
#!/usr/bin/env bash
# Forge CI - Multi-Platform Release Cross-Compilation and Packaging Script
# Builds forge-api and forge-runner binaries across Linux, macOS, and Windows.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo "v1.0.0")}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo "none")"
BUILD_DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

echo "================================================================"
echo " Forge CI Release Packaging"
echo " Version:    ${VERSION}"
echo " Commit:     ${COMMIT}"
echo " Build Date: ${BUILD_DATE}"
echo " Output Dir: ${DIST_DIR}"
echo "================================================================"

rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

# Build Frontend Web Dashboard if node & npm are available
if command -v npm &>/dev/null && [ -d "${ROOT_DIR}/web" ]; then
    echo "==> Building React 18 production frontend..."
    pushd "${ROOT_DIR}/web" > /dev/null
    if [ ! -d "node_modules" ]; then
        npm ci || npm install
    fi
    npm run build
    popd > /dev/null
    echo "==> Frontend built successfully into web/dist."
else
    echo "==> Skipping frontend build (node/npm not found or web/ already built)."
fi

# Target matrix: OS/ARCH
TARGETS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
)

LDFLAGS="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}"

for TARGET in "${TARGETS[@]}"; do
    GOOS="${TARGET%%/*}"
    GOARCH="${TARGET##*/}"
    
    EXT=""
    if [ "${GOOS}" == "windows" ]; then
        EXT=".exe"
    fi
    
    TARGET_NAME="forge-ci_${VERSION}_${GOOS}_${GOARCH}"
    STAGE_DIR="${DIST_DIR}/${TARGET_NAME}"
    mkdir -p "${STAGE_DIR}/bin"
    
    echo "==> Compiling for ${GOOS}/${GOARCH}..."
    
    # 1. Build forge-api
    CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go build \
        -trimpath \
        -ldflags "${LDFLAGS}" \
        -o "${STAGE_DIR}/bin/forge-api${EXT}" \
        "${ROOT_DIR}/cmd/api"
        
    # 2. Build forge-runner
    CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go build \
        -trimpath \
        -ldflags "${LDFLAGS}" \
        -o "${STAGE_DIR}/bin/forge-runner${EXT}" \
        "${ROOT_DIR}/cmd/runner"

    # 3. Copy support files & migrations
    cp -r "${ROOT_DIR}/migrations" "${STAGE_DIR}/migrations"
    cp -r "${ROOT_DIR}/deployments" "${STAGE_DIR}/deployments" 2>/dev/null || true
    cp "${ROOT_DIR}/README.md" "${STAGE_DIR}/README.md" 2>/dev/null || true
    if [ -f "${ROOT_DIR}/LICENSE" ]; then
        cp "${ROOT_DIR}/LICENSE" "${STAGE_DIR}/LICENSE"
    fi
    if [ -d "${ROOT_DIR}/web/dist" ]; then
        cp -r "${ROOT_DIR}/web/dist" "${STAGE_DIR}/web-dist"
    fi

    # 4. Package Archive (.tar.gz or .zip)
    echo "==> Creating archive for ${TARGET_NAME}..."
    pushd "${DIST_DIR}" > /dev/null
    if [ "${GOOS}" == "windows" ] && command -v zip &>/dev/null; then
        zip -q -r "${TARGET_NAME}.zip" "${TARGET_NAME}"
    else
        tar -czf "${TARGET_NAME}.tar.gz" "${TARGET_NAME}"
    fi
    rm -rf "${STAGE_DIR}"
    popd > /dev/null
done

# 5. Generate Checksums
echo "==> Computing SHA256 checksums..."
pushd "${DIST_DIR}" > /dev/null
if command -v sha256sum &>/dev/null; then
    sha256sum forge-ci_* > checksums.txt
elif command -v shasum &>/dev/null; then
    shasum -a 256 forge-ci_* > checksums.txt
fi
popd > /dev/null

echo "================================================================"
echo " Packaging completed successfully!"
ls -la "${DIST_DIR}"
echo "================================================================"

