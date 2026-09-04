#!/usr/bin/env bats

load helpers

@test "push-flags-order-verification" {
  run_buildah 125 push img1 dest1 -q
  check_options_flag_err "-q"

  run_buildah 125 push img1 --tls-verify dest1
  check_options_flag_err "--tls-verify"

  run_buildah 125 push img1 dest1 arg3 --creds user1:pass1
  check_options_flag_err "--creds"

  run_buildah 125 push img1 --creds=user1:pass1 dest1
  check_options_flag_err "--creds=user1:pass1"
}

@test "push" {
  skip_if_rootless_environment
  touch ${TEST_SCRATCH_DIR}/reference-time-file
  for source in scratch scratch-image; do
    run_buildah from --quiet --pull=false $WITH_POLICY_JSON ${source}
    cid=$output
    for format in "" docker oci ; do
      mkdir -p ${TEST_SCRATCH_DIR}/committed${format:+.${format}}
      # Force no compression to generate what we push.
      run_buildah commit -D ${format:+--format ${format}} --reference-time ${TEST_SCRATCH_DIR}/reference-time-file $WITH_POLICY_JSON "$cid" scratch-image${format:+-${format}}
      run_buildah commit -D ${format:+--format ${format}} --reference-time ${TEST_SCRATCH_DIR}/reference-time-file $WITH_POLICY_JSON "$cid" dir:${TEST_SCRATCH_DIR}/committed${format:+.${format}}
      mkdir -p ${TEST_SCRATCH_DIR}/pushed${format:+.${format}}
      run_buildah push -D $WITH_POLICY_JSON scratch-image${format:+-${format}} dir:${TEST_SCRATCH_DIR}/pushed${format:+.${format}}
      # Re-encode the manifest to lose variations due to different encoders or definitions of structures.
      imgtype -expected-manifest-type "*" -rebuild-manifest -show-manifest dir:${TEST_SCRATCH_DIR}/committed${format:+.${format}} > ${TEST_SCRATCH_DIR}/manifest.committed${format:+.${format}}
      imgtype -expected-manifest-type "*" -rebuild-manifest -show-manifest dir:${TEST_SCRATCH_DIR}/pushed${format:+.${format}} > ${TEST_SCRATCH_DIR}/manifest.pushed${format:+.${format}}
      diff -u ${TEST_SCRATCH_DIR}/manifest.committed${format:+.${format}} ${TEST_SCRATCH_DIR}/manifest.pushed${format:+.${format}}
    done
    run_buildah rm "$cid"
  done
}

@test "push with manifest type conversion" {
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir

  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah push --retry 4 --retry-delay 4s $WITH_POLICY_JSON --format oci alpine dir:$mytmpdir
  run cat $mytmpdir/manifest.json
  expect_output --substring "application/vnd.oci.image.config.v1\\+json"

  run_buildah push $WITH_POLICY_JSON --format v2s2 alpine dir:$mytmpdir
  run cat $mytmpdir/manifest.json
  expect_output --substring "application/vnd.docker.distribution.manifest.v2\\+json"
}

@test "push with imageid" {
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir

  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah images -q
  imageid=$output
  run_buildah push $WITH_POLICY_JSON $imageid dir:$mytmpdir
}

@test "push with imageid and digest file" {
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir

  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah images -q
  imageid=$output
  run_buildah push --digestfile=${TEST_SCRATCH_DIR}/digest.txt $WITH_POLICY_JSON $imageid dir:$mytmpdir
  cat ${TEST_SCRATCH_DIR}/digest.txt
  test -s ${TEST_SCRATCH_DIR}/digest.txt
  test -z $(tail -c1 ${TEST_SCRATCH_DIR}/digest.txt)
}

@test "push without destination" {
  _prefetch busybox
  run_buildah pull $WITH_POLICY_JSON busybox
  run_buildah 125 push $WITH_POLICY_JSON busybox
  expect_output --substring "busybox"
}

@test "push should fail with nonexistent authfile" {
  _prefetch alpine
  run_buildah from --quiet --pull $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah images -q
  imageid=$output
  run_buildah 125 push $WITH_POLICY_JSON --authfile /tmp/nonexistent $imageid dir:${TEST_SCRATCH_DIR}/my-tmp-dir
}

