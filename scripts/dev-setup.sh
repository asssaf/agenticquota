#!/bin/bash
set -euo pipefail

# Script to set up Go, Google Cloud SDK (gcloud) and dev_appserver.py
# Target directory: ~/host-cache/gcloud

# Install Go (golang) if not already installed
if ! command -v go &> /dev/null; then
    echo "Go is not installed. Installing golang..."
    sudo apt update
    sudo apt install -y golang
fi

# Configure Go workspace and build cache to be under ~/host-cache/go
echo "Configuring Go workspace and cache locations under ~/host-cache/go..."
mkdir -p "$HOME/host-cache/go"
GOTOOLCHAIN=local go env -w GOPATH="$HOME/host-cache/go"
GOTOOLCHAIN=local go env -w GOCACHE="$HOME/host-cache/go/cache"



TARGET_DIR="$HOME/host-cache/gcloud"
SDK_DIR="$TARGET_DIR/google-cloud-sdk"

echo "Setting up Google Cloud SDK under $TARGET_DIR..."

# 1. Create target directory
mkdir -p "$TARGET_DIR"

# 2. Check if already installed
if [ -d "$SDK_DIR" ]; then
    echo "Google Cloud SDK already exists under $SDK_DIR."
else
    echo "Downloading Google Cloud SDK..."
    TARBALL="google-cloud-cli-linux-x86_64.tar.gz"
    DOWNLOAD_URL="https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/${TARBALL}"

    # Download tarball into target directory
    curl -o "$TARGET_DIR/$TARBALL" -L "$DOWNLOAD_URL"

    echo "Extracting SDK archive..."
    tar -xzf "$TARGET_DIR/$TARBALL" -C "$TARGET_DIR"

    # Clean up tarball
    rm -f "$TARGET_DIR/$TARBALL"
fi

# 3. Run installation script in non-interactive mode
echo "Running installation script..."
"$SDK_DIR/install.sh" --quiet


# 4. Install App Engine Go components
echo "Installing App Engine Go components..."
# Ensure gcloud CLI is in path for the component install
export PATH="$SDK_DIR/bin:$PATH"

gcloud components install app-engine-go --quiet

echo "--------------------------------------------------------"
echo "Google Cloud SDK and App Engine Go component installed!"
echo "To use gcloud and dev_appserver.py, run:"
echo "  export PATH=\"\$HOME/host-cache/gcloud/google-cloud-sdk/bin:\$PATH\""
echo "--------------------------------------------------------"


# Avoid "Compute Engine Metadata server unavailable"
export GOOGLE_APPLICATION_CREDENTIALS=/dev/null

# Install Elm if not already installed
ELM_DIR="$HOME/host-cache/elm"
ELM_BIN="$ELM_DIR/elm"

echo "Setting up Elm under $ELM_DIR..."
mkdir -p "$ELM_DIR"

if [ -f "$ELM_BIN" ] && [ -x "$ELM_BIN" ]; then
    echo "Elm is already installed at $ELM_BIN."
else
    echo "Downloading Elm..."
    # Download official Elm binary for linux-64-bit
    curl -L -o "$ELM_DIR/elm.gz" "https://github.com/elm/compiler/releases/download/0.19.1/binary-for-linux-64-bit.gz"
    echo "Decompressing Elm..."
    gunzip -f "$ELM_DIR/elm.gz"
    chmod +x "$ELM_BIN"
    echo "Elm installed successfully at $ELM_BIN"
fi

