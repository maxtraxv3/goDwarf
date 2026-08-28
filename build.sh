#!/bin/bash
set -e
cd "$(dirname "$0")"

export ANDROID_HOME="${ANDROID_HOME:-$HOME/android-sdk}"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export EBITENMOBILE="${EBITENMOBILE:-$HOME/.local/go/bin/ebitenmobile}"

bash build/aar/build_apk.sh