@test "push-denied-by-registry-sources" {
  _prefetch busybox

  export BUILD_REGISTRY_SOURCES='{"blockedRegistries": ["registry.example.com"]}'

  run_buildah from --quiet $WITH_POLICY_JSON --quiet busybox
  cid=$output
  run_buildah 125 commit $WITH_POLICY_JSON ${cid} docker://registry.example.com/busierbox
  expect_output --substring 'commit to registry at "registry.example.com" denied by policy: it is in the blocked registries list'

  run_buildah pull $WITH_POLICY_JSON --quiet busybox
  run_buildah 125 push $WITH_POLICY_JSON busybox docker://registry.example.com/evenbusierbox

  export BUILD_REGISTRY_SOURCES='{"allowedRegistries": ["some-other-registry.example.com"]}'

  run_buildah from --quiet $WITH_POLICY_JSON --quiet busybox
  cid=$output
  run_buildah 125 commit $WITH_POLICY_JSON ${cid} docker://registry.example.com/busierbox
  expect_output --substring 'commit to registry at "registry.example.com" denied by policy: not in allowed registries list'

  run_buildah pull $WITH_POLICY_JSON --quiet busybox
  run_buildah 125 push $WITH_POLICY_JSON busybox docker://registry.example.com/evenbusierbox
  expect_output --substring 'registry "registry.example.com" denied by policy: not in allowed registries list'
}


@test "buildah push image to containers-storage" {
  _prefetch busybox
  run_buildah push $WITH_POLICY_JSON busybox containers-storage:newimage:latest
  run_buildah images
  expect_output --substring "newimage"
}

@test "buildah push image to docker-archive and oci-archive" {
  _prefetch busybox
  for dest in docker-archive oci-archive; do
    mkdir ${TEST_SCRATCH_DIR}/tmp
    run_buildah push $WITH_POLICY_JSON busybox $dest:${TEST_SCRATCH_DIR}/tmp/busybox.tar:latest
    ls ${TEST_SCRATCH_DIR}/tmp/busybox.tar
    rm -rf ${TEST_SCRATCH_DIR}/tmp
  done
}

@test "buildah push image to docker and docker registry" {
  skip_if_no_docker

  _prefetch busybox
  run_buildah push $WITH_POLICY_JSON busybox docker-daemon:buildah/busybox:latest
  run docker images
  expect_output --substring "buildah/busybox"
  docker rmi buildah/busybox

  start_registry
  run_buildah push $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword docker.io/busybox:latest docker://localhost:${REGISTRY_PORT}/buildah/busybox:latest
  docker login localhost:${REGISTRY_PORT} --username testuser --password-stdin <<<testpassword
  docker pull localhost:${REGISTRY_PORT}/buildah/busybox:latest
  output=$(docker images)
  expect_output --substring "buildah/busybox"
  docker rmi localhost:${REGISTRY_PORT}/buildah/busybox:latest
  docker logout localhost:${REGISTRY_PORT}
}

