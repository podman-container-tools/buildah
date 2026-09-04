#!/usr/bin/env bats

load helpers

# Regression test for #2904
@test "local-image resolution" {
  run_buildah pull -q busybox
  iid=$output
  run_buildah tag ${iid} localhost/image

  # We want to make sure that "image" will always resolve to "localhost/image"
  # (given a local image with that name exists).  The trick we're using is to
  # force a failed pull and look at the error message which *must* include the
  # the resolved image name (localhost/image:latest).
  run_buildah 125 pull --policy=always image
  assert "$output" =~ "initializing source docker://localhost/image:latest"
  run_buildah rmi localhost/image ${iid}
}

@test "pull-flags-order-verification" {
  run_buildah 125 pull --retry 4 --retry-delay 4s image1 --tls-verify
  check_options_flag_err "--tls-verify"

  run_buildah 125 pull image1 --authfile=/tmp/somefile
  check_options_flag_err "--authfile=/tmp/somefile"

  run_buildah 125 pull image1 -q --cred bla:bla --authfile=/tmp/somefile
  check_options_flag_err "-q"
}

@test "pull-blocked" {
  run_buildah 125 --registries-conf ${TEST_SOURCES}/registries.conf.block pull $WITH_POLICY_JSON docker.io/alpine
  expect_output --substring "registry docker.io is blocked in"

  run_buildah --retry pull $WITH_POLICY_JSON docker.io/alpine
}

@test "pull-from-registry" {
  run_buildah --retry pull $WITH_POLICY_JSON busybox:glibc
  run_buildah pull $WITH_POLICY_JSON busybox:latest
  run_buildah images --format "{{.Name}}:{{.Tag}}"
  expect_output --substring "busybox:glibc"
  expect_output --substring "busybox:latest"
  run_buildah --retry pull $WITH_POLICY_JSON quay.io/libpod/alpine_nginx:latest
  run_buildah images --format "{{.Name}}:{{.Tag}}"
  expect_output --substring "alpine_nginx:latest"

  run_buildah rmi quay.io/libpod/alpine_nginx:latest
  run_buildah --retry pull $WITH_POLICY_JSON quay.io/libpod/alpine_nginx
  run_buildah images --format "{{.Name}}:{{.Tag}}"
  expect_output --substring "alpine_nginx:latest"

  run_buildah --retry pull $WITH_POLICY_JSON alpine@sha256:634a8f35b5f16dcf4aaa0822adc0b1964bb786fca12f6831de8ddc45e5986a00

  run_buildah 125 pull $WITH_POLICY_JSON fakeimage/fortest
  run_buildah images --format "{{.Name}}:{{.Tag}}"
  assert "$output" !~ "fakeimage/fortest" "fakeimage/fortest found in buildah images"
}

@test "pull-from-docker-archive" {
  run_buildah --retry pull $WITH_POLICY_JSON alpine
  run_buildah push $WITH_POLICY_JSON docker.io/library/alpine:latest docker-archive:${TEST_SCRATCH_DIR}/alp.tar:alpine:latest
  run_buildah rmi alpine
  run_buildah --retry pull $WITH_POLICY_JSON docker-archive:${TEST_SCRATCH_DIR}/alp.tar
  run_buildah images --format "{{.Name}}:{{.Tag}}"
  expect_output --substring "alpine"
  run_buildah 125 pull --all-tags $WITH_POLICY_JSON docker-archive:${TEST_SCRATCH_DIR}/alp.tar
  expect_output --substring "pulling all tags is not supported for docker-archive transport"
}

@test "pull-from-oci-archive" {
  run_buildah --retry pull $WITH_POLICY_JSON alpine
  run_buildah push $WITH_POLICY_JSON docker.io/library/alpine:latest oci-archive:${TEST_SCRATCH_DIR}/alp.tar:alpine
  run_buildah rmi alpine
  run_buildah pull $WITH_POLICY_JSON oci-archive:${TEST_SCRATCH_DIR}/alp.tar
  run_buildah images --format "{{.Name}}:{{.Tag}}"
  expect_output --substring "alpine"
  run_buildah 125 pull --all-tags $WITH_POLICY_JSON oci-archive:${TEST_SCRATCH_DIR}/alp.tar
  expect_output --substring "pulling all tags is not supported for oci-archive transport"
}

