#!/bin/sh
# Installs lumen from the releases on GitHub:
#
#     curl -fsSL https://raw.githubusercontent.com/dani-gouken/lumen-lang/main/install.sh | sh
#
# LUMEN_VERSION picks a release (default: the latest), LUMEN_INSTALL the
# folder the binary goes in (default: ~/.local/bin).
set -eu

repo="dani-gouken/lumen-lang"
version="${LUMEN_VERSION:-latest}"
target="${LUMEN_INSTALL:-$HOME/.local/bin}"

fail() {
    echo "lumen: $*" >&2
    exit 1
}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) fail "no binary for $(uname -s); build it from the source: git clone https://github.com/$repo && cd lumen-lang && make build" ;;
esac
case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) fail "no binary for $(uname -m)" ;;
esac

name="lumen-$os-$arch"
if [ "$version" = latest ]; then
    url="https://github.com/$repo/releases/latest/download/$name.tar.gz"
    sums="https://github.com/$repo/releases/latest/download/checksums.txt"
else
    url="https://github.com/$repo/releases/download/$version/$name.tar.gz"
    sums="https://github.com/$repo/releases/download/$version/checksums.txt"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "Downloading $url"
curl -fsSL "$url" -o "$work/$name.tar.gz" || fail "could not download $url"
curl -fsSL "$sums" -o "$work/checksums.txt" || fail "could not download $sums"

expected="$(grep " $name.tar.gz\$" "$work/checksums.txt" | cut -d' ' -f1)"
if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$work/$name.tar.gz" | cut -d' ' -f1)"
else
    actual="$(shasum -a 256 "$work/$name.tar.gz" | cut -d' ' -f1)"
fi
[ -n "$expected" ] && [ "$expected" = "$actual" ] || fail "the download does not match its checksum"

tar -xzf "$work/$name.tar.gz" -C "$work"
mkdir -p "$target"
mv "$work/$name/lumen" "$target/lumen"
chmod +x "$target/lumen"

echo "Installed $("$target/lumen" version) in $target"
case ":$PATH:" in
    *":$target:"*) ;;
    *) echo "Add it to your PATH: export PATH=\"$target:\$PATH\"" ;;
esac
