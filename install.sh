#!/bin/sh
set -e

echo ""
echo " ▄▀▀▄▄▀▀▄   BitCode"
echo " █▄▄██▄▄█"
echo "  ▀▀  ▀▀"
echo ""

VERSION=$(curl -fsSL https://api.github.com/repos/sazid/bitcode/releases/latest | grep '"tag_name"' | sed 's/.*"v\([^"]*\)".*/\1/')
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

URL="https://github.com/sazid/bitcode/releases/download/v${VERSION}/bitcode-${VERSION}-${OS}-${ARCH}"

echo "Installing v${VERSION} (${OS}/${ARCH})..."
curl -fsSL "$URL" -o bitcode
chmod +x bitcode

echo "Done! Run ./bitcode to get started."
