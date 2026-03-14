#!/bin/bash

# Script to update ACP schema files from the official repository
# Official repository: https://github.com/zed-industries/agent-client-protocol
#
# Usage:
#   ./update.sh            # Update stable schemas only
#   ./update.sh --unstable # Update both stable and unstable schemas

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCHEMA_DIR="$SCRIPT_DIR"

OFFICIAL_REPO_BASE="https://raw.githubusercontent.com/zed-industries/agent-client-protocol/refs/heads/main/schema"

# Files to download: name and target path
STABLE_FILES=("schema.json" "meta.json")
UNSTABLE_FILES=("schema.unstable.json" "meta.unstable.json")

download_file() {
    local file="$1"
    echo "Downloading $file..."
    if curl -fsSL "$OFFICIAL_REPO_BASE/$file" -o "$SCHEMA_DIR/$file.tmp"; then
        mv "$SCHEMA_DIR/$file.tmp" "$SCHEMA_DIR/$file"
        echo "  OK"
    else
        echo "  FAILED"
        rm -f "$SCHEMA_DIR/$file.tmp"
        exit 1
    fi
}

# Parse arguments
INCLUDE_UNSTABLE=false
for arg in "$@"; do
    case "$arg" in
        --unstable) INCLUDE_UNSTABLE=true ;;
        *)
            echo "Unknown option: $arg"
            echo "Usage: $0 [--unstable]"
            exit 1
            ;;
    esac
done

echo "Updating ACP schema files from official repository..."
echo "Schema directory: $SCHEMA_DIR"
echo ""

# Download stable files
for file in "${STABLE_FILES[@]}"; do
    download_file "$file"
done

# Download unstable files if requested
if [ "$INCLUDE_UNSTABLE" = true ]; then
    echo ""
    echo "Including unstable schemas..."
    for file in "${UNSTABLE_FILES[@]}"; do
        download_file "$file"
    done
fi

# Show file information
echo ""
echo "Updated files:"
if [ "$INCLUDE_UNSTABLE" = true ]; then
    ls -la "$SCHEMA_DIR"/schema*.json "$SCHEMA_DIR"/meta*.json
else
    ls -la "$SCHEMA_DIR/schema.json" "$SCHEMA_DIR/meta.json"
fi

echo ""
echo "Done."
echo ""
echo "Next steps:"
echo "  1. Review the changes in the updated schema files"
echo "  2. Update Go types if necessary"
echo "  3. Run tests to ensure compatibility: go test ./..."
echo "  4. Update documentation if new features are added"
