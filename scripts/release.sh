#!/usr/bin/env bash
# Builds the release archives for one version, reproducibly.
#
#   scripts/release.sh vX.Y.Z [dist-dir]
#
# Every target is built with CGO disabled, -trimpath, no VCS stamp and no
# build id, so the same commit yields byte-identical binaries anywhere.
# Archive members carry the commit time (SOURCE_DATE_EPOCH) so the
# archives themselves are identical too. Output: <dist>/thawr_<version>_
# <os>_<arch>.tar.gz|.zip, <dist>/SHA256SUMS and, when both darwin and
# both linux targets are built, <dist>/thawr.rb for a Homebrew tap.
# TARGETS narrows the build, e.g. TARGETS="linux/amd64 darwin/arm64".
set -euo pipefail

VERSION=${1:?usage: scripts/release.sh vX.Y.Z [dist-dir]}
DIST=${2:-dist}
TARGETS=${TARGETS:-"linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64"}

if [[ ! $VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "release: version $VERSION must look like v1.2.3 or v1.2.3-rc1" >&2
  exit 2
fi

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

COMMIT=$(git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
EPOCH=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || date +%s)}
DATE=$(date -u -d "@$EPOCH" +%Y-%m-%d)
LDFLAGS="-s -w -buildid= -X main.version=$VERSION -X main.commit=$COMMIT -X main.builtAt=$DATE"

rm -rf "$DIST"
mkdir -p "$DIST"

for target in $TARGETS; do
  goos=${target%/*}
  goarch=${target#*/}
  name="thawr_${VERSION}_${goos}_${goarch}"
  stage="$DIST/$name"
  mkdir -p "$stage"
  bin="$stage/thawr"
  [[ $goos == windows ]] && bin="$bin.exe"

  echo "release: building $name"
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch GOFLAGS=-mod=readonly \
    go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$bin" ./cmd/thawr
  cp LICENSE NOTICE README.md "$stage/"
  cp config/server.example.yaml "$stage/server.example.yaml"
  find "$stage" -exec touch -h -d "@$EPOCH" {} +

  if [[ $goos == windows ]]; then
    (cd "$DIST" && find "$name" | LC_ALL=C sort | TZ=UTC zip -X -q "$name.zip" -@)
  else
    tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$EPOCH" \
      -C "$DIST" -cf - "$name" | gzip -n -9 >"$DIST/$name.tar.gz"
  fi
  rm -rf "$stage"
done

shopt -s nullglob
(cd "$DIST" && LC_ALL=C sha256sum -- *.tar.gz *.zip | LC_ALL=C sort -k2 >SHA256SUMS)
shopt -u nullglob

# sum prints the checksum of one archive or nothing when it was not built.
sum() { awk -v f="thawr_${VERSION}_$1" '$2 == f".tar.gz" { print $1 }' "$DIST/SHA256SUMS"; }
darwin_arm64=$(sum darwin_arm64)
darwin_amd64=$(sum darwin_amd64)
linux_arm64=$(sum linux_arm64)
linux_amd64=$(sum linux_amd64)
if [[ -n $darwin_arm64 && -n $darwin_amd64 && -n $linux_arm64 && -n $linux_amd64 ]]; then
  sed -e "s/@VERSION_NUMBER@/${VERSION#v}/g" \
    -e "s/@SHA_DARWIN_ARM64@/$darwin_arm64/" -e "s/@SHA_DARWIN_AMD64@/$darwin_amd64/" \
    -e "s/@SHA_LINUX_ARM64@/$linux_arm64/" -e "s/@SHA_LINUX_AMD64@/$linux_amd64/" \
    packaging/homebrew/thawr.rb.tmpl >"$DIST/thawr.rb"
fi

echo "release: $VERSION ($COMMIT, $DATE) in $DIST/"
cat "$DIST/SHA256SUMS"
