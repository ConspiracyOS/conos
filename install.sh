#!/bin/sh
# Install conos CLI — downloads the latest release binary for your platform.
# Usage: curl -fsSL https://conspiracyos.com/install.sh | sh
set -e

REPO="ConspiracyOS/conos"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $ARCH" >&2
        exit 1
        ;;
esac

case "$OS" in
    linux|darwin) ;;
    *)
        echo "Unsupported OS: $OS" >&2
        exit 1
        ;;
esac

BINARY="conos-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"

echo "Downloading conos for ${OS}/${ARCH}..."
if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o /tmp/conos "$URL"
elif command -v wget >/dev/null 2>&1; then
    wget -q -O /tmp/conos "$URL"
else
    echo "Neither curl nor wget found. Install one and retry." >&2
    exit 1
fi

chmod +x /tmp/conos

# Install to INSTALL_DIR (may need sudo)
if [ -w "$INSTALL_DIR" ]; then
    mv /tmp/conos "$INSTALL_DIR/conos"
else
    echo "Installing to $INSTALL_DIR (requires sudo)..."
    sudo mv /tmp/conos "$INSTALL_DIR/conos"
fi

echo ""
echo "conos installed to $INSTALL_DIR/conos"
echo ""
echo "Next steps:"
echo "  export CONOS_OPENROUTER_API_KEY=sk-or-your-key-here"
echo "  conos install"
echo ""
echo "This will pull the container image, start it, configure SSH,"
echo "and generate your config. Then:"
echo "  conos agent \"What agents are running?\""
