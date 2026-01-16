#!/bin/bash
set -e

# Configuration
REPO_URL="https://github.com/MystenLabs/sui-apis.git"
TEMP_DIR="sui-apis-temp"
TARGET_PROTO_DIR="internal/infra/chain/sui/proto"
GEN_DIR="internal/infra/chain/sui/generated"

echo "Using temporary directory: $TEMP_DIR"

# Cleanup previous temp
rm -rf $TEMP_DIR
# We do NOT remove TARGET_PROTO_DIR right away to avoid breaking things if clone fails, 
# but effectively we will overwrite.

# Clone the repository
echo "Cloning Sui APIs..."
git clone --depth 1 $REPO_URL $TEMP_DIR

# Prepare target directory
echo "Preparing target directory..."
rm -rf $TARGET_PROTO_DIR
mkdir -p $TARGET_PROTO_DIR

# Copy proto files
# Depending on the structure of sui-apis, we copy the relevant parts.
# Usually they are at the root or under 'sui'.
echo "Copying proto files..."
cp -r $TEMP_DIR/* $TARGET_PROTO_DIR/

# Clean up temp
rm -rf $TEMP_DIR

# Inject go_package option
# This is necessary because the official protos might not have the go_package matching our structure
echo "Injecting go_package options..."
find $TARGET_PROTO_DIR -name "*.proto" -exec sed -i 's|option go_package = ".*";|option go_package = "github.com/vietddude/watcher/internal/infra/chain/sui/generated/sui/rpc/v2";|g' {} +
# Also generic injection if missing (simple heuristic)
# find $TARGET_PROTO_DIR -name "*.proto" -exec sed -i '/syntax = "proto3";/a option go_package = "github.com/vietddude/watcher/internal/infra/chain/sui/generated/sui/rpc/v2";' {} +

# Generate Go code
echo "Generating Go code..."
mkdir -p $GEN_DIR

# We need to include the proto directory itself as include path
protoc --proto_path=$TARGET_PROTO_DIR \
       --go_out=$GEN_DIR --go_opt=paths=source_relative \
       --go-grpc_out=$GEN_DIR --go-grpc_opt=paths=source_relative \
       $(find $TARGET_PROTO_DIR -name "*.proto")

echo "Done."
