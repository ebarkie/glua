#!/bin/sh
# Builds C Lua 5.4.9 from source into bench/.clua for the comparison benchmarks, and
# records the compiler and flags next to the binary.
set -eu

VERSION=5.4.9
SHA256=2335b6c582a52654f94612bf10d2f4672805d05329aa6568b1d8cd9e5c6fb8e6
DIR=$(cd "$(dirname "$0")" && pwd)/.clua
SRC=$DIR/lua-$VERSION

mkdir -p "$DIR"
if [ ! -x "$SRC/src/lua" ]; then
	cd "$DIR"
	curl -sSfO "https://www.lua.org/ftp/lua-$VERSION.tar.gz"
	echo "$SHA256  lua-$VERSION.tar.gz" | sha256sum -c -
	tar xzf "lua-$VERSION.tar.gz"
	make -C "$SRC" linux >/dev/null
fi

{
	echo "lua $VERSION, sha256 $SHA256"
	echo "cc: $(${CC:-gcc} --version | head -1)"
	grep -E '^(CC|CFLAGS|MYCFLAGS)\s*=' "$SRC/src/Makefile"
	echo "target: linux (-DLUA_USE_LINUX)"
} > "$DIR/BUILDINFO"
cat "$DIR/BUILDINFO"
echo "$SRC/src/lua"
