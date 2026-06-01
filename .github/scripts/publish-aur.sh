#!/usr/bin/env bash
set -euo pipefail

repo="${GITHUB_REPOSITORY:-suethttps/npm-jail}"
version="${VERSION:?VERSION is required}"
pkgver="${version#v}"
pkgbase="${AUR_PKGBASE:-npm-jail-bin}"
upstream="https://github.com/${repo}"
release_url="${upstream}/releases/download/v${pkgver}"
checksums_url="${release_url}/checksums.txt"

if [ -z "${AUR_SSH_PRIVATE_KEY:-}" ]; then
  echo "AUR_SSH_PRIVATE_KEY is not set; skipping AUR publish."
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

curl -fsSL "${checksums_url}" -o "${tmp}/checksums.txt"

sha_for() {
  local asset="$1"
  awk -v asset="${asset}" '$2 == asset { print $1 }' "${tmp}/checksums.txt"
}

sha_x86_64="$(sha_for npm-jail_Linux_x86_64.tar.gz)"
sha_aarch64="$(sha_for npm-jail_Linux_aarch64.tar.gz)"

if [ -z "${sha_x86_64}" ] || [ -z "${sha_aarch64}" ]; then
  echo "Could not find expected npm-jail checksums in ${checksums_url}" >&2
  exit 1
fi

mkdir -p "${HOME}/.ssh"
chmod 700 "${HOME}/.ssh"
printf '%s\n' "${AUR_SSH_PRIVATE_KEY}" > "${HOME}/.ssh/aur"
chmod 600 "${HOME}/.ssh/aur"
ssh-keyscan aur.archlinux.org >> "${HOME}/.ssh/known_hosts"

export GIT_SSH_COMMAND="ssh -i ${HOME}/.ssh/aur -o IdentitiesOnly=yes"

git clone "ssh://aur@aur.archlinux.org/${pkgbase}.git" "${tmp}/${pkgbase}"
cd "${tmp}/${pkgbase}"

cat > PKGBUILD <<EOF
pkgname=${pkgbase}
pkgver=${pkgver}
pkgrel=1
pkgdesc='Run npm commands inside a bubblewrap sandbox'
arch=('x86_64' 'aarch64')
url='${upstream}'
license=('GPL-3.0-only')
depends=('bubblewrap')
provides=('npm-jail')
conflicts=('npm-jail')

source_x86_64=("${release_url}/npm-jail_Linux_x86_64.tar.gz")
source_aarch64=("${release_url}/npm-jail_Linux_aarch64.tar.gz")
sha256sums_x86_64=('${sha_x86_64}')
sha256sums_aarch64=('${sha_aarch64}')

package() {
  install -Dm755 npm-jail "\${pkgdir}/usr/bin/npm-jail"
  install -Dm644 README.md "\${pkgdir}/usr/share/doc/npm-jail/README.md"
  install -Dm644 LICENSE "\${pkgdir}/usr/share/licenses/npm-jail/LICENSE"
}
EOF

cat > .SRCINFO <<EOF
pkgbase = ${pkgbase}
	pkgdesc = Run npm commands inside a bubblewrap sandbox
	pkgver = ${pkgver}
	pkgrel = 1
	url = ${upstream}
	arch = x86_64
	arch = aarch64
	license = GPL-3.0-only
	depends = bubblewrap
	provides = npm-jail
	conflicts = npm-jail
	source_x86_64 = ${release_url}/npm-jail_Linux_x86_64.tar.gz
	sha256sums_x86_64 = ${sha_x86_64}
	source_aarch64 = ${release_url}/npm-jail_Linux_aarch64.tar.gz
	sha256sums_aarch64 = ${sha_aarch64}

pkgname = ${pkgbase}
EOF

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git add PKGBUILD .SRCINFO

if git diff --cached --quiet; then
  echo "AUR package ${pkgbase} is already up to date."
  exit 0
fi

git commit -m "Update to ${pkgver}"
git push
