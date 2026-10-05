#!/bin/sh
# Baut den SIGNL Download-Server für alle Systeme nach dist/.
# Voraussetzung: Go ab 1.22.   Aufruf: ./build.sh [Version]
set -e
cd "$(dirname "$0")"
VERSION="${1:-1.0.0}"
rm -rf dist && mkdir -p dist

build() { # os arch name
  out="dist/signl-server-$VERSION-$1-$2"
  mkdir -p "$out"
  bin="signl-server"; [ "$1" = windows ] && bin="signl-server.exe"
  CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "-s -w -X main.Version=$VERSION" -o "$out/$bin" .
  cp LIES-MICH.md "$out/"
  case "$1" in
    windows) cp packaging/windows/*.bat "$out/" ;;
    linux)   cp packaging/linux/signl-server.service "$out/" ;;
    darwin)  cp packaging/macos/*.plist "$out/" ;;
  esac
  if [ "$1" = windows ]; then (cd dist && zip -qr "$(basename "$out").zip" "$(basename "$out")")
  else (cd dist && tar -czf "$(basename "$out").tar.gz" "$(basename "$out")"); fi
  rm -rf "$out"
  echo "  ✓ $1/$2"
}

build darwin arm64
build darwin amd64
build linux amd64
build linux arm64
build linux arm
build windows amd64
build windows arm64
( cd dist && shasum -a 256 * > SHA256SUMS.txt )
echo "Fertig: $(pwd)/dist"