@test "buildah oci encrypt and push local oci" {
  skip_if_rootless_environment
  _prefetch busybox
  mkdir ${TEST_SCRATCH_DIR}/tmp
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey.pem 1024
  openssl rsa -in ${TEST_SCRATCH_DIR}/tmp/mykey.pem -pubout > ${TEST_SCRATCH_DIR}/tmp/mykey.pub
  run_buildah push $WITH_POLICY_JSON --encryption-key jwe:${TEST_SCRATCH_DIR}/tmp/mykey.pub busybox oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc
  imgtype  -show-manifest oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc | grep "+encrypted"
  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "buildah oci encrypt and push registry" {
  _prefetch busybox
  mkdir ${TEST_SCRATCH_DIR}/tmp
  start_registry
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey.pem 1024
  openssl rsa -in ${TEST_SCRATCH_DIR}/tmp/mykey.pem -pubout > ${TEST_SCRATCH_DIR}/tmp/mykey.pub
  run_buildah push $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword --encryption-key jwe:${TEST_SCRATCH_DIR}/tmp/mykey.pub busybox docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest
  # this test, just checks the ability to push an image
  # there is no good way to test the details of the image unless with ./buildah pull, test will be in pull.bats
  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "buildah push to registry allowed by BUILD_REGISTRY_SOURCES" {
  _prefetch busybox
  start_registry
  export BUILD_REGISTRY_SOURCES='{"insecureRegistries": ["localhost:${REGISTRY_PORT}"]}'

  run_buildah 125 push --creds testuser:testpassword $WITH_POLICY_JSON --tls-verify=true busybox docker://localhost:${REGISTRY_PORT}/buildah/busybox:latest
  expect_output --substring "certificate signed by unknown authority"

  run_buildah push --creds testuser:testpassword  $WITH_POLICY_JSON --cert-dir ${TEST_SCRATCH_DIR}/registry busybox docker://localhost:${REGISTRY_PORT}/buildah/busybox:latest
}

@test "push with authfile" {
  _prefetch busybox
  mkdir ${TEST_SCRATCH_DIR}/tmp
  start_registry
  run_buildah login --authfile ${TEST_SCRATCH_DIR}/tmp/test.auth --username testuser --password testpassword --tls-verify=false localhost:${REGISTRY_PORT}
  run_buildah push --authfile ${TEST_SCRATCH_DIR}/tmp/test.auth $WITH_POLICY_JSON --tls-verify=false busybox docker://localhost:${REGISTRY_PORT}/buildah/busybox:latest
  expect_output --substring "Copying"

  run_buildah manifest create localhost:${REGISTRY_PORT}/testmanifest
  run_buildah manifest push --authfile ${TEST_SCRATCH_DIR}/tmp/test.auth $WITH_POLICY_JSON --tls-verify=false localhost:${REGISTRY_PORT}/testmanifest
  expect_output --substring "Writing manifest list to image destination"
}

@test "push with --quiet" {
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir

  _prefetch alpine
  run_buildah push --quiet $WITH_POLICY_JSON alpine dir:$mytmpdir
  expect_output ""
}

@test "push with --compression-format" {
  _prefetch alpine
  run_buildah from --quiet --pull alpine
  cid=$output
  run_buildah images -q
  imageid=$output
  run_buildah push --format oci --compression-format zstd:chunked $imageid dir:${TEST_SCRATCH_DIR}/zstd
  # Verify there is some zstd compressed layer.
  grep application/vnd.oci.image.layer.v1.tar+zstd ${TEST_SCRATCH_DIR}/zstd/manifest.json
}

@test "push should respect compression_format from containers.conf" {
  which skopeo || skip "skopeo is not installed"
  _prefetch alpine

  local contextdir=${TEST_SCRATCH_DIR}/push-compression-conf
  mkdir -p $contextdir
  cat > $contextdir/Containerfile << _EOF
FROM alpine
RUN echo "layer1-$(random_string)"
_EOF
  cat > $contextdir/containers.conf << _EOF
[engine]
compression_format="zstd"
_EOF
  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}
  local imgname="img-push-compress-$(safename)"
  local finalrepo="localhost:${REGISTRY_PORT}/${imgname}"

  CONTAINERS_CONF=$contextdir/containers.conf \
    run_buildah build \
      $WITH_POLICY_JSON \
      -t ${imgname} \
      --no-cache \
      -f $contextdir/Containerfile \
      $contextdir

  CONTAINERS_CONF=$contextdir/containers.conf \
    run_buildah push \
      $WITH_POLICY_JSON \
      --tls-verify=false \
      --authfile ${TEST_SCRATCH_DIR}/test.auth \
      ${imgname} \
      docker://${finalrepo}

  run skopeo inspect \
      --authfile ${TEST_SCRATCH_DIR}/test.auth \
      --tls-verify=false \
      --raw \
      docker://${finalrepo}
  assert $status -eq 0 "skopeo inspect should succeed"

  expect_output --substring "zstd" \
    "layers should use zstd compression per containers.conf"
  assert "$output" !~ "tar+gzip" \
    "layers should NOT use gzip when containers.conf specifies zstd"
}

@test "push should respect --compression-format flag with registry" {
  which skopeo || skip "skopeo is not installed"
  _prefetch alpine

  local contextdir=${TEST_SCRATCH_DIR}/push-compression-flag
  mkdir -p $contextdir
  cat > $contextdir/Containerfile << _EOF
FROM alpine
RUN echo "layer1-$(random_string)"
_EOF
  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}
  local imgname="img-push-compress-flag-$(safename)"
  local finalrepo="localhost:${REGISTRY_PORT}/${imgname}"

  run_buildah build \
    $WITH_POLICY_JSON \
    -t ${imgname} \
    --no-cache \
    -f $contextdir/Containerfile \
    $contextdir

  run_buildah push \
    $WITH_POLICY_JSON \
    --tls-verify=false \
    --authfile ${TEST_SCRATCH_DIR}/test.auth \
    --compression-format zstd \
    ${imgname} \
    docker://${finalrepo}

  run skopeo inspect \
      --authfile ${TEST_SCRATCH_DIR}/test.auth \
      --tls-verify=false \
      --raw \
      docker://${finalrepo}
  assert $status -eq 0 "skopeo inspect should succeed"

  expect_output --substring "zstd" \
    "layers should use zstd compression per --compression-format flag"
  assert "$output" !~ "tar+gzip" \
    "layers should NOT use gzip when --compression-format zstd is specified"
}
