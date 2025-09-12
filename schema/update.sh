#!/bin/bash

# Script to update ACP schema files from the official repository
# Official repository: https://github.com/zed-industries/agent-client-protocol

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCHEMA_DIR="$SCRIPT_DIR"

OFFICIAL_REPO_BASE="https://raw.githubusercontent.com/zed-industries/agent-client-protocol/refs/heads/main/schema"

echo "🔄 Updating ACP schema files from official repository..."
echo "📁 Schema directory: $SCHEMA_DIR"

# Download schema.json
echo "⬇️  Downloading schema.json..."
if curl -fsSL "$OFFICIAL_REPO_BASE/schema.json" -o "$SCHEMA_DIR/schema.json.tmp"; then
    mv "$SCHEMA_DIR/schema.json.tmp" "$SCHEMA_DIR/schema.json"
    echo "✅ schema.json updated successfully"
else
    echo "❌ Failed to download schema.json"
    rm -f "$SCHEMA_DIR/schema.json.tmp"
    exit 1
fi

# Download meta.json
echo "⬇️  Downloading meta.json..."
if curl -fsSL "$OFFICIAL_REPO_BASE/meta.json" -o "$SCHEMA_DIR/meta.json.tmp"; then
    mv "$SCHEMA_DIR/meta.json.tmp" "$SCHEMA_DIR/meta.json"
    echo "✅ meta.json updated successfully"
else
    echo "❌ Failed to download meta.json"
    rm -f "$SCHEMA_DIR/meta.json.tmp"
    exit 1
fi

# Show file information
echo ""
echo "📊 Updated files:"
ls -la "$SCHEMA_DIR/schema.json" "$SCHEMA_DIR/meta.json"

echo ""
echo "🎉 Schema update completed successfully!"

echo ""
echo "⚡ Next steps:"
echo "   1. Review the changes in the updated schema files"
echo "   2. Update Go types if necessary"
echo "   3. Run tests to ensure compatibility: go test ./..."
echo "   4. Update documentation if new features are added"