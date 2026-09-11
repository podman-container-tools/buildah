#!/usr/bin/env bash

set -eo pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" && pwd )

source "$SCRIPT_DIR/lib.sh"

AUTOMATION_RELEASE="${AUTOMATION_RELEASE:-20260902t121119z}" # TODO should be renovate managed
LIMA_VM_NAME=buildah-ci

REPO_DIR="$SCRIPT_DIR/../.."

parse_args "$@"

IMAGE="$DISTRO_NAME.x86_64.qcow2.zst"

IMAGE_URL_BASE="${IMAGE_URL_BASE:-https://objectstorage.us-ashburn-1.oraclecloud.com/n/id0lmbbwgcdv/b/podman-ci-vm-images/o/releases}"
IMAGE_URL="$IMAGE_URL_BASE/$AUTOMATION_RELEASE/$IMAGE"

trap "limactl delete --force $LIMA_VM_NAME" EXIT

echo "::group::Starting VM"
limactl --yes start --plain --name=$LIMA_VM_NAME --cpus $(nproc) --memory 8 --disk 150 --nested-virt \
    --set ".images=[{\"location\":\"$IMAGE_URL\", \"arch\": \"x86_64\"}]" \
    "$SCRIPT_DIR/template.lima.yml"

limactl copy "$REPO_DIR" $LIMA_VM_NAME:/var/tmp/buildah
echo "::endgroup::"

set +e

limactl shell --workdir /var/tmp/buildah $LIMA_VM_NAME ./hack/ci/runner.sh "${@}"
rc=$?

echo "::group::Collecting logs"
limactl shell --workdir /var/tmp/buildah $LIMA_VM_NAME sudo hack/ci/logcollector.sh journal &> "$SCRIPT_DIR/journal.log"
echo "::endgroup::"

exit $rc