@test "pull-from-local-directory" {
  mkdir ${TEST_SCRATCH_DIR}/buildahtest
  run_buildah --retry pull $WITH_POLICY_JSON alpine
  run_buildah push $WITH_POLICY_JSON docker.io/library/alpine:latest dir:${TEST_SCRATCH_DIR}/buildahtest
  run_buildah rmi alpine
  run_buildah pull --quiet $WITH_POLICY_JSON dir:${TEST_SCRATCH_DIR}/buildahtest
  imageID="$output"
  # Images pulled via the dir transport are untagged.
  run_buildah images --format "{{.Name}}:{{.Tag}}"
  expect_output --substring "<none>:<none>"
  run_buildah 125 pull --all-tags $WITH_POLICY_JSON dir:$imageID
  expect_output --substring "pulling all tags is not supported for dir transport"
}

@test "pull-from-docker-daemon" {
  skip_if_no_docker

  run docker pull alpine
  echo "$output"
  assert "$status" -eq 0 "status of docker (yes, docker) pull alpine"
  run_buildah pull $WITH_POLICY_JSON docker-daemon:docker.io/library/alpine:latest
  run_buildah images --format "{{.Name}}:{{.Tag}}"
  expect_output --substring "alpine:latest"
  run_buildah rmi alpine
  run_buildah 125 pull --all-tags $WITH_POLICY_JSON docker-daemon:docker.io/library/alpine:latest
  expect_output --substring "pulling all tags is not supported for docker-daemon transport"
}

@test "pull-all-tags" {
  start_registry
  declare -a tags=(0.9 0.9.1 1.1 alpha beta gamma2.0 latest)

  # setup: pull alpine, and push it repeatedly to localhost using those tags
  opts="--signature-policy ${TEST_SOURCES}/policy.json --tls-verify=false --creds testuser:testpassword"
  run_buildah --retry pull --quiet $WITH_POLICY_JSON alpine
  for tag in "${tags[@]}"; do
      run_buildah push $opts alpine localhost:${REGISTRY_PORT}/myalpine:$tag
  done

  run_buildah images -q
  expect_line_count 1 "There's only one actual image ID"
  alpine_iid=$output

  # Remove it, and confirm.
  run_buildah rmi alpine
  run_buildah images -q
  expect_output "" "After buildah rmi, there are no locally stored images"

  # Now pull with --all-tags, and confirm that we see all expected tag strings
  run_buildah pull $opts --all-tags localhost:${REGISTRY_PORT}/myalpine
  for tag in "${tags[@]}"; do
      expect_output --substring "Trying to pull localhost:${REGISTRY_PORT}/myalpine:$tag"
  done

  # Confirm that 'images -a' lists all of them. <Brackets> help confirm
  # that tag names are exact, e.g we don't confuse 0.9 and 0.9.1
  run_buildah images -a --format '<{{.Tag}}>'
  expect_line_count "${#tags[@]}" "number of tagged images"
  for tag in "${tags[@]}"; do
      expect_output --substring "<$tag>"
  done

  # Finally, make sure that there's actually one and exactly one image
  run_buildah images -q
  expect_output $alpine_iid "Pulled image has the same IID as original alpine"
}

@test "pull-from-oci-directory" {
  run_buildah --retry pull $WITH_POLICY_JSON alpine

  run_buildah 125 pull --all-tags $WITH_POLICY_JSON oci:${TEST_SCRATCH_DIR}/alpine
  expect_output --substring "pulling all tags is not supported for oci transport"

  # Create on OCI image with reference and one without.  The first is expected
  # to preserve the reference while the latter should be unnamed.
  name="foo.com/name"
  tag="tag"
  withref="oci:${TEST_SCRATCH_DIR}/withref:$name:$tag"
  noref="oci:${TEST_SCRATCH_DIR}/noref"

  run_buildah push $WITH_POLICY_JSON docker.io/library/alpine:latest $withref
  run_buildah push $WITH_POLICY_JSON docker.io/library/alpine:latest $noref
  run_buildah rmi alpine

  # Image without optional reference is unnamed.
  run_buildah pull -q $WITH_POLICY_JSON $noref
  run_buildah images --format "{{.Name}}:{{.Tag}}" $output
  expect_output "<none>:<none>"

  # Image with optional reference is named.
  run_buildah pull -q $WITH_POLICY_JSON $withref
  run_buildah images --format "{{.Name}}:{{.Tag}}" $output
  expect_output "$name:$tag"
}

