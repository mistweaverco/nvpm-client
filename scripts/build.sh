#!/usr/bin/env bash

build_wrapper() {
  echo "Building for $1 $2"
  local windows_file_extension=""
  if [ "$1" == "windows" ]; then
    windows_file_extension=".exe"
  fi
  GOOS=$1 GOARCH=$2 CGO_ENABLED=0 go build -ldflags "-X 'github.com/mistweaverco/nvpm-client/internal/lib/version.VERSION=${VERSION}'" -o "dist/nvpm-$1-$2$windows_file_extension"
}

build_linux_debug() {
  echo "Building for linux debug version $VERSION"
  GOOS=linux go build -gcflags "all=-N -l" -ldflags "-X 'github.com/mistweaverco/nvpm-client/lib/internal/version.VERSION=${VERSION}'" -o dist/nvpm-linux-debug
}

build_linux_arm64() {
  build_wrapper "linux" "arm64"
}

build_linux_x86() {
  build_wrapper "linux" "386"
}

build_linux_x86_64() {
  build_wrapper "linux" "amd64"
}

build_linux() {
  build_linux_x86
  build_linux_x86_64
}

build_macos_arm64() {
  build_wrapper "darwin" "arm64"
}

build_macos_x86_64() {
  build_wrapper "darwin" "amd64"
}

build_macos() {
  build_macos_arm64
  build_macos_x86_64
}

build_windows_x86() {
  build_wrapper "windows" "386"
}

build_windows_x86_64() {
  build_wrapper "windows" "amd64"
}

build_windows() {
  build_windows_x86
  build_windows_x86_64
}

case $PLATFORM in
  "linux")
    build_linux
    ;;
  "linux-arm64")
    build_linux_arm64
    ;;
  "linux-debug")
    build_linux_debug
    ;;
  "macos")
    build_macos
    ;;
  "windows")
    build_windows
    ;;
  *)
    echo "Error: PLATFORM $PLATFORM is not supported"
    exit 1
    ;;
esac
