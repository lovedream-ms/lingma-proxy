#!/bin/bash
# Windows invokes this helper as root inside an existing Ubuntu WSL distro.
set -euo pipefail
source_archive=${1:?Missing source archive}
output_dir=${2:?Missing output directory}
recipe=${3:?Missing package recipe}
cache_dir=${4:?Missing download cache}
build_root=${5:?Missing isolated rootfs path}
module_cache=${6:?Missing module cache}
appindicator_deb=${7:?Missing Focal libappindicator3-1 deb}
if [ "$(id -u)" != 0 ] || [[ ! "$build_root" =~ ^/var/tmp/lingma-proxy-[A-Za-z0-9._/-]+$ ]] || [[ "$build_root" == *..* ]]; then
  echo 'Run as root with an isolated /var/tmp/lingma-proxy-* build directory.' >&2
  exit 1
fi
mkdir -p "$build_root" "$cache_dir" "$output_dir"
owned_mounts=()
cleanup() {
  for ((i=${#owned_mounts[@]}-1; i>=0; i--)); do umount "${owned_mounts[i]}"; done
}
trap cleanup EXIT
test "$(dpkg-deb -f "$appindicator_deb" Package)" = libappindicator3-1
test "$(dpkg-deb -f "$appindicator_deb" Version)" = 12.10.1+20.04.20200408.1-0ubuntu1
test "$(dpkg-deb -f "$appindicator_deb" Architecture)" = amd64
if [ ! -f "$build_root/etc/os-release" ]; then
  base_url=https://cdimage.ubuntu.com/ubuntu-base/releases/20.04/release
  curl -fL --retry 2 -o "$cache_dir/SHA256SUMS" "$base_url/SHA256SUMS"
  curl -fL --retry 2 -o "$cache_dir/SHA256SUMS.gpg" "$base_url/SHA256SUMS.gpg"
  gpgv --keyring /usr/share/keyrings/ubuntu-archive-keyring.gpg "$cache_dir/SHA256SUMS.gpg" "$cache_dir/SHA256SUMS"
  base=ubuntu-base-20.04.5-base-amd64.tar.gz
  if [ ! -f "$cache_dir/$base" ]; then curl -fL --retry 2 -o "$cache_dir/$base" "$base_url/$base"; fi
  (cd "$cache_dir" && grep " $base\$" SHA256SUMS | sha256sum -c -)
  tar -xzf "$cache_dir/$base" -C "$build_root"
fi
if ! grep -q '^VERSION_ID="20.04"$' "$build_root/etc/os-release"; then
  echo 'The isolated rootfs must be Ubuntu 20.04.' >&2
  exit 1
fi
cp -L /etc/resolv.conf "$build_root/etc/resolv.conf"
mkdir -p "$build_root/dev" "$build_root/proc" "$build_root/modcache"
if ! mountpoint -q "$build_root/dev"; then
  mount --bind /dev "$build_root/dev"
  owned_mounts+=("$build_root/dev")
  mount --make-rslave "$build_root/dev"
fi
if ! mountpoint -q "$build_root/dev/pts"; then
  mount --bind /dev/pts "$build_root/dev/pts"
  owned_mounts+=("$build_root/dev/pts")
fi
if ! mountpoint -q "$build_root/proc"; then
  mount -t proc proc "$build_root/proc"
  owned_mounts+=("$build_root/proc")
fi
if ! mountpoint -q "$build_root/modcache"; then
  mount --bind "$module_cache" "$build_root/modcache"
  owned_mounts+=("$build_root/modcache")
fi
printf '#!/bin/sh\nexit 101\n' > "$build_root/usr/sbin/policy-rc.d"
chmod 755 "$build_root/usr/sbin/policy-rc.d"
if ! chroot "$build_root" /bin/bash -c 'command -v dpkg-shlibdeps >/dev/null && command -v desktop-file-validate >/dev/null && command -v gcc >/dev/null && command -v curl >/dev/null && pkg-config --exists gtk+-3.0 webkit2gtk-4.0'; then
  chroot "$build_root" /usr/bin/env DEBIAN_FRONTEND=noninteractive /bin/bash -c 'apt-get update && apt-get install -y --no-install-recommends ca-certificates curl build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.0-dev desktop-file-utils file'
fi
dependency_name=$(basename "$appindicator_deb")
install -m644 "$appindicator_deb" "$build_root/tmp/$dependency_name"
chroot "$build_root" /usr/bin/env DEBIAN_FRONTEND=noninteractive /bin/bash -c 'apt-get update && apt-get install -y --no-install-recommends "/tmp/$1" libappindicator3-dev=12.10.1+20.04.20200408.1-0ubuntu1' bash "$dependency_name"
# Force the supplied file after APT resolves its transitive dependencies.
chroot "$build_root" dpkg -i "/tmp/$dependency_name"
test "$(chroot "$build_root" dpkg-query -W -f='${Version}' libappindicator3-1)" = 12.10.1+20.04.20200408.1-0ubuntu1
if ! chroot "$build_root" /usr/bin/env GOROOT=/usr/local/go /usr/local/go/bin/go version 2>/dev/null | grep -F 'go1.23.6 linux/amd64' >/dev/null; then
  go_archive=go1.23.6.linux-amd64.tar.gz
  if [ ! -f "$cache_dir/$go_archive" ]; then
    curl -fL --retry 2 -o "$cache_dir/$go_archive" "https://go.dev/dl/$go_archive"
  fi
  (cd "$cache_dir" && printf '%s  %s\n' 9379441ea310de000f33a4dc767bd966e72ab2826270e038e78b2c53c2e7802d "$go_archive" | sha256sum -c -)
  tar -xzf "$cache_dir/$go_archive" -C "$build_root/usr/local"
fi
job=$(basename "$source_archive" .tar.gz)
work=/work-builds/$job
mkdir -p "$build_root$work" "$build_root/output"
tar -xzf "$source_archive" -C "$build_root$work"
install -m755 "$recipe" "$build_root$work/build-ubuntu2004-gui.sh"
chroot "$build_root" /usr/bin/env PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin GOROOT=/usr/local/go GOMODCACHE=/modcache GOCACHE=/root/.cache/go-build GOTOOLCHAIN=local SKIP_FRONTEND=1 /bin/bash "$work/build-ubuntu2004-gui.sh" "$work" /output
version=$(sed -n 's/.*"productVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$build_root$work/desktop/wails.json")
package="lingma-proxy-desktop-ubuntu20.04_${version}_amd64.deb"
cp "$build_root/output/$package" "$output_dir/$package"
echo "Ubuntu 20.04 desktop package: $output_dir/$package"