@test "pull-denied-by-registry-sources" {
  export BUILD_REGISTRY_SOURCES='{"blockedRegistries": ["docker.io"]}'

  run_buildah 125 pull $WITH_POLICY_JSON --registries-conf ${TEST_SOURCES}/registries.conf.hub --quiet busybox
  expect_output --substring 'registry "docker.io" denied by policy: it is in the blocked registries list'

  run_buildah 125 pull $WITH_POLICY_JSON --registries-conf ${TEST_SOURCES}/registries.conf.hub --quiet busybox
  expect_output --substring 'registry "docker.io" denied by policy: it is in the blocked registries list'

  export BUILD_REGISTRY_SOURCES='{"allowedRegistries": ["some-other-registry.example.com"]}'

  run_buildah 125 pull $WITH_POLICY_JSON --registries-conf ${TEST_SOURCES}/registries.conf.hub --quiet busybox
  expect_output --substring 'registry "docker.io" denied by policy: not in allowed registries list'

  run_buildah 125 pull $WITH_POLICY_JSON --registries-conf ${TEST_SOURCES}/registries.conf.hub --quiet busybox
  expect_output --substring 'registry "docker.io" denied by policy: not in allowed registries list'
}

@test "pull should fail with nonexistent authfile" {
  run_buildah 125 pull --authfile /tmp/nonexistent $WITH_POLICY_JSON alpine
}

