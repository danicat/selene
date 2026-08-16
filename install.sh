#!/usr/bin/env bash

# install.sh - Selene Installer
# 1. Downloads prebuilt release binary (via GoReleaser from GitHub)
# 2. Falls back to 'go install github.com/danicat/selene/cmd/selene@latest' if download fails or if --build is requested

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

REPO="danicat/selene"
VERSION="latest"
BUILD_FROM_SOURCE="false"

print_usage() {
  cat << 'EOF'
Selene Installer

Usage:
  install.sh [options]
  curl -fsSL https://raw.githubusercontent.com/danicat/selene/main/install.sh | bash -s -- [options]

Options:
  -v, --version <v>        Target Selene release version (Default: latest)
      --build              Build from source via 'go install' instead of prebuilt binary
  -h, --help               Show this help message

Examples:
  ./install.sh                      # Install prebuilt binary for current OS/Arch
  ./install.sh -v v0.2.0            # Install specific version
  ./install.sh --build              # Build from source via 'go install'
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      print_usage
      exit 0
      ;;
    --build)
      BUILD_FROM_SOURCE="true"
      shift
      ;;
    -v|--version)
      VERSION="$2"
      shift 2
      ;;
    *)
      echo -e "${RED}Unknown option: $1${NC}"
      print_usage
      exit 1
      ;;
  esac
done

echo -e "${BLUE}===============================================${NC}"
echo -e "${BLUE}           Selene Installer                    ${NC}"
echo -e "${BLUE}===============================================${NC}"
echo -e "Version: ${BOLD}${VERSION}${NC}"
echo ""

# Determine target install directory for binary
GOPATH_BIN=""
if command -v go &> /dev/null; then
  GOBIN="$(go env GOBIN)"
  if [ -n "${GOBIN}" ]; then
    INSTALL_BIN_DIR="${GOBIN}"
  else
    INSTALL_BIN_DIR="$(go env GOPATH)/bin"
  fi
else
  INSTALL_BIN_DIR="${HOME}/.local/bin"
fi

mkdir -p "${INSTALL_BIN_DIR}"
BIN_PATH="${INSTALL_BIN_DIR}/selene"
BINARY_INSTALLED="false"

# 1. Attempt prebuilt release binary download (GoReleaser)
if [ "${BUILD_FROM_SOURCE}" != "true" ]; then
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  case "${ARCH}" in
    x86_64|amd64) ARCH="x64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) ARCH="" ;;
  esac

  if [ -n "${ARCH}" ] && [[ "${OS}" =~ ^(darwin|linux)$ ]]; then
    echo -e "📦 ${BLUE}[Binary] Fetching prebuilt binary for ${OS}.${ARCH}...${NC}"

    if [ "${VERSION}" = "latest" ]; then
      RELEASE_URL="https://github.com/${REPO}/releases/latest/download/${OS}.${ARCH}.selene.tar.gz"
    else
      CLEAN_VER="${VERSION#v}"
      RELEASE_URL="https://github.com/${REPO}/releases/download/v${CLEAN_VER}/${OS}.${ARCH}.selene.tar.gz"
    fi

    TMP_DIR="$(mktemp -d)"
    trap 'rm -rf "${TMP_DIR}"' EXIT
    TAR_FILE="${TMP_DIR}/selene.tar.gz"

    if curl -fsSL -o "${TAR_FILE}" "${RELEASE_URL}" 2>/dev/null; then
      tar -xzf "${TAR_FILE}" -C "${TMP_DIR}"
      if [ -f "${TMP_DIR}/bin/selene" ]; then
        mv "${TMP_DIR}/bin/selene" "${BIN_PATH}"
      elif [ -f "${TMP_DIR}/selene" ]; then
        mv "${TMP_DIR}/selene" "${BIN_PATH}"
      fi
      chmod +x "${BIN_PATH}"
      BINARY_INSTALLED="true"
      echo -e "${GREEN}✓ Downloaded and installed prebuilt binary to ${BIN_PATH}${NC}"
    else
      echo -e "${YELLOW}Notice: Prebuilt binary not found for ${OS}.${ARCH} at ${RELEASE_URL}. Falling back to 'go install'...${NC}"
    fi
  fi
fi

# 2. Fallback to 'go install' if binary not downloaded
if [ "${BINARY_INSTALLED}" != "true" ]; then
  if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: 'go' toolchain is required to build from source.${NC}"
    exit 1
  fi
  echo -e "🔨 ${BLUE}[Source] Building and installing via 'go install'...${NC}"
  go install "github.com/${REPO}/cmd/selene@${VERSION}"
  BINARY_INSTALLED="true"
  echo -e "${GREEN}✓ Installed via go install to ${BIN_PATH}${NC}"
fi

echo ""
echo -e "${GREEN}===============================================${NC}"
echo -e "${GREEN}✓ Selene is ready!${NC}"
echo -e "${GREEN}===============================================${NC}"
echo -e "Location: ${BOLD}${BIN_PATH}${NC}"

# Check PATH
if [[ ":$PATH:" != *":${INSTALL_BIN_DIR}:"* ]]; then
  echo ""
  echo -e "${YELLOW}⚠️  ${INSTALL_BIN_DIR} is not currently in your \$PATH.${NC}"
  echo -e "Add it to your shell configuration (.bashrc / .zshrc):"
  echo -e "  ${BOLD}export PATH=\"${INSTALL_BIN_DIR}:\$PATH\"${NC}"
fi

echo ""
"${BIN_PATH}" -version || true
