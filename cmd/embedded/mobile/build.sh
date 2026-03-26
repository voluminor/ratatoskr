#!/usr/bin/env bash
set -euo pipefail

# Builds gomobile bindings for Android and/or iOS.
# Usage: ./build.sh [android|ios|all]
# Default: all

TARGET="${1:-all}"
OUT_DIR="$(cd "$(dirname "$0")" && pwd)/dist"
mkdir -p "$OUT_DIR"

# Detect Android SDK
if [[ -z "${ANDROID_HOME:-}" ]]; then
  for candidate in \
    "$HOME/Library/Android/sdk" \
    "$HOME/Android/Sdk" \
    "/opt/android-sdk"
  do
    if [[ -d "$candidate" ]]; then
      export ANDROID_HOME="$candidate"
      break
    fi
  done
fi

build_android() {
  echo "Building Android AAR..."
  gomobile bind \
    -target=android \
    -androidapi=21 \
    -javapkg="link.yggdrasil.ratatoskr" \
    -ldflags="-s -w -checklinkname=0" \
    -o="$OUT_DIR/ratatoskr.aar" \
    .
  echo "Android build complete: $OUT_DIR/ratatoskr.aar"
}

build_ios() {
  echo "Building iOS XCFramework..."
  gomobile bind \
    -target=ios \
    -ldflags="-s -w" \
    -o="$OUT_DIR/Ratatoskr.xcframework" \
    .
  echo "iOS build complete: $OUT_DIR/Ratatoskr.xcframework"
}

case "$TARGET" in
  android) build_android ;;
  ios)     build_ios ;;
  all)     build_android; build_ios ;;
  *) echo "Usage: $0 [android|ios|all]"; exit 1 ;;
esac
