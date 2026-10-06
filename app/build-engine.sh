#!/bin/bash
set -euo pipefail
cd "$SRCROOT/.."
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
mkdir -p "$TARGET_BUILD_DIR/$UNLOCALIZED_RESOURCES_FOLDER_PATH"
# Produce the architecture(s) Xcode requested, including universal Release builds.
outputs=()
for arch in $ARCHS; do
    case "$arch" in arm64) goarch=arm64 ;; x86_64) goarch=amd64 ;; *) echo "Unsupported architecture: $arch" >&2; exit 1 ;; esac
    output="$DERIVED_FILE_DIR/kioku-$arch"
    CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" CGO_CFLAGS="-arch $arch" CGO_LDFLAGS="-arch $arch" \
        go build -tags sqlite_fts5 -ldflags "-s -w" -o "$output" .
    outputs+=("$output")
done
if (( ${#outputs[@]} == 1 )); then
    cp "${outputs[0]}" "$TARGET_BUILD_DIR/$UNLOCALIZED_RESOURCES_FOLDER_PATH/kioku"
else
    lipo -create "${outputs[@]}" -output "$TARGET_BUILD_DIR/$UNLOCALIZED_RESOURCES_FOLDER_PATH/kioku"
fi
cp LICENSE "$TARGET_BUILD_DIR/$UNLOCALIZED_RESOURCES_FOLDER_PATH/Kioku-LICENSE"
cp internal/cjk/LICENSE "$TARGET_BUILD_DIR/$UNLOCALIZED_RESOURCES_FOLDER_PATH/cjk-LICENSE"
chmod 755 "$TARGET_BUILD_DIR/$UNLOCALIZED_RESOURCES_FOLDER_PATH/kioku"
codesign --force --sign - "$TARGET_BUILD_DIR/$UNLOCALIZED_RESOURCES_FOLDER_PATH/kioku"
