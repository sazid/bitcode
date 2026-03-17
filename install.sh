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

TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT
curl -fsSL "$URL" -o "$TMPFILE"
chmod +x "$TMPFILE"

# --- Find a suitable install directory ---

# Candidate user-writable directories (checked in preference order)
USER_BIN_DIRS="$HOME/.local/bin $HOME/bin"

INSTALL_DIR=""
for dir in $USER_BIN_DIRS; do
    case ":$PATH:" in
        *":$dir:"*)
            if [ -d "$dir" ] && [ -w "$dir" ]; then
                INSTALL_DIR="$dir"
                break
            fi
            ;;
    esac
done

if [ -n "$INSTALL_DIR" ]; then
    mv "$TMPFILE" "$INSTALL_DIR/bitcode"
    echo "Installed to $INSTALL_DIR/bitcode"
    echo "Run 'bitcode' to get started."
    exit 0
fi

# No user-writable PATH directory found — ask the user
echo ""
echo "No user-writable directory found in your PATH."
echo ""
echo "Choose an install method:"
echo "  1) Install to ~/.local/bin (will add to PATH in your shell config)"
echo "  2) Install to /usr/local/bin (requires sudo)"
echo ""
printf "Choice [1]: "
read -r CHOICE </dev/tty

CHOICE=${CHOICE:-1}

case "$CHOICE" in
    1)
        INSTALL_DIR="$HOME/.local/bin"
        mkdir -p "$INSTALL_DIR"
        mv "$TMPFILE" "$INSTALL_DIR/bitcode"

        # Add to PATH in shell rc if not already present
        EXPORT_LINE='export PATH="$HOME/.local/bin:$PATH"'

        add_to_rc() {
            rcfile="$1"
            if [ -f "$rcfile" ]; then
                if ! grep -qF '.local/bin' "$rcfile"; then
                    printf '\n# Added by BitCode installer\n%s\n' "$EXPORT_LINE" >> "$rcfile"
                    echo "Updated $rcfile"
                fi
            fi
        }

        SHELL_NAME=$(basename "$SHELL" 2>/dev/null || echo "")
        UPDATED_RC=""

        case "$SHELL_NAME" in
            zsh)
                add_to_rc "$HOME/.zshrc"
                UPDATED_RC="$HOME/.zshrc"
                ;;
            bash)
                # Prefer .bashrc, fall back to .bash_profile on macOS
                if [ -f "$HOME/.bashrc" ]; then
                    add_to_rc "$HOME/.bashrc"
                    UPDATED_RC="$HOME/.bashrc"
                elif [ -f "$HOME/.bash_profile" ]; then
                    add_to_rc "$HOME/.bash_profile"
                    UPDATED_RC="$HOME/.bash_profile"
                else
                    add_to_rc "$HOME/.bashrc"
                    UPDATED_RC="$HOME/.bashrc"
                fi
                ;;
            fish)
                FISH_CONF="$HOME/.config/fish/config.fish"
                if [ -f "$FISH_CONF" ]; then
                    if ! grep -qF '.local/bin' "$FISH_CONF"; then
                        printf '\n# Added by BitCode installer\nfish_add_path "$HOME/.local/bin"\n' >> "$FISH_CONF"
                        echo "Updated $FISH_CONF"
                    fi
                fi
                UPDATED_RC="$FISH_CONF"
                ;;
            *)
                # Try both common rc files
                for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
                    if [ -f "$rc" ]; then
                        add_to_rc "$rc"
                        UPDATED_RC="$rc"
                    fi
                done
                ;;
        esac

        echo ""
        echo "Installed to $INSTALL_DIR/bitcode"
        if [ -n "$UPDATED_RC" ]; then
            echo "Restart your shell or run: source $UPDATED_RC"
        fi
        echo "Then run 'bitcode' to get started."
        ;;
    2)
        INSTALL_DIR="/usr/local/bin"
        sudo mv "$TMPFILE" "$INSTALL_DIR/bitcode"
        echo ""
        echo "Installed to $INSTALL_DIR/bitcode"
        echo "Run 'bitcode' to get started."
        ;;
    *)
        echo "Invalid choice. Aborted."
        exit 1
        ;;
esac
