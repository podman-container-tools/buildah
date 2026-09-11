#!/usr/bin/env bash

set -eo pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" && pwd )

source "$SCRIPT_DIR/lib.sh"

parse_args "$@"

export PRIV_NAME="$PRIV"
export STORAGE_DRIVER

echo "::group::System setup"

PRESERVE_ENVS="STORAGE_DRIVER,PRIV_NAME,BUILDAH_RUNTIME,IN_PODMAN,IN_PODMAN_NAME,IN_PODMAN_IMAGE,TEST_BUILD_TAGS,GOPATH,GOCACHE,GOSRC,CI_USE_REGISTRY_CACHE,TMPDIR"

LCR=/var/cache/local-registry/local-cache-registry
if [[ -x $LCR ]]; then
    while read new_image; do
        $LCR cache "$new_image"
    done < <(grep '^[^#]' tests/NEW-IMAGES 2>/dev/null || true)
    export CI_USE_REGISTRY_CACHE=1
fi
SUDO=""
if [[ "$PRIV" == "root" ]]; then
    SUDO="sudo --preserve-env=$PRESERVE_ENVS"
fi

conf=/etc/containers/storage.conf
if [[ ! -e $conf ]]; then
    sudo tee $conf <<EOF
[storage]
driver = "$STORAGE_DRIVER"
EOF
fi

for which in uid gid; do
    if ! grep -qE '^containers:' /etc/sub$which; then
        echo 'containers:10000000:1048576' | sudo tee --append /etc/sub$which
    fi
done

if [[ "$TEST" == "conformance" ]]; then
    case "$OS_RELEASE_ID" in
        fedora)
            die "conformance tests are not supported on fedora"
            ;;
        debian)
            sudo dpkg -i /var/cache/download/containerd.io*.deb /var/cache/download/docker-ce*.deb
            ;;
    esac
    sudo systemctl start docker || true
fi

if [[ "$DISTRO_NAME" == "fedora-rawhide" ]]; then
    export TEST_BUILD_TAGS="${TEST_BUILD_TAGS:-containers_image_sequoia}"
fi
echo "::endgroup::" # System setup

echo "::group::Logging system info"
"$SCRIPT_DIR/logcollector.sh" packages
echo "::endgroup::"


export GOSRC="$(pwd)"

function run_cross() {
    make -j cross CGO_ENABLED=0
}

function run_unit() {
    $SUDO make test-unit RACEFLAGS=""
}

function run_conformance() {
    # /tmp is tmpfs (RAM-backed) and the 16 parallel conformance test workers fill it
    # with VFS layers; redirect to a disk-backed dir and pass it explicitly through sudo.
    sudo mkdir -p /var/lib/ci-tmp
    sudo chmod 1777 /var/lib/ci-tmp
    export TMPDIR=/var/lib/ci-tmp
    $SUDO env "TMPDIR=$TMPDIR" make test-conformance
}

# The integration tests need a few packages (networking helpers, mostly) that
# were dropped from the CI VM images in podman-container-tools/automation#25.
# Install them here so the tests that depend on them keep passing.
function install_integration_deps() {
    local packages=(containernetworking-plugins iptables slirp4netns)
    echo "::group::Installing integration test dependencies"
    case "$OS_RELEASE_ID" in
        fedora)
            sudo dnf -y install "${packages[@]}"
            ;;
        debian)
            sudo apt-get -y update
            sudo apt-get -y install "${packages[@]}"
            ;;
    esac
    echo "::endgroup::"
}

function run_integration() {
    install_integration_deps
    $SUDO make test-integration
}

function run_in_podman() {
    install_integration_deps
    export IN_PODMAN=true
    export BUILDAH_ISOLATION=chroot
    export STORAGE_DRIVER=vfs
    $SUDO make test-integration
}

run_$TEST