@test "pull encrypted local image" {
  _prefetch busybox
  mkdir ${TEST_SCRATCH_DIR}/tmp
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey.pem 1024
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey2.pem 1024
  openssl rsa -in ${TEST_SCRATCH_DIR}/tmp/mykey.pem -pubout > ${TEST_SCRATCH_DIR}/tmp/mykey.pub
  run_buildah push $WITH_POLICY_JSON --encryption-key jwe:${TEST_SCRATCH_DIR}/tmp/mykey.pub busybox  oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc

  # Try to pull encrypted image without key should fail
  run_buildah 125 pull $WITH_POLICY_JSON oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc
  expect_output --substring "does not match config's DiffID"

  # Try to pull encrypted image with wrong key should fail
  run_buildah 125 pull $WITH_POLICY_JSON --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey2.pem oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc
  expect_output --substring "decrypting layer .* no suitable key unwrapper found or none of the private keys could be used for decryption"

  # Providing the right key should succeed
  run_buildah pull $WITH_POLICY_JSON --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey.pem oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "pull encrypted registry image" {
  _prefetch busybox
  start_registry
  mkdir ${TEST_SCRATCH_DIR}/tmp
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey.pem 1024
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey2.pem 1024
  openssl rsa -in ${TEST_SCRATCH_DIR}/tmp/mykey.pem -pubout > ${TEST_SCRATCH_DIR}/tmp/mykey.pub
  run_buildah push $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword --encryption-key jwe:${TEST_SCRATCH_DIR}/tmp/mykey.pub busybox docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest

  # Try to pull encrypted image without key should fail
  run_buildah 125 pull $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest
  expect_output --substring "does not match config's DiffID"

  # Try to pull encrypted image with wrong key should fail, with diff. msg
  run_buildah 125 pull $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey2.pem docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest
  expect_output --substring "decrypting layer .* no suitable key unwrapper found or none of the private keys could be used for decryption"

  # Providing the right key should succeed
  run_buildah pull $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey.pem docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest

  run_buildah rmi localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "pull encrypted registry image from commit" {
  _prefetch busybox
  start_registry
  mkdir ${TEST_SCRATCH_DIR}/tmp
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey.pem 1024
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey2.pem 1024
  openssl rsa -in ${TEST_SCRATCH_DIR}/tmp/mykey.pem -pubout > ${TEST_SCRATCH_DIR}/tmp/mykey.pub
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah commit --iidfile /dev/null --tls-verify=false --creds testuser:testpassword $WITH_POLICY_JSON --encryption-key jwe:${TEST_SCRATCH_DIR}/tmp/mykey.pub -q $cid docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest

  # Try to pull encrypted image without key should fail
  run_buildah 125 pull $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest
  expect_output --substring "does not match config's DiffID"

  # Try to pull encrypted image with wrong key should fail
  run_buildah 125 pull $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey2.pem docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest
  expect_output --substring "decrypting layer .* no suitable key unwrapper found or none of the private keys could be used for decryption"

  # Providing the right key should succeed
  run_buildah pull $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey.pem docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest

  run_buildah rmi localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "pull image into a full storage" {
  skip_if_rootless_environment
  mkdir /tmp/buildah-test
  mount -t tmpfs -o size=5M tmpfs /tmp/buildah-test
  run dd if=/dev/urandom of=/tmp/buildah-test/full
  run_buildah 125 --root=/tmp/buildah-test pull $WITH_POLICY_JSON alpine
  expect_output --substring "no space left on device"
  umount /tmp/buildah-test
  rm -rf /tmp/buildah-test
}

@test "pull with authfile" {
  _prefetch busybox
  start_registry
  mkdir ${TEST_SCRATCH_DIR}/tmp
  run_buildah push --creds testuser:testpassword --tls-verify=false busybox docker://localhost:${REGISTRY_PORT}/buildah/busybox:latest
  run_buildah login --authfile ${TEST_SCRATCH_DIR}/tmp/test.auth --username testuser --password testpassword --tls-verify=false localhost:${REGISTRY_PORT}
  run_buildah pull --authfile ${TEST_SCRATCH_DIR}/tmp/test.auth --tls-verify=false docker://localhost:${REGISTRY_PORT}/buildah/busybox:latest
  run_buildah rmi localhost:${REGISTRY_PORT}/buildah/busybox:latest

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "pull quietly" {
  run_buildah pull -q busybox
  iid=$output
  run_buildah rmi ${iid}
}

@test "pull-policy" {
  mkdir ${TEST_SCRATCH_DIR}/buildahtest
  run_buildah 125 pull $WITH_POLICY_JSON --policy bogus alpine
  expect_output --substring "unsupported pull policy \"bogus\""

  #  If image does not exist the never will fail
  run_buildah 125 pull -q $WITH_POLICY_JSON --policy never alpine
  expect_output --substring "image not known"
  run_buildah 125 inspect --type image alpine
  expect_output --substring "image not known"

  # create bogus alpine image
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah commit -q $cid docker.io/library/alpine
  iid=$output

  #  If image does not exist the never will succeed, but iid should not change
  run_buildah pull -q $WITH_POLICY_JSON --policy never alpine
  expect_output $iid

  # Pull image by default should change the image id
  run_buildah pull -q --policy always $WITH_POLICY_JSON alpine
  assert "$output" != "$iid" "pulled image should have a new IID"

  # Recreate image
  run_buildah commit -q $cid docker.io/library/alpine
  iid=$output

  # Make sure missing image works
  run_buildah pull -q $WITH_POLICY_JSON --policy missing alpine
  expect_output $iid

  run_buildah rmi alpine
  run_buildah pull -q $WITH_POLICY_JSON alpine
  run_buildah inspect alpine

  run_buildah rmi alpine
  run_buildah pull -q $WITH_POLICY_JSON --policy missing alpine
  run_buildah inspect alpine

  run_buildah rmi alpine
}

@test "pull --arch" {
  mkdir ${TEST_SCRATCH_DIR}/buildahtest
  run_buildah 125 pull $WITH_POLICY_JSON --arch bogus alpine
  expect_output --substring "no image found in manifest list"

  # Make sure missing image works
  run_buildah pull -q $WITH_POLICY_JSON --arch arm64 alpine

  run_buildah inspect --format "{{ .Docker.Architecture }}" alpine
  expect_output arm64

  run_buildah inspect --format "{{ .OCIv1.Architecture }}" alpine
  expect_output arm64

  run_buildah rmi alpine
}

@test "pull --platform" {
  mkdir ${TEST_SCRATCH_DIR}/buildahtest
  run_buildah 125 pull $WITH_POLICY_JSON --platform linux/bogus alpine
  expect_output --substring "no image found in manifest list"

  # Make sure missing image works
  run_buildah pull -q $WITH_POLICY_JSON --platform linux/arm64 alpine

  run_buildah inspect --format "{{ .Docker.Architecture }}" alpine
  expect_output arm64

  run_buildah inspect --format "{{ .OCIv1.Architecture }}" alpine
  expect_output arm64

  run_buildah rmi alpine
}

@test "pull image with TMPDIR set" {
  skip_if_rootless_environment
  testdir=${TEST_SCRATCH_DIR}/buildah-test
  mkdir -p $testdir
  mount -t tmpfs -o size=1M tmpfs $testdir

  TMPDIR=$testdir run_buildah 125 pull --policy always $WITH_POLICY_JSON quay.io/libpod/alpine_nginx:latest
  expect_output --substring "no space left on device"

  run_buildah pull --policy always $WITH_POLICY_JSON quay.io/libpod/alpine_nginx:latest
  umount $testdir
  rm -rf $testdir
}

@test "pull-policy --missing --arch" {
  # Make sure missing image works
  run_buildah pull -q $WITH_POLICY_JSON --policy missing --arch amd64 alpine
  amdiid=$output

  run_buildah pull -q $WITH_POLICY_JSON --policy missing --arch arm64 alpine
  armiid=$output

  assert "$amdiid" != "$armiid" "AMD and ARM ids should differ"
}

@test "pull-policy-values" {
  image=quay.io/libpod/busybox
  for value in missing ifmissing notpresent always true never false ifnewer newer ; do
    run_buildah pull --platform=linux/amd64 --policy="${value}" "${image}"
  done
}
