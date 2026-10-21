#!/bin/bash
# Run in Ubuntu 20.04 with Go 1.23.6, GTK3 and WebKitGTK 4.0 development files.
set -euo pipefail
source_root=${1:?Usage: build-ubuntu2004-gui.sh SOURCE_ROOT OUTPUT_DIR}
output_dir=${2:?Usage: build-ubuntu2004-gui.sh SOURCE_ROOT OUTPUT_DIR}
source_root=$(cd "$source_root" && pwd)
version=$(tr -d '[:space:]' < "$source_root/VERSION")
desktop_version=$(sed -n 's/.*"productVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$source_root/desktop/wails.json")
if [ "$version" != "$desktop_version" ]; then
  echo 'VERSION and desktop/wails.json differ; run scripts/sync-version.sh.' >&2
  exit 1
fi
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'Expected a plain major.minor.patch productVersion in wails.json.' >&2
  exit 1
fi
build_dir=$(mktemp -d)
stage="$build_dir/package"
dependency_root="$build_dir/dependencies"
trap 'rm -rf -- "$build_dir"' EXIT
mkdir -p "$output_dir"
output_dir=$(cd "$output_dir" && pwd)
. /etc/os-release
if [ "$ID" != ubuntu ] || [ "$VERSION_ID" != 20.04 ]; then
  echo 'Build this package inside Ubuntu 20.04.' >&2
  exit 1
fi
if [ "$(dpkg --print-architecture)" != amd64 ]; then
  echo 'This package recipe currently targets amd64.' >&2
  exit 1
fi
pkg-config --exists gtk+-3.0 webkit2gtk-4.0 appindicator3-0.1
appindicator_version=12.10.1+20.04.20200408.1-0ubuntu1
if [ "$(dpkg-query -W -f='${Version}' libappindicator3-1)" != "$appindicator_version" ]; then
  echo 'Install the supplied Ubuntu Focal libappindicator3-1 deb before packaging.' >&2
  exit 1
fi
export GOTOOLCHAIN=${GOTOOLCHAIN:-go1.23.6}
if [ "${SKIP_FRONTEND:-0}" != 1 ]; then
  command -v npm >/dev/null || { echo 'Node.js 20/npm is required to build the frontend.' >&2; exit 1; }
  (cd "$source_root/desktop/frontend" && npm ci && npm run build)
fi
test -s "$source_root/desktop/frontend/dist/index.html"
cd "$source_root"
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o "$build_dir/lingma-proxy" ./cmd/lingma-ipc-proxy
cd desktop
CGO_ENABLED=1 go build -trimpath -tags desktop,production,devtools,legacy_appindicator -ldflags '-s -w -X main.devtoolsBuild=true' -o "$build_dir/LingmaProxy" .
needed=$(readelf -d "$build_dir/LingmaProxy")
if ! grep -F 'libwebkit2gtk-4.0.so.37' <<< "$needed" >/dev/null || grep -F 'libwebkit2gtk-4.1' <<< "$needed" >/dev/null; then
  echo 'Ubuntu 20.04 GUI must link WebKitGTK 4.0 exclusively.' >&2
  exit 1
fi
if ! grep -F 'libappindicator3.so.1' <<< "$needed" >/dev/null || grep -F 'libayatana-appindicator' <<< "$needed" >/dev/null; then
  echo 'Focal tray must link the requested legacy libappindicator3.so.1.' >&2
  exit 1
fi
cd "$build_dir"
mkdir -p "$stage/DEBIAN" "$stage/opt/lingma-proxy" "$stage/usr/bin" "$stage/usr/share/applications" "$stage/usr/share/icons/hicolor/256x256/apps" "$stage/usr/share/doc/lingma-proxy-desktop"
install -m755 LingmaProxy "$stage/opt/lingma-proxy/LingmaProxy"
install -m755 lingma-proxy "$stage/opt/lingma-proxy/lingma-proxy"
install -m644 "$source_root/desktop/build/appicon.png" "$stage/usr/share/icons/hicolor/256x256/apps/lingma-proxy.png"
install -m644 "$source_root/LICENSE" "$stage/usr/share/doc/lingma-proxy-desktop/copyright"
sed 's/\r$//' "$source_root/desktop/build/linux/lingma-proxy-desktop" > "$stage/usr/bin/lingma-proxy-desktop"
sed 's/\r$//' "$source_root/desktop/build/linux/lingma-proxy.desktop" > "$stage/usr/share/applications/lingma-proxy.desktop"
chmod 755 "$stage/usr/bin/lingma-proxy-desktop"
sed 's/\r$//' "$source_root/desktop/build/linux/postinstall.sh" > "$stage/DEBIAN/postinst"
sed 's/\r$//' "$source_root/desktop/build/linux/postremove.sh" > "$stage/DEBIAN/postrm"
chmod 755 "$stage/DEBIAN/postinst" "$stage/DEBIAN/postrm"
ln -s /opt/lingma-proxy/lingma-proxy "$stage/usr/bin/lingma-proxy"
mkdir -p "$dependency_root/debian"
printf 'Source: lingma-proxy-desktop\n\nPackage: lingma-proxy-desktop\nArchitecture: any\nDescription: Lingma Proxy\n' > "$dependency_root/debian/control"
depends=$(cd "$dependency_root" && dpkg-shlibdeps -O -e "$build_dir/LingmaProxy" 2>/dev/null | sed -n 's/^shlibs:Depends=//p')
test -n "$depends"
if [[ "$depends" == *libwebkit2gtk-4.1* ]]; then
  echo 'Unexpected WebKitGTK 4.1 dependency in Ubuntu 20.04 package.' >&2
  exit 1
fi
cat > "$stage/DEBIAN/control" <<EOF
Package: lingma-proxy-desktop
Version: $version
Section: utils
Priority: optional
Architecture: amd64
Maintainer: Lingma Proxy local build <noreply@localhost>
Depends: $depends, libappindicator3-1 (= $appindicator_version)
Recommends: fonts-wqy-zenhei | fonts-noto-cjk
Description: Lingma Proxy desktop for Ubuntu 20.04
 OpenAI/Anthropic compatible proxy with close-to-tray support.
EOF
desktop-file-validate "$stage/usr/share/applications/lingma-proxy.desktop"
dpkg-deb --root-owner-group --build "$stage" "$output_dir/lingma-proxy-desktop-ubuntu20.04_${version}_amd64.deb"
echo "Ubuntu 20.04 desktop package created: $version"
