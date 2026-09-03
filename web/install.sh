#!/usr/bin/env sh
set -e

DOMAIN="tunnel.swag.best"
BASE_URL="https://${DOMAIN}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        echo "[-] Unsupported architecture: $ARCH"
        echo "    Supported: x86_64 (amd64), aarch64 (arm64)"
        exit 1
        ;;
esac

BINARY="dt-${OS}-${ARCH}"
URL="${BASE_URL}/releases/${BINARY}"

if [ -z "$XDG_BIN_HOME" ]; then
    XDG_BIN_HOME="$HOME/.local/bin"
fi

INSTALL_DIR="$XDG_BIN_HOME"
EXE_PATH="$INSTALL_DIR/dt"

echo ""
echo "  [+] dreamTunnel Installer"
echo "  [+] Platform: ${OS}/${ARCH}"
echo "  [+] Installing to: ${INSTALL_DIR}"
echo "  [+] Downloading: ${URL}"
echo ""

mkdir -p "$INSTALL_DIR" 2>/dev/null || true

if command -v curl > /dev/null 2>&1; then
    curl -fsSL "$URL" -o "$EXE_PATH"
elif command -v wget > /dev/null 2>&1; then
    wget -q "$URL" -O "$EXE_PATH"
else
    echo "[-] Neither curl nor wget found. Please install one of them."
    exit 1
fi

chmod +x "$EXE_PATH"

case ":$PATH:" in
    *":$INSTALL_DIR:"*)
        ;;
    *)
        echo "  [+] Adding ${INSTALL_DIR} to PATH"
        if [ -f "$HOME/.bashrc" ]; then
            echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$HOME/.bashrc"
        fi
        if [ -f "$HOME/.zshrc" ]; then
            echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$HOME/.zshrc"
        fi
        if [ -f "$HOME/.profile" ]; then
            echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$HOME/.profile"
        fi
        echo "  [+] Open a NEW terminal (or: export PATH=\"\$HOME/.local/bin:\$PATH\")"
        ;;
esac

echo ""
echo "  [+] Installed: $EXE_PATH"
echo ""
echo "  Usage:"
echo "      dt http 8080              expose local HTTP server"
echo "      dt http 8080 my-site      custom subdomain -> https://my-site.maiuni.online"
echo "      dt tcp 25565              raw TCP tunnel"
echo ""
echo "  Docs: https://${DOMAIN}/"
echo ""
