#!/usr/bin/env bats

load helpers

@test "bud with a path to a Dockerfile (-f) containing a non-directory entry" {
  run_buildah 125 build -f $BUDFILES/non-directory-in-path/non-directory/Dockerfile
  expect_output --substring "non-directory/Dockerfile: not a directory"
}

@test "bud stdio is usable pipes" {
  _prefetch alpine
  run_buildah build $BUDFILES/stdio
}

@test "bud: build manifest list and --add-compression zstd" {
  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}

  imgname="img-$(safename)"
  run_buildah build $WITH_POLICY_JSON -t "${imgname}1" --platform linux/amd64 -f $BUDFILES/dockerfile/Dockerfile
  run_buildah build $WITH_POLICY_JSON -t "${imgname}2" --platform linux/arm64 -f $BUDFILES/dockerfile/Dockerfile

  run_buildah manifest create foo
  run_buildah manifest add foo "${imgname}1"
  run_buildah manifest add foo "${imgname}2"

  run_buildah manifest push $WITH_POLICY_JSON --authfile ${TEST_SCRATCH_DIR}/test.auth --all --add-compression zstd --tls-verify=false foo docker://localhost:${REGISTRY_PORT}/list

  run_buildah manifest inspect --authfile ${TEST_SCRATCH_DIR}/test.auth --tls-verify=false localhost:${REGISTRY_PORT}/list
  list="$output"

  validate_instance_compression "0" "$list" "amd64" "gzip"
  validate_instance_compression "1" "$list" "arm64" "gzip"
  validate_instance_compression "2" "$list" "amd64" "zstd"
  validate_instance_compression "3" "$list" "arm64" "zstd"
}

@test "bud: build manifest list and --add-compression with containers.conf" {
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
_EOF

  cat > $contextdir/containers.conf << _EOF
[engine]
add_compression = ["zstd"]
_EOF

  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}

  imgname="img-$(safename)"
  run_buildah build $WITH_POLICY_JSON -t "${imgname}1" --platform linux/amd64 -f $contextdir/Dockerfile1
  run_buildah build $WITH_POLICY_JSON -t "${imgname}2" --platform linux/arm64 -f $contextdir/Dockerfile1

  run_buildah manifest create foo
  run_buildah manifest add foo "${imgname}1"
  run_buildah manifest add foo "${imgname}2"

  CONTAINERS_CONF=$contextdir/containers.conf run_buildah manifest push $WITH_POLICY_JSON --authfile ${TEST_SCRATCH_DIR}/test.auth --all --tls-verify=false foo docker://localhost:${REGISTRY_PORT}/list

  run_buildah manifest inspect --authfile ${TEST_SCRATCH_DIR}/test.auth --tls-verify=false localhost:${REGISTRY_PORT}/list
  list="$output"

  validate_instance_compression "0" "$list" "amd64" "gzip"
  validate_instance_compression "1" "$list" "arm64" "gzip"
  validate_instance_compression "2" "$list" "amd64" "zstd"
  validate_instance_compression "3" "$list" "arm64" "zstd"
}

@test "bud: build manifest list with --add-compression zstd, --compression and --force-compression" {
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
_EOF

  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}

  imgname="img-$(safename)"
  run_buildah build $WITH_POLICY_JSON -t "${imgname}1" --platform linux/amd64 -f $contextdir/Dockerfile1
  run_buildah build $WITH_POLICY_JSON -t "${imgname}2" --platform linux/arm64 -f $contextdir/Dockerfile1

  run_buildah manifest create foo
  run_buildah manifest add foo "${imgname}1"
  run_buildah manifest add foo "${imgname}2"

  run_buildah manifest push $WITH_POLICY_JSON --authfile ${TEST_SCRATCH_DIR}/test.auth --all --add-compression zstd --tls-verify=false foo docker://localhost:${REGISTRY_PORT}/list

  run_buildah manifest inspect --authfile ${TEST_SCRATCH_DIR}/test.auth --tls-verify=false localhost:${REGISTRY_PORT}/list
  list="$output"

  validate_instance_compression "0" "$list" "amd64" "gzip"
  validate_instance_compression "1" "$list" "arm64" "gzip"
  validate_instance_compression "2" "$list" "amd64" "zstd"
  validate_instance_compression "3" "$list" "arm64" "zstd"

  # Pushing again should keep every thing intact if original compression is `gzip` and `--force-compression` is specified
  run_buildah manifest push $WITH_POLICY_JSON --authfile ${TEST_SCRATCH_DIR}/test.auth --all --add-compression zstd --compression-format gzip --force-compression --tls-verify=false foo docker://localhost:${REGISTRY_PORT}/list

  run_buildah manifest inspect --authfile ${TEST_SCRATCH_DIR}/test.auth --tls-verify=false localhost:${REGISTRY_PORT}/list
  list="$output"

  validate_instance_compression "0" "$list" "amd64" "gzip"
  validate_instance_compression "1" "$list" "arm64" "gzip"
  validate_instance_compression "2" "$list" "amd64" "zstd"
  validate_instance_compression "3" "$list" "arm64" "zstd"

  # Pushing again without --force-compression but with --compression-format should do the same thing
  run_buildah manifest push $WITH_POLICY_JSON --authfile ${TEST_SCRATCH_DIR}/test.auth --all --add-compression zstd --compression-format gzip --tls-verify=false foo docker://localhost:${REGISTRY_PORT}/list

  run_buildah manifest inspect --authfile ${TEST_SCRATCH_DIR}/test.auth --tls-verify=false localhost:${REGISTRY_PORT}/list
  list="$output"

  validate_instance_compression "0" "$list" "amd64" "gzip"
  validate_instance_compression "1" "$list" "arm64" "gzip"
  validate_instance_compression "2" "$list" "amd64" "zstd"
  validate_instance_compression "3" "$list" "arm64" "zstd"
}

@test "Multi-stage should not remove used base-image without --layers" {
  run_buildah build -t parent-one -f $BUDFILES/multi-stage-only-base/Containerfile1
  run_buildah build -t parent-two -f $BUDFILES/multi-stage-only-base/Containerfile2
  run_buildah build -t multi-stage -f $BUDFILES/multi-stage-only-base/Containerfile3
  run_buildah images -a
  expect_output --substring "parent-one" "parent one must not be removed"
}

@test "no layer should be created on scratch" {
  imgname="img-$(safename)"

  run_buildah build --layers --label "label1=value1" -t $imgname -f $BUDFILES/from-scratch/Containerfile
  run_buildah inspect -f '{{len .Docker.RootFS.DiffIDs}}' $imgname
  expect_output "0" "layer should not exist"
  run_buildah build --layers -t $imgname -f $BUDFILES/from-scratch/Containerfile
  run_buildah inspect -f '{{len .Docker.RootFS.DiffIDs}}' $imgname
  expect_output "0" "layer should not exist"
}

@test "no empty layer for metadata-only instructions without --layers" {
  _prefetch alpine
  imgname="img-$(safename)"

  # Get the base image layer count
  run_buildah inspect -f '{{len .Docker.RootFS.DiffIDs}}' alpine
  base_layers="$output"

  # Build without --layers (single-layer mode) with only metadata instructions
  run_buildah build --layers=false -t $imgname -f $BUDFILES/metadata-only/Containerfile
  run_buildah inspect -f '{{len .Docker.RootFS.DiffIDs}}' $imgname
  expect_output "$base_layers" "metadata-only instructions should not add a layer"
}

@test "bud and test --inherit-annotations" {
  base=quay.io/libpod/testimage:20241011
  _prefetch $base
  target=exp

  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}

  buildah inspect --format '{{ .ImageAnnotations }}' $base
  not_want_output='map[]'
  assert "$output" != "$not_want_output" "expected some annotations to be set in base image $base"

  ## Since we are inheriting annotations, built image should contain some default annotations
  run_buildah build $WITH_POLICY_JSON --inherit-annotations=true -t $target --from $base $BUDFILES/base-with-labels
  not_want_output='map[]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  assert "$output" != "$not_want_output" "expected some annotations to be set in base image $base"

  ## Since we are inheriting no annotations, built image should not contain any annotations.
  run_buildah build $WITH_POLICY_JSON --inherit-annotations=false --created-annotation=false -t $target --from $base $BUDFILES/base-with-labels

  # no annotations should be inherited from base image
  want_output='map[]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"

  ## Build again but set a new annotation and don't inherit annotations from base image
  run_buildah build $WITH_POLICY_JSON --inherit-annotations=false --created-annotation=false --annotation hello=world -t $target --from $base $BUDFILES/base-with-labels

  # no annotations should be inherited from base image
  want_output='map["hello":"world"]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"

  ## Try similar thing with another Containerfile
  run_buildah build $WITH_POLICY_JSON --inherit-annotations=false --created-annotation=false -t $target -f $BUDFILES/base-with-labels/Containerfile2
  # no annotations should be inherited from base image
  want_output='map[]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"

  run_buildah build $WITH_POLICY_JSON --inherit-annotations=false --created-annotation=false --annotation hello=world -t $target -f $BUDFILES/base-with-labels/Containerfile2
  # no annotations should be inherited from base image
  want_output='map["hello":"world"]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"
}

@test "bud and test --inherit-annotations with --layers" {
  base=quay.io/libpod/testimage:20241011
  _prefetch $base
  target=exp

  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}

  buildah inspect --format '{{ .ImageAnnotations }}' $base
  not_want_output='map[]'
  assert "$output" != "$not_want_output" "expected some annotations to be set in base image $base"

  ## Build without removing annotations
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid1 -t $target --from $base $BUDFILES/base-with-labels
  ## Second build must use cache
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid2 -t $target --from $base $BUDFILES/base-with-labels
  ## Must use cache
  expect_output --substring " Using cache"
  cmp ${TEST_SCRATCH_DIR}/iid1 ${TEST_SCRATCH_DIR}/iid2

  ## Since we are inheriting no annotations, this should not use previous image present in the cache.
  run_buildah build $WITH_POLICY_JSON --layers --inherit-annotations=false --created-annotation=false -t $target --from $base $BUDFILES/base-with-labels
  ## should not contain `Using Cache`
  assert "$output" !~ "Using cache"

  # no annotations should be inherited from base image
  want_output='map[]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"

  ## Build again but set a new annotation and don't inherit annotations from base image
  run_buildah build $WITH_POLICY_JSON --layers --inherit-annotations=false --created-annotation=false --annotation hello=world -t $target --from $base $BUDFILES/base-with-labels

  # no annotations should be inherited from base image
  want_output='map["hello":"world"]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"


  ### Run similar test again with multiple instructions now
  ## Build without removing annotations
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid1 -t $target -f $BUDFILES/base-with-labels/Containerfile2
  ## Second build must use cache
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid2 -t $target -f $BUDFILES/base-with-labels/Containerfile2
  ## Must use cache
  expect_output --substring " Using cache"
  cmp ${TEST_SCRATCH_DIR}/iid1 ${TEST_SCRATCH_DIR}/iid2

  ## Since we are inheriting no annotations, this should not use previous image present in the cache.
  run_buildah build $WITH_POLICY_JSON --layers --inherit-annotations=false --created-annotation=false -t $target -f $BUDFILES/base-with-labels/Containerfile2
  ## but this time since 1st instruction is cached it should still use cache for some part, only annotations should not be there
  expect_output --substring " Using cache"

  # no annotations should be inherited from base image
  want_output='map[]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"

  ## Build again but set a new annotation and don't inherit annotations from base image
  run_buildah build $WITH_POLICY_JSON --layers --inherit-annotations=false --created-annotation=false --annotation hello=world -t $target -f $BUDFILES/base-with-labels/Containerfile2
  ## but this time since 1st instruction is cached it should still use cache for some part, only last instruction will be changed since we are adding
  ## annotations to it
  expect_output --substring " Using cache"

  # no annotations should be inherited from base image
  want_output='map["hello":"world"]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"
}

@test "bud with no instructions but with CLI flags that require a new image be written" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir $contextdir
  # Build a multiple-layer image based on busybox.
  cat > $contextdir/Dockerfile <<-EOF
  FROM busybox
  RUN pwd > /pwd.txt
  LABEL baseimage=true
EOF
  run_buildah build --layers --annotation annotation1=annotationvalue1 --iidfile ${TEST_SCRATCH_DIR}/baseiid.txt ${contextdir}
  run cat ${TEST_SCRATCH_DIR}/baseiid.txt
  local baseiid=${output##*:}

  # Set up to build an image that's based on our image, but with one CLI-motivated change.
  cat > $contextdir/Dockerfile <<-EOF
  FROM ${baseiid}
EOF

  # Try them all.
  for mutatorArg in --squash --label=A=B --env=C=D --annotation=E=F --unsetenv=PATH --unsetlabel=io.buildah.version --unsetannotation=org.opencontainers.image.created --inherit-labels=false --inherit-annotations=false ; do
    : > ${TEST_SCRATCH_DIR}/iid.txt
    run_buildah build --iidfile ${TEST_SCRATCH_DIR}/iid.txt ${mutatorArg} ${contextdir}
    test -s ${TEST_SCRATCH_DIR}/iid.txt
    run cat ${TEST_SCRATCH_DIR}/iid.txt
    local iid="${output##*:}"
    assert ${iid} != ${baseiid}
    run_buildah inspect -t image ${iid}
    case "${mutatorArg}" in
    --squash)
      # Should have exactly one layer, even though our base had at least two.
      run jq '.OCIv1.rootfs.diff_ids|length' <<< "$output"
      assert ${status} == 0
      assert "${output}" == 1
      # Go back and check the base, as a control.
      run_buildah inspect -t image ${baseiid}
      run jq '.OCIv1.rootfs.diff_ids|length' <<< "$output"
      assert ${status} == 0
      assert "${output}" != 1
      ;;
    --label=A=B)
      # Should have had the requested label set.
      run jq -r '.OCIv1.config.Labels["A"]' <<< "$output"
      assert ${status} == 0
      assert "${output}" == B
      # Go back and check the base, as a control.
      run_buildah inspect -t image ${baseiid}
      run jq '.OCIv1.config.Labels["A"]' <<< "$output"
      assert ${status} == 0
      assert "${output}" == null
      ;;
    --annotation=E=F)
      # Should have had the requested annotation set.
      run jq -r '.ImageAnnotations["E"]' <<< "$output"
      assert ${status} == 0
      assert "${output}" == F
      # Go back and check the base, as a control.
      run_buildah inspect -t image ${baseiid}
      run jq '.ImageAnnotations["E"]' <<< "$output"
      assert ${status} == 0
      assert "${output}" == null
      ;;
    --unsetenv=PATH)
      # Should NOT have had a $PATH set.
      run jq -r '.OCIv1.config.Env' <<< "$output"
      assert ${status} == 0
      assert "${output}" !~ PATH
      # Go back and check the base, as a control.
      run_buildah inspect -t image ${baseiid}
      run jq '.OCIv1.config.Env' <<< "$output"
      assert ${status} == 0
      assert "${output}" =~ PATH
      ;;
    --unsetannotation=org.opencontainers.image.created)
      # Should NOT have had a creation annotation set.
      run jq -r '.ImageAnnotations' <<< "$output"
      assert ${status} == 0
      assert "${output}" !~ created
      # Go back and check the base, as a control.
      run_buildah inspect -t image ${baseiid}
      run jq '.ImageAnnotations' <<< "$output"
      assert ${status} == 0
      assert "${output}" =~ org.opencontainers.image.created
      ;;
    --inherit-labels=false)
      # Should NOT have had a "baseimage" label.
      run jq -r '.OCIv1.config.Labels["baseimage"]' <<< "$output"
      assert ${status} == 0
      assert "${output}" == null
      # Go back and check the base, as a control.
      run_buildah inspect -t image ${baseiid}
      run jq -r '.OCIv1.config.Labels["baseimage"]' <<< "$output"
      assert ${status} == 0
      assert "${output}" == true
      ;;
    --inherit-annotations=false)
      # Should NOT have had an "annotation1" annotation.
      run jq -r '.ImageAnnotations["annotation1"]' <<< "$output"
      assert ${status} == 0
      assert "${output}" == null
      # Go back and check the base, as a control.
      run_buildah inspect -t image ${baseiid}
      run jq -r '.ImageAnnotations["annotation1"]' <<< "$output"
      assert ${status} == 0
      assert "${output}" == annotationvalue1
      ;;
    esac
  done
}

@test "bud: build push with --force-compression" {
  blobcachedir=${TEST_SCRATCH_DIR}/blobcachelocal
  mkdir -p ${blobcachedir}
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  # Make sure this is an image never used in any other zstd tests,
  # nor with any layers used in zstd tests. That could lead to a
  # different test pushing it zstd, and a "did not expect zstd"
  # failure below.
  echo "$(date --utc --iso-8601=seconds) this is a unique layer $(random_string)" >$contextdir/therecanbeonly1
  cat > $contextdir/Containerfile << _EOF
FROM scratch
COPY /therecanbeonly1 /uniquefile
_EOF

  imgname="img-$(safename)"

  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}
  run_buildah build $WITH_POLICY_JSON -t $imgname --platform linux/amd64 $contextdir

  # Helper function. push our image with the given options, and run imgtype to inspect
  function _test_buildah_push() {
    run_buildah push \
                --blob-cache=${blobcachedir} \
                $WITH_POLICY_JSON \
                --authfile ${TEST_SCRATCH_DIR}/test.auth \
                --tls-verify=false \
                $* \
                $imgname \
                docker://localhost:${REGISTRY_PORT}/$imgname

    echo "# imgtype -show-manifest docker://localhost:${REGISTRY_PORT}/$imgname"
    run imgtype -show-manifest \
        -authfile=${TEST_SCRATCH_DIR}/test.auth \
        -tls-verify=false \
        docker://localhost:${REGISTRY_PORT}/$imgname
    assert $status -eq 0 "imgtype failed"
    echo "$output"
  }

  # layers should have no trace of zstd since push was with --compression-format gzip
  _test_buildah_push --compression-format gzip
  assert "$output" !~ "zstd" "zstd found in layers where push was with --compression-format gzip"

  # layers should have no trace of zstd since push is --force-compression=false
  _test_buildah_push --compression-format zstd --force-compression=false
  assert "$output" !~ "zstd" "zstd found even though push was without --force-compression"

  # layers should contain `zstd`
  _test_buildah_push --compression-format zstd
  expect_output --substring "zstd" "layers must contain zstd compression"

  # layers should contain `zstd`
  _test_buildah_push --compression-format zstd --force-compression
  expect_output --substring "zstd" "layers must contain zstd compression"
}

@test "bud with --dns* flags" {
  _prefetch alpine

  for dnsopt in --dns --dns-option --dns-search; do
    run_buildah 125 build $dnsopt=example.com --network=none $WITH_POLICY_JSON -f $BUDFILES/dns/Dockerfile  $BUDFILES/dns
    expect_output "Error: the $dnsopt option cannot be used with --network=none" "dns options should not be allowed with --network=none"
  done

  run_buildah build --dns-search=example.com --dns=223.5.5.5 --dns-option=use-vc  $WITH_POLICY_JSON -f $BUDFILES/dns/Dockerfile  $BUDFILES/dns
  expect_output --substring "search example.com"
  expect_output --substring "nameserver 223.5.5.5"
  expect_output --substring "options use-vc"
}

@test "build with inline RUN --network=host" {
  _prefetch alpine
  #hostns=$(readlink /proc/self/ns/net)
  run readlink /proc/self/ns/net
  hostns="$output"
  run_buildah build $WITH_POLICY_JSON -t source -f $BUDFILES/inline-network/Dockerfile1
  expect_output --from="${lines[2]}" "${hostns}"
}

@test "build with inline RUN --network=none" {
  _prefetch alpine
  run_buildah 1 build $WITH_POLICY_JSON -t source -f $BUDFILES/inline-network/Dockerfile2
  expect_output --substring "wget: bad address"
}

@test "build with inline RUN --network=fake" {
  _prefetch alpine
  run_buildah 125 build $WITH_POLICY_JSON -t source -f $BUDFILES/inline-network/Dockerfile3
  expect_output --substring "unsupported value"
}

@test "build with inline default RUN --network=default" {
  skip_if_chroot
  _prefetch alpine
  run readlink /proc/self/ns/net
  hostns=$output
  run_buildah build --network=host $WITH_POLICY_JSON -t source -f $BUDFILES/inline-network/Dockerfile4
  firstns=${lines[2]}
  assert "${hostns}" == "$firstns"
  run_buildah build --network=private $WITH_POLICY_JSON -t source -f $BUDFILES/inline-network/Dockerfile4
  secondns=${lines[2]}
  assert "$secondns" != "$firstns"
}


@test "bud with ignoresymlink on default file" {
  _prefetch alpine
  echo hello > ${TEST_SCRATCH_DIR}/private_file
  cp -a $BUDFILES/container-ignoresymlink ${TEST_SCRATCH_DIR}/container-ignoresymlink
  ln -s ${TEST_SCRATCH_DIR}/private_file ${TEST_SCRATCH_DIR}/container-ignoresymlink/.dockerignore
  run_buildah build $WITH_POLICY_JSON -t test -f Dockerfile $BUDFILES/container-ignoresymlink
  # Should ignore a .dockerignore or .containerignore that's a symlink to somewhere outside of the build context
  expect_output --substring "hello"
}

# Verify https://github.com/containers/buildah/issues/4342
@test "buildkit-mount type=cache should not hang if cache is wiped in between" {
  _prefetch alpine
  containerfile=$BUDFILES/cache-mount-locked/Containerfile
  run_buildah build $WITH_POLICY_JSON --build-arg WIPE_CACHE=1 -t source -f $containerfile $BUDFILES/cache-mount-locked
  # build should be success and must contain `hello` from `file` in last step
  expect_output --substring "hello"
}

# Test for https://github.com/containers/buildah/pull/4295
@test "build test warning for preconfigured TARGETARCH, TARGETOS, TARGETPLATFORM or TARGETVARIANT" {
  containerfile=$BUDFILES/platform-sets-args/Containerfile

  # Containerfile must contain one or more (four, as of 2022-10) lines
  # of the form 'ARG TARGETxxx' for each of the variables of interest.
  local -a checkvars=($(sed -ne 's/^ARG //p' <$containerfile))
  assert "${checkvars[*]}" != "" \
         "INTERNAL ERROR! No 'ARG xxx' lines in $containerfile!"

  ARCH=$(go env GOARCH)
  # With explicit and full --platform, buildah should not warn.
  run_buildah build $WITH_POLICY_JSON --platform linux/amd64/v2 \
              -t source -f $containerfile
  assert "$output" =~ "image platform \(linux/amd64\) does not match the expected platform" \
         "With explicit --platform, buildah should warn about pulling difference in platform"
  assert "$output" =~ "TARGETOS=linux" " --platform TARGETOS set correctly"
  assert "$output" =~ "TARGETARCH=amd64" " --platform TARGETARCH set correctly"
  # By default, BUILDVARIANT/TARGETVARIANT should be empty.
  assert "$output" =~ "TARGETVARIANT=[[:cntrl:]]" " --platform TARGETVARIANT set correctly"
  assert "$output" =~ "TARGETPLATFORM=linux/amd64/v2" " --platform TARGETPLATFORM set correctly"

  # Likewise with individual args
  run_buildah build $WITH_POLICY_JSON --os linux --arch amd64 --variant v2 \
              -t source -f $containerfile
  assert "$output" =~ "image platform \(linux/amd64\) does not match the expected platform" \
         "With explicit --variant, buildah should warn about pulling difference in platform"
  assert "$output" =~ "TARGETOS=linux" "--os --arch --variant TARGETOS set correctly"
  assert "$output" =~ "TARGETARCH=amd64" "--os --arch --variant TARGETARCH set correctly"
  # By default, BUILDVARIANT/TARGETVARIANT should be empty.
  assert "$output" =~ "TARGETVARIANT=[[:cntrl:]]" "--os --arch --variant TARGETVARIANT set correctly"
  assert "$output" =~ "TARGETPLATFORM=linux/amd64" "--os --arch --variant TARGETPLATFORM set correctly"

  run_buildah build $WITH_POLICY_JSON --os linux -t source -f $containerfile
  assert "$output" !~ "WARNING" \
         "With explicit --os (but no arch/variant), buildah should not warn about TARGETOS"
  assert "$output" =~ "TARGETOS=linux" "--os TARGETOS set correctly"
  assert "$output" =~ "TARGETARCH=${ARCH}" "--os TARGETARCH set correctly"
  # By default, BUILDVARIANT/TARGETVARIANT should be empty.
  assert "$output" =~ "TARGETVARIANT=[[:cntrl:]]" "--os TARGETVARIANT set correctly"
  assert "$output" =~ "TARGETPLATFORM=linux/${ARCH}" "--os TARGETPLATFORM set correctly"

  run_buildah build $WITH_POLICY_JSON --arch amd64 -t source -f $containerfile
  assert "$output" !~ "WARNING" \
         "With explicit --os (but no arch/variant), buildah should not warn about TARGETOS"
  assert "$output" =~ "TARGETOS=linux" "--arch TARGETOS set correctly"
  assert "$output" =~ "TARGETARCH=amd64" "--arch TARGETARCH set correctly"
  # By default, BUILDVARIANT/TARGETVARIANT should be empty.
  assert "$output" =~ "TARGETVARIANT=[[:cntrl:]]" "--arch TARGETVARIANT set correctly"
  assert "$output" =~ "TARGETPLATFORM=linux/amd64" "--arch TARGETPLATFORM set correctly"

  for option in "--arch=arm64" "--os=windows" "--variant=v2"; do
    run_buildah 125 build $WITH_POLICY_JSON --platform linux/amd64 ${option} \
                -t source -f $containerfile
    assert "$output" =~ "invalid --platform may not be used with --os, --arch, or --variant" "can't use --platform and one of --os, --arch or --variant together"
  done
}

@test "build-conflicting-isolation-chroot-and-network" {
  _prefetch alpine
  cat > ${TEST_SCRATCH_DIR}/Containerfile << _EOF
FROM alpine
RUN ping -c 1 4.2.2.2
_EOF

  run_buildah 125 build --network=none --isolation=chroot $WITH_POLICY_JSON ${TEST_SCRATCH_DIR}
  expect_output --substring "cannot set --network other than host with --isolation chroot"

  BUILDAH_ISOLATION=chroot run_buildah 125 build --network=none $WITH_POLICY_JSON ${TEST_SCRATCH_DIR}
  expect_output --substring "cannot set --network other than host with --isolation chroot"
}

@test "bud with .dockerignore #1" {
  _prefetch alpine busybox
  run_buildah 125 build -t testbud $WITH_POLICY_JSON -f $BUDFILES/dockerignore/Dockerfile $BUDFILES/dockerignore
  expect_output --substring 'building.*"COPY subdir \./".*no such file or directory'

  run_buildah build -t testbud $WITH_POLICY_JSON -f $BUDFILES/dockerignore/Dockerfile.succeed $BUDFILES/dockerignore

  run_buildah from --name myctr testbud

  run_buildah 1 run myctr ls -l test1.txt

  run_buildah run myctr ls -l test2.txt

  run_buildah 1 run myctr ls -l sub1.txt

  run_buildah 1 run myctr ls -l sub2.txt

  run_buildah 1 run myctr ls -l subdir/
}

@test "bud --layers with --mount type bind should burst cache if symlink is changed" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/samplefile1 << _EOF
First symlink content
_EOF

  cat > $contextdir/samplefile2 << _EOF
Second symlink content
_EOF

  # Create symlink to samplefile1 and we will mount that
  ln -s samplefile1 $contextdir/tomount
  pwd
  ls $contextdir/
  cat $contextdir/tomount

  cat > $contextdir/Containerfile << _EOF
FROM alpine
RUN --mount=type=bind,source=tomount,target=file,Z cat file
_EOF

  # on first run since there is no cache so `samplefile1` must be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  expect_output --substring "First symlink content"

  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  # output should not contain content from the file since entire build is cached
  assert "$output" !~ "First symlink content"

  # Modify the symlink
  ln -sf samplefile2 $contextdir/tomount

  # on third run since we have changed symlink so cache must burst.
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  expect_output --substring "Second symlink content"
}

@test "bud --layers with --mount type bind should burst cache if content is changed" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/samplefile << _EOF
samplefile
_EOF

  cat > $contextdir/Containerfile << _EOF
FROM alpine
RUN --mount=type=bind,target=/test,Z ls /test
_EOF

  # on first run since there is no cache so `samplefile` must be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  expect_output --substring "samplefile"

  # on second run since there is cache so `samplefile` should not be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  # output should not contain `samplefile`
  assert "$output" !~ "samplefile"

  cat > $contextdir/anotherfile << _EOF
anotherfile
_EOF

  # on third run since we have added new file `anotherfile` so cache must burst.
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  expect_output --substring "samplefile"
  expect_output --substring "anotherfile"
}

@test "bud --layers with --mount type bind should burst and multiple mounts cache if content is changed" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/samplefile << _EOF
samplefile
_EOF

  cat > $contextdir/testfile << _EOF
Helloworld
_EOF

  cat > $contextdir/Containerfile << _EOF
FROM alpine
RUN --mount=type=bind,target=/test,Z --mount=type=bind,source=testfile,target=testfile,Z ls /test && cat testfile
_EOF

  # on first run since there is no cache so `samplefile` must be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  expect_output --substring "samplefile"
  expect_output --substring "Helloworld"

  # on second run since there is cache so `samplefile` should not be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  # output should not contain `samplefile`
  assert "$output" !~ "samplefile"

  # Modify sample file 2
  cat > $contextdir/testfile << _EOF
Helloworld2
_EOF

  # on third run since we have modified `testfile` so cache must burst.
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  expect_output --substring "samplefile"
  expect_output --substring "Helloworld2"
}

@test "bud --layers with --mount type bind should burst cache if content is changed - source is additional build context" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/samplefile << _EOF
samplefile2
_EOF

  cat > $contextdir/Containerfile << _EOF
FROM alpine
RUN --mount=type=bind,from=one,target=/test,Z ls /test
_EOF

  # on first run since there is no cache so `samplefile` must be printed
  run_buildah build $WITH_POLICY_JSON --build-context one=$contextdir --layers -t source -f $contextdir/Containerfile
  expect_output --substring "samplefile"

  # on second run since there is cache so `samplefile` should not be printed
  run_buildah build $WITH_POLICY_JSON --build-context one=$contextdir --layers -t source -f $contextdir/Containerfile
  # output should not `samplefile` since cache is being used
  assert "$output" !~ "samplefile"

  cat > $contextdir/anotherfile << _EOF
anotherfile2
_EOF

  # on third run since we have added new file `anotherfile` so cache must burst.
  run_buildah build $WITH_POLICY_JSON --build-context one=$contextdir --layers -t source -f $contextdir/Containerfile
  expect_output --substring "samplefile"
  expect_output --substring "anotherfile"
}

@test "bud --layers with --mount type bind should burst cache if content is changed - source is previous stage" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/cache
  mkdir -p $contextdir

  for iteration in 1 2 ; do
    for content in 1 2 ; do
      # based on reproducer from #6845
      cat > $contextdir/Containerfile <<-_EOF
      FROM busybox AS base

      FROM base AS builder
      RUN mkdir /build && echo "$content" > /build/foo.txt

      FROM base AS prod
      RUN --mount=type=bind,source=/build,from=builder,target=/build cp /build/foo.txt bar.txt
_EOF
      run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/$content.$iteration.txt $contextdir
    done
  done
  assert $(< ${TEST_SCRATCH_DIR}/1.1.txt) == $(< ${TEST_SCRATCH_DIR}/1.2.txt)
  assert $(< ${TEST_SCRATCH_DIR}/2.1.txt) == $(< ${TEST_SCRATCH_DIR}/2.2.txt)
  assert $(< ${TEST_SCRATCH_DIR}/1.1.txt) != $(< ${TEST_SCRATCH_DIR}/2.1.txt)
}

@test "bud --layers should not hit cache if heredoc is changed" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN <<EOF
echo "Cache burst" >> /hello
echo "Cache burst second line" >> /hello
EOF
RUN cat hello
_EOF

  # on first run since there is no cache so `Cache burst` must be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile
  expect_output --substring "Cache burst second line"

  # on second run since there is cache so `Cache burst` should not be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile
  # output should not contain cache burst
  assert "$output" !~ "Cache burst second line"

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN <<EOF
echo "Cache burst add diff" >> /hello
EOF
RUN cat hello
_EOF

  # on third run since we have changed heredoc so `Cache burst` must be printed.
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile
  expect_output --substring "Cache burst add diff"
}

@test "bud --layers with --mount type bind should preserve cache when file mod time changes but content stays same" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/samplefile << _EOF
samplefile content unchanged
_EOF

  cat > $contextdir/Containerfile << _EOF
FROM alpine
RUN --mount=type=bind,source=samplefile,target=file,Z cat file
_EOF

  # on first run since there is no cache so content must be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  expect_output --substring "samplefile content unchanged"

  # on second run since there is cache so content should not be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  # output should not contain content from the file since entire build is cached
  assert "$output" !~ "samplefile content unchanged"

  # Change mod time of this file (this changes modification time)
 touch -d "@1577836800" $contextdir/samplefile

  # on third run since content is unchanged, cache should still be used despite different mod time
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile $contextdir
  # output should not contain content from the file since cache should still be valid
  assert "$output" !~ "samplefile content unchanged"
}

@test "bud --layers should not hit cache if heredoc is changed - with ARG" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
ARG key=value
RUN <<EOF
echo "Cache burst" >> /hello
echo "Cache burst second line" >> /hello
EOF
RUN cat hello
_EOF

  # on first run since there is no cache so `Cache burst` must be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile
  expect_output --substring "Cache burst second line"

  # on second run since there is cache so `Cache burst` should not be printed
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile
  # output should not contain cache burst
  assert "$output" !~ "Cache burst second line"

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
ARG key=value
RUN <<EOF
echo "Cache burst add diff" >> /hello
EOF
RUN cat hello
_EOF

  # on third run since we have changed heredoc so `Cache burst` must be printed.
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile
  expect_output --substring "Cache burst add diff"
}

@test "bud build without multiple layers, with and without layer-creating instructions" {
  local base=busybox
  _prefetch $base

  contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p ${contextdir}
  createrandom ${contextdir}/randomfile

  # count the number of layers in the base image
  run_buildah inspect --type=image --format "{{ len .Docker.RootFS.DiffIDs }}" $base
  local baselayers="$output"
  local baselayersplusone=$(( ${baselayers} + 1 ))
  run_buildah inspect --type=image --format "{{ len .Docker.History }}" $base
  local basehistory="$output"

  for instruction in \
    "ADD randomfile /" \
    "ARG A=B" \
    "COPY randomfile /" \
    "ENTRYPOINT /bin/bash" \
    "ENV A=B C=D" \
    "EXPOSE 85" \
    "HEALTHCHECK CMD /bin/pwd" \
    "LABEL A=B C=D" \
    "MAINTAINER who knows" \
    "RUN pwd" \
    "USER 1:2" \
    "VOLUME /pants" \
    "WORKDIR /pants" \
    ; do
    cat > ${contextdir}/Dockerfile << _EOF
FROM $base
$instruction
_EOF
    # build an image based on $base with one instruction added"
    run_buildah build $WITH_POLICY_JSON --iidfile ${TEST_SCRATCH_DIR}/iid.txt ${contextdir}
    # expect one new history entry
    run_buildah inspect --type=image --format "{{ len .Docker.History }}" $(< ${TEST_SCRATCH_DIR}/iid.txt)
    local derivedhistory="$output"
    assert $derivedhistory -eq $(( ${basehistory} + 1 )) "expected one new history entry in derived image"
    # count the number of layers in the newly-built image
    run_buildah inspect --type=image --format "{{ len .Docker.RootFS.DiffIDs }}" $(< ${TEST_SCRATCH_DIR}/iid.txt)
    local derivedlayers="$output"
    # expectations vary based on what that instruction was
    case "$instruction" in
      # if it's ADD, COPY, or RUN, accept either the same set of layers, or one more layer added,
      # but not with a digest that matches a turns-out-to-be-empty layer
      ADD*|COPY*|RUN*)
        if test ${baselayersplusone} -eq ${derivedlayers} ; then
          run_buildah inspect --type=image --format "{{ index .Docker.RootFS.DiffIDs $baselayers }}" $(< ${TEST_SCRATCH_DIR}/iid.txt)
          local lastdiffid="$output"
          # sha256sum([0]{})
          assert lastdiffid != sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
          # sha256sum([1024]{})
          assert lastdiffid != sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef
          # sha512sum([0]{})
          assert lastdiffid != sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e
          # sha512sum([1024]{})
          assert lastdiffid != sha512:8efb4f73c5655351c444eb109230c556d39e2c7624e9c11abc9e3fb4b9b9254218cc5085b454a9698d085cfa92198491f07a723be4574adc70617b73eb0b6461
        else
          assert ${baselayers} -eq ${derivedlayers} "should have omitted turned-out-empty layer"
        fi
        ;;
      # every other instruction -> no new layer
      *) assert ${baselayers} -eq ${derivedlayers} "should not have added new layers for $instruction" ;;
    esac
  done
}

@test "bud build with heredoc content" {
  _prefetch alpine
  run_buildah build -t heredoc $WITH_POLICY_JSON -f $BUDFILES/heredoc/Containerfile .
  expect_output --substring "print first line from heredoc"
  expect_output --substring "print second line from heredoc"
  expect_output --substring "Heredoc writing first file"
  expect_output --substring "some text of first file"
  expect_output --substring "file passed to program"
  expect_output --substring "and it contains multiple"
  expect_output --substring "(your index page goes here)"
  expect_output --substring "(robots content)"
  expect_output --substring "(humans content)"
  expect_output --substring "this is the output of test6 part1"
  expect_output --substring "this is the output of test6 part2"
  expect_output --substring "this is the output of test7 part1"
  expect_output --substring "this is the output of test7 part2"
  expect_output --substring "this is the output of test7 part3"
  expect_output --substring "this is the output of test8 part1"
  expect_output --substring "this is the output of test8 part2"

  # verify that build output contains summary of heredoc content
  expect_output --substring 'RUN <<EOF \(echo "print first line from heredoc"...)'
  expect_output --substring 'RUN <<EOF \(echo "Heredoc writing first file" >> /file1...)'
  expect_output --substring 'RUN cat <<EOF \(file passed to program...)'
  expect_output --substring 'ADD <<EOF /index.html \(\(your index page goes here))'
  expect_output --substring 'COPY <<robots.txt <<humans.txt /test/ \(\(robots content)) \(\(humans content))'
}

@test "bud build with heredoc with COPY instructionw with .containerignore set" {
  run_buildah build -t heredoc $WITH_POLICY_JSON -f $BUDFILES/heredoc-ignore/Containerfile --ignorefile $BUDFILES/heredoc-ignore/.containerignore .
  expect_output --substring "This is a file"
  expect_output --substring "This is a line from file"
}

@test "bud build with heredoc content which is a bash file" {
  skip_if_in_container
  _prefetch busybox
  run_buildah build -t heredoc $WITH_POLICY_JSON -f $BUDFILES/heredoc/Containerfile.bash_file .
  expect_output --substring "this is the output of test9"
  expect_output --substring "this is the output of test10"
}

@test "bud build with heredoc content with inline interpreter" {
  skip_if_in_container
  _prefetch busybox
  run_buildah build -t heredoc $WITH_POLICY_JSON -f $BUDFILES/heredoc/Containerfile.she_bang .
  expect_output --substring "#
this is the output of test11
this is the output of test12"
}

@test "bud build with heredoc verify mount leak" {
  skip_if_in_container
  _prefetch alpine
  run_buildah 1 build -t heredoc $WITH_POLICY_JSON -f $BUDFILES/heredoc/Containerfile.verify_mount_leak .
  expect_output --substring "this is the output of test"
  expect_output --substring "ls: /dev/pipes: No such file or directory"
}

@test "bud with .containerignore" {
  _prefetch alpine busybox
  run_buildah 125 build -t testbud $WITH_POLICY_JSON -f $BUDFILES/containerignore/Dockerfile $BUDFILES/containerignore
  expect_output --substring 'building.*"COPY subdir \./".*no such file or directory'

  run_buildah build -t testbud $WITH_POLICY_JSON -f $BUDFILES/containerignore/Dockerfile.succeed $BUDFILES/containerignore

  run_buildah from --name myctr testbud

  run_buildah 1 run myctr ls -l test1.txt

  run_buildah run myctr ls -l test2.txt

  run_buildah 1 run myctr ls -l sub1.txt

  run_buildah 1 run myctr ls -l sub2.txt

  run_buildah 1 run myctr ls -l subdir/
}

@test "bud with .dockerignore - unmatched" {
  # Here .dockerignore contains 'unmatched', which will not match anything.
  # Therefore everything in the subdirectory should be copied into the image.
  #
  # We need to do this from a tmpdir, not the original or distributed
  # bud subdir, because of rpm: as of 2020-04-01 rpmbuild 4.16 alpha
  # on rawhide no longer packages circular symlinks (rpm issue #1159).
  # We used to include these symlinks in git and the rpm; now we need to
  # set them up manually as part of test setup to be able to package tests.
  local contextdir=${TEST_SCRATCH_DIR}/dockerignore2
  cp -a $BUDFILES/dockerignore2 $contextdir

  # Create symlinks, including bad ones
  ln -sf subdir        $contextdir/symlink
  ln -sf circular-link $contextdir/subdir/circular-link
  ln -sf no-such-file  $contextdir/subdir/dangling-link

  # Build, create a container, mount it, and list all files therein
  run_buildah build -t testbud2 $WITH_POLICY_JSON $contextdir

  run_buildah from --pull=false testbud2
  cid=$output

  run_buildah_mount $cid
  mnt=$output
  run find $mnt -printf "%P(%l)\n"
  filelist=$(LC_ALL=C sort <<<"$output")
  run_buildah_umount $cid

  # Format is: filename, and, in parentheses, symlink target (usually empty)
  # The list below has been painstakingly crafted; please be careful if
  # you need to touch it (e.g. if you add new files/symlinks)
  expect="()
.dockerignore()
Dockerfile()
subdir()
subdir/circular-link(circular-link)
subdir/dangling-link(no-such-file)
subdir/sub1.txt()
subdir/subsubdir()
subdir/subsubdir/subsub1.txt()
symlink(subdir)"

  # If this test ever fails, the 'expect' message will be almost impossible
  # for humans to read -- sorry, I never implemented multi-line comparisons.
  # Should this ever happen, uncomment these two lines and run tests in
  # your own vm; then diff the two files.
  #echo "$filelist" >${TMPDIR}/filelist.actual
  #echo "$expect"   >${TMPDIR}/filelist.expect

  expect_output --from="$filelist" "$expect" "container file list"
}

@test "bud with .dockerignore #2" {
  _prefetch busybox
  run_buildah 125 build -t testbud3 $WITH_POLICY_JSON $BUDFILES/dockerignore3
  expect_output --substring 'building.*"COPY test1.txt /upload/test1.txt".*no such file or directory'
  expect_output --substring 'filtered out using /[^ ]*/.dockerignore'
}

@test "bud with .dockerignore #4" {
  _prefetch busybox
  run_buildah 125 build -t testbud3 $WITH_POLICY_JSON -f Dockerfile.test $BUDFILES/dockerignore4
  expect_output --substring 'building.*"COPY test1.txt /upload/test1.txt".*no such file or directory'
  expect_output --substring '1 filtered out using /[^ ]*/Dockerfile.test.dockerignore'
}

@test "bud with .dockerignore #6" {
  _prefetch alpine busybox
  run_buildah 125 build -t testbud $WITH_POLICY_JSON -f $BUDFILES/dockerignore6/Dockerfile $BUDFILES/dockerignore6
  expect_output --substring 'building.*"COPY subdir \./".*no such file or directory'

  run_buildah build -t testbud $WITH_POLICY_JSON -f $BUDFILES/dockerignore6/Dockerfile.succeed $BUDFILES/dockerignore6

  run_buildah from --name myctr testbud

  run_buildah 1 run myctr ls -l test1.txt

  run_buildah run myctr ls -l test2.txt

  run_buildah 1 run myctr ls -l sub1.txt

  run_buildah 1 run myctr ls -l sub2.txt

  run_buildah 1 run myctr ls -l subdir/
}

@test "build with --platform without OS" {
  run_buildah info --format '{{.host.arch}}'
  myarch="$output"

  run_buildah build --platform $myarch $WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile
  expect_output --substring "This is built for $myarch"

  ## podman-remote binding has a bug where is sends `--platform as /`
  run_buildah build --platform "/" $WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile
  expect_output --substring "This is built for $myarch"
}

@test "build with basename resolving default arg" {
  run_buildah info --format '{{.host.os}}/{{.host.arch}}{{if .host.variant}}/{{.host.variant}}{{end}}'
  myplatform="$output"
  run_buildah info --format '{{.host.arch}}'
  myarch="$output"

  run_buildah build --platform ${myplatform} $WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile
  expect_output --substring "This is built for $myarch"

  run_buildah build                          $WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile
  expect_output --substring "This is built for $myarch"
}

@test "build with basename resolving user arg" {
  _prefetch alpine
  run_buildah build --build-arg CUSTOM_TARGET=first $WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile2
  expect_output --substring "This is built for first"
  run_buildah build --build-arg CUSTOM_TARGET=second $WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile2
  expect_output --substring "This is built for second"
}

@test "build with basename resolving user arg from file" {
  _prefetch alpine
  run_buildah build \
	--build-arg-file $BUDFILES/base-with-arg/first.args \
	$WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile2
  expect_output --substring "This is built for first"

  run_buildah build \
	--build-arg-file $BUDFILES/base-with-arg/second.args \
	$WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile2
  expect_output --substring "This is built for second"
}

@test "build with basename resolving user arg from latest file in arg list" {
  _prefetch alpine
  run_buildah build \
	--build-arg-file $BUDFILES/base-with-arg/second.args \
	--build-arg-file $BUDFILES/base-with-arg/first.args \
	$WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile2
  expect_output --substring "This is built for first"
}

@test "build with basename resolving user arg from in arg list" {
  _prefetch alpine
  run_buildah build \
	--build-arg-file $BUDFILES/base-with-arg/second.args \
	--build-arg CUSTOM_TARGET=first \
	$WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfile2
  expect_output --substring "This is built for first"
}

# Following test should fail since we are trying to use build-arg which
# was not declared. Honors discussion here: https://github.com/containers/buildah/pull/4061/commits/1237c04d6ae0ee1f027a1f02bf3ab5c57ac7d9b6#r906188374
@test "build with basename resolving user arg - should fail" {
  _prefetch alpine
  run_buildah 125 build --build-arg CUSTOM_TARGET=first $WITH_POLICY_JSON -t test -f $BUDFILES/base-with-arg/Containerfilebad
  expect_output --substring "invalid reference format"
}

# Try building with arch and variant
# Issue: https://github.com/containers/buildah/issues/4276
@test "build-with-inline-platform-and-variant" {
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  cat > $contextdir/Dockerfile << _EOF
FROM --platform=freebsd/arm64/v8 scratch
COPY . .
_EOF

  run_buildah build $WITH_POLICY_JSON -t test $contextdir
  run_buildah inspect --format '{{ .OCIv1.Architecture }}' test
  expect_output --substring "arm64"
  run_buildah inspect --format '{{ .OCIv1.Variant }}' test
  expect_output --substring "v8"
}

# Following test must fail since we are trying to run linux/arm64 on linux/amd64
# Issue: https://github.com/containers/buildah/issues/3712
@test "build-with-inline-platform" {
  # Host arch
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  run_buildah info --format '{{.host.arch}}'
  myarch="$output"
  otherarch="arm64"

  # just make sure that other arch is not equivalent to host arch
  if [[ "$otherarch" == "$myarch" ]]; then
    otherarch="amd64"
  fi
  # ...create a Containerfile with --platform=linux/$otherarch
  cat > $contextdir/Dockerfile << _EOF
FROM --platform=linux/${otherarch} alpine
RUN uname -m
_EOF

  run_buildah '?' build $WITH_POLICY_JSON -t test $contextdir
  if [[ $status -eq 0 ]]; then
    run_buildah inspect --format '{{ .OCIv1.Architecture }}' test
    expect_output --substring "$otherarch"
  else
    # Build failed: we DO NOT have qemu-user-static installed.
    expect_output --substring "format error"
  fi
}

@test "build-with-inline-platform-and-rely-on-defaultbuiltinargs" {
  # Get host arch
  run_buildah info --format '{{.host.arch}}'
  myarch="$output"
  otherarch="arm64"
  # just make sure that other arch is not equivalent to host arch
  if [[ "$otherarch" == "$myarch" ]]; then
    otherarch="amd64"
  fi

  run_buildah build --platform linux/$otherarch $WITH_POLICY_JSON -t test -f $BUDFILES/multiarch/Dockerfile.built-in-args
  expect_output --substring "I'm compiling for linux/$otherarch"
  expect_output --substring "and tagging for linux/$otherarch"
  expect_output --substring "and OS linux"
  expect_output --substring "and ARCH $otherarch"
  run_buildah inspect --format '{{ .OCIv1.Architecture }}' test
  expect_output --substring "$otherarch"
}

# Buildkit parity: this verifies if we honor custom overrides of TARGETOS, TARGETVARIANT, TARGETARCH and TARGETPLATFORM if user wants
@test "build-with-inline-platform-and-rely-on-defaultbuiltinargs-check-custom-override" {
  run_buildah build --platform linux/arm64 $WITH_POLICY_JSON --build-arg TARGETOS=android -t test -f $BUDFILES/multiarch/Dockerfile.built-in-args
  expect_output --substring "I'm compiling for linux/arm64"
  expect_output --substring "and tagging for linux/arm64"
  ## Note since we used --build-arg and overrode OS, OS must be android
  expect_output --substring "and OS android"
  expect_output --substring "and ARCH $otherarch"
  run_buildah inspect --format '{{ .OCIv1.Architecture }}' test
  expect_output --substring "$otherarch"
}

# Following test must pass since we want to tag image as host arch
# Test for use-case described here: https://github.com/containers/buildah/issues/3261
@test "build-with-inline-platform-amd-but-tag-as-arm" {
  # Host arch
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  run_buildah info --format '{{.host.arch}}'
  myarch="$output"
  targetarch="arm64"

  if [[ "$targetArch" == "$myarch" ]]; then
    targetarch="amd64"
  fi

  cat > $contextdir/Dockerfile << _EOF
FROM --platform=linux/${myarch} alpine
RUN uname -m
_EOF

  # Tries building image where baseImage has --platform=linux/HostArch
  run_buildah build --platform linux/${targetarch} $WITH_POLICY_JSON -t test $contextdir
  run_buildah inspect --format '{{ .OCIv1.Architecture }}' test
  # base image is pulled as HostArch but tagged as non host arch
  expect_output --substring $targetarch
}

# Test build with --add-history=false
@test "build-with-omit-history-to-true should not add history" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
RUN echo hello
RUN echo world
_EOF

  # Built image must not contain history for the layers which we have just built.
  run_buildah build $WITH_POLICY_JSON --omit-history -t source -f $contextdir/Dockerfile1
  run_buildah inspect --format "{{index .Docker.History}}" source
  expect_output "[]"
  run_buildah inspect --format "{{index .OCIv1.History}}" source
  expect_output "[]"
  run_buildah inspect --format "{{index .History}}" source
  expect_output "[]"
}

# Test building with --userns=auto
@test "build with --userns=auto also with size" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  user=$USER

  if [[ "$user" == "root" ]]; then
    user="containers"
  fi

  if ! grep -q $user "/etc/subuid"; then
    skip "cannot find mappings for the current user"
  fi

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN cat /proc/self/uid_map
RUN echo hello

FROM alpine
COPY --from=0 /tmp /tmp
RUN cat /proc/self/uid_map
RUN ls -a
_EOF

  run_buildah build --userns=auto $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  expect_output --substring "1024"
  run_buildah build --userns=auto:size=500 $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  expect_output --substring "500"
}

# Test building with --userns=auto with uidmapping
@test "build with --userns=auto with uidmapping" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  user=$USER

  if [[ "$user" == "root" ]]; then
    user="containers"
  fi

  if ! grep -q $user "/etc/subuid"; then
    skip "cannot find mappings for the current user"
  fi

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN cat /proc/self/uid_map
_EOF

  run_buildah build --userns=auto:size=8192,uidmapping=0:0:1 $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  expect_output --substring "8191"
  run_buildah build --userns=auto:uidmapping=0:0:1 $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  expect_output --substring "         0          0          1"
}

# Test building with --userns=auto with gidmapping
@test "build with --userns=auto with gidmapping" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  user=$USER

  if [[ "$user" == "root" ]]; then
    user="containers"
  fi

  if ! grep -q $user "/etc/subuid"; then
    skip "cannot find mappings for the current user"
  fi

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN cat /proc/self/gid_map
_EOF

  run_buildah build --userns=auto:size=8192,gidmapping=0:0:1 $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  expect_output --substring "8191"
  run_buildah build --userns=auto:gidmapping=0:0:1 $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  expect_output --substring "         0          0          1"
}

# Test bud with prestart hook
@test "build-test with OCI prestart hook" {
  skip_if_in_container # This works in privileged container setup but does not works in CI setup
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir/hooks

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo hello
_EOF

  cat > $contextdir/hooks/test.json << _EOF
{
  "version": "1.0.0",
  "hook": {
    "path": "$contextdir/hooks/test"
  },
  "when": {
    "always": true
  },
  "stages": ["prestart"]
}
_EOF

  cat > $contextdir/hooks/test << _EOF
#!/bin/sh
echo from-hook > $contextdir/hooks/hook-output
_EOF

  # make actual hook executable
  chmod +x $contextdir/hooks/test
  run_buildah build $WITH_POLICY_JSON -t source --hooks-dir=$contextdir/hooks -f $contextdir/Dockerfile
  run cat $contextdir/hooks/hook-output
  expect_output --substring "from-hook"
}

@test "build with add resolving to invalid HTTP status code" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/build-context
  mkdir -p $contextdir

  local contentdir=${TEST_SCRATCH_DIR}/content
  mkdir -p $contentdir
  starthttpd $contentdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
ADD http://0.0.0.0:${HTTP_SERVER_PORT}/test /
_EOF

  run_buildah 125 build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  expect_output --substring "invalid response status"
}

@test "build test has gid in supplemental groups" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON -t source -f $BUDFILES/supplemental-groups/Dockerfile
  # gid 1000 must be in supplemental groups
  expect_output --substring "Groups:	1000"
}

@test "build-mount-cache-with-id-mappings" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir ${contextdir}

  # with no ID mappings
  local cacheid=${SRANDOM}
  cat > ${contextdir}/Dockerfile << EOF
  FROM alpine
  USER 1000:1000
  RUN --mount=type=cache,id=${cacheid},target=/var/tmp,uid=1000,gid=1000 stat / /var/tmp
  RUN --mount=type=cache,id=${cacheid},target=/var/tmp,uid=1000,gid=1000 test \`stat -c %u /var/tmp\` -eq 1000
  RUN --mount=type=cache,id=${cacheid},target=/var/tmp,uid=1000,gid=1000 touch /var/tmp/should-be-able-to-write
  RUN --mount=type=cache,id=${cacheid},target=/new-parent/var/tmp,uid=1000,gid=1000 touch /var/tmp/should-be-able-to-write
  RUN --mount=type=cache,id=${cacheid},target=/var/new-parent/tmp,uid=1000,gid=1000 touch /var/tmp/should-be-able-to-write
EOF
  run_buildah build $WITH_POLICY_JSON ${contextdir}

  # with non-default ID mappings
  local cacheid=${SRANDOM}
  cat > ${contextdir}/Dockerfile << EOF
  FROM alpine
  USER 1000:1000
  RUN --mount=type=cache,id=${cacheid},target=/var/tmp,uid=1000,gid=1000 stat / /var/tmp
  RUN --mount=type=cache,id=${cacheid},target=/var/tmp,uid=1000,gid=1000 test \`stat -c %u /var/tmp\` -eq 1000
  RUN --mount=type=cache,id=${cacheid},target=/var/tmp,uid=1000,gid=1000 touch /var/tmp/should-be-able-to-write
  RUN --mount=type=cache,id=${cacheid},target=/new/parent/var/tmp,uid=1000,gid=1000 touch /new/parent/var/tmp/should-be-able-to-write
  RUN --mount=type=cache,id=${cacheid},target=/var/new/parent/tmp,uid=1000,gid=1000 touch /var/new/parent/tmp/should-be-able-to-write
EOF
  if test `id -u` -eq 0 ; then
    run_buildah build --userns-uid-map 0:1:1023 --userns-gid-map 0:1:1023 $WITH_POLICY_JSON ${contextdir}
  else
    run_buildah build --userns auto:size=1023 $WITH_POLICY_JSON ${contextdir}
  fi
}

@test "build-mount-cache-writeable-as-unprivileged-user" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir ${contextdir}

  cat > ${contextdir}/Dockerfile << EOF
  FROM busybox
  USER 1000:1000
  RUN --mount=type=cache,target=/usr/local/bin,id=/usr/local/bin/$$,uid=1000,gid=1000 touch /usr/local/bin/new-file
  RUN --mount=type=cache,target=/var/not/already/there,id=/var/not/already/there/$$,uid=1000,gid=1000 touch /var/not/already/there/new-file
EOF
  run_buildah build $WITH_POLICY_JSON ${contextdir}
}

@test "build-mount-bind-readable-as-unprivileged-user" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir ${contextdir}

  cat > ${contextdir}/Dockerfile << EOF
  FROM busybox
  USER 1000:1000
  RUN --mount=type=bind,target=/usr/local,from=busybox busybox ls /usr/local/bin/busybox
  RUN --mount=type=bind,target=/var/not/already/there,from=busybox busybox ls /var/not/already/there/bin/busybox
EOF
  run_buildah build $WITH_POLICY_JSON ${contextdir}
}

@test "build-mount-secret-readable-as-unprivileged-user" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir ${contextdir}
  local secretfile=${TEST_SCRATCH_DIR}/secret.txt

  echo -n hidingInPlainSight > ${secretfile}
  cat > ${contextdir}/Dockerfile << EOF
  FROM busybox
  USER 1000:1000
  RUN --mount=type=secret,id=theSecret,target=/var/not/already/there,uid=1000,gid=1000 wc -c /var/not/already/there
EOF
  run_buildah build --secret id=theSecret,type=file,src=${secretfile} $WITH_POLICY_JSON ${contextdir}
  cat > ${contextdir}/Dockerfile << EOF
  FROM busybox
  USER 1000:1000
  RUN --mount=type=secret,id=theSecret,target=/top/var/tmp/there,uid=1000,gid=1000 wc -c /top/var/tmp/there
EOF
  run_buildah build --secret id=theSecret,type=file,src=${secretfile} $WITH_POLICY_JSON ${contextdir}
}

@test "build test if supplemental groups has gid with --isolation chroot" {
  test "${BUILDAH_ISOLATION}" != chroot || skip "BUILDAH_ISOLATION=${BUILDAH_ISOLATION} overrides --isolation"

  _prefetch alpine
  run_buildah build --isolation chroot $WITH_POLICY_JSON -t source -f $BUDFILES/supplemental-groups/Dockerfile
  # gid 1000 must be in supplemental groups
  expect_output --substring "Groups:	1000"
}

@test "build-test --mount=type=secret test relative to workdir mount" {
  _prefetch alpine
  local contextdir=$BUDFILES/secret-relative
  run_buildah build $WITH_POLICY_JSON --no-cache --secret id=secret-foo,src=$contextdir/secret1.txt --secret id=secret-bar,src=$contextdir/secret2.txt -t test -f $contextdir/Dockerfile
  expect_output --substring "secret:foo"
  expect_output --substring "secret:bar"
}

@test "build-test --mount=type=cache test relative to workdir mount" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  ## write-cache
  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN mkdir test
WORKDIR test
RUN --mount=type=cache,id=YfHI60aApFM-target,target=target echo world > /test/target/hello
_EOF

  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN mkdir test
WORKDIR test
RUN --mount=type=cache,id=YfHI60aApFM-target,target=target cat /test/target/hello
_EOF

  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  expect_output --substring "world"
}

@test "build-test do not use mount stage from cache if it was rebuilt" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine as dependencies

RUN mkdir /build && echo v1 > /build/version

FROM alpine

RUN --mount=type=bind,source=/build,target=/build,from=dependencies \
    cp /build/version /version

RUN cat /version
_EOF

  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile
  run_buildah build $WITH_POLICY_JSON --layers -t source2 -f $contextdir/Dockerfile
  expect_output --substring "Using cache"

  # First stage i.e dependencies is changed so it should not use the steps in second stage from
  # cache
  cat > $contextdir/Dockerfile << _EOF
FROM alpine as dependencies

RUN mkdir /build && echo v2 > /build/version

FROM alpine

RUN --mount=type=bind,source=/build,target=/build,from=dependencies \
    cp /build/version /version

RUN cat /version
_EOF

  run_buildah build $WITH_POLICY_JSON --layers -t source3 -f $contextdir/Dockerfile
  assert "$output" !~ "Using cache"

}

@test "build-test use image from cache with --mount and burst when image is changed" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Containerfile << _EOF
FROM alpine
RUN touch firstfile
_EOF

  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Containerfile

  cat > $contextdir/Containerfile2 << _EOF
FROM alpine

RUN --mount=type=bind,source=.,target=/build,from=source ls /build

_EOF

  run_buildah build $WITH_POLICY_JSON --layers -t source2 -f $contextdir/Containerfile2
  expect_output --substring "firstfile"

  # Building again must use cache
  run_buildah build $WITH_POLICY_JSON --layers -t source2 -f $contextdir/Containerfile2
  expect_output --substring "Using cache"
  assert "$output" !~ "firstfile"
}

# Verify: https://github.com/containers/buildah/issues/4572
@test "build-test verify no dangling containers are left" {
  _prefetch alpine busybox
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine AS alpine_builder
FROM busybox AS busybox_builder
FROM scratch
COPY --from=alpine_builder /etc/alpine* .
COPY --from=busybox_builder /bin/busybox /bin/busybox
_EOF

  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  # No leftover containers, just the header line.
  run_buildah containers
  expect_line_count 1
}

# Verify: https://github.com/containers/buildah/issues/4485
# Verify: https://github.com/containers/buildah/issues/4319
@test "No default warning for TARGETARCH, TARGETOS, TARGETPLATFORM " {
  local contextdir=$BUDFILES/targetarch

  run_buildah build $WITH_POLICY_JSON --platform=linux/amd64,linux/arm64 -f $contextdir/Dockerfile
  assert "$output" !~ "one or more build args were not consumed" \
	 "No warning for default args should be there"

  run_buildah build $WITH_POLICY_JSON --os linux -f $contextdir/Dockerfile
  assert "$output" !~ "Try adding" \
	"No Warning for default args should be there"
}


@test "build-test skipping unwanted stages with --skip-unused-stages=false and --skip-unused-stages=true" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo "first unwanted stage"

FROM alpine as one
RUN echo "needed stage"

FROM alpine
RUN echo "another unwanted stage"

FROM one
RUN echo "target stage"
_EOF

  # with --skip-unused-stages=false
  run_buildah build $WITH_POLICY_JSON --skip-unused-stages=false -t source -f $contextdir/Dockerfile
  expect_output --substring "needed stage"
  expect_output --substring "target stage"
  # this is expected since user specified `--skip-unused-stages=false`
  expect_output --substring "first unwanted stage"
  expect_output --substring "another unwanted stage"

  # with --skip-unused-stages=true
  run_buildah build $WITH_POLICY_JSON --skip-unused-stages=true -t source -f $contextdir/Dockerfile
  expect_output --substring "needed stage"
  expect_output --substring "target stage"
  assert "$output" !~ "unwanted stage"
}

@test "build-test: do not warn for instructions declared in unused stages" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo "first unwanted stage"

FROM alpine as one
RUN echo "needed stage"

FROM alpine
ARG FOO_BAR
RUN echo "another unwanted stage"

FROM one
RUN echo "target stage"
_EOF

  # with --skip-unused-stages=true no warning should be printed since ARG is declared in stage which is not used
  run_buildah build $WITH_POLICY_JSON --skip-unused-stages=true -t source -f $contextdir/Dockerfile
  expect_output --substring "needed stage"
  expect_output --substring "target stage"
  assert "$output" !~ "unwanted stage"
  # must not contain warning "missing FOO_BAR"
  assert "$output" !~ "missing"

  # with --skip-unused-stages=false should print unwanted stage as well as warning for unused arg
  run_buildah build $WITH_POLICY_JSON --skip-unused-stages=false -t source -f $contextdir/Dockerfile
  expect_output --substring "needed stage"
  expect_output --substring "target stage"
  expect_output --substring "unwanted stage"
  expect_output --substring "missing"
}

# Test skipping images with FROM
@test "build-test skipping unwanted stages with FROM" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo "unwanted stage"

FROM alpine as one
RUN echo "needed stage"

FROM alpine
RUN echo "another unwanted stage"

FROM one
RUN echo "target stage"
_EOF

  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile
  expect_output --substring "needed stage"
  expect_output --substring "target stage"
  assert "$output" !~ "unwanted stage"
}

# Note: Please skip this tests in case of podman-remote build
@test "build: test race in updating image name while performing parallel commits" {
  _prefetch alpine
  # Run 25 parallel builds using the same Containerfile
  local count=25
  for i in $(seq --format '%02g' 1 $count); do
      timeout --foreground -v --kill=10 300 \
              ${BUILDAH_BINARY} ${BUILDAH_REGISTRY_OPTS} ${ROOTDIR_OPTS} $WITH_POLICY_JSON build --quiet --squash --iidfile ${TEST_SCRATCH_DIR}/id.$i --timestamp 0 -f $BUDFILES/check-race/Containerfile >/dev/null &
  done
  # Wait for all background builds to complete. Note that this succeeds
  # even if some of the individual builds fail! Our actual test is below.
  wait
  # Number of output bytes must be always same, which confirms that there is no race.
  assert "$(cat ${TEST_SCRATCH_DIR}/id.* | wc -c)" = 1775 "Total chars in all id.* files"
}

# Test skipping images with FROM but stage name also conflicts with additional build context
# so selected stage should be still skipped since it is not being actually used by additional build
# context is being used.
@test "build-test skipping unwanted stages with FROM and conflict with additional build context" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  # add file on original context
  echo something > $contextdir/somefile

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo "unwanted stage"

FROM alpine as one
RUN echo "unwanted stage"
RUN echo "from stage unwanted stage"

FROM alpine
RUN echo "another unwanted stage"

FROM alpine
COPY --from=one somefile .
RUN cat somefile
_EOF

  run_buildah build $WITH_POLICY_JSON --build-context one=$contextdir -t source -f $contextdir/Dockerfile
  expect_output --substring "something"
  assert "$output" !~ "unwanted stage"
}

# Test skipping unwanted stage with COPY from stage name
@test "build-test skipping unwanted stages with COPY from stage name" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  echo something > $contextdir/somefile
  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo "unwanted stage"

FROM alpine as one
RUN echo "needed stage"
COPY somefile file

FROM alpine
COPY --from=one file .
RUN cat file
RUN echo "target stage"
_EOF

  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile $contextdir
  expect_output --substring "needed stage"
  expect_output --substring "something"
  expect_output --substring "target stage"
  assert "$output" !~ "unwanted stage"
}

@test "build test --retry and --retry-delay" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  echo something > $contextdir/somefile
  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo hello
_EOF

  run_buildah --log-level debug build --retry 4 --retry-delay 5s $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile $contextdir
  expect_output --substring "Setting MaxPullPushRetries to 4 and PullPushRetryDelay to 5s"
}

# Test skipping unwanted stage with COPY from stage index
@test "build-test skipping unwanted stages with COPY from stage index" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  echo something > $contextdir/somefile
  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo "unwanted stage"

FROM alpine
RUN echo "needed stage"
COPY somefile file

FROM alpine
RUN echo "another unwanted stage"

FROM alpine
COPY --from=1 file .
RUN cat file
RUN echo "target stage"
_EOF

  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile $contextdir
  expect_output --substring "needed stage"
  expect_output --substring "something"
  expect_output --substring "target stage"
  assert "$output" !~ "unwanted stage"
}

# Test if our cache is working in optimal way for COPY use case
@test "build test optimal cache working for COPY instruction" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  echo something > $contextdir/somefile
  cat > $contextdir/Dockerfile << _EOF
FROM alpine
COPY somefile .
_EOF

  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile $contextdir
  # Run again and verify if we hit cache in first pass
  run_buildah --log-level debug build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile $contextdir
  expect_output --substring "Found a cache hit in the first iteration"
}

# Test if our cache is working in optimal way for ADD use case
@test "build test optimal cache working for ADD instruction" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  echo something > $contextdir/somefile
  cat > $contextdir/Dockerfile << _EOF
FROM alpine
ADD somefile .
_EOF

  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile $contextdir
  # Run again and verify if we hit cache in first pass
  run_buildah --log-level debug build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile $contextdir
  expect_output --substring "Found a cache hit in the first iteration"
}

# Test skipping unwanted stage with --mount from another stage
@test "build-test skipping unwanted stages with --mount from stagename" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  echo something > $contextdir/somefile
  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo "unwanted stage"

FROM alpine as one
RUN echo "needed stage"
COPY somefile file

FROM alpine
RUN echo "another unwanted stage"

FROM alpine
RUN --mount=type=bind,from=one,target=/test cat /test/file
RUN echo "target stage"
_EOF

  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile $contextdir
  expect_output --substring "needed stage"
  expect_output --substring "something"
  expect_output --substring "target stage"
  assert "$output" !~ "unwanted stage"
}

# Test skipping unwanted stage with --mount from another stage
@test "build-test skipping unwanted stages with --mount from stagename with flag order changed" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  echo something > $contextdir/somefile
  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN echo "unwanted stage"

FROM alpine as one
RUN echo "needed stage"
COPY somefile file

FROM alpine
RUN echo "another unwanted stage"

FROM alpine
RUN --mount=from=one,target=/test,type=bind cat /test/file
RUN echo "target stage"
_EOF

  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile $contextdir
  expect_output --substring "needed stage"
  expect_output --substring "something"
  expect_output --substring "target stage"
  assert "$output" !~ "unwanted stage"
}

# Test pinning image using additional build context
@test "build-with-additional-build-context and COPY, test pinning image" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
RUN touch hello
RUN echo world > hello
_EOF

  cat > $contextdir/Dockerfile2 << _EOF
FROM alpine
COPY --from=busybox hello .
RUN cat hello
_EOF

  # Build a first image which we can use as source
  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile1
  # Pin upstream busybox to local image source
  run_buildah build $WITH_POLICY_JSON --build-context busybox=docker://source -t test -f $contextdir/Dockerfile2
  expect_output --substring "world"
}

# Test conflict between stage short name and additional-context conflict
# Buildkit parity give priority to additional-context over stage names.
@test "build-with-additional-build-context and COPY, stagename and additional-context conflict" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
RUN touch hello
RUN echo world > hello
_EOF

  cat > $contextdir/Dockerfile2 << _EOF
FROM alpine as some-stage
RUN echo world

# hello should get copied since we are giving priority to additional context
COPY --from=some-stage hello .
RUN cat hello
_EOF

  # Build a first image which we can use as source
  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile1
  # Pin upstream busybox to local image source
  run_buildah build $WITH_POLICY_JSON --build-context some-stage=docker://source -t test -f $contextdir/Dockerfile2
  expect_output --substring "world"
}

# When numeric index of stage is used and stage exists but additional context also exist with name
# same as stage in such situations always use additional context.
@test "build-with-additional-build-context and COPY, additionalContext and numeric value of stage" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
RUN touch hello
RUN echo override-numeric > hello
_EOF

  cat > $contextdir/Dockerfile2 << _EOF
FROM alpine as some-stage
RUN echo world > hello

# hello should get copied since we are accessing stage from its numeric value and not
# additional build context where some-stage is docker://alpine
FROM alpine
COPY --from=0 hello .
RUN cat hello
_EOF

  # Build a first image which we can use as source
  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile1
  run_buildah build $WITH_POLICY_JSON --build-context some-stage=docker://source -t test -f $contextdir/Dockerfile2
  expect_output --substring "override-numeric"
}

# Test conflict between stage short name and additional-context conflict on FROM
# Buildkit parity give priority to additional-context over stage names.
@test "build-with-additional-build-context and FROM, stagename and additional-context conflict" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
RUN touch hello
RUN echo world > hello
_EOF

  cat > $contextdir/Dockerfile2 << _EOF
FROM alpine as some-stage
RUN echo world

# hello should be there since we are giving priority to additional context
FROM some-stage
RUN cat hello
_EOF

  # Build a first image which we can use as source
  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile1
  # Second FROM should choose base as `source` instead of local-stage named `some-stage`.
  run_buildah build $WITH_POLICY_JSON --build-context some-stage=docker://source -t test -f $contextdir/Dockerfile2
  expect_output --substring "world"
}

# Test adding additional build context
@test "build-with-additional-build-context and COPY, additional context from host" {
  _prefetch alpine
  local contextdir1=${TEST_SCRATCH_DIR}/bud/platform
  local contextdir2=${TEST_SCRATCH_DIR}/bud/platform2
  mkdir -p $contextdir1 $contextdir2

  # add file on original context
  echo something > $contextdir1/somefile
  # add file on additional context
  echo hello_world > $contextdir2/hello

  cat > $contextdir1/Dockerfile << _EOF
FROM alpine
COPY somefile .
RUN cat somefile
COPY --from=context2 hello .
RUN cat hello
_EOF

  # Test additional context
  run_buildah build $WITH_POLICY_JSON -t source --build-context context2=$contextdir2 $contextdir1
  expect_output --substring "something"
  expect_output --substring "hello_world"
}

# Test adding additional build context but download tar
@test "build-with-additional-build-context and COPY, additional context from external URL" {
  _prefetch alpine
  starthttpd $(cd ${BUDFILES}/..; pwd)
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
COPY --from=repo-context . .
RUN ls objects/pack/
_EOF

  # Test additional context but download from tar
  run_buildah build $WITH_POLICY_JSON -t source --build-context repo-context=http://0.0.0.0:${HTTP_SERVER_PORT}/git-daemon/bare-podman-repo.tar.gz $contextdir
  # additional context from tar must show one of the pack files
  expect_output --substring "pack-"
}

# Test pinning image
@test "build-with-additional-build-context and FROM, pin busybox to alpine" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM busybox
RUN ls /etc/*release
_EOF

  # Test additional context but download from tar
  # We are pinning busybox to alpine so we must always pull alpine and use that
  run_buildah build $WITH_POLICY_JSON -t source --build-context busybox=docker://alpine $contextdir
  # We successfully pinned binary cause otherwise busybox should not contain alpine-release binary
  expect_output --substring "alpine-release"
}

# Test usage of RUN --mount=from=<name> with additional context and also test conflict with stage-name
@test "build-with-additional-build-context and RUN --mount=from=, additional-context and also test conflict with stagename" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
RUN touch hello
RUN echo world > hello
_EOF

  cat > $contextdir/Dockerfile2 << _EOF
FROM alpine as some-stage
RUN echo something_random

# hello should get copied since we are giving priority to additional context
FROM alpine
RUN --mount=type=bind,from=some-stage,target=/test cat /test/hello
_EOF

  # Build a first image which we can use as source
  run_buildah build $WITH_POLICY_JSON -t source -f $contextdir/Dockerfile1
  # Additional Context for RUN --mount is additional image and it should not conflict with stage
  run_buildah build $WITH_POLICY_JSON --build-context some-stage=docker://source -t test -f $contextdir/Dockerfile2
  expect_output --substring "world"
}

# Test usage of RUN --mount=from=<name> with additional context and also test conflict with stage-name, when additionalContext is on host
@test "build-with-additional-build-context and RUN --mount=from=, additional-context not image and also test conflict with stagename" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  echo world > $contextdir/hello

  cat > $contextdir/Dockerfile2 << _EOF
FROM alpine as some-stage
RUN echo some_text

# hello should get copied since we are giving priority to additional context
FROM alpine
RUN --mount=type=bind,from=some-stage,target=/test,z cat /test/hello
_EOF

  # Additional context for RUN --mount is file on host
  run_buildah build $WITH_POLICY_JSON --build-context some-stage=$contextdir -t test -f $contextdir/Dockerfile2
  expect_output --substring "world"
}

@test "build-with-additional-build-context must use cache if built with layers" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir
  echo world > $contextdir/hello

  cat > $contextdir/Containerfile2 << _EOF
FROM alpine as some-stage
RUN echo some_text

# hello should get copied since we are giving priority to additional context
FROM alpine
RUN --mount=type=bind,from=some-stage,target=/test,z cat /test/hello
_EOF

  # Additional context for RUN --mount is file on host
  run_buildah build $WITH_POLICY_JSON --layers --build-context some-stage=$contextdir -t test -f $contextdir/Containerfile2
  expect_output --substring "world"

  run_buildah build $WITH_POLICY_JSON --layers --build-context some-stage=$contextdir -t test -f $contextdir/Containerfile2
  expect_output --substring "Using cache"
  assert "$output" !~ "world"
}

# Test usage of RUN --mount=from=<name> with additional context is URL and mount source is relative using src
@test "build-with-additional-build-context and RUN --mount=from=, additional-context is URL and mounted from subdir" {
  _prefetch alpine
  starthttpd $(cd ${BUDFILES}/..; pwd)
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine as some-stage
RUN echo world

# hello should get copied since we are giving priority to additional context
FROM alpine
RUN --mount=type=bind,src=objects/pack,from=some-stage,target=/test,z ls /test
_EOF

  # Additional context for RUN --mount is file on host
  run_buildah build $WITH_POLICY_JSON --build-context some-stage=http://0.0.0.0:${HTTP_SERVER_PORT}/git-daemon/bare-podman-repo.tar.gz -t test $contextdir
  expect_output --substring "pack-"
}

@test "build-with-additional-build-context and COPY, ensure .containerignore is being respected" {
  _prefetch alpine
  local additionalcontextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $additionalcontextdir
  touch $additionalcontextdir/hello
  cat > $additionalcontextdir/.containerignore << _EOF
hello
_EOF

  cat > $additionalcontextdir/Containerfile << _EOF
FROM alpine
RUN echo world

# hello should not be available since
# it's excluded as per the additional
# build context's .containerignore file
COPY --from=project hello .
RUN cat hello
_EOF

  run_buildah 125 build $WITH_POLICY_JSON --build-context project=$additionalcontextdir -t test -f $additionalcontextdir/Containerfile
  expect_output --substring "COPY --from=project hello .\": no items matching glob"
}

@test "bud with --layers and --no-cache flags" {
  _prefetch alpine

  local contentdir=${TEST_SCRATCH_DIR}/content
  mkdir -p $contentdir
  echo somebody told me that this counts as a README file > ${contentdir}/README.md
  starthttpd ${contentdir}

  local contextdir=${TEST_SCRATCH_DIR}/use-layers
  cp -a $BUDFILES/use-layers $contextdir

  # Run with --pull-always to have a regression test for
  # containers/podman/issues/10307.
  run_buildah build --pull-always --build-arg=HTTP_SERVER_PORT=${HTTP_SERVER_PORT} $WITH_POLICY_JSON --layers -t test1 $contextdir
  run_buildah images -a
  expect_line_count 9

  run_buildah build --pull-never --build-arg=HTTP_SERVER_PORT=${HTTP_SERVER_PORT} $WITH_POLICY_JSON --layers -t test2 $contextdir
  run_buildah images -a
  expect_line_count 10
  run_buildah inspect --format "{{index .Docker.ContainerConfig.Env 1}}" test1
  expect_output "foo=bar"
  run_buildah inspect --format "{{index .Docker.ContainerConfig.Env 1}}" test2
  expect_output "foo=bar"
  run_buildah inspect --format "{{.Docker.ContainerConfig.ExposedPorts}}" test1
  expect_output "map[8080/tcp:{}]"
  run_buildah inspect --format "{{.Docker.ContainerConfig.ExposedPorts}}" test2
  expect_output "map[8080/tcp:{}]"
  run_buildah inspect --format "{{index .Docker.History 2}}" test1
  expect_output --substring "FROM docker.io/library/alpine:latest"

  run_buildah build $WITH_POLICY_JSON --layers -t test3 -f Dockerfile.2 $contextdir
  run_buildah images -a
  expect_line_count 12

  mkdir -p $contextdir/mount/subdir
  run_buildah build $WITH_POLICY_JSON --layers -t test4 -f Dockerfile.3 $contextdir
  run_buildah images -a
  expect_line_count 14

  run_buildah build $WITH_POLICY_JSON --layers -t test5 -f Dockerfile.3 $contextdir
  run_buildah images -a
  expect_line_count 15

  touch $contextdir/mount/subdir/file.txt
  run_buildah build $WITH_POLICY_JSON --layers -t test6 -f Dockerfile.3 $contextdir
  run_buildah images -a
  expect_line_count 17

  run_buildah build $WITH_POLICY_JSON --no-cache -t test7 -f Dockerfile.2 $contextdir
  run_buildah images -a
  expect_line_count 18
}

@test "bud with no --layers comment" {
  _prefetch alpine

  local contentdir=${TEST_SCRATCH_DIR}/content
  mkdir -p $contentdir
  echo i heard a rumor that this counts as a README file > ${contentdir}/README.md
  starthttpd ${contentdir}

  run_buildah build --pull-never --build-arg=HTTP_SERVER_PORT=${HTTP_SERVER_PORT} $WITH_POLICY_JSON --layers=false --no-cache -t test $BUDFILES/use-layers
  run_buildah images -a
  expect_line_count 3
  run_buildah inspect --format "{{index .Docker.History 2}}" test
  expect_output --substring "FROM docker.io/library/alpine:latest"
}

@test "bud with --layers and single and two line Dockerfiles" {
  _prefetch alpine
  run_buildah inspect --format "{{.FromImageDigest}}" alpine
  fromDigest="$output"

  run_buildah build $WITH_POLICY_JSON --layers -t test -f Dockerfile.5 $BUDFILES/use-layers
  run_buildah images -a
  expect_line_count 3

  # Also check for base-image annotations.
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' test
  expect_output "$fromDigest" "base digest from alpine"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.name" }}' test
  expect_output "docker.io/library/alpine:latest" "base name from alpine"

  run_buildah build $WITH_POLICY_JSON --layers -t test1 -f Dockerfile.6 $BUDFILES/use-layers
  run_buildah images -a
  expect_line_count 4

  # Note that the base-image annotations are empty here since a Container with
  # a single FROM line is effectively just a tag and it does not create a new
  # image.
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' test1
  expect_output "" "base digest from alpine"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.name" }}' test1
  expect_output "" "base name from alpine"
}

@test "bud with --layers, multistage, and COPY with --from" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/use-layers
  cp -a $BUDFILES/use-layers $contextdir

  mkdir -p $contextdir/uuid
  uuidgen > $contextdir/uuid/data
  mkdir -p $contextdir/date
  date > $contextdir/date/data

  run_buildah build $WITH_POLICY_JSON --layers -t test1 -f Dockerfile.multistage-copy $contextdir
  run_buildah images -a
  expect_line_count 6
  # The second time through, the layers should all get reused.
  run_buildah build $WITH_POLICY_JSON --layers -t test1 -f Dockerfile.multistage-copy $contextdir
  run_buildah images -a
  expect_line_count 6
  # The third time through, the layers should all get reused, but we'll have a new line of output for the new name.

  run_buildah build $WITH_POLICY_JSON --layers -t test2 -f Dockerfile.multistage-copy $contextdir
  run_buildah images -a
  expect_line_count 7

  # Both interim images will be different, and all of the layers in the final image will be different.
  uuidgen > $contextdir/uuid/data
  date > $contextdir/date/data
  run_buildah build $WITH_POLICY_JSON --layers -t test3 -f Dockerfile.multistage-copy $contextdir
  run_buildah images -a
  expect_line_count 11
  # No leftover containers, just the header line.
  run_buildah containers
  expect_line_count 1

  run_buildah from --quiet $WITH_POLICY_JSON test3
  ctr=$output
  run_buildah_mount ${ctr}
  mnt=$output
  test -e $mnt/uuid
  test -e $mnt/date

  # Layers won't get reused because this build won't use caching.
  run_buildah build $WITH_POLICY_JSON -t test4 -f Dockerfile.multistage-copy $contextdir
  run_buildah images -a
  expect_line_count 12
}

@test "bud-multistage-partial-cache" {
  _prefetch alpine
  target=foo
  # build the first stage
  run_buildah build $WITH_POLICY_JSON --layers -f $BUDFILES/cache-stages/Dockerfile.1 $BUDFILES/cache-stages
  # expect alpine + 1 image record for the first stage
  run_buildah images -a
  expect_line_count 3
  # build the second stage, itself not cached, when the first stage is found in the cache
  run_buildah build $WITH_POLICY_JSON --layers -f $BUDFILES/cache-stages/Dockerfile.2 -t ${target} $BUDFILES/cache-stages
  # expect alpine + 1 image record for the first stage, then two more image records for the second stage
  run_buildah images -a
  expect_line_count 5
}

@test "bud-multistage-copy-final-slash" {
  # This test requires a statically linked busybox which is not
  # the case for quay.io/libpod/busybox on ppc64le & s390x
  skip_unless_arch amd64 arm64
  _prefetch busybox
  target=foo
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/dest-final-slash
  run_buildah from --pull=false $WITH_POLICY_JSON ${target}
  cid="$output"
  run_buildah run ${cid} /test/ls -lR /test/ls
}

@test "bud-multistage-reused" {
  _prefetch alpine busybox
  run_buildah inspect --format "{{.FromImageDigest}}" busybox
  fromDigest="$output"

  target=foo

  # Check the base-image annotations in a single-layer build where the last stage is just an earlier stage.
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds/Dockerfile.reused $BUDFILES/multi-stage-builds

  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' ${target}
  expect_output "$fromDigest" "base digest from busybox"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.name" }}' ${target}
  expect_output "docker.io/library/busybox:latest" "base name from busybox"

  run_buildah from $WITH_POLICY_JSON ${target}
  run_buildah rmi -f ${target}

  # Check the base-image annotations in a multi-layer build where the last stage is just an earlier stage.
  run_buildah build $WITH_POLICY_JSON -t ${target} --layers -f $BUDFILES/multi-stage-builds/Dockerfile.reused $BUDFILES/multi-stage-builds

  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' ${target}
  expect_output "$fromDigest" "base digest from busybox"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.name" }}' ${target}
  expect_output "docker.io/library/busybox:latest" "base name from busybox"

  run_buildah from $WITH_POLICY_JSON ${target}
  run_buildah rmi -f ${target}

  # Check the base-image annotations in a single-layer build where the last stage is based on an earlier stage.
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds/Dockerfile.reused2 $BUDFILES/multi-stage-builds

  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' ${target}
  expect_output "$fromDigest" "base digest from busybox"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.name" }}' ${target}
  expect_output "docker.io/library/busybox:latest" "base name from busybox"

  run_buildah from $WITH_POLICY_JSON ${target}
  run_buildah rmi -f ${target}

  # Check the base-image annotations in a multi-layer build where the last stage is based on an earlier stage.
  run_buildah build $WITH_POLICY_JSON -t ${target} --layers -f $BUDFILES/multi-stage-builds/Dockerfile.reused2 $BUDFILES/multi-stage-builds

  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' ${target}
  expect_output "$fromDigest" "base digest from busybox"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.name" }}' ${target}
  expect_output "docker.io/library/busybox:latest" "base name from busybox"

  run_buildah from $WITH_POLICY_JSON ${target}
  run_buildah rmi -f ${target}
}

@test "bud-multistage-cache" {
  _prefetch alpine busybox
  target=foo
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds/Dockerfile.extended $BUDFILES/multi-stage-builds
  run_buildah from $WITH_POLICY_JSON ${target}
  cid="$output"
  run_buildah_mount "$cid"
  root="$output"
  # cache should have used this one
  test -r "$root"/tmp/preCommit
  # cache should not have used this one
  ! test -r "$root"/tmp/postCommit
}

@test "bud-multistage-pull-always" {
  _prefetch busybox
  run_buildah build --pull-always $WITH_POLICY_JSON -f $BUDFILES/multi-stage-builds/Dockerfile.extended $BUDFILES/multi-stage-builds
}

@test "bud with --layers and symlink file" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/use-layers
  cp -a $BUDFILES/use-layers $contextdir
  echo 'echo "Hello World!"' > $contextdir/hello.sh
  ln -s hello.sh $contextdir/hello_world.sh
  run_buildah build $WITH_POLICY_JSON --layers -t test -f Dockerfile.4 $contextdir
  run_buildah images -a
  expect_line_count 4

  run_buildah build $WITH_POLICY_JSON --layers -t test1 -f Dockerfile.4 $contextdir
  run_buildah images -a
  expect_line_count 5

  echo 'echo "Hello Cache!"' > $contextdir/hello.sh
  run_buildah build $WITH_POLICY_JSON --layers -t test2 -f Dockerfile.4 $contextdir
  run_buildah images -a
  expect_line_count 7
}

@test "bud with --layers and dangling symlink" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/use-layers
  cp -a $BUDFILES/use-layers $contextdir
  mkdir $contextdir/blah
  ln -s ${TEST_SOURCES}/policy.json $contextdir/blah/policy.json

  run_buildah build $WITH_POLICY_JSON --layers -t test -f Dockerfile.dangling-symlink $contextdir
  run_buildah images -a
  expect_line_count 3

  run_buildah build $WITH_POLICY_JSON --layers -t test1 -f Dockerfile.dangling-symlink $contextdir
  run_buildah images -a
  expect_line_count 4

  run_buildah from --quiet $WITH_POLICY_JSON test
  cid=$output
  run_buildah run $cid ls /tmp
  expect_output "policy.json"
}

@test "bud with --layers and --build-args" {
  _prefetch alpine
  # base plus 3, plus the header line
  run_buildah build $WITH_POLICY_JSON --build-arg=user=0 --layers -t test -f Dockerfile.build-args $BUDFILES/use-layers
  run_buildah images -a
  expect_line_count 5

  # running the same build again does not run the commands again
  run_buildah build $WITH_POLICY_JSON --build-arg=user=0 --layers -t test -f Dockerfile.build-args $BUDFILES/use-layers
  if [[ "$output" =~ "MAo=" ]]; then
    # MAo= is the base64 of "0\n" (i.e. `echo 0`)
    printf "Expected command not to run again if layer is cached\n" >&2
    false
  fi

  # two more, starting at the "echo $user | base64" instruction
  run_buildah build $WITH_POLICY_JSON --build-arg=user=1 --layers -t test1 -f Dockerfile.build-args $BUDFILES/use-layers
  run_buildah images -a
  expect_line_count 7

  # one more, because we added a new name to the same image
  run_buildah build $WITH_POLICY_JSON --build-arg=user=1 --layers -t test2 -f Dockerfile.build-args $BUDFILES/use-layers
  run_buildah images -a
  expect_line_count 8

  # two more, starting at the "echo $user | base64" instruction
  run_buildah build $WITH_POLICY_JSON --layers -t test3 -f Dockerfile.build-args $BUDFILES/use-layers
  run_buildah images -a
  expect_line_count 11
}


@test "bud with --layers and --build-args: override ARG with ENV and image must be cached" {
  _prefetch alpine
  #when ARG is overridden by config
  run_buildah build $WITH_POLICY_JSON --build-arg=FOO=1 --layers -t args-cache -f $BUDFILES/with-arg/Dockerfile
  run_buildah inspect -f '{{.FromImageID}}' args-cache
  idbefore="$output"
  run_buildah build $WITH_POLICY_JSON --build-arg=FOO=12 --layers -t args-cache -f $BUDFILES/with-arg/Dockerfile
  run_buildah inspect -f '{{.FromImageID}}' args-cache
  expect_output --substring ${idbefore}
}

@test "bud with --layers and --build-args: use raw ARG and cache should not be used" {
  _prefetch alpine
  # when ARG is used as a raw value
  run_buildah build $WITH_POLICY_JSON --build-arg=FOO=1 --layers -t args-cache -f $BUDFILES/with-arg/Dockerfile2
  run_buildah inspect -f '{{.FromImageID}}' args-cache
  idbefore="$output"
  run_buildah build $WITH_POLICY_JSON --build-arg=FOO=12 --layers -t args-cache -f $BUDFILES/with-arg/Dockerfile2
  run_buildah inspect -f '{{.FromImageID}}' args-cache
  idafter="$output"

  assert "$idbefore" != "$idafter" \
         ".Args changed so final image id should be different"
}

@test "bud with --rm flag" {
  _prefetch alpine

  local contentdir=${TEST_SCRATCH_DIR}/content
  mkdir -p $contentdir
  echo all the cool kids say this counts as a README file > ${contentdir}/README.md
  starthttpd ${contentdir}

  run_buildah build --build-arg=HTTP_SERVER_PORT=${HTTP_SERVER_PORT} $WITH_POLICY_JSON --layers -t test1 $BUDFILES/use-layers
  run_buildah containers
  expect_line_count 1

  run_buildah build --build-arg=HTTP_SERVER_PORT=${HTTP_SERVER_PORT} $WITH_POLICY_JSON --rm=false --layers -t test2 $BUDFILES/use-layers
  run_buildah containers
  expect_line_count 8
}

@test "bud with --force-rm flag" {
  _prefetch alpine
  run_buildah 125 build $WITH_POLICY_JSON --force-rm --layers -t test1 -f Dockerfile.fail-case $BUDFILES/use-layers
  run_buildah containers
  expect_line_count 1

  run_buildah 125 build $WITH_POLICY_JSON --layers -t test2 -f Dockerfile.fail-case $BUDFILES/use-layers
  run_buildah containers
  expect_line_count 2
}

@test "bud --layers with non-existent/down registry" {
  _prefetch alpine
  run_buildah 125 build $WITH_POLICY_JSON --force-rm --layers -t test1 -f Dockerfile.non-existent-registry $BUDFILES/use-layers
  expect_output --substring "no such host"
}

@test "bud from base image should have base image ENV also" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON -t test -f Dockerfile.check-env $BUDFILES/env
  run_buildah from --quiet $WITH_POLICY_JSON test
  cid=$output
  run_buildah config --env random=hello,goodbye ${cid}
  run_buildah commit $WITH_POLICY_JSON ${cid} test1
  run_buildah inspect --format '{{index .Docker.ContainerConfig.Env 1}}' test1
  expect_output "foo=bar"
  run_buildah inspect --format '{{index .Docker.ContainerConfig.Env 2}}' test1
  expect_output "random=hello,goodbye"
}

@test "bud-from-scratch" {
  target=scratch-image
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  run_buildah from ${target}
  expect_output "${target}-working-container"
}

@test "bud-with-unlimited-memory-swap" {
  target=scratch-image
  run_buildah build $WITH_POLICY_JSON --memory-swap -1 -t ${target} $BUDFILES/from-scratch
}

@test "build with --no-cache and --layer" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN echo hello
RUN echo world
_EOF

  # This should do a fresh build and just populate build cache
  run_buildah build --layers $WITH_POLICY_JSON -t test -f $mytmpdir/Containerfile .
  # This should also do a fresh build and just populate build cache
  run_buildah build --no-cache --layers $WITH_POLICY_JSON -t test -f $mytmpdir/Containerfile .
  # This should use everything from build cache
  run_buildah build --layers $WITH_POLICY_JSON -t test -f $mytmpdir/Containerfile .
  expect_output --substring "Using cache"

}

@test "build --unsetenv PATH" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
ENV date="today"
ENV foo="bar"
ENV container="buildah"
_EOF
  target=unsetenv-image
  run_buildah build --unsetenv PATH $WITH_POLICY_JSON -t oci-${target} -f $mytmpdir/Containerfile .
  run_buildah inspect --type=image --format '{{.OCIv1.Config.Env}}' oci-${target}
  expect_output "[date=today foo=bar container=buildah]" "No Path should be defined"
  run_buildah inspect --type=image --format '{{.Docker.Config.Env}}' oci-${target}
  expect_output "[date=today foo=bar container=buildah]" "No Path should be defined"
  cat > $mytmpdir/Containerfile << _EOF
FROM oci-${target}
ENV date="tomorrow"
_EOF
  run_buildah build --format docker --unsetenv PATH --unsetenv foo $WITH_POLICY_JSON -t docker-${target} -f $mytmpdir/Containerfile .
  run_buildah inspect --type=image --format '{{.OCIv1.Config.Env}}' docker-${target}
  expect_output "[container=buildah date=tomorrow]" "No Path should be defined"
  run_buildah inspect --type=image --format '{{.Docker.Config.Env}}' docker-${target}
  expect_output "[container=buildah date=tomorrow]" "No Path should be defined"
  cat > $mytmpdir/Containerfile << _EOF
FROM oci-${target}
_EOF
  run_buildah build --format docker --unsetenv PATH --unsetenv foo $WITH_POLICY_JSON -t docker-${target} -f $mytmpdir/Containerfile .
  run_buildah inspect --type=image --format '{{.OCIv1.Config.Env}}' docker-${target}
  expect_output "[date=today container=buildah]" "No Path should be defined"
  run_buildah inspect --type=image --format '{{.Docker.Config.Env}}' docker-${target}
  expect_output "[date=today container=buildah]" "No Path should be defined"
}

@test "bud with --env" {
  target=scratch-image
  run_buildah build --quiet=false --iidfile ${TEST_SCRATCH_DIR}/output.iid --env PATH $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  iid=$(< ${TEST_SCRATCH_DIR}/output.iid)
  run_buildah inspect --format '{{.Docker.Config.Env}}' $iid
  expect_output "[PATH=$PATH]"

  run_buildah build --quiet=false --iidfile ${TEST_SCRATCH_DIR}/output.iid --env PATH=foo $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  iid=$(< ${TEST_SCRATCH_DIR}/output.iid)
  run_buildah inspect --format '{{.Docker.Config.Env}}' $iid
  expect_output "[PATH=foo]"

  # --unsetenv takes precedence over --env, since we don't know the relative order of the two
  run_buildah build --quiet=false --iidfile ${TEST_SCRATCH_DIR}/output.iid --unsetenv PATH --env PATH=foo --env PATH= $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  iid=$(< ${TEST_SCRATCH_DIR}/output.iid)
  run_buildah inspect --format '{{.Docker.Config.Env}}' $iid
  expect_output "[]"

  # Reference foo=baz from process environment
  foo=baz run_buildah build --quiet=false --iidfile ${TEST_SCRATCH_DIR}/output.iid --env foo $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  iid=$(< ${TEST_SCRATCH_DIR}/output.iid)
  run_buildah inspect --format '{{.Docker.Config.Env}}' $iid
  expect_output --substring "foo=baz"
}

@test "build with custom build output and output rootfs to directory" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN echo 'hello'> hello
_EOF
  run_buildah build --output type=local,dest=$mytmpdir/rootfs $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  ls $mytmpdir/rootfs
  # exported rootfs must contain `hello` file which we created inside the image
  expect_output --substring 'hello'
}

@test "build with custom build output for multi-stage and output rootfs to directory" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine as builder
RUN touch rogue

FROM builder as intermediate
RUN touch artifact

FROM scratch as outputs
COPY --from=intermediate artifact target
_EOF
  run_buildah build --output type=local,dest=$mytmpdir/rootfs $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  ls $mytmpdir/rootfs
  # exported rootfs must contain only 'target' from last/final stage and not contain file `rogue` from first stage
  expect_output --substring 'target'
  # must not contain rogue from first stage
  assert "$output" =~ "rogue"
  # must not contain artifact from second stage
  assert "$output" =~ "artifact"
}

@test "build with custom build output for multi-stage-cached and output rootfs to directory" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine as builder
RUN touch rogue

FROM builder as intermediate
RUN touch artifact

FROM scratch as outputs
COPY --from=intermediate artifact target
_EOF
  # Populate layers but don't generate --output
  run_buildah build --layers $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  # Reuse cached layers and check if --output still works as expected
  run_buildah build --output type=local,dest=$mytmpdir/rootfs $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  ls $mytmpdir/rootfs
  # exported rootfs must contain only 'target' from last/final stage and not contain file `rogue` from first stage
  expect_output --substring 'target'
  # must not contain rogue from first stage
  assert "$output" =~ "rogue"
  # must not contain artifact from second stage
  assert "$output" =~ "artifact"
}

@test "build with custom build output for single-stage-cached and output rootfs to directory" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine as builder
RUN touch rogue
_EOF
  # Populate layers but don't generate --output
  run_buildah build --layers $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  # Reuse cached layers and check if --output still works as expected
  run_buildah build --output type=local,dest=$mytmpdir/rootfs $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  ls $mytmpdir/rootfs
  # exported rootfs must contain only 'rogue' even if build from cache.
  expect_output --substring 'rogue'
}

@test "build with custom build output and output rootfs to tar" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN echo 'hello'> hello
_EOF
  run_buildah build --output type=tar,dest=$mytmpdir/rootfs.tar $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  # verify tar content
  run tar -tf $mytmpdir/rootfs.tar
  expect_output --substring 'hello'
}

@test "build with custom build output and output rootfs to tar by pipe" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN echo 'hello'> hello
_EOF
  # Using buildah() defined in helpers.bash since run_buildah adds unwanted chars to tar created by pipe.
  buildah build $WITH_POLICY_JSON -o - -t test-bud -f $mytmpdir/Containerfile . > $mytmpdir/rootfs.tar
  # verify tar content
  run tar -tf $mytmpdir/rootfs.tar
  expect_output --substring 'hello'

  # test with long syntax as well
  buildah build $WITH_POLICY_JSON --output type=tar,dest=- -t test-bud -f $mytmpdir/Containerfile . > $mytmpdir/rootfs2.tar
  run tar -tf $mytmpdir/rootfs2.tar
  expect_output --substring 'hello'
}

@test "build with custom build output and output rootfs to tar with no additional step" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  # We only want content of alpine nothing else
  # so just `FROM alpine` should work.
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
_EOF
  run_buildah build --output type=tar,dest=$mytmpdir/rootfs.tar $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  # verify tar content
  run tar -tf $mytmpdir/rootfs.tar
  # exported rootfs must contain `var`,`bin` directory which exists in alpine
  # so output of `ls $mytmpdir/rootfs` must contain following strings
  expect_output --substring 'var'
  expect_output --substring 'bin'
}

@test "build with custom build output must fail for bad input" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN echo 'hello'> hello
_EOF
  run_buildah 125 build --output type=tar $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  expect_output --substring 'missing required key "dest"'
  run_buildah 125 build --output type=tar, $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  expect_output --substring 'invalid'
  run_buildah 125 build --output type=wrong,dest=hello $WITH_POLICY_JSON -t test-bud -f $mytmpdir/Containerfile .
  expect_output --substring 'invalid'
}

@test "bud-from-scratch-untagged" {
  run_buildah build --iidfile ${TEST_SCRATCH_DIR}/output.iid $WITH_POLICY_JSON $BUDFILES/from-scratch
  iid=$(< ${TEST_SCRATCH_DIR}/output.iid)
  expect_output --substring --from="$iid" '^sha256:[0-9a-f]{64}$'
  run_buildah from ${iid}
  buildctr="$output"
  run_buildah commit $buildctr new-image

  run_buildah inspect --format "{{.FromImageDigest}}" $iid
  fromDigest="$output"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' new-image
  expect_output "$fromDigest" "digest for untagged base image"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.name" }}' new-image
  expect_output "" "no base name for untagged base image"
}

@test "bud with --tag" {
  target=scratch-image
  run_buildah build --quiet=false --tag test1 $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  expect_output --substring "Successfully tagged localhost/test1:latest"

  run_buildah build --quiet=false --tag test1 --tag test2 $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  expect_output --substring "Successfully tagged localhost/test1:latest"
  expect_output --substring "Successfully tagged localhost/test2:latest"
}

@test "bud with bad --tag" {
  target=scratch-image
  run_buildah 125 build --quiet=false --tag TEST1 $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  expect_output --substring "tag TEST1: invalid reference format: repository name must be lowercase"

  run_buildah 125 build --quiet=false --tag test1 --tag TEST2 $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  expect_output --substring "tag TEST2: invalid reference format: repository name must be lowercase"
}

@test "bud-from-scratch-iid" {
  target=scratch-image
  run_buildah build --iidfile ${TEST_SCRATCH_DIR}/output.iid $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  iid=$(< ${TEST_SCRATCH_DIR}/output.iid)
  expect_output --substring --from="$iid" '^sha256:[0-9a-f]{64}$'
  run_buildah from ${iid}
  expect_output "${target}-working-container"
}

@test "bud-from-scratch-label" {
  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}
  want_output='map["io.buildah.version":"'$buildah_version'" "test":"label"]'

  target=scratch-image
  run_buildah build --label "test=label" $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' ${target}
  expect_output "$want_output"

  want_output='map["io.buildah.version":"'$buildah_version'" "test":""]'
  run_buildah build --label test $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' ${target}
  expect_output "$want_output"
}

@test "bud-with-identity-label" {
  run_buildah build $WITH_POLICY_JSON -t scratch-image-1 $BUDFILES/from-scratch
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-1
  assert "$output" != "map[]"
  run_buildah build --identity-label=true $WITH_POLICY_JSON -t scratch-image-2 $BUDFILES/from-scratch
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-2
  assert "$output" != "map[]"
  local withlabels="$output"
  run_buildah build --identity-label=false $WITH_POLICY_JSON --from scratch-image-2 -t scratch-image-3 $BUDFILES/from-scratch
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-3
  assert "$output" != "map[]"
  assert "$output" = "$withlabels" "expected identity label to not be removed when building with --identity-label=false"
}

@test "bud-without-identity-label" {
  target=scratch-image
  run_buildah build --identity-label=false $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' ${target}
  assert "$output" = "map[]"
}

@test "bud-suppressed-identity-label" {
  run_buildah build --source-date-epoch=60 $WITH_POLICY_JSON -t scratch-image-1 $BUDFILES/from-scratch
  run_buildah images scratch-image-1
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-1
  assert "$output" = "map[]"

  export SOURCE_DATE_EPOCH=90
  run_buildah build $WITH_POLICY_JSON -t scratch-image-2 $BUDFILES/from-scratch
  unset SOURCE_DATE_EPOCH
  run_buildah images scratch-image-2
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-2
  assert "$output" = "map[]"

  run_buildah build --timestamp=60 $WITH_POLICY_JSON -t scratch-image-3 $BUDFILES/from-scratch
  run_buildah images scratch-image-3
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-3
  assert "$output" = "map[]"
}

@test "bud-from-scratch-annotation" {
  target=scratch-image
  run_buildah build --annotation "test=annotation1,annotation2=z" $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  run_buildah inspect --format '{{index .ImageAnnotations "test"}}' ${target}
  expect_output "annotation1,annotation2=z"
}

@test "bud-from-scratch-layers" {
  target=scratch-image
  run_buildah build $WITH_POLICY_JSON -f  $BUDFILES/from-scratch/Containerfile2 -t ${target} $BUDFILES/from-scratch
  run_buildah build $WITH_POLICY_JSON -f  $BUDFILES/from-scratch/Containerfile2 -t ${target} $BUDFILES/from-scratch
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah images
  expect_line_count 3
  run_buildah rm ${cid}
  expect_line_count 1
}

@test "bud-from-multiple-files-one-from" {
  target=scratch-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/from-multiple-files/Dockerfile1.scratch -f $BUDFILES/from-multiple-files/Dockerfile2.nofrom $BUDFILES/from-multiple-files
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  cmp $root/Dockerfile1 $BUDFILES/from-multiple-files/Dockerfile1.scratch
  cmp $root/Dockerfile2.nofrom $BUDFILES/from-multiple-files/Dockerfile2.nofrom
  test ! -s $root/etc/passwd
  run_buildah_umount ${cid}
  run_buildah rm ${cid}
  run_buildah rmi -a

  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile1.alpine -f Dockerfile2.nofrom $BUDFILES/from-multiple-files
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  cmp $root/Dockerfile1 $BUDFILES/from-multiple-files/Dockerfile1.alpine
  cmp $root/Dockerfile2.nofrom $BUDFILES/from-multiple-files/Dockerfile2.nofrom
  test -s $root/etc/passwd
}

@test "bud-from-multiple-files-two-froms" {
  _prefetch alpine
  target=scratch-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile1.scratch -f Dockerfile2.withfrom $BUDFILES/from-multiple-files
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  test ! -s $root/Dockerfile1
  cmp $root/Dockerfile2.withfrom $BUDFILES/from-multiple-files/Dockerfile2.withfrom
  test -s $root/etc/passwd
  run_buildah_umount ${cid}
  run_buildah rm ${cid}
  run_buildah rmi -a

  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile1.alpine -f Dockerfile2.withfrom $BUDFILES/from-multiple-files
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  test ! -s $root/Dockerfile1
  cmp $root/Dockerfile2.withfrom $BUDFILES/from-multiple-files/Dockerfile2.withfrom
  test -s $root/etc/passwd
}

@test "build using --layer-label and test labels on intermediate images" {
  _prefetch alpine
  label="l_$(random_string)"
  labelvalue="v_$(random_string)"

  run_buildah build --no-cache --layers --layer-label $label=$labelvalue --layer-label emptylabel $WITH_POLICY_JSON -t exp -f $BUDFILES/simple-multi-step/Containerfile

  # Final image must not contain the layer-label
  run_buildah inspect --format '{{ index .Docker.Config.Labels "'$label'"}}' exp
  expect_output "" "label on actual image"

  # Find all intermediate images...
  run_buildah images -a --format '{{.ID}}' --filter intermediate=true
  # ...and confirm that they have both $label and emptylabel
  for image in "${lines[@]}";do
    run_buildah inspect $image
    inspect="$output"

    run jq -r ".Docker.config.Labels.$label" <<<"$inspect"
    assert "$output" = "$labelvalue" "label in intermediate layer $image"

    run jq -r ".Docker.config.Labels.emptylabel" <<<"$inspect"
    assert "$output" = "" "emptylabel in intermediate layer $image"
  done
}

@test "bud and test --unsetlabel" {
  base=quay.io/libpod/testimage:20241011
  _prefetch $base
  target=exp

  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}

  buildah inspect --format '{{ .Docker.Config.Labels }}' $base
  not_want_output='map[]'
  assert "$output" != "$not_want_output" "expected some labels to be set in base image $base"

  labels=$(buildah inspect --format '{{ range $key, $value := .Docker.Config.Labels }}{{ $key }} {{end}}' $base)
  labelflags="--label hello=world"
  for label in $labels; do
    if test $label != io.buildah.version ; then
      labelflags="$labelflags --unsetlabel $label"
    fi
  done

  run_buildah build $WITH_POLICY_JSON $labelflags -t $target --from $base $BUDFILES/base-with-labels

  # no labels should be inherited from base image, only the buildah version label
  # and `hello=world` which we just added using cli flag
  want_output='map["hello":"world" "io.buildah.version":"'$buildah_version'"]'
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' $target
  expect_output "$want_output"
}

@test "bud and test --unsetannotation" {
  base=quay.io/libpod/testimage:20241011
  _prefetch $base
  target=exp

  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}

  buildah inspect --format '{{ .ImageAnnotations }}' $base
  not_want_output='map[]'
  assert "$output" != "$not_want_output" "expected some annotations to be set in base image $base"

  annotations=$(buildah inspect --format '{{ range $key, $value := .ImageAnnotations }}{{ $key }} {{end}}' $base)
  annotationflags="--annotation hello=world --unsetannotation=org.opencontainers.image.created"
  for annotation in $annotations; do
    if test $annotation != io.buildah.version ; then
      annotationflags="$annotationflags --unsetannotation $annotation"
    fi
  done

  run_buildah build $WITH_POLICY_JSON $annotationflags -t $target --from $base $BUDFILES/base-with-labels

  # no annotations should be inherited from base image
  # and `hello=world` which we just added using cli flag
  want_output='map["hello":"world"]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"
}

@test "bud and test --unsetannotation with --layers" {
  base=quay.io/libpod/testimage:20241011
  _prefetch $base
  target=exp

  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}

  buildah inspect --format '{{ .ImageAnnotations }}' $base
  not_want_output='map[]'
  assert "$output" != "$not_want_output" "expected some annotations to be set in base image $base"

  ## Build without removing annotations
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid1 -t $target --from $base $BUDFILES/base-with-labels
  ## Second build must use cache
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid2 -t $target --from $base $BUDFILES/base-with-labels
  ## Must use cache
  expect_output --substring " Using cache"
  cmp ${TEST_SCRATCH_DIR}/iid1 ${TEST_SCRATCH_DIR}/iid2

  annotations=$(buildah inspect --format '{{ range $key, $value := .ImageAnnotations }}{{ $key }} {{end}}' $base)
  annotationflags="--annotation hello=world --unsetannotation=org.opencontainers.image.created"
  for annotation in $annotations; do
    if test $annotation != io.buildah.version ; then
      annotationflags="$annotationflags --unsetannotation $annotation"
    fi
  done

  ## Since we are unsetting something, this should not use previous image present in the cache.
  run_buildah build $WITH_POLICY_JSON --layers $annotationflags -t $target --from $base $BUDFILES/base-with-labels
  ## should not contain `Using Cache`
  #assert "$output" !~ "Using cache"

  # no annotations should be inherited from base image
  # and `hello=world` which we just added using cli flag
  want_output='map["hello":"world"]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"
}

@test "bud and test --unsetannotation with only base image" {
  base=quay.io/libpod/testimage:20241011
  _prefetch $base
  target=exp

  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}

  buildah inspect --format '{{ .ImageAnnotations }}' $base
  not_want_output='map[]'
  assert "$output" != "$not_want_output" "expected some annotations to be set in base image $base"

  annotations=$(buildah inspect --format '{{ range $key, $value := .ImageAnnotations }}{{ $key }} {{end}}' $base)
  annotationflags="--annotation hello=world --created-annotation=false"
  for annotation in $annotations; do
    if test $annotation != io.buildah.version ; then
      annotationflags="$annotationflags --unsetannotation $annotation"
    fi
  done

  run_buildah build $WITH_POLICY_JSON $annotationflags -t $target --from $base $BUDFILES/only-base
  want_output='map["hello":"world"]'
  run_buildah inspect --format '{{printf "%q" .ImageAnnotations}}' $target
  expect_output "$want_output"
}

@test "bud and test inherit-labels" {
  base=quay.io/libpod/testimage:20241011
  _prefetch $base
  _prefetch alpine
  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}
  run_buildah build $WITH_POLICY_JSON -t exp -f $BUDFILES/base-with-labels/Containerfile

  run_buildah inspect --format '{{ index .Docker.Config.Labels "created_at"}}' exp
  expect_output "2024-10-11T12:26:00Z" "created_at must be inherited from base image"
  run_buildah inspect --format '{{ index .Docker.Config.Labels "created_by"}}' exp
  expect_output "test/system/build-testimage" "created_by must be inherited from base image"

  run_buildah build $WITH_POLICY_JSON --inherit-labels=false --label created_by=world -t exp -f $BUDFILES/base-with-labels/Containerfile
  # no labels should be inherited from base image, only the buildah version label
  # and `created_by=world` which we just added using cli flag
  want_output='map["created_by":"world" "io.buildah.version":"'$buildah_version'"]'
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' exp
  expect_output "$want_output"

  # Try building another file with multiple layers
  run_buildah build $WITH_POLICY_JSON --iidfile ${TEST_SCRATCH_DIR}/id1 --layers -t exp -f $BUDFILES/base-with-labels/Containerfile.layer
  run_buildah inspect --format '{{ index .Docker.Config.Labels "created_at"}}' exp
  expect_output "2024-10-11T12:26:00Z" "created_at must be inherited from base image"
  run_buildah inspect --format '{{ index .Docker.Config.Labels "created_by"}}' exp
  expect_output "world" "created_by must be world from Containerfile"

  # Now build same file with  --inherit-labels=false and verify if we are not using the cache again.
  run_buildah build $WITH_POLICY_JSON --layers --inherit-labels=false --iidfile ${TEST_SCRATCH_DIR}/inherit_false_1 -t exp -f $BUDFILES/base-with-labels/Containerfile.layer
  # Should not contain `Using cache` at all since
  assert "$output" !~ "Using cache"
  want_output='map["created_by":"world" "io.buildah.version":"'$buildah_version'"]'
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' exp
  expect_output "$want_output"

  run_buildah build $WITH_POLICY_JSON --layers --inherit-labels=false --iidfile ${TEST_SCRATCH_DIR}/inherit_false_2 -t exp -f $BUDFILES/base-with-labels/Containerfile.layer
  # Should contain `Using cache`
  expect_output --substring " Using cache"
  want_output='map["created_by":"world" "io.buildah.version":"'$buildah_version'"]'
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' exp
  expect_output "$want_output"
  assert "$(< ${TEST_SCRATCH_DIR}/inherit_false_1)" = "$(< ${TEST_SCRATCH_DIR}/inherit_false_2)" "expected image ids to not change"

  # Now build same file with  --inherit-labels=true and verify if using the cache
  run_buildah build $WITH_POLICY_JSON --iidfile ${TEST_SCRATCH_DIR}/id2 --layers --inherit-labels=true -t exp -f $BUDFILES/base-with-labels/Containerfile.layer
  expect_output --substring " Using cache"
  run_buildah inspect --format '{{ index .Docker.Config.Labels "created_at"}}' exp
  expect_output "2024-10-11T12:26:00Z" "created_at must be inherited from base image"
  run_buildah inspect --format '{{ index .Docker.Config.Labels "created_by"}}' exp
  expect_output "world" "created_by must be world from Containerfile"
  # Final image id should be exactly same as the one image which was built in the past.
  assert "$(< ${TEST_SCRATCH_DIR}/id1)" = "$(< ${TEST_SCRATCH_DIR}/id2)" "expected image ids to not change"

  # Now build same file with  --inherit-labels=false and verify if target stage did not inherit any labels from base stage.
  run_buildah build $WITH_POLICY_JSON --layers --inherit-labels=false -t exp -f $BUDFILES/base-with-labels/Containerfile.multi-stage
  want_output='map["io.buildah.version":"'$buildah_version'"]'
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' exp
  expect_output "$want_output"

  # Now build same file with  --inherit-labels=true and verify if target stage inherits labels from the base stage.
  run_buildah build $WITH_POLICY_JSON --iidfile ${TEST_SCRATCH_DIR}/id3 --layers --inherit-labels=true -t exp -f $BUDFILES/base-with-labels/Containerfile.multi-stage
  want_output='map["created_by":"world" "io.buildah.version":"'$buildah_version'"]'
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' exp
  expect_output "$want_output"

  # Rebuild again with layers should not build image again at all.
  run_buildah build $WITH_POLICY_JSON --iidfile ${TEST_SCRATCH_DIR}/id4 --layers --inherit-labels=true -t exp -f $BUDFILES/base-with-labels/Containerfile.multi-stage
  want_output='map["created_by":"world" "io.buildah.version":"'$buildah_version'"]'
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' exp
  expect_output "$want_output"
  assert "$(< ${TEST_SCRATCH_DIR}/id3)" = "$(< ${TEST_SCRATCH_DIR}/id4)" "expected image ids to not change"
}

@test "build using intermediate images should not inherit label" {
  _prefetch alpine

  # Build imageone, with a label
  run_buildah build --no-cache --layers --label somefancylabel=true $WITH_POLICY_JSON -t imageone -f Dockerfile.name $BUDFILES/multi-stage-builds
  run_buildah inspect --format '{{ index .Docker.Config.Labels "somefancylabel"}}' imageone
  expect_output "true" "imageone: somefancylabel"

  # Build imagetwo. Must use all steps from cache but should not contain label
  run_buildah build --layers $WITH_POLICY_JSON -t imagetwo -f Dockerfile.name $BUDFILES/multi-stage-builds
  for i in 2 6;do
      expect_output --substring --from="${lines[$i]}" "Using cache" \
                    "build imagetwo (no label), line $i"
  done
  run_buildah inspect --format '{{ index .Docker.Config.Labels "somefancylabel"}}' imagetwo
  expect_output "" "imagetwo: somefancylabel"

  # build another multi-stage image with different label, it should use stages from cache from previous build
  run_buildah build --layers $WITH_POLICY_JSON --label anotherfancylabel=true -t imagethree -f Dockerfile.name $BUDFILES/multi-stage-builds
  for i in 2 6;do
      expect_output --substring --from="${lines[$i]}" "Using cache" \
                    "build imagethree ('anotherfancylabel'), line $i"
  done

  run_buildah inspect --format '{{ index .Docker.Config.Labels "somefancylabel"}}' imagethree
  expect_output "" "imagethree: somefancylabel"

  run_buildah inspect --format '{{ index .Docker.Config.Labels "anotherfancylabel"}}' imagethree
  expect_output "true" "imagethree: anotherfancylabel"
}

@test "bud-multi-stage-builds" {
  _prefetch alpine
  target=multi-stage-index
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds/Dockerfile.index $BUDFILES/multi-stage-builds
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  cmp $root/Dockerfile.index $BUDFILES/multi-stage-builds/Dockerfile.index
  test -s $root/etc/passwd
  run_buildah_umount ${cid}
  run_buildah rm ${cid}
  run_buildah rmi -a

  _prefetch alpine
  target=multi-stage-name
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.name $BUDFILES/multi-stage-builds
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  cmp $root/Dockerfile.name $BUDFILES/multi-stage-builds/Dockerfile.name
  test ! -s $root/etc/passwd
  run_buildah_umount ${cid}
  run_buildah rm ${cid}
  run_buildah rmi -a

  target=multi-stage-mixed
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds/Dockerfile.mixed $BUDFILES/multi-stage-builds
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  cmp $root/Dockerfile.name $BUDFILES/multi-stage-builds/Dockerfile.name
  cmp $root/Dockerfile.index $BUDFILES/multi-stage-builds/Dockerfile.index
  cmp $root/Dockerfile.mixed $BUDFILES/multi-stage-builds/Dockerfile.mixed
}

@test "bud-multi-stage-builds-small-as" {
  _prefetch alpine
  target=multi-stage-index
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds-small-as/Dockerfile.index $BUDFILES/multi-stage-builds-small-as
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  cmp $root/Dockerfile.index $BUDFILES/multi-stage-builds-small-as/Dockerfile.index
  test -s $root/etc/passwd
  run_buildah_umount ${cid}
  run_buildah rm ${cid}
  run_buildah rmi -a

  _prefetch alpine
  target=multi-stage-name
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.name $BUDFILES/multi-stage-builds-small-as
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  cmp $root/Dockerfile.name $BUDFILES/multi-stage-builds-small-as/Dockerfile.name
  test ! -s $root/etc/passwd
  run_buildah_umount ${cid}
  run_buildah rm ${cid}
  run_buildah rmi -a

  _prefetch alpine
  target=multi-stage-mixed
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds-small-as/Dockerfile.mixed $BUDFILES/multi-stage-builds-small-as
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  cmp $root/Dockerfile.name $BUDFILES/multi-stage-builds-small-as/Dockerfile.name
  cmp $root/Dockerfile.index $BUDFILES/multi-stage-builds-small-as/Dockerfile.index
  cmp $root/Dockerfile.mixed $BUDFILES/multi-stage-builds-small-as/Dockerfile.mixed
}

@test "bud-preserve-subvolumes" {
  # This Dockerfile needs us to be able to handle a working RUN instruction.
  skip_if_no_runtime

  _prefetch alpine
  for layers in "" --layers ; do
    for compat in "" --compat-volumes ; do
      target=volume-image$compat$layers
      run_buildah build $WITH_POLICY_JSON -t ${target} ${layers} ${compat} $BUDFILES/preserve-volumes
      run_buildah from --quiet ${target}
      cid=$output
      run_buildah_mount ${cid}
      root=$output
      # these files were created before VOLUME instructions froze the directories that contained them
      test -s $root/vol/subvol/subsubvol/subsubvolfile
      test -s $root/vol/volfile
      if test "$compat" != "" ; then
        # true, these files should have been discarded after they were created by RUN instructions
        test ! -s $root/vol/subvol/subvolfile
        test ! -s $root/vol/anothervolfile
      else
        # false, these files should not have been discarded, despite being created by RUN instructions
        test -s $root/vol/subvol/subvolfile
        test -s $root/vol/anothervolfile
      fi
      # and these were ADDed
      test -s $root/vol/Dockerfile
      test -s $root/vol/Dockerfile2
      run_buildah_umount ${cid}
      run_buildah rm ${cid}
      run_buildah rmi ${target}
    done
  done
}

# assert_no_tempdir_leaks checks $1 has no leftover temp download directories.
function assert_no_tempdir_leaks() {
  local tmpdir=$1
  local leftovers=$(find $tmpdir -mindepth 1 -maxdepth 1 -print)
  assert "$leftovers" == ""
}

# Helper function for several of the tests which pull from http.
#
#  Usage:  _test_http  SUBDIRECTORY  URL_PATH  [EXTRA ARGS]
#
#     SUBDIRECTORY   is a subdirectory path under the 'bud' subdirectory.
#                    This will be the argument to starthttpd(), i.e. where
#                    the httpd will serve files.
#
#     URL_PATH       is the path requested by buildah from the http server,
#                    probably 'Dockerfile' or 'context.tar'
#
#     [EXTRA ARGS]   if present, will be passed to buildah on the 'build'
#                    command line; it is intended for '-f subdir/Dockerfile'.
#
function _test_http() {
  local testdir=$1; shift;        # in: subdirectory under bud/
  local urlpath=$1; shift;        # in: path to request from localhost

  starthttpd "$BUDFILES/$testdir"
  target=scratch-image
  local tmpdir=${TEST_SCRATCH_DIR}/tmpdir
  mkdir -p $tmpdir
  TMPDIR=$tmpdir run_buildah build $WITH_POLICY_JSON \
	      -t ${target} \
	      "$@"         \
	      http://0.0.0.0:${HTTP_SERVER_PORT}/$urlpath
  stophttpd
  assert_no_tempdir_leaks $tmpdir
  run_buildah from ${target}
}

# Helper function for several of the tests which verifies compression.
#
#  Usage:  validate_instance_compression INDEX MANIFEST ARCH COMPRESSION
#
#     INDEX             instance which needs to be verified in
#                       provided manifest list.
#
#     MANIFEST          OCI manifest specification in json format
#
#     ARCH              instance architecture
#
#     COMPRESSION       compression algorithm name; e.g "zstd".
#
function validate_instance_compression {
  case $4 in

   gzip)
    run jq -r '.manifests['$1'].annotations' <<< $2
    # annotation is `null` for gzip compression
    assert "$output" = "null" ".manifests[$1].annotations (null means gzip)"
    ;;

  zstd)
    # annotation `'"io.github.containers.compression.zstd": "true"'` must be there for zstd compression
    run jq -r '.manifests['$1'].annotations."io.github.containers.compression.zstd"' <<< $2
    assert "$output" = "true" ".manifests[$1].annotations.'io.github.containers.compression.zstd' (io.github.containers.compression.zstd must be set)"
    ;;
  esac

  run jq -r '.manifests['$1'].platform.architecture' <<< $2
  assert "$output" = $3 ".manifests[$1].platform.architecture"
}

@test "bud-http-Dockerfile" {
  _test_http from-scratch Containerfile
}

@test "bud-http-context-with-Dockerfile" {
  _test_http http-context context.tar
}

@test "bud-http-context-dir-with-Dockerfile" {
  _test_http http-context-subdir context.tar -f context/Dockerfile
}

@test "bud-git-context" {
  # We need git to be around to handle cloning a repository.
  if ! which git ; then
    skip "no git in PATH"
  fi
  target=giturl-image
  # Any repo would do, but this one is small, is FROM: scratch, and local.
  if ! start_git_daemon ; then
    skip "error running git daemon"
  fi
  gitrepo=git://localhost:${GITPORT}/repo
  run_buildah build $WITH_POLICY_JSON -t ${target} "${gitrepo}"
  run_buildah from ${target}
}

@test "bud-git-context-subdirectory" {
  # We need git to be around to handle cloning a repository.
  if ! which git ; then
    skip "no git in PATH"
  fi
  target=giturl-image
  # Any repo would do, but this one is small, is FROM: scratch, local, and has
  # its entire build context in a subdirectory of the repository.
  if ! start_git_daemon ${TEST_SOURCES}/git-daemon/subdirectory.tar.gz ; then
    skip "error running git daemon"
  fi
  gitrepo=git://localhost:${GITPORT}/repo#main:nested/subdirectory
  tmpdir="${TEST_SCRATCH_DIR}/build"
  mkdir -p "${tmpdir}"
  TMPDIR="${tmpdir}" run_buildah build $WITH_POLICY_JSON -t ${target} "${gitrepo}"
  run_buildah from "${target}"
  assert_no_tempdir_leaks $tmpdir
}

@test "bud-git-context-failure" {
  # We need git to be around to try cloning a repository, even though it'll fail
  # and buildah will exit with return code 125.
  if ! which git ; then
    skip "no git in PATH"
  fi
  target=giturl-image
  gitrepo=git:///tmp/no-such-repository
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} "${gitrepo}"
  # Expect part of what git would have told us... before things went horribly wrong
  expect_output --substring "failed while performing"
  expect_output --substring "git fetch"
}

@test "bud-github-context" {
  target=github-image
  # Any repo should do, but this one is small and is FROM: scratch.
  gitrepo=github.com/projectatomic/nulecule-library
  run_buildah build $WITH_POLICY_JSON -t ${target} "${gitrepo}"
  run_buildah from ${target}
}

# Containerfile in this repo should only exist on older commit and
# not on HEAD or the default branch.
@test "bud-github-context-from-commit" {
  if ! which git ; then
    skip "no git in PATH"
  fi
  target=giturl-image
  # Any repo would do, but this one is small, is FROM: scratch, local, and has
  # its entire build context in a subdirectory of the repository.
  if ! start_git_daemon ${TEST_SOURCES}/git-daemon/repo-with-containerfile-on-old-commit.tar.gz ; then
    skip "error running git daemon"
  fi
  # Containerfile in this repo should only exist on older commit and
  # not on HEAD or the default branch.
  gitrepo=git://localhost:${GITPORT}/repo#f94193d34548eb58650a10a5183936d32c2d3280
  run_buildah build $WITH_POLICY_JSON -t ${target} "${gitrepo}"
  expect_output --substring "FROM scratch"
  expect_output --substring "COMMIT giturl-image"
  # Verify that build must fail on default `main` branch since we
  # don't have a `Containerfile` on main branch.
  gitrepo=git://localhost:${GITPORT}/repo#main
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} "${gitrepo}"
  expect_output --substring "cannot find Containerfile or Dockerfile"
}

@test "bud-github-context-with-branch-subdir-commit" {
  subdir=tests/bud/from-scratch
  target=github-image
  gitrepo=https://github.com/containers/buildah.git#main:$subdir
  run_buildah build $WITH_POLICY_JSON -t ${target} "${gitrepo}"
  # check syntax only for subdirectory
  gitrepo=https://github.com/containers/buildah.git#:$subdir
  run_buildah build $WITH_POLICY_JSON -t ${target} "${gitrepo}"
  # Try pulling repo with specific commit
  # This commit is the initial commit, which used Dockerfile rather then Containerfile
  gitrepo=https://github.com/containers/buildah.git#761597056c8dc2bb1efd67e937a196ddff1fa7a6:$subdir
  run_buildah build $WITH_POLICY_JSON -t ${target} "${gitrepo}"
}

@test "bud-additional-tags" {
  target=scratch-image
  target2=another-scratch-image
  target3=so-many-scratch-images
  run_buildah build $WITH_POLICY_JSON -t ${target} -t docker.io/${target2} -t ${target3} $BUDFILES/from-scratch
  run_buildah images
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah rm ${cid}
  run_buildah from --quiet $WITH_POLICY_JSON library/${target2}
  cid=$output
  run_buildah rm ${cid}
  run_buildah from --quiet $WITH_POLICY_JSON ${target3}:latest
  run_buildah rm $output

  run_buildah rmi $target3 $target2 $target
  expect_line_count 4
  for i in 0 1 2;do
      expect_output --substring --from="${lines[$i]}" "untagged: "
  done
  expect_output --substring --from="${lines[3]}" '^[0-9a-f]{64}$'
}

@test "bud-additional-tags-cached" {
  _prefetch busybox
  target=tagged-image
  target2=another-tagged-image
  target3=yet-another-tagged-image
  target4=still-another-tagged-image
  run_buildah build --layers $WITH_POLICY_JSON -t ${target} $BUDFILES/addtl-tags
  run_buildah build --layers $WITH_POLICY_JSON -t ${target2} -t ${target3} -t ${target4} $BUDFILES/addtl-tags
  run_buildah inspect -f '{{.FromImageID}}' busybox
  busyboxid="$output"
  run_buildah inspect -f '{{.FromImageID}}' ${target}
  targetid="$output"
  assert "$targetid" != "$busyboxid" "FromImageID(target) != busybox"
  run_buildah inspect -f '{{.FromImageID}}' ${target2}
  expect_output "$targetid" "target2 -> .FromImageID"
  run_buildah inspect -f '{{.FromImageID}}' ${target3}
  expect_output "$targetid" "target3 -> .FromImageID"
  run_buildah inspect -f '{{.FromImageID}}' ${target4}
  expect_output "$targetid" "target4 -> .FromImageID"
}

@test "bud-volume-perms" {
  # This Dockerfile needs us to be able to handle a working RUN instruction.
  skip_if_no_runtime

  _prefetch alpine
  for layers in "" --layers ; do
    for compat in "" --compat-volumes ; do
      target=volume-image$compat$layers
      run_buildah build $WITH_POLICY_JSON -t ${target} ${layers} ${compat} $BUDFILES/volume-perms
      run_buildah from --quiet $WITH_POLICY_JSON ${target}
      cid=$output
      run_buildah_mount ${cid}
      root=$output
      if test "$compat" != "" ; then
        # true, /vol/subvol should not have contents, and its permissions should be the default 0755
        test -d $root/vol/subvol
        test ! -s $root/vol/subvol/subvolfile
        run stat -c %a $root/vol/subvol
        assert "$status" -eq 0 "status code from stat $root/vol/subvol"
        expect_output "755" "stat($root/vol/subvol)"
      else
        # true, /vol/subvol should have contents, and its permissions should be the changed 0711
        test -d $root/vol/subvol
        test -s $root/vol/subvol/subvolfile
        run stat -c %a $root/vol/subvol
        assert "$status" -eq 0 "status code from stat $root/vol/subvol"
        expect_output "711" "stat($root/vol/subvol)"
      fi
      run_buildah_umount ${cid}
      run_buildah rm ${cid}
      run_buildah rmi ${target}
    done
  done
}

@test "bud-volume-ownership" {
  # This Dockerfile needs us to be able to handle a working RUN instruction.
  skip_if_no_runtime

  _prefetch alpine
  target=volume-image
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/volume-ownership
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  cid=$output
  run_buildah run $cid stat -c "%U %G" /vol/subvol
  expect_output "testuser testgroup"
}

@test "bud-builtin-volume-symlink" {
  # This Dockerfile needs us to be able to handle a working RUN instruction.
  skip_if_no_runtime

  _prefetch alpine
  target=volume-symlink
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/volume-symlink
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  cid=$output
  run_buildah run $cid echo hello
  expect_output "hello"

  target=volume-no-symlink
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/volume-symlink/Dockerfile.no-symlink $BUDFILES/volume-symlink
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  cid=$output
  run_buildah run $cid echo hello
  expect_output "hello"
}

@test "bud-from-glob" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile2.glob $BUDFILES/from-multiple-files
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  cmp $root/Dockerfile1.alpine $BUDFILES/from-multiple-files/Dockerfile1.alpine
  cmp $root/Dockerfile2.withfrom $BUDFILES/from-multiple-files/Dockerfile2.withfrom
}

@test "bud-maintainer" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/maintainer
  run_buildah inspect --type=image --format '{{.Docker.Author}}' ${target}
  expect_output "kilroy"
  run_buildah inspect --type=image --format '{{.OCIv1.Author}}' ${target}
  expect_output "kilroy"
}

@test "bud-unrecognized-instruction" {
  _prefetch alpine
  target=alpine-image
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} $BUDFILES/unrecognized
  expect_output --substring "BOGUS"
}

@test "bud-shell" {
  _prefetch alpine
  target=alpine-image
  run_buildah build --format docker $WITH_POLICY_JSON -t ${target} $BUDFILES/shell
  run_buildah inspect --type=image --format '{{printf "%q" .Docker.Config.Shell}}' ${target}
  expect_output '["/bin/sh" "-c"]' ".Docker.Config.Shell (original)"
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  ctr=$output
  run_buildah config --shell "/bin/bash -c" ${ctr}
  run_buildah inspect --type=container --format '{{printf "%q" .Docker.Config.Shell}}' ${ctr}
  expect_output '["/bin/bash" "-c"]' ".Docker.Config.Shell (changed)"
}

@test "bud-shell during build in Docker format" {
  _prefetch alpine
  target=alpine-image
  run_buildah build --format docker $WITH_POLICY_JSON -t ${target} -f $BUDFILES/shell/Dockerfile.build-shell-default $BUDFILES/shell
  expect_output --substring "SHELL=/bin/sh"
}

@test "bud-shell during build in OCI format" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/shell/Dockerfile.build-shell-default $BUDFILES/shell
  expect_output --substring "SHELL=/bin/sh"
}

@test "bud-shell changed during build in Docker format" {
  _prefetch ubuntu
  target=ubuntu-image
  run_buildah build --format docker $WITH_POLICY_JSON -t ${target} -f $BUDFILES/shell/Dockerfile.build-shell-custom $BUDFILES/shell
  expect_output --substring "SHELL=/bin/bash"
}

@test "bud-shell changed during build in OCI format" {
  _prefetch ubuntu
  target=ubuntu-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/shell/Dockerfile.build-shell-custom $BUDFILES/shell
  expect_output --substring "SHELL=/bin/bash"

  # should work, but should also emit warning that it won't be persisted into saved image
  expect_output --substring "SHELL is not persisted in the OCI image format, \[/bin/bash -c\]"
}

@test "bud with symlinks" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/symlink
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  run ls $root/data/log
  assert "$status" -eq 0 "status from ls $root/data/log"
  expect_output --substring "test"     "ls \$root/data/log"
  expect_output --substring "blah.txt" "ls \$root/data/log"

  run ls -al $root
  assert "$status" -eq 0 "status from ls -al $root"
  expect_output --substring "test-log -> /data/log" "ls -l \$root/data/log"
  expect_output --substring "blah -> /test-log"     "ls -l \$root/data/log"
}

@test "bud with symlinks to relative path" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.relative-symlink $BUDFILES/symlink
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  run ls $root/log
  assert "$status" -eq 0 "status from ls $root/log"
  expect_output --substring "test" "ls \$root/log"

  run ls -al $root
  assert "$status" -eq 0 "status from ls -al $root"
  expect_output --substring "test-log -> ../log" "ls -l \$root/log"
  test -r $root/var/data/empty
}

@test "bud with multiple symlinks in a path" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/symlink/Dockerfile.multiple-symlinks $BUDFILES/symlink
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  run ls $root/data/log
  assert "$status" -eq 0 "status from ls $root/data/log"
  expect_output --substring "bin"      "ls \$root/data/log"
  expect_output --substring "blah.txt" "ls \$root/data/log"

  run ls -al $root/myuser
  assert "$status" -eq 0 "status from ls -al $root/myuser"
  expect_output --substring "log -> /test" "ls -al \$root/myuser"

  run ls -al $root/test
  assert "$status" -eq 0 "status from ls -al $root/test"
  expect_output --substring "bar -> /test-log" "ls -al \$root/test"

  run ls -al $root/test-log
  assert "$status" -eq 0 "status from ls -al $root/test-log"
  expect_output --substring "foo -> /data/log" "ls -al \$root/test-log"
}

@test "bud with multiple symlink pointing to itself" {
  _prefetch alpine
  target=alpine-image
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/symlink/Dockerfile.symlink-points-to-itself $BUDFILES/symlink
  assert "$output" =~ "building .* open /test-log/test: too many levels of symbolic links"
}

@test "bud multi-stage with symlink to absolute path" {
  _prefetch ubuntu
  target=ubuntu-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.absolute-symlink $BUDFILES/symlink
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  run ls $root/bin
  assert "$status" -eq 0 "status from ls $root/bin"
  expect_output --substring "myexe" "ls \$root/bin"

  run cat $root/bin/myexe
  assert "$status" -eq 0 "status from cat $root/bin/myexe"
  expect_output "symlink-test" "cat \$root/bin/myexe"
}

@test "bud multi-stage with dir symlink to absolute path" {
  _prefetch ubuntu
  target=ubuntu-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.absolute-dir-symlink $BUDFILES/symlink
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  run ls $root/data
  assert "$status" -eq 0 "status from ls $root/data"
  expect_output --substring "myexe" "ls \$root/data"
}

@test "bud with ENTRYPOINT and RUN" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.entrypoint-run $BUDFILES/run-scenarios
  expect_output --substring "unique.test.string"
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
}

@test "bud with ENTRYPOINT and empty RUN" {
  _prefetch alpine
  target=alpine-image
  run_buildah 2 bud $WITH_POLICY_JSON -t ${target} -f Dockerfile.entrypoint-empty-run $BUDFILES/run-scenarios
  expect_output --substring " -c requires an argument"
  expect_output --substring "building at STEP.*: exit status 2"
}

@test "bud with CMD and RUN" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/run-scenarios/Dockerfile.cmd-run $BUDFILES/run-scenarios
  expect_output --substring "unique.test.string"
  run_buildah from --quiet $WITH_POLICY_JSON ${target}
}

@test "bud with CMD and empty RUN" {
  _prefetch alpine
  target=alpine-image
  run_buildah 2 bud $WITH_POLICY_JSON -t ${target} -f Dockerfile.cmd-empty-run $BUDFILES/run-scenarios
  expect_output --substring " -c requires an argument"
  expect_output --substring "building at STEP.*: exit status 2"
}

@test "bud with ENTRYPOINT, CMD and RUN" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/run-scenarios/Dockerfile.entrypoint-cmd-run $BUDFILES/run-scenarios
  expect_output --substring "unique.test.string"
  run_buildah from $WITH_POLICY_JSON ${target}
}

@test "bud with ENTRYPOINT, CMD and empty RUN" {
  _prefetch alpine
  target=alpine-image
  run_buildah 2 bud $WITH_POLICY_JSON -t ${target} -f $BUDFILES/run-scenarios/Dockerfile.entrypoint-cmd-empty-run $BUDFILES/run-scenarios
  expect_output --substring " -c requires an argument"
  expect_output --substring "building at STEP.*: exit status 2"
}

# Determines if a variable set with ENV is available to following commands in the Dockerfile
@test "bud access ENV variable defined in same source file" {
  _prefetch alpine
  target=env-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/env/Dockerfile.env-same-file $BUDFILES/env
  expect_output --substring ":unique.test.string:"
  run_buildah from $WITH_POLICY_JSON ${target}
}

# Determines if a variable set with ENV in an image is available to commands in downstream Dockerfile
@test "bud access ENV variable defined in FROM image" {
  _prefetch alpine
  from_target=env-from-image
  target=env-image
  run_buildah build $WITH_POLICY_JSON -t ${from_target} -f $BUDFILES/env/Dockerfile.env-same-file $BUDFILES/env
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/env/Dockerfile.env-from-image $BUDFILES/env
  expect_output --substring "@unique.test.string@"
  run_buildah from --quiet ${from_target}
  from_cid=$output
  run_buildah from ${target}
}

@test "bud ENV preserves special characters after commit" {
  _prefetch ubuntu
  from_target=special-chars
  run_buildah build $WITH_POLICY_JSON -t ${from_target} -f $BUDFILES/env/Dockerfile.special-chars $BUDFILES/env
  run_buildah from --quiet ${from_target}
  cid=$output
  run_buildah run ${cid} env
  expect_output --substring "LIB=\\$\(PREFIX\)/lib"
}

@test "bud with Dockerfile from valid URL" {
  target=url-image
  starthttpd $BUDFILES
  url=http://0.0.0.0:${HTTP_SERVER_PORT}/from-scratch/Dockerfile
  run_buildah build $WITH_POLICY_JSON -t ${target} ${url}
  run_buildah from ${target}
}

@test "bud with Dockerfile from invalid URL" {
  target=url-image
  starthttpd $BUDFILES
  url=http://0.0.0.0:${HTTP_SERVER_PORT}/from-scratch/Dockerfile.bogus
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} ${url}
  expect_output --substring "invalid response status 404"
}

# When provided with a -f flag and directory, buildah will look for the alternate Dockerfile name in the supplied directory
@test "bud with -f flag, alternate Dockerfile name" {
  target=fileflag-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.noop-flags $BUDFILES/run-scenarios
  run_buildah from ${target}
}

# Following flags are configured to result in noop but should not affect buildah bud behavior
@test "bud with --cache-from noop flag" {
  target=noop-image
  run_buildah build --cache-from=invalidimage $WITH_POLICY_JSON -t ${target} -f Dockerfile.noop-flags $BUDFILES/run-scenarios
  run_buildah from ${target}
}

@test "bud with --compress noop flag" {
  target=noop-image
  run_buildah build --compress $WITH_POLICY_JSON -t ${target} -f Dockerfile.noop-flags $BUDFILES/run-scenarios
  run_buildah from ${target}
}

@test "bud with --cpu-shares flag, no argument" {
  target=bud-flag
  run_buildah 125 build --cpu-shares $WITH_POLICY_JSON -t ${target} -f $BUDFILES/from-scratch/Containerfile $BUDFILES/from-scratch
  expect_output --substring "invalid argument .* invalid syntax"
}

@test "bud with --cpu-shares flag, invalid argument" {
  target=bud-flag
  run_buildah 125 build --cpu-shares bogus $WITH_POLICY_JSON -t ${target} -f $BUDFILES/from-scratch/Containerfile $BUDFILES/from-scratch
  expect_output --substring "invalid argument \"bogus\" for "
}

@test "bud with --cpu-shares flag, valid argument" {
  target=bud-flag
  run_buildah build --cpu-shares 2 $WITH_POLICY_JSON -t ${target} -f $BUDFILES/from-scratch/Containerfile $BUDFILES/from-scratch
  run_buildah from ${target}
}

@test "bud with --cpu-shares short flag (-c), no argument" {
  target=bud-flag
  run_buildah 125 build -c $WITH_POLICY_JSON -t ${target} -f $BUDFILES/from-scratch/Containerfile $BUDFILES/from-scratch
  expect_output --substring "invalid argument .* invalid syntax"
}

@test "bud with --cpu-shares short flag (-c), invalid argument" {
  target=bud-flag
  run_buildah 125 build -c bogus $WITH_POLICY_JSON -t ${target} -f $BUDFILES/from-scratch/Containerfile $BUDFILES/from-scratch
  expect_output --substring "invalid argument \"bogus\" for "
}

@test "bud with --cpu-shares short flag (-c), valid argument" {
  target=bud-flag
  run_buildah build -c 2 $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  run_buildah from ${target}
}

@test "bud-onbuild-without-layers" {
  _prefetch alpine
  run_buildah build --format docker $WITH_POLICY_JSON --iidfile ${TEST_SCRATCH_DIR}/iid.txt $BUDFILES/onbuild
  local iid=$(<${TEST_SCRATCH_DIR}/iid.txt)
  run_buildah inspect --format '{{printf "%q" .Docker.Config.OnBuild}}' ${iid}
  expect_output '["RUN touch /onbuild1" "RUN touch /onbuild2"]'

  run_buildah from --cidfile ${TEST_SCRATCH_DIR}/cid.txt ${iid}
  local cid=$(<${TEST_SCRATCH_DIR}/cid.txt)
  run_buildah_mount ${cid}
  root=$output
  test -e ${root}/onbuild1
  test -e ${root}/onbuild2
  run_buildah_umount ${cid}

  run_buildah build --format docker $WITH_POLICY_JSON --iidfile ${TEST_SCRATCH_DIR}/iid.txt --from ${iid} -f Dockerfile1 $BUDFILES/onbuild
  local iid=$(<${TEST_SCRATCH_DIR}/iid.txt)
  run_buildah inspect --format '{{printf "%q" .Docker.Config.OnBuild}}' ${iid}
  expect_output '["RUN touch /onbuild3"]'

  run_buildah from --cidfile ${TEST_SCRATCH_DIR}/cid.txt ${iid}
  local cid=$(<${TEST_SCRATCH_DIR}/cid.txt)
  run_buildah_mount ${cid}
  root=$output
  test -e ${root}/onbuild1
  test -e ${root}/onbuild2
  test -e ${root}/onbuild3
  run_buildah_umount ${cid}

  cp -v ${TEST_SCRATCH_DIR}/root/${STORAGE_DRIVER:-vfs}-containers/${cid}/userdata/buildah.json \
        ${TEST_SCRATCH_DIR}/root/${STORAGE_DRIVER:-vfs}-containers/${cid}/userdata/buildah.json.bak
  run_buildah config --onbuild "RUN touch /onbuild4" ${cid}
  run_buildah inspect --format '{{printf "%q" .Docker.Config.OnBuild}}' ${cid}
  expect_output '["RUN touch /onbuild4"]'

  run_buildah commit $WITH_POLICY_JSON --iidfile ${TEST_SCRATCH_DIR}/iid.txt --format docker ${cid} ${target}
  local iid=$(<${TEST_SCRATCH_DIR}/iid.txt)
  run_buildah inspect --format '{{printf "%q" .Docker.Config.OnBuild}}' ${iid}
  expect_output '["RUN touch /onbuild4"]'
}

@test "bud-onbuild-layers" {
  _prefetch alpine
  target=onbuild
  run_buildah build --format docker $WITH_POLICY_JSON --layers -t ${target} -f Dockerfile2 $BUDFILES/onbuild
  run_buildah inspect --format '{{printf "%q" .Docker.Config.OnBuild}}' ${target}
  expect_output '["RUN touch /onbuild1" "RUN touch /onbuild2"]'
}

@test "bud-logfile" {
  _prefetch alpine
  rm -f ${TEST_SCRATCH_DIR}/logfile
  run_buildah build --logfile ${TEST_SCRATCH_DIR}/logfile $WITH_POLICY_JSON $BUDFILES/preserve-volumes
  test -s ${TEST_SCRATCH_DIR}/logfile
}

@test "bud-logfile-with-split-logfile-by-platform" {
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir

  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
COPY . .
_EOF

  rm -f ${TEST_SCRATCH_DIR}/logfile
  run_buildah build --logfile ${TEST_SCRATCH_DIR}/logfile --logsplit --platform linux/arm64,linux/amd64 $WITH_POLICY_JSON ${mytmpdir}
  run cat ${TEST_SCRATCH_DIR}/logfile_linux_arm64
  expect_output --substring "FROM alpine"
  expect_output --substring "[linux/arm64]"
  run cat ${TEST_SCRATCH_DIR}/logfile_linux_amd64
  expect_output --substring "FROM alpine"
  expect_output --substring "[linux/amd64]"
}

@test "bud with ARGS" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.args $BUDFILES/run-scenarios
  expect_output --substring "arg_value"
}

@test "bud with unused ARGS" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.multi-args --build-arg USED_ARG=USED_VALUE $BUDFILES/run-scenarios
  expect_output --substring "USED_VALUE"
  assert "$output" !~ "one or more build args were not consumed: [UNUSED_ARG]"
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.multi-args --build-arg USED_ARG=USED_VALUE --build-arg UNUSED_ARG=whaaaat $BUDFILES/run-scenarios
  expect_output --substring "USED_VALUE"
  expect_output --substring "one or more build args were not consumed: \[UNUSED_ARG\]"
}

@test "bud with multi-value ARGS" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile.multi-args --build-arg USED_ARG=plugin1,plugin2,plugin3 $BUDFILES/run-scenarios
  expect_output --substring "plugin1,plugin2,plugin3"
   if [[ "$output" =~ "one or more build args were not consumed" ]]; then
      expect_output "[not expecting to see 'one or more build args were not consumed']"
  fi
}

@test "bud-from-stdin" {
  target=scratch-image
  cat $BUDFILES/from-multiple-files/Dockerfile1.scratch | run_buildah build $WITH_POLICY_JSON -t ${target} -f - $BUDFILES/from-multiple-files
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  test -s $root/Dockerfile1
}

@test "bud with preprocessor" {
  _prefetch busybox
  target=alpine-image
  starthttpd $BUDFILES/preprocess
  run_buildah build --build-arg HTTP_SERVER_PORT=${HTTP_SERVER_PORT} --network=host $WITH_POLICY_JSON -t ${target} -f Decomposed.in $BUDFILES/preprocess
}

@test "bud with preprocessor error" {
  _prefetch busybox
  target=alpine-image
  starthttpd $BUDFILES/preprocess
  run_buildah bud --build-arg HTTP_SERVER_PORT=${HTTP_SERVER_PORT} --network=host $WITH_POLICY_JSON -t ${target} -f Error.in $BUDFILES/preprocess
  expect_output --substring "Ignoring <stdin>:5:2: error: #error"
}

@test "bud-with-rejected-name" {
  target=ThisNameShouldBeRejected
  run_buildah 125 build -q $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  expect_output --substring "must be lower"
}

@test "bud with chown copy" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chown
  run_buildah build $WITH_POLICY_JSON -t ${imgName} $BUDFILES/copy-chown
  expect_output --substring "user:2367 group:3267"
  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run alpine-chown -- stat -c '%u' /tmp/copychown.txt
  # Validate that output starts with "2367"
  expect_output --substring "2367"

  run_buildah run alpine-chown -- stat -c '%g' /tmp/copychown.txt
  # Validate that output starts with "3267"
  expect_output --substring "3267"
}

@test "bud with combined chown and chmod copy" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chmod
  run_buildah build $WITH_POLICY_JSON  -t ${imgName} -f $BUDFILES/copy-chmod/Dockerfile.combined $BUDFILES/copy-chmod
  expect_output --substring "chmod:777 user:2367 group:3267"
}

@test "bud with combined chown and chmod add" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chmod
  run_buildah build $WITH_POLICY_JSON  -t ${imgName} -f $BUDFILES/add-chmod/Dockerfile.combined $BUDFILES/add-chmod
  expect_output --substring "chmod:777 user:2367 group:3267"
}

@test "bud with chown copy with bad chown flag in Dockerfile with --layers" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chown
  run_buildah 125 build $WITH_POLICY_JSON --layers -t ${imgName} -f $BUDFILES/copy-chown/Dockerfile.bad $BUDFILES/copy-chown
  expect_output --substring "COPY only supports the --chmod=<permissions>, --chown=<uid:gid>, --from=<image\|stage>, and --exclude=<pattern> flags"
}

@test "bud with chown copy with unknown substitutions in Dockerfile" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chown
  run_buildah 125 build $WITH_POLICY_JSON -t ${imgName} -f $BUDFILES/copy-chown/Dockerfile.bad2 $BUDFILES/copy-chown
  expect_output --substring "looking up UID/GID for \":\": can't find uid for user"
}

@test "bud with chmod copy" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chmod
  run_buildah build $WITH_POLICY_JSON -t ${imgName} $BUDFILES/copy-chmod
  expect_output --substring "rwxrwxrwx"
  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run alpine-chmod ls -l /tmp/copychmod.txt
  # Validate that output starts with 777 == "rwxrwxrwx"
  expect_output --substring "rwxrwxrwx"
}

@test "bud with chmod copy with bad chmod flag in Dockerfile with --layers" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chmod
  run_buildah 125 build $WITH_POLICY_JSON --layers -t ${imgName} -f $BUDFILES/copy-chmod/Dockerfile.bad $BUDFILES/copy-chmod
  expect_output --substring "COPY only supports the --chmod=<permissions>, --chown=<uid:gid>, --from=<image\|stage>, and --exclude=<pattern> flags"
}

@test "bud with chmod add" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chmod
  run_buildah build $WITH_POLICY_JSON -t ${imgName} $BUDFILES/add-chmod
  expect_output --substring "rwxrwxrwx"
  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run alpine-chmod ls -l /tmp/addchmod.txt
  # Validate that rights equal 777 == "rwxrwxrwx"
  expect_output --substring "rwxrwxrwx"
}

@test "bud with chown add" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chown
  run_buildah build $WITH_POLICY_JSON -t ${imgName} $BUDFILES/add-chown
  expect_output --substring "user:2367 group:3267"
  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run alpine-chown -- stat -c '%u' /tmp/addchown.txt
  # Validate that output starts with "2367"
  expect_output --substring "2367"

  run_buildah run alpine-chown -- stat -c '%g' /tmp/addchown.txt
  # Validate that output starts with "3267"
  expect_output --substring "3267"
}

@test "bud with chown add with bad chown flag in Dockerfile with --layers" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chown
  run_buildah 125 build $WITH_POLICY_JSON --layers -t ${imgName} -f $BUDFILES/add-chown/Dockerfile.bad $BUDFILES/add-chown
  expect_output --substring "ADD only supports the --chmod=<permissions>, --chown=<uid:gid>, --checksum=<checksum>, and --exclude=<pattern> flags"
}

@test "bud with chmod add with bad chmod flag in Dockerfile with --layers" {
  _prefetch alpine
  imgName=alpine-image
  ctrName=alpine-chmod
  run_buildah 125 build $WITH_POLICY_JSON --layers -t ${imgName} -f $BUDFILES/add-chmod/Dockerfile.bad $BUDFILES/add-chmod
  expect_output --substring "ADD only supports the --chmod=<permissions>, --chown=<uid:gid>, --checksum=<checksum>, and --exclude=<pattern> flags"
}

@test "bud with ADD with checksum flag" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t alpine-image -f $BUDFILES/add-checksum/Containerfile $BUDFILES/add-checksum
  run_buildah from --quiet $WITH_POLICY_JSON --name alpine-ctr alpine-image
  run_buildah run alpine-ctr -- ls -l /README.md
  expect_output --substring "README.md"
}

@test "bud with ADD with bad checksum" {
  _prefetch alpine
  target=alpine-image
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/add-checksum/Containerfile.bad-checksum $BUDFILES/add-checksum
  expect_output --substring "unexpected response digest for \"https://raw.githubusercontent.com/containers/buildah/bf3b55ba74102cc2503eccbaeffe011728d46b20/README.md\": sha256:4fd3aed66b5488b45fe83dd11842c2324fadcc38e1217bb45fbd28d660afdd39, want sha256:0000000000000000000000000000000000000000000000000000000000000000"
}

@test "bud with ADD with bad checksum flag" {
  _prefetch alpine
  target=alpine-image
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/add-checksum/Containerfile.bad $BUDFILES/add-checksum
  expect_output --substring "ADD only supports the --chmod=<permissions>, --chown=<uid:gid>, --checksum=<checksum>, and --exclude=<pattern> flags"
}

@test "bud with ADD file construct" {
  _prefetch busybox
  run_buildah build $WITH_POLICY_JSON -t test1 $BUDFILES/add-file
  run_buildah images -a
  expect_output --substring "test1"

  run_buildah from --quiet $WITH_POLICY_JSON test1
  ctr=$output
  run_buildah containers -a
  expect_output --substring "test1"

  run_buildah run $ctr ls /var/file2
  expect_output --substring "/var/file2"
}

@test "bud with COPY of single file creates absolute path with correct permissions" {
  _prefetch ubuntu
  imgName=ubuntu-image
  ctrName=ubuntu-copy
  run_buildah build $WITH_POLICY_JSON -t ${imgName} $BUDFILES/copy-create-absolute-path
  expect_output --substring "permissions=755"

  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run ${ctrName} -- stat -c "%a" /usr/lib/python3.7/distutils
  expect_output "755"
}

@test "bud with COPY of single file creates relative path with correct permissions" {
  _prefetch ubuntu
  imgName=ubuntu-image
  ctrName=ubuntu-copy
  run_buildah build $WITH_POLICY_JSON -t ${imgName} $BUDFILES/copy-create-relative-path
  expect_output --substring "permissions=755"

  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run ${ctrName} -- stat -c "%a" lib/custom
  expect_output "755"
}

@test "bud with ADD of single file creates absolute path with correct permissions" {
  _prefetch ubuntu
  imgName=ubuntu-image
  ctrName=ubuntu-copy
  run_buildah build $WITH_POLICY_JSON -t ${imgName} $BUDFILES/add-create-absolute-path
  expect_output --substring "permissions=755"

  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run ${ctrName} -- stat -c "%a" /usr/lib/python3.7/distutils
  expect_output "755"
}

@test "bud with ADD of single file creates relative path with correct permissions" {
  _prefetch ubuntu
  imgName=ubuntu-image
  ctrName=ubuntu-copy
  run_buildah build $WITH_POLICY_JSON -t ${imgName} $BUDFILES/add-create-relative-path
  expect_output --substring "permissions=755"

  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run ${ctrName} -- stat -c "%a" lib/custom
  expect_output "755"
}

@test "bud multi-stage COPY creates absolute path with correct permissions" {
  _prefetch ubuntu
  imgName=ubuntu-image
  ctrName=ubuntu-copy
  run_buildah build $WITH_POLICY_JSON -f $BUDFILES/copy-multistage-paths/Dockerfile.absolute -t ${imgName} $BUDFILES/copy-multistage-paths
  expect_output --substring "permissions=755"

  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run ${ctrName} -- stat -c "%a" /my/bin
  expect_output "755"
}

@test "bud multi-stage COPY creates relative path with correct permissions" {
  _prefetch ubuntu
  imgName=ubuntu-image
  ctrName=ubuntu-copy
  run_buildah build $WITH_POLICY_JSON -f $BUDFILES/copy-multistage-paths/Dockerfile.relative -t ${imgName} $BUDFILES/copy-multistage-paths
  expect_output --substring "permissions=755"

  run_buildah from --name ${ctrName} ${imgName}
  run_buildah run ${ctrName} -- stat -c "%a" my/bin
  expect_output "755"
}

@test "bud multi-stage COPY with invalid from statement" {
  _prefetch ubuntu
  imgName=ubuntu-image
  ctrName=ubuntu-copy
  run_buildah 125 build $WITH_POLICY_JSON -f $BUDFILES/copy-multistage-paths/Dockerfile.invalid_from -t ${imgName} $BUDFILES/copy-multistage-paths
  expect_output --substring "COPY only supports the --chmod=<permissions>, --chown=<uid:gid>, --from=<image\|stage>, and --exclude=<pattern> flags"
}

@test "bud COPY to root succeeds" {
  _prefetch ubuntu
  run_buildah build $WITH_POLICY_JSON $BUDFILES/copy-root
}

@test "bud with FROM AS construct" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON -t test1 $BUDFILES/from-as
  run_buildah images -a
  expect_output --substring "test1"

  run_buildah from --quiet $WITH_POLICY_JSON test1
  ctr=$output
  run_buildah containers -a
  expect_output --substring "test1"

  run_buildah inspect --format "{{.Docker.ContainerConfig.Env}}" --type image test1
  expect_output --substring "LOCAL=/1"
}

@test "bud with FROM AS construct with layers" {
  _prefetch alpine
  run_buildah build --layers $WITH_POLICY_JSON -t test1 $BUDFILES/from-as
  run_buildah images -a
  expect_output --substring "test1"

  run_buildah from --quiet $WITH_POLICY_JSON test1
  ctr=$output
  run_buildah containers -a
  expect_output --substring "test1"

  run_buildah inspect --format "{{.Docker.ContainerConfig.Env}}" --type image test1
  expect_output --substring "LOCAL=/1"
}

@test "bud with FROM AS skip FROM construct" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON -t test1 -f $BUDFILES/from-as/Dockerfile.skip $BUDFILES/from-as
  expect_output --substring "LOCAL=/1"
  expect_output --substring "LOCAL2=/2"

  run_buildah images -a
  expect_output --substring "test1"

  run_buildah from --quiet $WITH_POLICY_JSON test1
  ctr=$output
  run_buildah containers -a
  expect_output --substring "test1"

  run_buildah_mount $ctr
  mnt=$output
  test   -e $mnt/1
  test ! -e $mnt/2

  run_buildah inspect --format "{{.Docker.ContainerConfig.Env}}" --type image test1
  expect_output "[PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin LOCAL=/1]"
}

@test "build with -f pointing to not a file should fail" {
  _prefetch alpine
  target=alpine-image
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/dockerfile/
  expect_output --substring "cannot be path to a directory"
}

@test "bud with symlink Dockerfile not specified in file" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/symlink/Dockerfile $BUDFILES/symlink
  expect_output --substring "FROM alpine"
}

@test "bud with ARG before FROM default value" {
  _prefetch busybox
  target=leading-args-default
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/leading-args/Dockerfile $BUDFILES/leading-args
}

@test "bud with ARG before FROM" {
  _prefetch busybox:musl
  target=leading-args
  run_buildah build $WITH_POLICY_JSON -t ${target} --build-arg=VERSION=musl -f $BUDFILES/leading-args/Dockerfile $BUDFILES/leading-args

  # Verify https://github.com/containers/buildah/issues/4312
  # stage `FROM stage_${my_env}` must be resolved with default arg value and build should be successful.
  run_buildah build $WITH_POLICY_JSON -t source -f $BUDFILES/multi-stage-builds/Dockerfile.arg_in_stage

  # Verify https://github.com/containers/buildah/issues/4573
  # stage `COPY --from=stage_${my_env}` must be resolved with default arg value and build should be successful.
  run_buildah build $WITH_POLICY_JSON -t source -f $BUDFILES/multi-stage-builds/Dockerfile.arg_in_copy
}

@test "bud COPY --from= with ARG in stage scope" {
  _prefetch busybox

  imgname=copy-from-stage-scoped-arg-$(safename)
  ctrname=copy-from-stage-scoped-arg-ctr-$(safename)
  run_buildah build $WITH_POLICY_JSON -t ${imgname} -f $BUDFILES/copy-from-stage-scoped-arg/Containerfile $BUDFILES/copy-from-stage-scoped-arg
  run_buildah from --name ${ctrname} ${imgname}
  run_buildah run ${ctrname} cat /variant
  assert "$output" =~ "build:staging"

  imgname_multi=copy-from-stage-scoped-arg-multi-$(safename)
  ctrname_multi=copy-from-stage-scoped-arg-multi-ctr-$(safename)
  run_buildah build $WITH_POLICY_JSON -t ${imgname_multi} -f $BUDFILES/copy-from-stage-scoped-arg/Containerfile.multi-copy-arg-override $BUDFILES/copy-from-stage-scoped-arg
  run_buildah from --name ${ctrname_multi} ${imgname_multi}
  run_buildah run ${ctrname_multi} cat /variant_first
  assert "$output" =~ "build:staging"
  run_buildah run ${ctrname_multi} cat /variant_second
  assert "$output" =~ "build:dev"
}

@test "bud ARG scope: table of scope combinations" {
  _prefetch alpine busybox
  local ctx=$BUDFILES/arg-scope
  # containerfile_suffix | build_arg (or empty) | path | expected_substring
  local cases=(
    "from-arg::/base.txt:alpine"
    "from-arg:BASE=busybox:/base.txt:busybox"
    "stage-overrides-header::/foo.txt:stage"
    "copy-from-arg::/from.txt:a"
    "copy-from-arg:SRC=b:/from.txt:b"
    "stage-overrides-header-copy-from::/from.txt:b"
    "multiple-args-stage-overrides-one::/out.txt:9 1"
    "multiple-args-stage-only::/out.txt:1 2"
    "multiple-args-one-step::/out.txt:0 1"
    "multiple-args-one-step:bar=88:/out.txt:0 88"
    "multiple-args-one-step:foo=99 bar=88:/out.txt:99 88"
    "arg-mixed-defaults::/out.txt:set"
    "arg-mixed-defaults:a=given:/out.txt:given set"
  )
  for c in "${cases[@]}"; do
    IFS=: read -r cf_suffix build_arg path expected <<< "$c"
    build_arg_safe=$(echo "${build_arg//=/-}" | tr '[:upper:]' '[:lower:]' | tr ' ' '-')
    imgname=arg-scope-$(safename)-${cf_suffix}-${build_arg_safe:-none}
    build_args=""
    if [[ -n "$build_arg" ]]; then
      for arg in $build_arg; do build_args="$build_args --build-arg $arg"; done
    fi

    run_buildah build $WITH_POLICY_JSON $build_args -t ${imgname} -f ${ctx}/Containerfile.${cf_suffix} ${ctx}

    run_buildah from $WITH_POLICY_JSON ${imgname}
    ctr=$(echo "$output" | tail -1)
    run_buildah_mount ${ctr}
    mnt=$output
    run cat ${mnt}${path}
    expect_output --substring "$expected" "case: \"$c\""
    run_buildah_umount ${ctr}
  done
}

@test "bud-with-healthcheck" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} --format docker $BUDFILES/healthcheck
  run_buildah inspect -f '{{printf "%q" .Docker.Config.Healthcheck.Test}} {{printf "%d" .Docker.Config.Healthcheck.StartInterval}} {{printf "%d" .Docker.Config.Healthcheck.StartPeriod}} {{printf "%d" .Docker.Config.Healthcheck.Interval}} {{printf "%d" .Docker.Config.Healthcheck.Timeout}} {{printf "%d" .Docker.Config.Healthcheck.Retries}}' ${target}
  second=1000000000
  threeseconds=$(( 3 * $second ))
  thirtyseconds=$(( 30 * $second ))
  fiveminutes=$(( 5 * 60 * $second ))
  tenminutes=$(( 10 * 60 * $second ))
  expect_output '["CMD-SHELL" "curl -f http://localhost/ || exit 1"]'" $thirtyseconds $tenminutes $fiveminutes $threeseconds 4" "Healthcheck config"
}

@test "bud with unused build arg" {
  _prefetch alpine busybox
  target=busybox-image
  run_buildah build $WITH_POLICY_JSON -t ${target} --build-arg foo=bar --build-arg foo2=bar2 -f $BUDFILES/build-arg/Dockerfile $BUDFILES/build-arg
  expect_output --substring "one or more build args were not consumed: \[foo2\]"
  run_buildah build $WITH_POLICY_JSON -t ${target} --build-arg IMAGE=alpine -f $BUDFILES/build-arg/Dockerfile2 $BUDFILES/build-arg
  assert "$output" !~ "one or more build args were not consumed: \[IMAGE\]"
  expect_output --substring "FROM alpine"
}

@test "bud with copy-from and cache" {
  _prefetch busybox
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid1 -f $BUDFILES/copy-from/Dockerfile2 $BUDFILES/copy-from
  cat ${TEST_SCRATCH_DIR}/iid1
  test -s ${TEST_SCRATCH_DIR}/iid1
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid2 -f $BUDFILES/copy-from/Dockerfile2 $BUDFILES/copy-from
  cat ${TEST_SCRATCH_DIR}/iid2
  test -s ${TEST_SCRATCH_DIR}/iid2
  cmp ${TEST_SCRATCH_DIR}/iid1 ${TEST_SCRATCH_DIR}/iid2
}

@test "bud with copy-from in Dockerfile no prior FROM" {
  want_tag=20221018
  _prefetch busybox quay.io/libpod/testimage:$want_tag
  target=no-prior-from
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/copy-from/Dockerfile $BUDFILES/copy-from

  run_buildah from --quiet $WITH_POLICY_JSON ${target}
  ctr=$output
  run_buildah_mount ${ctr}
  mnt=$output

  newfile="/home/busyboxpodman/copied-testimage-id"
  test -e $mnt/$newfile
  expect_output --from="$(< $mnt/$newfile)" "$want_tag" "Contents of $newfile"
}

@test "bud with copy-from with bad from flag in Dockerfile with --layers" {
  _prefetch busybox
  target=bad-from-flag
  run_buildah 125 build $WITH_POLICY_JSON --layers -t ${target} -f $BUDFILES/copy-from/Dockerfile.bad $BUDFILES/copy-from
  expect_output --substring "COPY only supports the --chmod=<permissions>, --chown=<uid:gid>, --from=<image\|stage>, and --exclude=<pattern> flags"
}

@test "bud with copy-from referencing the base image" {
  _prefetch busybox
  target=busybox-derived
  target_mt=busybox-mt-derived
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/copy-from/Dockerfile3 $BUDFILES/copy-from
  run_buildah build $WITH_POLICY_JSON --jobs 4 -t ${target} -f $BUDFILES/copy-from/Dockerfile3 $BUDFILES/copy-from

  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/copy-from/Dockerfile4 $BUDFILES/copy-from
  run_buildah build --no-cache $WITH_POLICY_JSON --jobs 4 -t ${target_mt} -f $BUDFILES/copy-from/Dockerfile4 $BUDFILES/copy-from

  run_buildah from  --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root_single_job=$output

  run_buildah from --quiet ${target_mt}
  cid=$output
  run_buildah_mount ${cid}
  root_multi_job=$output

  # Check that both the version with --jobs 1 and --jobs=N have the same number of files
  test $(find $root_single_job -type f | wc -l) = $(find $root_multi_job -type f | wc -l)
}

@test "bud with copy-from referencing the current stage" {
  _prefetch busybox
  target=busybox-derived
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/copy-from/Dockerfile2.bad $BUDFILES/copy-from
  expect_output --substring "COPY --from=build: no stage or image found with that name"
}

@test "bud-target" {
  _prefetch alpine ubuntu
  target=target
  run_buildah build $WITH_POLICY_JSON -t ${target} --target mytarget $BUDFILES/target
  expect_output --substring "\[1/2] STEP 1/3: FROM ubuntu:latest"
  expect_output --substring "\[2/2] STEP 1/3: FROM alpine:latest AS mytarget"
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output
  test   -e ${root}/2
  test ! -e ${root}/3
}

@test "bud-no-target-name" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON $BUDFILES/maintainer
}

@test "bud-multi-stage-nocache-nocommit" {
  _prefetch alpine
  # okay, build an image with two stages
  run_buildah --log-level=debug bud $WITH_POLICY_JSON -f $BUDFILES/multi-stage-builds/Dockerfile.name $BUDFILES/multi-stage-builds
  # debug messages should only record us creating one new image: the one for the second stage, since we don't base anything on the first
  run grep "created new image ID" <<< "$output"
  expect_line_count 1
}

@test "bud-multi-stage-cache-nocontainer" {
  skip "FIXME: Broken in CI right now"
  _prefetch alpine
  # first time through, quite normal
  run_buildah build --layers -t base $WITH_POLICY_JSON -f $BUDFILES/multi-stage-builds/Dockerfile.rebase $BUDFILES/multi-stage-builds
  # second time through, everything should be cached, and we shouldn't create a container based on the final image
  run_buildah --log-level=debug bud --layers -t base $WITH_POLICY_JSON -f $BUDFILES/multi-stage-builds/Dockerfile.rebase $BUDFILES/multi-stage-builds
  # skip everything up through the final COMMIT step, and make sure we didn't log a "Container ID:" after it
  run sed '0,/COMMIT base/ d' <<< "$output"
  echo "$output" >&2
  test "${#lines[@]}" -gt 1
  run grep "Container ID:" <<< "$output"
  expect_output ""
}

@test "bud copy to symlink" {
  _prefetch alpine
  target=alpine-image
  ctr=alpine-ctr
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/dest-symlink
  expect_output --substring "STEP 5/6: RUN ln -s "

  run_buildah from $WITH_POLICY_JSON --name=${ctr} ${target}
  expect_output --substring ${ctr}

  run_buildah run ${ctr} ls -alF /etc/hbase
  expect_output --substring "/etc/hbase -> /usr/local/hbase/"

  run_buildah run ${ctr} ls -alF /usr/local/hbase
  expect_output --substring "Dockerfile"
}

@test "bud copy to dangling symlink" {
  _prefetch ubuntu
  target=ubuntu-image
  ctr=ubuntu-ctr
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/dest-symlink-dangling
  expect_output --substring "STEP 3/5: RUN ln -s "

  run_buildah from $WITH_POLICY_JSON --name=${ctr} ${target}
  expect_output --substring ${ctr}

  run_buildah run ${ctr} ls -alF /src
  expect_output --substring "/src -> /symlink"

  run_buildah run ${ctr} ls -alF /symlink
  expect_output --substring "Dockerfile"
}

@test "bud WORKDIR isa symlink" {
  _prefetch alpine
  target=alpine-image
  ctr=alpine-ctr
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/workdir-symlink
  expect_output --substring "STEP 3/6: RUN ln -sf "

  run_buildah from $WITH_POLICY_JSON --name=${ctr} ${target}
  expect_output --substring ${ctr}

  run_buildah run ${ctr} ls -alF /tempest
  expect_output --substring "/tempest -> /var/lib/tempest/"

  run_buildah run ${ctr} ls -alF /etc/notareal.conf
  expect_output --substring "\-rw\-rw\-r\-\-"
}

@test "bud WORKDIR isa symlink no target dir" {
  _prefetch alpine
  target=alpine-image
  ctr=alpine-ctr
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile-2 $BUDFILES/workdir-symlink
  expect_output --substring "STEP 2/6: RUN ln -sf "

  run_buildah from $WITH_POLICY_JSON --name=${ctr} ${target}
  expect_output --substring ${ctr}

  run_buildah run ${ctr} ls -alF /tempest
  expect_output --substring "/tempest -> /var/lib/tempest/"

  run_buildah run ${ctr} ls /tempest
  expect_output --substring "Dockerfile-2"

  run_buildah run ${ctr} ls -alF /etc/notareal.conf
  expect_output --substring "\-rw\-rw\-r\-\-"
}

@test "bud WORKDIR isa symlink no target dir and follow on dir" {
  _prefetch alpine
  target=alpine-image
  ctr=alpine-ctr
  run_buildah build $WITH_POLICY_JSON -t ${target} -f Dockerfile-3 $BUDFILES/workdir-symlink
  expect_output --substring "STEP 2/9: RUN ln -sf "

  run_buildah from $WITH_POLICY_JSON --name=${ctr} ${target}
  expect_output --substring ${ctr}

  run_buildah run ${ctr} ls -alF /tempest
  expect_output --substring "/tempest -> /var/lib/tempest/"

  run_buildah run ${ctr} ls /tempest
  expect_output --substring "Dockerfile-3"

  run_buildah run ${ctr} ls /tempest/lowerdir
  expect_output --substring "Dockerfile-3"

  run_buildah run ${ctr} ls -alF /etc/notareal.conf
  expect_output --substring "\-rw\-rw\-r\-\-"
}

@test "buildah bud --volume" {
  voldir=${TEST_SCRATCH_DIR}/bud-volume
  mkdir -p ${voldir}

  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON -v ${voldir}:/testdir $BUDFILES/mount
  expect_output --substring "/testdir"
  run_buildah build $WITH_POLICY_JSON -v ${voldir}:/testdir:rw $BUDFILES/mount
  expect_output --substring "/testdir"
  run_buildah build $WITH_POLICY_JSON -v ${voldir}:/testdir:rw,z $BUDFILES/mount
  expect_output --substring "/testdir"
}

@test "bud-copy-dot with --layers picks up changed file" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/use-layers
  cp -a $BUDFILES/use-layers $contextdir

  mkdir -p $contextdir/subdir
  touch $contextdir/file.txt
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid1 -f Dockerfile.7 $contextdir

  touch $contextdir/file.txt
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/iid2 -f Dockerfile.7 $contextdir

  if [[ $(< ${TEST_SCRATCH_DIR}/iid1) != $(< ${TEST_SCRATCH_DIR}/iid2) ]]; then
    echo "Expected image id to not change after touching a file copied into the image" >&2
    false
  fi
}

@test "buildah-bud-policy" {
  target=foo

  # A deny-all policy should prevent us from pulling the base image.
  run_buildah 125 build --signature-policy ${TEST_SOURCES}/deny.json -t ${target} -v ${TEST_SOURCES}:/testdir $BUDFILES/mount
  expect_output --substring 'Source image rejected: Running image .* rejected by policy.'

  # A docker-only policy should allow us to pull the base image and commit.
  run_buildah build --signature-policy ${TEST_SOURCES}/docker.json -t ${target} -v ${TEST_SOURCES}:/testdir $BUDFILES/mount
  # A deny-all policy shouldn't break pushing, since policy is only evaluated
  # on the source image, and we force it to allow local storage.
  run_buildah push --signature-policy ${TEST_SOURCES}/deny.json ${target} dir:${TEST_SCRATCH_DIR}/mount
  run_buildah rmi ${target}

  # A docker-only policy should allow us to pull the base image first...
  run_buildah pull --signature-policy ${TEST_SOURCES}/docker.json alpine
  # ... and since we don't need to pull the base image, a deny-all policy shouldn't break a build.
  run_buildah build --signature-policy ${TEST_SOURCES}/deny.json -t ${target} -v ${TEST_SOURCES}:/testdir $BUDFILES/mount
  # A deny-all policy shouldn't break pushing, since policy is only evaluated
  # on the source image, and we force it to allow local storage.
  run_buildah push --signature-policy ${TEST_SOURCES}/deny.json ${target} dir:${TEST_SCRATCH_DIR}/mount
  # Similarly, a deny-all policy shouldn't break committing directly to other locations.
  run_buildah build --signature-policy ${TEST_SOURCES}/deny.json -t dir:${TEST_SCRATCH_DIR}/mount -v ${TEST_SOURCES}:/testdir $BUDFILES/mount
}

@test "bud-copy-replace-symlink" {
  local contextdir=${TEST_SCRATCH_DIR}/top
  mkdir -p $contextdir
  cp $BUDFILES/symlink/Dockerfile.replace-symlink $contextdir/
  ln -s Dockerfile.replace-symlink $contextdir/symlink
  echo foo > $contextdir/.dockerignore
  run_buildah build $WITH_POLICY_JSON -f $contextdir/Dockerfile.replace-symlink $contextdir
}

@test "bud-copy-recurse" {
  local contextdir=${TEST_SCRATCH_DIR}/recurse
  mkdir -p $contextdir
  cp $BUDFILES/recurse/Dockerfile $contextdir
  echo foo > $contextdir/.dockerignore
  run_buildah build $WITH_POLICY_JSON $contextdir
}

@test "bud copy with .dockerignore #1" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir/stuff/huge/usr/bin/
  touch $mytmpdir/stuff/huge/usr/bin/{file1,file2}
  touch $mytmpdir/stuff/huge/usr/file3

  cat > $mytmpdir/.dockerignore << _EOF
stuff/huge/*
!stuff/huge/usr/bin/*
_EOF

  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
COPY stuff /tmp/stuff
RUN find /tmp/stuff -type f
_EOF

  run_buildah build -t testbud $WITH_POLICY_JSON ${mytmpdir}
  expect_output --substring "file1"
  expect_output --substring "file2"
  assert "$output" !~ "file3"
}

@test "bud copy with .dockerignore #2" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir1
  mkdir -p $mytmpdir/stuff/huge/usr/bin/
  touch $mytmpdir/stuff/huge/usr/bin/{file1,file2}

  cat > $mytmpdir/.dockerignore << _EOF
stuff/huge/*
_EOF

  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
COPY stuff /tmp/stuff
RUN find /tmp/stuff -type f
_EOF

  run_buildah build -t testbud $WITH_POLICY_JSON ${mytmpdir}
  assert "$output" !~ file1
  assert "$output" !~ file2
}

@test "bud-copy-workdir" {
  target=testimage
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/copy-workdir
  run_buildah from ${target}
  cid="$output"
  run_buildah_mount "${cid}"
  root="$output"
  test -s "${root}"/file1.txt
  test -d "${root}"/subdir
  test -s "${root}"/subdir/file2.txt
}

# regression test for https://github.com/containers/podman/issues/10671
@test "bud-copy-workdir --layers" {
  _prefetch alpine

  target=testimage
  run_buildah build $WITH_POLICY_JSON --layers -t ${target} -f Dockerfile.2 $BUDFILES/copy-workdir
  run_buildah from ${target}
  cid="$output"
  run_buildah_mount "${cid}"
  root="$output"
  test -d "${root}"/subdir
  test -s "${root}"/subdir/file1.txt
}

@test "bud-build-arg-cache" {
  _prefetch busybox alpine
  target=derived-image
  run_buildah build $WITH_POLICY_JSON --layers -t ${target} -f Dockerfile3 $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' ${target}
  targetid="$output"

  # With build args, we should not find the previous build as a cached result. This will be true because there is a RUN command after all the ARG
  # commands in the containerfile, so this does not truly test if the ARG commands were using cache or not. There is a test for that case below.
  run_buildah build $WITH_POLICY_JSON --layers -t ${target} -f Dockerfile3 --build-arg=UID=17122 --build-arg=CODE=/copr/coprs_frontend --build-arg=USERNAME=praiskup --build-arg=PGDATA=/pgdata $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' ${target}
  argsid="$output"
  assert "$argsid" != "$initialid" \
         ".FromImageID of test-img-2 ($argsid) == same as test-img, it should be different"

  # With build args, even in a different order, we should end up using the previous build as a cached result.
  run_buildah build $WITH_POLICY_JSON --layers -t ${target} -f Dockerfile3 --build-arg=UID=17122 --build-arg=CODE=/copr/coprs_frontend --build-arg=USERNAME=praiskup --build-arg=PGDATA=/pgdata $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' ${target}
  expect_output "$argsid" "FromImageID of build 3"

  run_buildah build $WITH_POLICY_JSON --layers -t ${target} -f Dockerfile3 --build-arg=CODE=/copr/coprs_frontend --build-arg=USERNAME=praiskup --build-arg=PGDATA=/pgdata --build-arg=UID=17122 $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' ${target}
  expect_output "$argsid" "FromImageID of build 4"

  run_buildah build $WITH_POLICY_JSON --layers -t ${target} -f Dockerfile3 --build-arg=USERNAME=praiskup --build-arg=PGDATA=/pgdata --build-arg=UID=17122 --build-arg=CODE=/copr/coprs_frontend $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' ${target}
  expect_output "$argsid" "FromImageID of build 5"

  run_buildah build $WITH_POLICY_JSON --layers -t ${target} -f Dockerfile3 --build-arg=PGDATA=/pgdata --build-arg=UID=17122 --build-arg=CODE=/copr/coprs_frontend --build-arg=USERNAME=praiskup $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' ${target}
  expect_output "$argsid" "FromImageID of build 6"

  # If build-arg is specified via the command line and is different from the previous cached build, it should not use the cached layers.
  # Note, this containerfile does not have any RUN commands and we verify that the ARG steps are being rebuilt when a change is detected.
  run_buildah build $WITH_POLICY_JSON --layers -t test-img -f Dockerfile4 $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' test-img
  initialid="$output"

  # Build the same containerfile again and verify that the cached layers were used
  run_buildah build $WITH_POLICY_JSON --layers -t test-img-1 -f Dockerfile4 $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' test-img-1
  expect_output "$initialid" "FromImageID of test-img-1 should match test-img"

  # Set the build-arg flag and verify that the cached layers are not used
  run_buildah build $WITH_POLICY_JSON --layers -t test-img-2 --build-arg TEST=foo -f Dockerfile4 $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' test-img-2
  argsid="$output"
  assert "$argsid" != "$initialid" \
         ".FromImageID of test-img-2 ($argsid) == same as test-img, it should be different"

  # Set the build-arg via an ENV in the local environment and verify that the cached layers are not used
  export TEST=bar
  run_buildah build $WITH_POLICY_JSON --layers -t test-img-3 --build-arg TEST -f Dockerfile4 $BUDFILES/build-arg
  run_buildah inspect -f '{{.FromImageID}}' test-img-3
  argsid="$output"
  assert "$argsid" != "$initialid" \
         ".FromImageID of test-img-3 ($argsid) == same as test-img, it should be different"
}

@test "bud test RUN with a privileged command" {
  _prefetch busybox
  target=busyboxpriv
  starthttpd $BUDFILES/run-privd
  run_buildah build --network=host --build-arg HTTP_SERVER_PORT=${HTTP_SERVER_PORT} $WITH_POLICY_JSON -t ${target} -f $BUDFILES/run-privd/Dockerfile $BUDFILES/run-privd
  expect_output --substring "[^:][^[:graph:]]COMMIT ${target}"
  run_buildah images -q
  expect_line_count 2
}

@test "bud-copy-dockerignore-hardlinks" {
  skip_if_rootless_environment "unprivileged tests use sshfs which doesn't represent hard links"

  target=image
  local contextdir=${TEST_SCRATCH_DIR}/hardlinks
  mkdir -p $contextdir/subdir
  cp $BUDFILES/recurse/Dockerfile $contextdir
  echo foo > $contextdir/.dockerignore
  echo test1 > $contextdir/subdir/test1.txt
  ln $contextdir/subdir/test1.txt $contextdir/subdir/test2.txt
  ln $contextdir/subdir/test2.txt $contextdir/test3.txt
  ln $contextdir/test3.txt $contextdir/test4.txt
  run_buildah build $WITH_POLICY_JSON -t ${target} $contextdir
  run_buildah from ${target}
  ctrid="$output"
  run_buildah_mount "$ctrid"
  root="$output"

  run stat -c "%d:%i" ${root}/subdir/test1.txt
  id1=$output
  run stat -c "%h" ${root}/subdir/test1.txt
  expect_output 4 "test1: number of hardlinks"
  run stat -c "%d:%i" ${root}/subdir/test2.txt
  expect_output $id1 "stat(test2) == stat(test1)"
  run stat -c "%h" ${root}/subdir/test2.txt
  expect_output 4 "test2: number of hardlinks"
  run stat -c "%d:%i" ${root}/test3.txt
  expect_output $id1 "stat(test3) == stat(test1)"
  run stat -c "%h" ${root}/test3.txt
  expect_output 4 "test3: number of hardlinks"
  run stat -c "%d:%i" ${root}/test4.txt
  expect_output $id1 "stat(test4) == stat(test1)"
  run stat -c "%h" ${root}/test4.txt
  expect_output 4 "test4: number of hardlinks"
}

@test "bud without any arguments should succeed" {
  cd $BUDFILES/from-scratch
  run_buildah build --signature-policy ${TEST_SOURCES}/policy.json
}

@test "bud without any arguments should fail when no Dockerfile exists" {
  cd $TEST_SCRATCH_DIR
  run_buildah 125 build --signature-policy ${TEST_SOURCES}/policy.json
  expect_output --substring "no such file or directory"
}

@test "bud with specified context should fail if directory contains no Dockerfile" {
  mkdir -p $TEST_SCRATCH_DIR/empty-dir
  run_buildah 125 build $WITH_POLICY_JSON "$TEST_SCRATCH_DIR"/empty-dir
  expect_output --substring "no such file or directory"
}

@test "bud with specified context should fail if Dockerfile in context directory is actually a file" {
  mkdir -p "$TEST_SCRATCH_DIR"/Dockerfile
  run_buildah 125 build $WITH_POLICY_JSON "$TEST_SCRATCH_DIR"
  expect_output --substring "is not a file"
}

@test "bud with specified context should fail if context directory does not exist" {
  run_buildah 125 build $WITH_POLICY_JSON "$TEST_SCRATCH_DIR"/Dockerfile
  expect_output --substring "no such file or directory"
}

@test "bud with specified context should succeed if context contains existing Dockerfile" {
  _prefetch alpine
  echo "FROM alpine" > $TEST_SCRATCH_DIR/Dockerfile
  run_buildah bud $WITH_POLICY_JSON $TEST_SCRATCH_DIR/Dockerfile
}

@test "bud with specified context should fail if context contains empty Dockerfile" {
  touch $TEST_SCRATCH_DIR/Dockerfile
  run_buildah 125 build $WITH_POLICY_JSON $TEST_SCRATCH_DIR/Dockerfile
  expect_output --substring "no contents in \"$TEST_SCRATCH_DIR/Dockerfile\""
}

@test "bud-no-change" {
  _prefetch alpine
  parent=alpine
  target=no-change-image
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/no-change
  run_buildah inspect --format '{{printf "%q" .FromImageDigest}}' ${parent}
  parentid="$output"
  run_buildah inspect --format '{{printf "%q" .FromImageDigest}}' ${target}
  expect_output "$parentid"
}

@test "bud-no-change-label" {
  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}
  want_output='map["io.buildah.version":"'$buildah_version'" "test":"label"]'

  _prefetch alpine
  parent=alpine
  target=no-change-image
  run_buildah build --label "test=label" $WITH_POLICY_JSON -t ${target} $BUDFILES/no-change
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' ${target}
  expect_output "$want_output"
}

@test "bud-no-change-annotation" {
  _prefetch alpine
  target=no-change-image
  run_buildah build --annotation "test=annotation" $WITH_POLICY_JSON -t ${target} $BUDFILES/no-change
  run_buildah inspect --format '{{index .ImageAnnotations "test"}}' ${target}
  expect_output "annotation"
}

@test "bud-squash-layers" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON --squash $BUDFILES/layers-squash
}

@test "bud-squash-hardlinks" {
  _prefetch busybox
  run_buildah build $WITH_POLICY_JSON --squash $BUDFILES/layers-squash/Dockerfile.hardlinks
}

# Following test must pass for both rootless and rootfull
@test "rootless: support --device and renaming device using bind-mount" {
  _prefetch alpine
  skip_if_in_container # unable to perform mount of /dev/null for test in CI container setup
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << _EOF
FROM alpine
RUN ls /test/dev
_EOF
  run_buildah build $WITH_POLICY_JSON --device /dev/null:/test/dev/null  -t test -f $contextdir/Dockerfile
  expect_output --substring "null"
}

@test "bud with additional directory of devices" {
  skip_if_rootless_environment
  skip_if_chroot
  skip_if_rootless

  _prefetch alpine
  target=alpine-image
  local contextdir=${TEST_SCRATCH_DIR}/foo
  mkdir -p $contextdir
  mknod $contextdir/null c 1 3
  run_buildah build $WITH_POLICY_JSON --device $contextdir:/dev/fuse  -t ${target} -f $BUDFILES/device/Dockerfile $BUDFILES/device
  expect_output --substring "null"
}

@test "bud with additional device" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON --device /dev/fuse -t ${target} -f $BUDFILES/device/Dockerfile $BUDFILES/device
  expect_output --substring "/dev/fuse"
}

@test "bud with Containerfile" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/containerfile
  expect_output --substring "FROM alpine"
}

@test "bud with Containerfile.in, --cpp-flag" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/containerfile/Containerfile.in $BUDFILES/containerfile
  expect_output --substring "Ignoring In file included .* invalid preprocessing directive #This"
  expect_output --substring "FROM alpine"
  expect_output --substring "success"
  expect_output --substring "debug=no" "with no cpp-flag or BUILDAH_CPPFLAGS"

  run_buildah build $WITH_POLICY_JSON -t ${target} --cpp-flag "-DTESTCPPDEBUG" -f $BUDFILES/containerfile/Containerfile.in $BUDFILES/containerfile
  expect_output --substring "Ignoring In file included .* invalid preprocessing directive #This"
  expect_output --substring "FROM alpine"
  expect_output --substring "success"
  expect_output --substring "debug=yes" "with --cpp-flag -DTESTCPPDEBUG"
}

@test "bud with Containerfile.in, via envariable" {
  _prefetch alpine
  target=alpine-image

  BUILDAH_CPPFLAGS="-DTESTCPPDEBUG" run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/containerfile/Containerfile.in $BUDFILES/containerfile
  expect_output --substring "Ignoring In file included .* invalid preprocessing directive #This"
  expect_output --substring "FROM alpine"
  expect_output --substring "success"
  expect_output --substring "debug=yes" "with BUILDAH_CPPFLAGS=-DTESTCPPDEBUG"
}

@test "bud with Dockerfile" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/dockerfile
  expect_output --substring "FROM alpine"
}

@test "bud with Containerfile and Dockerfile" {
  _prefetch alpine
  target=alpine-image
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/containeranddockerfile
  expect_output --substring "FROM alpine"
}

@test "bud-http-context-with-Containerfile" {
  _test_http http-context-containerfile context.tar
}

@test "bud with Dockerfile from stdin" {
  _prefetch alpine
  target=df-stdin
  run_buildah build $WITH_POLICY_JSON -t ${target} - < $BUDFILES/context-from-stdin/Dockerfile
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output

  test -s $root/scratchfile
  run cat $root/scratchfile
  expect_output "stdin-context" "contents of \$root/scratchfile"

  # FROM scratch overrides FROM alpine
  test ! -s $root/etc/alpine-release
}

@test "bud with Dockerfile from stdin tar" {
  _prefetch alpine
  target=df-stdin
  # 'cmd1 < <(cmd2)' == 'cmd2 | cmd1' but runs cmd1 in this shell, not sub.
  run_buildah build $WITH_POLICY_JSON -t ${target} - < <(tar -c -C $BUDFILES/context-from-stdin .)
  run_buildah from --quiet ${target}
  cid=$output
  run_buildah_mount ${cid}
  root=$output

  test -s $root/scratchfile
  run cat $root/scratchfile
  expect_output "stdin-context" "contents of \$root/scratchfile"

  # FROM scratch overrides FROM alpine
  test ! -s $root/etc/alpine-release
}

@test "bud containerfile with args" {
  _prefetch alpine
  target=use-args
  touch $BUDFILES/use-args/abc.txt
  run_buildah build $WITH_POLICY_JSON -t ${target} --build-arg=abc.txt $BUDFILES/use-args
  expect_output --substring "COMMIT use-args"
  run_buildah from --quiet ${target}
  ctrID=$output
  run_buildah run $ctrID ls abc.txt
  expect_output --substring "abc.txt"

  run_buildah build $WITH_POLICY_JSON -t ${target} -f Containerfile.destination --build-arg=testArg=abc.txt --build-arg=destination=/tmp $BUDFILES/use-args
  expect_output --substring "COMMIT use-args"
  run_buildah from --quiet ${target}
  ctrID=$output
  run_buildah run $ctrID ls /tmp/abc.txt
  expect_output --substring "abc.txt"

  run_buildah build $WITH_POLICY_JSON -t ${target} -f Containerfile.dest_nobrace --build-arg=testArg=abc.txt --build-arg=destination=/tmp $BUDFILES/use-args
  expect_output --substring "COMMIT use-args"
  run_buildah from --quiet ${target}
  ctrID=$output
  run_buildah run $ctrID ls /tmp/abc.txt
  expect_output --substring "abc.txt"

  rm $BUDFILES/use-args/abc.txt
}

@test "bud using gitrepo and branch" {
  if ! start_git_daemon ${TEST_SOURCES}/git-daemon/release-1.11-rhel.tar.gz ; then
    skip "error running git daemon"
  fi
  run_buildah build $WITH_POLICY_JSON --layers -t gittarget -f $BUDFILES/shell/Dockerfile git://localhost:${GITPORT}/repo#release-1.11-rhel
}

@test "bud using gitrepo with .git and branch" {
  run_buildah build $WITH_POLICY_JSON --layers -t gittarget -f $BUDFILES/shell/Dockerfile https://github.com/containers/buildah.git#release-1.11-rhel
}

# Fixes #1906: buildah was not detecting changed tarfile
@test "bud containerfile with tar archive in copy" {
  _prefetch busybox
  # First check to verify cache is used if the tar file does not change
  target=copy-archive
  date > $BUDFILES/${target}/test
  tar -C $TEST_SOURCES -cJf $BUDFILES/${target}/test.tar.xz bud/${target}/test
  run_buildah build $WITH_POLICY_JSON --layers -t ${target} $BUDFILES/${target}
  expect_output --substring "COMMIT copy-archive"

  run_buildah build $WITH_POLICY_JSON --layers -t ${target} $BUDFILES/${target}
  expect_output --substring " Using cache"
  expect_output --substring "COMMIT copy-archive"

  # Now test that we do NOT use cache if the tar file changes
  echo This is a change >> $BUDFILES/${target}/test
  tar -C $TEST_SOURCES -cJf $BUDFILES/${target}/test.tar.xz bud/${target}/test
  run_buildah build $WITH_POLICY_JSON --layers -t ${target} $BUDFILES/${target}
  if [[ "$output" =~ " Using cache" ]]; then
      expect_output "[no instance of 'Using cache']" "no cache used"
  fi
  expect_output --substring "COMMIT copy-archive"

  rm -f $BUDFILES/${target}/test*
}

@test "bud pull never" {
  target=pull
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} --pull-never $BUDFILES/pull
  expect_output --substring "busybox: image not known"

  run_buildah 125 build $WITH_POLICY_JSON -t ${target} --pull=false $BUDFILES/pull
  expect_output --substring "busybox: image not known"

  run_buildah build $WITH_POLICY_JSON -t ${target} --pull $BUDFILES/pull
  expect_output --substring "COMMIT pull"

  run_buildah build $WITH_POLICY_JSON -t ${target} --pull=never $BUDFILES/pull
  expect_output --substring "COMMIT pull"
}

@test "bud pull false no local image" {
  target=pull
  run_buildah 125 build $WITH_POLICY_JSON -t ${target} --pull=false $BUDFILES/pull
  expect_output --substring "Error: creating build container: busybox: image not known"
}

@test "bud with Containerfile should fail with nonexistent authfile" {
  target=alpine-image
  run_buildah 125 build --authfile /tmp/nonexistent $WITH_POLICY_JSON -t ${target} $BUDFILES/containerfile
  assert "$output" =~ "Error: credential file is not accessible: (faccessat|stat) /tmp/nonexistent: no such file or directory"
}


@test "bud for multi-stage Containerfile with invalid registry and --authfile as a fd, should fail with no such host" {
  target=alpine-multi-stage-image
  run_buildah 125 build --authfile=<(echo "{ \"auths\": { \"myrepository.example\": { \"auth\": \"$(echo 'username:password' | base64 --wrap=0)\" } } }") -t ${target} --file $BUDFILES/from-invalid-registry/Containerfile
  # Should fail with `no such host` instead of: error reading JSON file "/dev/fd/x"
  expect_output --substring "no such host"
}

@test "bud COPY with URL should fail" {
  local contextdir=${TEST_SCRATCH_DIR}/budcopy
  mkdir $contextdir
  FILE=$contextdir/Dockerfile.url
  /bin/cat <<EOM >$FILE
FROM alpine:latest
COPY https://getfedora.org/index.html .
EOM

  run_buildah 125 build $WITH_POLICY_JSON -t foo -f $contextdir/Dockerfile.url
  expect_output --substring "building .* source can.t be a URL for COPY"
}

@test "bud quiet" {
  _prefetch alpine
  run_buildah build --format docker -t quiet-test $WITH_POLICY_JSON -q $BUDFILES/shell
  expect_line_count 1
  expect_output --substring '^[0-9a-f]{64}$'
}

@test "bud COPY with Env Var in Containerfile" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON -t testctr $BUDFILES/copy-envvar
  run_buildah from testctr
  run_buildah run testctr-working-container ls /file-0.0.1.txt
  run_buildah rm -a

  run_buildah build $WITH_POLICY_JSON --layers -t testctr $BUDFILES/copy-envvar
  run_buildah from testctr
  run_buildah run testctr-working-container ls /file-0.0.1.txt
  run_buildah rm -a
}

@test "bud with custom arch" {
  run_buildah build $WITH_POLICY_JSON \
    -f $BUDFILES/from-scratch/Containerfile \
    -t arch-test \
    --arch=arm

  run_buildah inspect --format "{{ .Docker.Architecture }}" arch-test
  expect_output arm

  run_buildah inspect --format "{{ .OCIv1.Architecture }}" arch-test
  expect_output arm
}

@test "bud with custom os" {
  run_buildah build $WITH_POLICY_JSON \
    -f $BUDFILES/from-scratch/Containerfile \
    -t os-test \
    --os=windows

  run_buildah inspect --format "{{ .Docker.OS }}" os-test
  expect_output windows

  run_buildah inspect --format "{{ .OCIv1.OS }}" os-test
  expect_output windows
}

@test "bud with custom os-version" {
  run_buildah build $WITH_POLICY_JSON \
    -f $BUDFILES/from-scratch/Containerfile \
    -t os-version-test \
    --os-version=1.0

  run_buildah inspect --format "{{ .Docker.OSVersion }}" os-version-test
  expect_output 1.0

  run_buildah inspect --format "{{ .OCIv1.OSVersion }}" os-version-test
  expect_output 1.0
}

@test "bud with custom os-features" {
  run_buildah build $WITH_POLICY_JSON \
    -f $BUDFILES/from-scratch/Containerfile \
    -t os-features-test \
    --os-feature removed --os-feature removed- --os-feature win32k

  run_buildah inspect --format "{{ .Docker.OSFeatures }}" os-features-test
  expect_output '[win32k]'

  run_buildah inspect --format "{{ .OCIv1.OSFeatures }}" os-features-test
  expect_output '[win32k]'
}

@test "bud with custom platform" {
  run_buildah build $WITH_POLICY_JSON \
    -f $BUDFILES/from-scratch/Containerfile \
    -t platform-test \
    --platform=windows/arm

  run_buildah inspect --format "{{ .Docker.OS }}" platform-test
  expect_output windows

  run_buildah inspect --format "{{ .OCIv1.OS }}" platform-test
  expect_output windows

  run_buildah inspect --format "{{ .Docker.Architecture }}" platform-test
  expect_output arm

  run_buildah inspect --format "{{ .OCIv1.Architecture }}" platform-test
  expect_output arm
}

@test "bud with custom platform and empty os or arch" {
  run_buildah build $WITH_POLICY_JSON \
    -f $BUDFILES/from-scratch/Containerfile \
    -t platform-test \
    --platform=windows/

  run_buildah inspect --format "{{ .Docker.OS }}" platform-test
  expect_output windows

  run_buildah inspect --format "{{ .OCIv1.OS }}" platform-test
  expect_output windows

  run_buildah build $WITH_POLICY_JSON \
    -f $BUDFILES/from-scratch/Containerfile \
    -t platform-test2 \
    --platform=/arm

  run_buildah inspect --format "{{ .Docker.Architecture }}" platform-test2
  expect_output arm

  run_buildah inspect --format "{{ .OCIv1.Architecture }}" platform-test2
  expect_output arm
}

@test "bud Add with linked tarball" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON -f $BUDFILES/symlink/Containerfile.add-tar-with-link -t testctr $BUDFILES/symlink
  run_buildah from testctr
  run_buildah run testctr-working-container ls /tmp/testdir/testfile.txt
  run_buildah rm -a
  run_buildah rmi -a -f

  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON -f $BUDFILES/symlink/Containerfile.add-tar-gz-with-link -t testctr $BUDFILES/symlink
  run_buildah from testctr
  run_buildah run testctr-working-container ls /tmp/testdir/testfile.txt
  run_buildah rm -a
  run_buildah rmi -a -f
}

@test "bud file above context directory" {
  run_buildah 125 build $WITH_POLICY_JSON -t testctr $BUDFILES/context-escape-dir/testdir
  expect_output --substring "escaping context directory error"
}

@test "bud-multi-stage-args-scope" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON --layers -t multi-stage-args --build-arg SECRET=secretthings -f Dockerfile.arg $BUDFILES/multi-stage-builds
  run_buildah from --name test-container multi-stage-args
  run_buildah run test-container -- cat test_file
  expect_output ""
}

@test "bud-multi-stage-args-history" {
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON --layers -t multi-stage-args --build-arg SECRET=secretthings -f Dockerfile.arg $BUDFILES/multi-stage-builds
  run_buildah inspect --format '{{range .History}}{{println .CreatedBy}}{{end}}' multi-stage-args
  run grep "secretthings" <<< "$output"
  expect_output ""

  run_buildah inspect --format '{{range .OCIv1.History}}{{println .CreatedBy}}{{end}}' multi-stage-args
  run grep "secretthings" <<< "$output"
  expect_output ""

  run_buildah inspect --format '{{range .Docker.History}}{{println .CreatedBy}}{{end}}' multi-stage-args
  run grep "secretthings" <<< "$output"
  expect_output ""
}

@test "bud-implicit-no-history" {
  _prefetch busybox
  local ocidir=${TEST_SCRATCH_DIR}/oci
  mkdir -p $ocidir/blobs/sha256
  # Build an image config and image manifest in parallel
  local configos=$(${BUILDAH_BINARY} info --format '{{.host.os}}')
  local configarch=$(${BUILDAH_BINARY} info --format '{{.host.arch}}')
  local configvariant=$(${BUILDAH_BINARY} info --format '{{.host.variant}}')
  local configvariantkv=${configvariant:+'"variant": "'${configvariant}'", '}
  echo '{"architecture": "'"${configarch}"'", "os": "'"${configos}"'", '"${configvariantkv}"'"rootfs": {"type": "layers", "diff_ids": [' > ${TEST_SCRATCH_DIR}/config.json
  echo '{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "layers": [' > ${TEST_SCRATCH_DIR}/manifest.json
  # Create some layers
  for layer in $(seq 8) ; do
    # Content for the layer
    createrandom ${TEST_SCRATCH_DIR}/file$layer $((RANDOM+1024))
    # Layer blob
    tar -c -C ${TEST_SCRATCH_DIR} -f ${TEST_SCRATCH_DIR}/layer$layer.tar file$layer
    # Get the layer blob's digest and size
    local diffid=$(sha256sum ${TEST_SCRATCH_DIR}/layer$layer.tar)
    local diffsize=$(wc -c ${TEST_SCRATCH_DIR}/layer$layer.tar)
    # Link the blob into where an OCI layout would put it.
    ln ${TEST_SCRATCH_DIR}/layer$layer.tar $ocidir/blobs/sha256/${diffid%% *}
    # Try to keep the resulting files at least kind of readable.
    if test $layer -gt 1 ; then
      echo "," >> ${TEST_SCRATCH_DIR}/config.json
      echo "," >> ${TEST_SCRATCH_DIR}/manifest.json
    fi
    # Add the layer to the config blob's list of diffIDs for its rootfs.
    echo -n '  "sha256:'${diffid%% *}'"' >> ${TEST_SCRATCH_DIR}/config.json
    # Add the layer blob to the manifest's list of blobs.
    echo -n '  {"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": "sha256:'${diffid%% *}'", "size": '${diffsize%% *}'}' >> ${TEST_SCRATCH_DIR}/manifest.json
  done
  # Finish the diffID and layer blob lists.
  echo >> ${TEST_SCRATCH_DIR}/config.json
  echo >> ${TEST_SCRATCH_DIR}/manifest.json
  # Finish the config blob with some boilerplate stuff.
  echo ']}, "config": { "Cmd": ["/bin/sh"], "Env": [ "PATH=/usr/local/sbin:/usr/sbin:/sbin:/usr/local/bin:/usr/bin:/bin" ]}}' >> ${TEST_SCRATCH_DIR}/config.json
  # Compute the config blob's digest and size, so that we can list it in the manifest.
  local configsize=$(wc -c ${TEST_SCRATCH_DIR}/config.json)
  local configdigest=$(sha256sum ${TEST_SCRATCH_DIR}/config.json)
  # Finish the manifest with information about the config blob.
  echo '], "config": { "mediaType": "application/vnd.oci.image.config.v1+json", "digest": "sha256:'${configdigest%% *}'", "size": '${configsize%% *}'}}' >> ${TEST_SCRATCH_DIR}/manifest.json
  # Compute the manifest's digest and size, so that we can list it in the OCI layout index.
  local manifestsize=$(wc -c ${TEST_SCRATCH_DIR}/manifest.json)
  local manifestdigest=$(sha256sum ${TEST_SCRATCH_DIR}/manifest.json)
  # Link the config blob and manifest into where an OCI layout would put them.
  ln ${TEST_SCRATCH_DIR}/config.json $ocidir/blobs/sha256/${configdigest%% *}
  ln ${TEST_SCRATCH_DIR}/manifest.json $ocidir/blobs/sha256/${manifestdigest%% *}
  # Write the layout index with just the one image manifest in it.
  echo '{"schemaVersion": 2, "manifests": [ {"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": "sha256:'${manifestdigest%% *}'", "size": '${manifestsize%% *}' } ]}' > $ocidir/index.json
  # Write the "this is an OCI layout directory" identifier.
  echo '{"imageLayoutVersion":"1.0.0"}' > $ocidir/oci-layout
  # Import the image from the OCI layout into buildah's normal storage.
  run_buildah pull --log-level=debug $WITH_POLICY_JSON oci:$ocidir
  # Tag the image (we know its ID is the config blob digest, since it's an OCI
  # image) with the name the Dockerfile will specify as its base image.
  run_buildah tag ${configdigest%% *} fakeregistry.podman.invalid/notreal
  # Double-check that the image has no history, which is what we wanted to get
  # out of all of this.
  run_buildah inspect --format '{{.History}}' fakeregistry.podman.invalid/notreal
  assert "${lines}" == '[]'  "base image generated for test had history field that was not an empty slice"
  # Build images using our image-with-no-history as a base, to check that we
  # don't trip over ourselves when doing so.
  run_buildah build $WITH_POLICY_JSON --pull=never --layers=false $BUDFILES/no-history
  run_buildah build $WITH_POLICY_JSON --pull=never --layers=true  $BUDFILES/no-history
}

@test "bud with encrypted FROM image" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/tmp
  mkdir $contextdir
  openssl genrsa -out $contextdir/mykey.pem 1024
  openssl genrsa -out $contextdir/mykey2.pem 1024
  openssl rsa -in $contextdir/mykey.pem -pubout > $contextdir/mykey.pub
  start_registry
  run_buildah push $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword --encryption-key jwe:$contextdir/mykey.pub busybox docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest

  target=busybox-image
  echo FROM localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest > $contextdir/Dockerfile

  # Try to build from encrypted image without key
  run_buildah 125 build $WITH_POLICY_JSON --tls-verify=false  --creds testuser:testpassword -t ${target} -f $contextdir/Dockerfile
  assert "$output" =~ "does not match config's DiffID"

  # Try to build from encrypted image with wrong key
  run_buildah 125 build $WITH_POLICY_JSON --tls-verify=false  --creds testuser:testpassword --decryption-key $contextdir/mykey2.pem -t ${target} -f $contextdir/Dockerfile
  assert "$output" =~ "no suitable key found for decrypting layer key"
  assert "$output" =~ "- JWE: No suitable private key found for decryption"

  # Try to build with the correct key
  run_buildah build $WITH_POLICY_JSON --tls-verify=false  --creds testuser:testpassword --decryption-key $contextdir/mykey.pem -t ${target} -f $contextdir/Dockerfile
  assert "$output" =~ "Successfully tagged localhost:$REGISTRY_PORT/"

  rm -rf $contextdir
}

@test "bud with --build-arg" {
  _prefetch alpine busybox
  target=busybox-image

  # Envariable not present at all
  run_buildah --log-level "warn" bud $WITH_POLICY_JSON -t ${target} $BUDFILES/build-arg
  expect_output --substring 'missing \\"foo\\" build argument. Try adding'

  # Envariable explicitly set on command line
  run_buildah build $WITH_POLICY_JSON -t ${target} --build-arg foo=bar $BUDFILES/build-arg
  assert "${lines[3]}" = "bar"

  # Envariable from environment
  export foo=$(random_string 20)
  run_buildah build $WITH_POLICY_JSON -t ${target} --build-arg foo $BUDFILES/build-arg
  assert "${lines[3]}" = "$foo"
}

@test "bud arg and env var with same name" {
  _prefetch busybox
  # Regression test for https://github.com/containers/buildah/issues/2345
  run_buildah build $WITH_POLICY_JSON -t testctr $BUDFILES/dupe-arg-env-name
  expect_output --substring "https://example.org/bar"
}

@test "bud copy chown with newuser" {
  _prefetch $SAFEIMAGE
  # Regression test for https://github.com/containers/buildah/issues/2192
  run_buildah build $WITH_POLICY_JSON -t testctr \
              --build-arg SAFEIMAGE=$SAFEIMAGE \
              -f $BUDFILES/copy-chown/Containerfile.chown_user $BUDFILES/copy-chown
  expect_output --substring "myuser:myuser"
}

@test "bud-builder-identity" {
  _prefetch alpine
  parent=alpine
  target=no-change-image
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
  run_buildah --version
  local -a output_fields=($output)
  buildah_version=${output_fields[2]}

  run_buildah inspect --format '{{ index .Docker.Config.Labels "io.buildah.version"}}' $target
  expect_output "$buildah_version"
}

@test "bud run check --from with arg" {
  skip_if_no_runtime

  find_oci_runtime

  ${OCI} --version
  _prefetch alpine busybox

  run_buildah build --build-arg base=alpine --build-arg toolchainname=busybox --build-arg destinationpath=/tmp --pull=false $WITH_POLICY_JSON -f $BUDFILES/from-with-arg/Containerfile .
  expect_output --substring "FROM alpine"
  expect_output --substring 'STEP 4/4: COPY --from=\$\{toolchainname\} \/ \$\{destinationpath\}'
  run_buildah rm -a
}

@test "bud preserve rootfs for --mount=type=bind,from=" {
  _prefetch alpine
  run_buildah build --build-arg NONCE="$(date)" --layers --pull=false $WITH_POLICY_JSON -f Dockerfile.3 $BUDFILES/cache-stages
  expect_output --substring "Worked"
}

@test "bud timestamp" {
  _prefetch alpine
  timestamp=40
  run_buildah build --timestamp=${timestamp} --quiet --pull=false $WITH_POLICY_JSON -t timestamp -f Dockerfile.1 $BUDFILES/cache-stages
  cid=$output
  run_buildah inspect --format '{{ .Docker.Created }}' timestamp
  expect_output --substring "1970-01-01"
  run_buildah inspect --format '{{ .OCIv1.Created }}' timestamp
  expect_output --substring "1970-01-01"
  run_buildah inspect --format '{{ .History }}' timestamp
  expect_output --substring "1970-01-01 00:00:${timestamp}"

  run_buildah from --quiet --pull=false $WITH_POLICY_JSON timestamp
  cid=$output
  run_buildah run $cid ls -l /tmpfile
  expect_output --substring "1970"

  run_buildah images --format "{{.Created}}" timestamp
  expect_output ${timestamp}

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "bud timestamp compare" {
  _prefetch alpine
  TIMESTAMP=$(date '+%s')
  run_buildah build --timestamp=${TIMESTAMP} --quiet --pull=false $WITH_POLICY_JSON -t timestamp -f Dockerfile.1 $BUDFILES/cache-stages
  cid=$output

  run_buildah images --format "{{.Created}}" timestamp
  expect_output ${timestamp}

  run_buildah build --timestamp=${TIMESTAMP} --quiet --pull=false $WITH_POLICY_JSON -t timestamp -f Dockerfile.1 $BUDFILES/cache-stages
  expect_output "$cid"

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "bud with-rusage" {
  _prefetch alpine
  run_buildah build --log-rusage --layers --pull=false --format docker $WITH_POLICY_JSON $BUDFILES/shell
  cid=$output
  # expect something that looks like it was formatted using pkg/rusage.FormatDiff()
  expect_output --substring ".*\(system\).*\(user\).*\(elapsed\).*input.*output"
}

@test "bud with-rusage-logfile" {
  _prefetch alpine
  run_buildah build --log-rusage --rusage-logfile ${TEST_SCRATCH_DIR}/foo.log --layers --pull=false --format docker $WITH_POLICY_JSON $BUDFILES/shell
  # the logfile should exist
  if [ ! -e ${TEST_SCRATCH_DIR}/foo.log ]; then die "rusage-logfile foo.log did not get created!"; fi
  # expect that foo.log only contains lines that were formatted using pkg/rusage.FormatDiff()
  formatted_lines=$(grep ".*\(system\).*\(user\).*\(elapsed\).*input.*output" ${TEST_SCRATCH_DIR}/foo.log | wc -l)
  line_count=$(wc -l <${TEST_SCRATCH_DIR}/foo.log)
  if [[ "$formatted_lines" -ne "$line_count" ]]; then
      die "Got ${formatted_lines} lines formatted with pkg/rusage.FormatDiff() but rusage-logfile has ${line_count} lines"
  fi
}

@test "bud-caching-from-scratch" {
  _prefetch alpine
  # run the build once
  run_buildah build --quiet --layers --pull=false --format docker $WITH_POLICY_JSON $BUDFILES/cache-scratch
  iid="$output"

  # now run it again - the cache should give us the same final image ID
  run_buildah build --quiet --layers --pull=false --format docker $WITH_POLICY_JSON $BUDFILES/cache-scratch
  assert "$output" = "$iid"

  # now run it *again*, except with more content added at an intermediate step, which should invalidate the cache
  run_buildah build --quiet --layers --pull=false --format docker $WITH_POLICY_JSON -f Dockerfile.different1 $BUDFILES/cache-scratch
  assert "$output" !~ "$iid"

  # now run it *again* again, except with more content added at an intermediate step, which should invalidate the cache
  run_buildah build --quiet --layers --pull=false --format docker $WITH_POLICY_JSON -f Dockerfile.different2 $BUDFILES/cache-scratch
  assert "$output" !~ "$iid"
}

@test "bud-caching-from-scratch-config" {
  _prefetch alpine
  # run the build once
  run_buildah build --quiet --layers --pull=false --format docker $WITH_POLICY_JSON -f Dockerfile.config $BUDFILES/cache-scratch
  iid="$output"

  # now run it again - the cache should give us the same final image ID
  run_buildah build --quiet --layers --pull=false --format docker $WITH_POLICY_JSON -f Dockerfile.config $BUDFILES/cache-scratch
  assert "$output" = "$iid"

  # now run it *again*, except with more content added at an intermediate step, which should invalidate the cache
  run_buildah build --quiet --layers --pull=false --format docker $WITH_POLICY_JSON -f Dockerfile.different1 $BUDFILES/cache-scratch
  assert "$output" !~ "$iid"

  # now run it *again* again, except with more content added at an intermediate step, which should invalidate the cache
  run_buildah build --quiet --layers --pull=false --format docker $WITH_POLICY_JSON -f Dockerfile.different2 $BUDFILES/cache-scratch
  assert "$output" !~ "$iid"
}

@test "bud capabilities test" {
  _prefetch busybox
  # something not enabled by default in containers.conf
  run_buildah build --cap-add cap_sys_ptrace -t testcap $WITH_POLICY_JSON -f $BUDFILES/capabilities/Dockerfile
  expect_output --substring "uid=3267"
  expect_output --substring "CapBnd:	00000000a80c25fb"
  expect_output --substring "CapEff:	0000000000000000"

  # some things enabled by default in containers.conf
  run_buildah build --cap-drop cap_chown,cap_dac_override,cap_fowner -t testcapd $WITH_POLICY_JSON -f $BUDFILES/capabilities/Dockerfile
  expect_output --substring "uid=3267"
  expect_output --substring "CapBnd:	00000000a80425f0"
  expect_output --substring "CapEff:	0000000000000000"
}

@test "bud does not gobble stdin" {
  _prefetch alpine

  ctxdir=${TEST_SCRATCH_DIR}/bud
  mkdir -p $ctxdir
  cat >$ctxdir/Dockerfile <<EOF
FROM alpine
RUN true
EOF

  random_msg=$(head -10 /dev/urandom | tr -dc a-zA-Z0-9 | head -c12)

  # Prior to #2708, buildah bud would gobble up its stdin even if it
  # didn't actually use it. This prevented the use of 'cmdlist | bash';
  # if 'buildah bud' was in cmdlist, everything past it would be lost.
  #
  # This is ugly but effective: it checks that buildah passes stdin untouched.
  passthru=$(echo "$random_msg" | (run_buildah build --quiet $WITH_POLICY_JSON -t stdin-test ${ctxdir} >/dev/null; cat))

  expect_output --from="$passthru" "$random_msg" "stdin was passed through"
}

@test "bud cache by format" {
  _prefetch alpine

  # Build first in Docker format.  Whether we do OCI or Docker first shouldn't matter, so we picked one.
  run_buildah build --iidfile ${TEST_SCRATCH_DIR}/first-docker  --metadata-file ${TEST_SCRATCH_DIR}/first-docker.meta  --format docker --layers --quiet $WITH_POLICY_JSON $BUDFILES/cache-format

  # Build in OCI format.  Cache should not reuse the same images, so we should get a different image ID.
  run_buildah build --iidfile ${TEST_SCRATCH_DIR}/first-oci     --metadata-file ${TEST_SCRATCH_DIR}/first-oci.meta     --format oci    --layers --quiet $WITH_POLICY_JSON $BUDFILES/cache-format

  # Build in Docker format again.  Cache traversal should 100% hit the Docker image, so we should get its image ID.
  run_buildah build --iidfile ${TEST_SCRATCH_DIR}/second-docker --metadata-file ${TEST_SCRATCH_DIR}/second-docker.meta --format docker --layers --quiet $WITH_POLICY_JSON $BUDFILES/cache-format

  # Build in OCI format again.  Cache traversal should 100% hit the OCI image, so we should get its image ID.
  run_buildah build --iidfile ${TEST_SCRATCH_DIR}/second-oci    --metadata-file ${TEST_SCRATCH_DIR}/second-oci.meta    --format oci    --layers --quiet $WITH_POLICY_JSON $BUDFILES/cache-format

  # Compare them.  The two images we built in Docker format should be the same, the two we built in OCI format
  # should be the same, but the OCI and Docker format images should be different.
  assert "$(< ${TEST_SCRATCH_DIR}/first-docker)" = "$(< ${TEST_SCRATCH_DIR}/second-docker)" \
         "iidfile(first docker) == iidfile(second docker)"
  assert "$(< ${TEST_SCRATCH_DIR}/first-oci)" = "$(< ${TEST_SCRATCH_DIR}/second-oci)" \
         "iidfile(first oci) == iidfile(second oci)"

  assert "$(< ${TEST_SCRATCH_DIR}/first-docker)" != "$(< ${TEST_SCRATCH_DIR}/first-oci)" \
         "iidfile(first docker) != iidfile(first oci)"

  assert "$(< ${TEST_SCRATCH_DIR}/first-docker.meta)" = "$(< ${TEST_SCRATCH_DIR}/second-docker.meta)" \
         "metadata(first docker) == metadata(second docker)"
  assert "$(< ${TEST_SCRATCH_DIR}/first-oci.meta)" = "$(< ${TEST_SCRATCH_DIR}/second-oci.meta)" \
         "metadata(first oci) == metadata(second oci)"

  assert "$(< ${TEST_SCRATCH_DIR}/first-docker.meta)" != "$(< ${TEST_SCRATCH_DIR}/first-oci.meta)" \
         "metadata(first docker) != metadata(first oci)"

  jq . ${TEST_SCRATCH_DIR}/first-docker.meta
  jq . ${TEST_SCRATCH_DIR}/first-oci.meta
  assert "$(wc -c < ${TEST_SCRATCH_DIR}/first-docker.meta)" -gt 2
  assert "$(wc -c < ${TEST_SCRATCH_DIR}/first-oci.meta)" -gt 2
}

@test "bud cache add-copy-chown" {
  contentdir=${TEST_SCRATCH_DIR}/content
  mkdir -p ${contentdir}
  echo sure, this counts as a readme file > ${TEST_SCRATCH_DIR}/content/README.md
  starthttpd $contentdir
  # Build each variation of COPY (from context, from previous stage) and ADD (from context, not overriding an archive, URL) twice.
  # Each second build should produce an image with the same ID as the first build, because the cache matches, but they should
  # otherwise all be different.
  local actions="copy prev add tar url";
  for i in 1 2 3; do
    for action in $actions; do
      # iidfiles are 1 2 3, but dockerfiles are only 1 2 then back to 1
      iidfile=${TEST_SCRATCH_DIR}/${action}${i}
      containerfile=Dockerfile.${action}$(((i-1) % 2 + 1))

      run_buildah build --build-arg=HTTP_SERVER_PORT=${HTTP_SERVER_PORT} --iidfile $iidfile --layers --quiet $WITH_POLICY_JSON -f $containerfile $BUDFILES/cache-chown
    done
  done

  for action in $actions; do
    # The third round of builds should match all of the first rounds by way
    # of caching.
    assert "$(< ${TEST_SCRATCH_DIR}/${action}1)" = "$(< ${TEST_SCRATCH_DIR}/${action}3)" \
           "iidfile(${action}1) = iidfile(${action}3)"

    # The second round of builds should not match the first rounds, since
    # the different ownership makes the changes look different to the cache,
    # except for cases where we extract an archive, where --chown is ignored.
    local op="!="
    if [[ $action = "tar" ]]; then
      op="=";
    fi
    assert "$(< ${TEST_SCRATCH_DIR}/${action}1)" $op "$(< ${TEST_SCRATCH_DIR}/${action}2)" \
           "iidfile(${action}1) $op iidfile(${action}2)"

    # The first rounds of builds should all be different from each other,
    # as a sanity thing.
    for other in $actions; do
      if [[ $other != $action ]]; then
        assert "$(< ${TEST_SCRATCH_DIR}/${action}1)" != "$(< ${TEST_SCRATCH_DIR}/${other}1)" \
               "iidfile(${action}1) != iidfile(${other}1)"
      fi
    done
  done
}

@test "bud-terminal" {
  _prefetch busybox
  run_buildah build $BUDFILES/terminal
}

@test "bud --ignorefile containerignore" {
  _prefetch alpine busybox

  CONTEXTDIR=${TEST_SCRATCH_DIR}/dockerignore
  cp -r $BUDFILES/dockerignore ${CONTEXTDIR}
  mv ${CONTEXTDIR}/.dockerignore ${TEST_SCRATCH_DIR}/containerignore

  run_buildah build -t testbud $WITH_POLICY_JSON -f ${CONTEXTDIR}/Dockerfile.succeed --ignorefile  ${TEST_SCRATCH_DIR}/containerignore  ${CONTEXTDIR}

  run_buildah from --name myctr testbud

  run_buildah 1 run myctr ls -l test1.txt
  expect_output --substring "ls: test1.txt: No such file or directory"

  run_buildah run myctr ls -l test2.txt

  run_buildah 1 run myctr ls -l sub1.txt
  expect_output --substring "ls: sub1.txt: No such file or directory"

  run_buildah 1 run myctr ls -l sub2.txt
  expect_output --substring "ls: sub2.txt: No such file or directory"

  run_buildah 1 run myctr ls -l subdir/
  expect_output --substring "ls: subdir/: No such file or directory"
}

@test "bud with network options" {
  skip_if_chroot
  _prefetch alpine
  target=alpine-image

  run_buildah build --network=none $WITH_POLICY_JSON -t ${target} $BUDFILES/containerfile
  expect_output --substring "FROM alpine"

  run_buildah build --network=private $WITH_POLICY_JSON -t ${target} $BUDFILES/containerfile
  expect_output --substring "FROM alpine"

  run_buildah build --network=container $WITH_POLICY_JSON -t ${target} $BUDFILES/containerfile
  expect_output --substring "FROM alpine"
}

@test "bud-replace-from-in-containerfile" {
  _prefetch alpine busybox
  # override the first FROM (fedora) image in the Containerfile
  # with alpine, leave the second (busybox) alone.
  run_buildah build $WITH_POLICY_JSON --from=alpine $BUDFILES/build-with-from
  expect_output --substring "\[1/2] STEP 1/1: FROM alpine AS builder"
  expect_output --substring "\[2/2] STEP 1/2: FROM busybox"
}

@test "bud test no --stdin" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN read -t 1 x && echo test got \<\$x\>
RUN touch /tmp/done
_EOF

  # fail without --stdin
  run_buildah 1 bud -t testbud $WITH_POLICY_JSON ${mytmpdir} <<< input
  expect_output --substring "building .*: exit status 1"

  run_buildah build --stdin -t testbud $WITH_POLICY_JSON ${mytmpdir} <<< input
  expect_output --substring "test got <input>"
}

@test "bud with --arch flag" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
cat > $mytmpdir/Containerfile << _EOF
FROM alpine
#RUN arch
_EOF

  run_buildah build --arch=arm64 -t arch-test $WITH_POLICY_JSON ${mytmpdir} <<< input
# expect_output --substring "aarch64"

#  run_buildah from --quiet --pull=false $WITH_POLICY_JSON arch-test
#  cid=$output
#  run_buildah run $cid arch
#  expect_output --substring "aarch64"
}

@test "bud with --manifest flag new manifest" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
cat > $mytmpdir/Containerfile << _EOF
from alpine
run echo hello
_EOF

  run_buildah build -q --manifest=testlist -t arch-test $WITH_POLICY_JSON ${mytmpdir} <<< input
  cid=$output
  run_buildah images
  expect_output --substring testlist

  run_buildah inspect --format '{{ .FromImageDigest }}' $cid
  digest=$output

  run_buildah manifest inspect testlist
  expect_output --substring $digest
}

@test "bud with --manifest flag existing manifest" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
cat > $mytmpdir/Containerfile << _EOF
from alpine
run echo hello
_EOF

  run_buildah manifest create testlist

  run_buildah build -q --manifest=testlist -t arch-test $WITH_POLICY_JSON ${mytmpdir} <<< input
  cid=$output
  run_buildah images
  expect_output --substring testlist

  run_buildah inspect --format '{{ .FromImageDigest }}' $cid
  digest=$output

  run_buildah manifest inspect testlist
  expect_output --substring $digest
}

@test "bud test empty newdir" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
cat > $mytmpdir/Containerfile << _EOF
FROM alpine as galaxy

RUN mkdir -p /usr/share/ansible/roles /usr/share/ansible/collections
RUN echo "bar"
RUN echo "foo" > /usr/share/ansible/collections/file.txt

FROM galaxy

RUN mkdir -p /usr/share/ansible/roles /usr/share/ansible/collections
COPY --from=galaxy /usr/share/ansible/roles /usr/share/ansible/roles
COPY --from=galaxy /usr/share/ansible/collections /usr/share/ansible/collections
_EOF

  run_buildah build --layers $WITH_POLICY_JSON -t testbud $mytmpdir
  expect_output --substring "COPY --from=galaxy /usr/share/ansible/collections /usr/share/ansible/collections"
}

@test "bud retain intermediary image" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
cat > $mytmpdir/Containerfile.a << _EOF
FROM alpine
LABEL image=a
RUN echo foo
_EOF

cat > $mytmpdir/Containerfile.b << _EOF
FROM image-a
FROM scratch
_EOF

  run_buildah build -f Containerfile.a -q --manifest=testlist -t image-a $WITH_POLICY_JSON ${mytmpdir} <<< input
  cid=$output
  run_buildah images -f "label=image=a"
  expect_output --substring image-a

  run_buildah build -f Containerfile.b -q --manifest=testlist -t image-b $WITH_POLICY_JSON ${mytmpdir} <<< input
  cid=$output
  run_buildah images
  expect_output --substring image-a
}

@test "bud --pull=ifmissing --arch test" {
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
cat > $mytmpdir/Containerfile << _EOF
FROM $SAFEIMAGE
_EOF
  run_buildah build --pull=ifmissing -q --arch=amd64 -t image-amd $WITH_POLICY_JSON ${mytmpdir}
  run_buildah inspect --format '{{ .OCIv1.Architecture }}' image-amd
  expect_output amd64

  # Tag the image to localhost/safeimage to make sure that the image gets
  # pulled since the local one does not match the requested architecture.
  run_buildah tag image-amd localhost/${SAFEIMAGE_NAME}:${SAFEIMAGE_TAG}
  run_buildah build --pull=ifmissing -q --arch=arm64 -t image-arm $WITH_POLICY_JSON ${mytmpdir}
  run_buildah inspect --format '{{ .OCIv1.Architecture }}' image-arm
  expect_output arm64

  run_buildah inspect --format '{{ .FromImageID }}' image-arm
  fromiid=$output

  run_buildah inspect --format '{{ .OCIv1.Architecture  }}'  $fromiid
  expect_output arm64
}

@test "bud --file with directory" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir1
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
_EOF

  run_buildah 125 build -t testbud $WITH_POLICY_JSON --file ${mytmpdir} .
}

@test "bud --authfile" {
  _prefetch alpine
  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}
  run_buildah push $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth alpine docker://localhost:${REGISTRY_PORT}/buildah/alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
FROM localhost:${REGISTRY_PORT}/buildah/alpine
RUN touch /test
_EOF
  run_buildah build -t myalpine --authfile ${TEST_SCRATCH_DIR}/test.auth --tls-verify=false $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  run_buildah rmi localhost:${REGISTRY_PORT}/buildah/alpine
  run_buildah rmi myalpine
}

@test "build verify cache behaviour with --cache-ttl" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
RUN touch hello
RUN echo world
_EOF

  # Build with --timestamp somewhere in the past
  run_buildah build $WITH_POLICY_JSON --timestamp 1628099045 --layers -t source -f $contextdir/Dockerfile1
  # Specify --cache-ttl 0.5s and cache should
  # not be used since cached image is created
  # with timestamp somewhere in past ( in ~2021 )
  run_buildah build $WITH_POLICY_JSON --cache-ttl=0.5s --layers -t source -f $contextdir/Dockerfile1
  # Should not contain `Using cache` since all
  # cached layers are 1s old.
  assert "$output" !~ "Using cache"
  # clean all images and cache
  run_buildah rmi --all -f
  _prefetch alpine
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile1
  # Cache should be used since our ttl is 1h but
  # cache layers are just built so they should be
  # few seconds old.
  run_buildah build $WITH_POLICY_JSON --cache-ttl=1h --layers -t source -f $contextdir/Dockerfile1
  # must use already cached images.
  expect_output --substring "Using cache"
}

@test "build verify cache must reuse most recent layer in conflict" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
RUN touch hello
RUN echo world
_EOF

  # Populate cache
  run_buildah build $WITH_POLICY_JSON --layers -t source -f $contextdir/Dockerfile1
  # Populate cache again with new layers
  run_buildah build $WITH_POLICY_JSON --layers --cache-ttl=.01s --iidfile ${TEST_SCRATCH_DIR}/image1.txt -t source -f $contextdir/Dockerfile1
  # Should not contain `Using cache` since all
  # cached layers are older than .01s.
  assert "$output" !~ "Using cache"
  # Build again use cache and make sure most recently built image is reused
  run_buildah build $WITH_POLICY_JSON --layers --iidfile ${TEST_SCRATCH_DIR}/image2.txt -t source -f $contextdir/Dockerfile1
  # Must use cache
  expect_output --substring "Using cache"
  # image id should be same as the most recently built image which already exists in cache.
  cmp ${TEST_SCRATCH_DIR}/image1.txt ${TEST_SCRATCH_DIR}/image2.txt
}

@test "build verify cache behaviour with --cache-ttl=0s" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile1 << _EOF
FROM alpine
RUN touch hello
RUN echo world
_EOF

  # Build with --timestamp somewhere in the past
  run_buildah build $WITH_POLICY_JSON --timestamp 1628099045 --layers -t source -f $contextdir/Dockerfile1
  # Specify --cache-ttl 0.5s and cache should
  # not be used since cached image is created
  # with timestamp somewhere in past ( in ~2021 )
  run_buildah --log-level debug build $WITH_POLICY_JSON --cache-ttl=0 --layers -t source -f $contextdir/Dockerfile1
  # Should not contain `Using cache` since all
  # cached layers are 1s old.
  assert "$output" !~ "Using cache"
  expect_output --substring "Setting --no-cache=true"
}

@test "build test pushing and pulling from multiple remote cache sources" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  echo something > ${mytmpdir}/somefile
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN echo hello
RUN echo world
RUN touch hello
ADD somefile somefile

FROM alpine
RUN echo hello
COPY --from=0 hello hello
_EOF

  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}

  # ------ Test case ------ #
  # prepare expected output beforehand
  # must push cache twice i.e for first step and second step
  run printf "STEP 2/5: RUN echo hello\nhello\n--> Pushing cache"
  step1=$output
  run printf "STEP 3/5: RUN echo world\nworld\n--> Pushing cache"
  step2=$output
  run printf "STEP 4/5: RUN touch hello\n--> Pushing cache"
  step3=$output
  run printf "STEP 5/5: ADD somefile somefile\n--> Pushing cache"
  step4=$output
  # First run step in second stage should not be pushed since its already pushed
  run printf "STEP 2/3: RUN echo hello\n--> Using cache"
  step5=$output
  # Last step is `COPY --from=0 hello hello' so it must be committed and pushed
  # actual output is `[2/2] STEP 3/3: COPY --from=0 hello hello\n[2/2] COMMIT test\n-->Pushing cache`
  # but lets match smaller suffix
  run printf "COMMIT test\n--> Pushing cache"
  step6=$output

  # actually run build
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-to localhost:${REGISTRY_PORT}/temp2 --cache-to localhost:${REGISTRY_PORT}/temp -t test -f ${mytmpdir}/Containerfile ${mytmpdir}
  expect_output --substring "$step1"
  expect_output --substring "$step2"
  expect_output --substring "$step3"
  expect_output --substring "$step4"
  expect_output --substring "$step5"
  expect_output --substring "$step6"

  # clean all cache and intermediate images
  # to make sure that we are only using cache
  # from remote repo and not the local storage.
  run_buildah rmi --all -f

  # ------ Test case ------ #
  # expect cache to be pushed on remote stream
  # now a build on clean slate must pull cache
  # from remote instead of actually computing the
  # run steps
  run printf "STEP 2/5: RUN echo hello\n--> Cache pulled from remote"
  step1=$output
  run printf "STEP 3/5: RUN echo world\n--> Cache pulled from remote"
  step2=$output
  run printf "STEP 4/5: RUN touch hello\n--> Cache pulled from remote"
  step3=$output
  run printf "STEP 5/5: ADD somefile somefile\n--> Cache pulled from remote"
  step4=$output
  # First run step in second stage should not be pulled since its already pulled
  run printf "STEP 2/3: RUN echo hello\n--> Using cache"
  step5=$output
  run printf "COPY --from=0 hello hello\n--> Cache pulled from remote"
  step6=$output
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-from localhost:${REGISTRY_PORT}/temp -t test -f ${mytmpdir}/Containerfile ${mytmpdir}
  expect_output --substring "$step1"
  expect_output --substring "$step2"
  expect_output --substring "$step3"
  expect_output --substring "$step4"
  expect_output --substring "$step5"
  expect_output --substring "$step6"

  ##### Test when cache source is: localhost:${REGISTRY_PORT}/temp2

  # clean all cache and intermediate images
  # to make sure that we are only using cache
  # from remote repo and not the local storage.
  run_buildah rmi --all -f

  # ------ Test case ------ #
  # expect cache to be pushed on remote stream
  # now a build on clean slate must pull cache
  # from remote instead of actually computing the
  # run steps
  run printf "STEP 2/5: RUN echo hello\n--> Cache pulled from remote"
  step1=$output
  run printf "STEP 3/5: RUN echo world\n--> Cache pulled from remote"
  step2=$output
  run printf "STEP 4/5: RUN touch hello\n--> Cache pulled from remote"
  step3=$output
  run printf "STEP 5/5: ADD somefile somefile\n--> Cache pulled from remote"
  step4=$output
  # First run step in second stage should not be pulled since its already pulled
  run printf "STEP 2/3: RUN echo hello\n--> Using cache"
  step5=$output
  run printf "COPY --from=0 hello hello\n--> Cache pulled from remote"
  step6=$output
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-from localhost:${REGISTRY_PORT}/temp2 -t test -f ${mytmpdir}/Containerfile ${mytmpdir}
  expect_output --substring "$step1"
  expect_output --substring "$step2"
  expect_output --substring "$step3"
  expect_output --substring "$step4"
  expect_output --substring "$step5"
  expect_output --substring "$step6"
}

@test "build test pushing and pulling from remote cache sources" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  echo something > ${mytmpdir}/somefile
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN echo hello
RUN echo world
RUN touch hello
ADD somefile somefile

FROM alpine
RUN echo hello
COPY --from=0 hello hello
RUN --mount=type=cache,id=YfHI60aApFM-target,target=/target echo world > /target/hello
_EOF

  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}

  # ------ Test case ------ #
  # prepare expected output beforehand
  # must push cache twice i.e for first step and second step
  run printf "STEP 2/5: RUN echo hello\nhello\n--> Pushing cache"
  step1=$output
  run printf "STEP 3/5: RUN echo world\nworld\n--> Pushing cache"
  step2=$output
  run printf "STEP 4/5: RUN touch hello\n--> Pushing cache"
  step3=$output
  run printf "STEP 5/5: ADD somefile somefile\n--> Pushing cache"
  step4=$output
  # First run step in second stage should not be pushed since its already pushed
  run printf "STEP 2/4: RUN echo hello\n--> Using cache"
  step5=$output
  # Last step is `COPY --from=0 hello hello' so it must be committed and pushed
  # actual output is `[2/2] STEP 3/3: COPY --from=0 hello hello\n[2/2] COMMIT test\n-->Pushing cache`
  # but lets match smaller suffix
  run printf "COMMIT test\n--> Pushing cache"
  step6=$output

  # actually run build
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-to localhost:${REGISTRY_PORT}/temp -t test -f ${mytmpdir}/Containerfile ${mytmpdir}
  expect_output --substring "$step1"
  expect_output --substring "$step2"
  expect_output --substring "$step3"
  expect_output --substring "$step4"
  expect_output --substring "$step5"
  expect_output --substring "$step6"

  # clean all cache and intermediate images
  # to make sure that we are only using cache
  # from remote repo and not the local storage.
  run_buildah rmi --all -f

  # ------ Test case ------ #
  # expect cache to be pushed on remote stream
  # now a build on clean slate must pull cache
  # from remote instead of actually computing the
  # run steps
  run printf "STEP 2/5: RUN echo hello\n--> Cache pulled from remote"
  step1=$output
  run printf "STEP 3/5: RUN echo world\n--> Cache pulled from remote"
  step2=$output
  run printf "STEP 4/5: RUN touch hello\n--> Cache pulled from remote"
  step3=$output
  run printf "STEP 5/5: ADD somefile somefile\n--> Cache pulled from remote"
  step4=$output
  # First run step in second stage should not be pulled since its already pulled
  run printf "STEP 2/4: RUN echo hello\n--> Using cache"
  step5=$output
  run printf "COPY --from=0 hello hello\n--> Cache pulled from remote"
  step6=$output
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-from localhost:${REGISTRY_PORT}/temp --cache-to localhost:${REGISTRY_PORT}/temp -t test -f ${mytmpdir}/Containerfile ${mytmpdir}
  expect_output --substring "$step1"
  expect_output --substring "$step2"
  expect_output --substring "$step3"
  expect_output --substring "$step4"
  expect_output --substring "$step5"
  expect_output --substring "$step6"

  # ------ Test case ------ #
  # Try building again with --cache-from to make sure
  # we don't pull image if we already have it in our
  # local storage
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-from localhost:${REGISTRY_PORT}/temp -t test -f ${mytmpdir}/Containerfile ${mytmpdir}
  # must use cache since we have cache in local storage
  expect_output --substring "Using cache"
  # should not pull cache if its already in local storage
  assert "$output" !~ "Cache pulled"

  # ------ Test case ------ #
  # Build again with --cache-to and --cache-from
  # Since intermediate images are already present
  # on local storage so nothing must be pulled but
  # intermediate must be pushed since buildah is not
  # aware if they on remote repo or not.
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-from localhost:${REGISTRY_PORT}/temp --cache-to localhost:${REGISTRY_PORT}/temp -t test -f ${mytmpdir}/Containerfile ${mytmpdir}
  # must use cache since we have cache in local storage
  expect_output --substring "Using cache"
  # must also push cache since nothing was pulled from remote repo
  expect_output --substring "Pushing cache"
  # should not pull cache if its already in local storage
  assert "$output" !~ "Cache pulled"
}

@test "build test pushing and pulling from remote cache sources - after adding content summary" {
  _prefetch alpine

  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}

  # ------ Test case ------ #
  # prepare expected output beforehand
  # must push cache twice i.e for first step and second step
  run printf "STEP 2/3: ARG VAR=hello\n--> Pushing cache"
  step1=$output
  run printf "STEP 3/3: RUN echo \"Hello \$VAR\""
  step2=$output
  run printf "Hello hello"
  step3=$output
  run printf "COMMIT test\n--> Pushing cache"
  step6=$output

  # actually run build
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-to localhost:${REGISTRY_PORT}/temp -t test -f $BUDFILES/cache-from/Containerfile
  expect_output --substring "$step1"
  #expect_output --substring "$step2"
  expect_output --substring "$step3"
  expect_output --substring "$step6"

  # clean all cache and intermediate images
  # to make sure that we are only using cache
  # from remote repo and not the local storage.

  # Important side-note: don't use `run_buildah rmi --all -f`
  # since on podman-remote test this will remove prefetched alpine
  # and it will try to pull alpine from docker.io with
  # completely different digest (ruining our cache logic).
  run_buildah rmi test

  # ------ Test case ------ #
  # expect cache to be pushed on remote stream
  # now a build on clean slate must pull cache
  # from remote instead of actually computing the
  # run steps
  run printf "STEP 2/3: ARG VAR=hello\n--> Cache pulled from remote"
  step1=$output
  run printf "VAR\"\n--> Cache pulled from remote"
  step2=$output
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-from localhost:${REGISTRY_PORT}/temp --cache-to localhost:${REGISTRY_PORT}/temp -t test -f $BUDFILES/cache-from/Containerfile
  expect_output --substring "$step1"
  expect_output --substring "$step2"

  # ------ Test case ------ #
  # Try building again with --cache-from to make sure
  # we don't pull image if we already have it in our
  # local storage
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-from localhost:${REGISTRY_PORT}/temp -t test -f $BUDFILES/cache-from/Containerfile
  # must use cache since we have cache in local storage
  expect_output --substring "Using cache"
  # should not pull cache if its already in local storage
  assert "$output" !~ "Cache pulled"

  # ------ Test case ------ #
  # Build again with --cache-to and --cache-from
  # Since intermediate images are already present
  # on local storage so nothing must be pulled but
  # intermediate must be pushed since buildah is not
  # aware if they on remote repo or not.
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-from localhost:${REGISTRY_PORT}/temp --cache-to localhost:${REGISTRY_PORT}/temp -t test -f $BUDFILES/cache-from/Containerfile
  # must use cache since we have cache in local storage
  expect_output --substring "Using cache"
  # must also push cache since nothing was pulled from remote repo
  expect_output --substring "Pushing cache"
  # should not pull cache if its already in local storage
  assert "$output" !~ "Cache pulled"
}

@test "build test run mounting stage cached from remote cache source" {
  _prefetch alpine

  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}

  # ------ Test case ------ #
  # prepare expected output beforehand
  run printf "STEP 2/2: COPY / /\n--> Pushing cache"
  step1_2=$output
  run printf "COMMIT test\n--> Pushing cache"
  step2_2=$output

  # actually run build
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-to localhost:${REGISTRY_PORT}/temp -t test -f $BUDFILES/cache-from-stage/Containerfile $BUDFILES/cache-from-stage
  expect_output --substring "$step1_2"
  #expect_output --substring "$step2_2"

  # clean all cache and intermediate images
  # to make sure that we are only using cache
  # from remote repo and not the local storage.

  # Important side-note: don't use `run_buildah rmi --all -f`
  # since on podman-remote test this will remove prefetched alpine
  # and it will try to pull alpine from docker.io with
  # completely different digest (ruining our cache logic).
  run_buildah rmi test

  # ------ Test case ------ #
  # expect cache to be pushed on remote stream
  # now a build on clean slate must pull cache
  # from remote instead of actually computing the
  # run steps
  run printf "STEP 2/2: COPY / /\n--> Using cache"
  step1_2=$output
  run printf "STEP 2/2: RUN --mount=type=bind,from=stage1,target=/mnt echo hi > test\n--> Cache pulled from remote"
  step2_2=$output
  run_buildah build $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --layers --cache-from localhost:${REGISTRY_PORT}/temp --cache-to localhost:${REGISTRY_PORT}/temp -t test -f $BUDFILES/cache-from-stage/Containerfile $BUDFILES/cache-from-stage
  expect_output --substring "$step1_2"
  expect_output --substring "$step2_2"

}

# Sets shell variable: contextdir
# Creates a Containerfile with alpine base and a unique layer.
function _setup_compression_context() {
  _prefetch alpine
  contextdir=${TEST_SCRATCH_DIR}/bud/compression-test
  mkdir -p $contextdir
  echo "unique-content-$(random_string)-$(date +%s)" > $contextdir/testfile
  cat > $contextdir/Containerfile << _EOF
FROM alpine
RUN echo "layer1-$(random_string)"
COPY testfile /testfile
_EOF
}

# Sets shell variables: contextdir, authfile
# Creates a Containerfile and starts a registry for push/cache tests.
function _setup_compression_test() {
  which skopeo || skip "skopeo is not installed"
  _setup_compression_context
  start_registry
  authfile=${TEST_SCRATCH_DIR}/test.auth
  run_buildah login --tls-verify=false --authfile $authfile --username testuser --password testpassword localhost:${REGISTRY_PORT}
}

function _build_with_cache() {
  local imgname=$1; shift
  local cacherepo=$1; shift
  run_buildah build \
    $WITH_POLICY_JSON \
    --tls-verify=false \
    --authfile $authfile \
    --layers \
    --no-cache \
    --cache-to ${cacherepo} \
    "$@" \
    -t ${imgname} \
    -f $contextdir/Containerfile \
    $contextdir
  expect_output --substring "Pushing cache"
}

# Inspect a cache entry from a --cache-to registry repo.
# Uses skopeo list-tags to find a cache tag, then inspects its raw manifest.
# Sets $output to the raw manifest JSON for subsequent assertions.
# Usage: _inspect_cache_entry cacherepo
function _inspect_cache_entry() {
  local cacherepo=$1

  run skopeo list-tags \
    --authfile $authfile \
    --tls-verify=false \
    docker://${cacherepo}
  assert $status -eq 0 "listing cache tags"
  local tag
  tag=$(jq -r '.Tags[0]' <<< "$output")
  assert "$tag" != "null" "cache repo should have at least one tag"

  run skopeo inspect \
    --authfile $authfile \
    --tls-verify=false \
    --raw \
    docker://${cacherepo}:${tag}
  assert $status -eq 0 "skopeo inspect of cache entry should succeed"
}

@test "build --cache-to should respect compression_format from containers.conf" {
  _setup_compression_test
  cat > $contextdir/containers.conf << _EOF
[engine]
compression_format="zstd"
_EOF
  local imgname="img-cache-conf-$(safename)"
  local cacherepo="localhost:${REGISTRY_PORT}/cache-conf-test"

  CONTAINERS_CONF=$contextdir/containers.conf \
    _build_with_cache ${imgname} ${cacherepo}

  _inspect_cache_entry ${cacherepo}
  expect_output --substring "zstd" \
    "cache layers should use zstd compression per containers.conf"
  assert "$output" !~ "tar+gzip" \
    "cache layers should NOT use gzip when containers.conf specifies zstd"
}

@test "build --cache-to with --compression-format and --force-compression" {
  _setup_compression_test
  local imgname="img-cache-cflag-$(safename)"

  for flags in "--compression-format zstd" "--compression-format zstd --force-compression"; do
    local cacherepo="localhost:${REGISTRY_PORT}/cache-test-${flags// /-}"

    _build_with_cache ${imgname} ${cacherepo} $flags

    _inspect_cache_entry ${cacherepo}
    expect_output --substring "zstd" \
      "cache layers should use zstd with: $flags"
    assert "$output" !~ "tar+gzip" \
      "cache layers should NOT use gzip with: $flags"
  done
}

@test "build --compression-format zstd to dir and oci-archive" {
  which skopeo || skip "skopeo is not installed"
  _setup_compression_context

  for transport in dir oci-archive; do
    local dest=${TEST_SCRATCH_DIR}/${transport}-out

    run_buildah build \
      $WITH_POLICY_JSON \
      --no-cache \
      --compression-format zstd \
      -t ${transport}:${dest} \
      -f $contextdir/Containerfile \
      $contextdir

    run skopeo inspect --raw ${transport}:${dest}
    assert $status -eq 0 "$transport: skopeo inspect should succeed"
    assert "$output" =~ "zstd" \
      "$transport: layers should use zstd mediaType"
    assert "$output" !~ "gzip" \
      "$transport: layers should NOT use gzip"
  done
}

@test "bud with undefined build arg directory" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir1
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
ARG SECRET="Itismysecret"
ARG NEWSECRET
RUN echo $SECRET
RUN touch hello
FROM alpine
COPY --from=0 hello .
RUN echo "$SECRET"
_EOF

  run_buildah build -t testbud $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  assert "$output" !~ '--build-arg SECRET=<VALUE>'
  expect_output --substring '\-\-build-arg NEWSECRET=<VALUE>'

  run_buildah build -t testbud $WITH_POLICY_JSON --build-arg NEWSECRET="VerySecret" --file ${mytmpdir}/Containerfile .
  assert "$output" !~ '--build-arg SECRET=<VALUE>'
  assert "$output" !~ '--build-arg NEWSECRET=<VALUE>'

# case should similarly honor globally declared args
  cat > $mytmpdir/Containerfile << _EOF
ARG SECRET="Itismysecret"
FROM alpine
ARG SECRET
ARG NEWSECRET
RUN echo $SECRET
RUN touch hello
FROM alpine
COPY --from=0 hello .
RUN echo "$SECRET"
_EOF

  run_buildah build -t testbud $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  assert "$output" !~ '--build-arg SECRET=<VALUE>'
  expect_output --substring '\-\-build-arg NEWSECRET=<VALUE>'

  run_buildah build -t testbud $WITH_POLICY_JSON --build-arg NEWSECRET="VerySecret" --file ${mytmpdir}/Containerfile .
  assert "$output" !~ '--build-arg SECRET=<VALUE>'
  assert "$output" !~ '--build-arg NEWSECRET=<VALUE>'
}

@test "bud with arg in from statement" {
  _prefetch alpine
  run_buildah build -t testbud $WITH_POLICY_JSON --build-arg app_type=x --build-arg another_app_type=m --file $BUDFILES/with-arg/Dockerfilefromarg .
  expect_output --substring 'world'
}

@test "bud with --runtime and --runtime-flag" {
  # This Containerfile needs us to be able to handle a working RUN instruction.
  skip_if_no_runtime
  skip_if_chroot

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
from alpine
run echo hello
_EOF

  local found_runtime=

  # runc-1.0.0-70.rc92 and 1.0.1-3 have completely different
  # debug messages. This is the only string common to both.
  local flag_accepted_rx="level=debug.*msg=.child process in init"
  if [ -n "$(command -v runc)" ]; then
    found_runtime=y
    run_buildah build --runtime=runc --runtime-flag=debug \
                    -q -t alpine-bud-runc $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
    expect_output --substring "$flag_accepted_rx"
  fi

  if [ -n "$(command -v crun)" ]; then
    found_runtime=y

    # Use seccomp to make crun output a warning message because crun writes few logs.
    cat > ${TEST_SCRATCH_DIR}/seccomp.json << _EOF
{
    "defaultAction": "SCMP_ACT_ALLOW",
    "syscalls": [
      {
        "name": "unknown",
        "action": "SCMP_ACT_KILL"
	    }
    ]
}
_EOF

    # crun caches seccomp profiles, so this test fails if run more than once.
    # See https://github.com/containers/crun/issues/1475
    cruntmp=${TEST_SCRATCH_DIR}/crun
    mkdir $cruntmp
    run_buildah build --runtime=crun --runtime-flag=debug --runtime-flag=root=$cruntmp \
                --security-opt seccomp=${TEST_SCRATCH_DIR}/seccomp.json \
                -q -t alpine-bud-crun $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
    expect_output --substring "unknown seccomp syscall"
  fi

  if [ -z "${found_runtime}" ]; then
    die "Did not find 'runc' nor 'crun' in \$PATH - could not run this test!"
  fi
}

@test "bud - invalid runtime flags test" {
  skip_if_no_runtime
  skip_if_chroot

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
from alpine
run echo hello
_EOF

    run_buildah 1 build $WITH_POLICY_JSON --runtime-flag invalidflag -t build_test $mytmpdir
    assert "$output" =~ ".*invalidflag" "failed when passing undefined flags to the runtime"
}

@test "bud - accept at most one arg" {
    run_buildah 125 build $WITH_POLICY_JSON $BUDFILES/dns extraarg
    assert "$output" =~ ".*accepts at most 1 arg\(s\), received 2" "Should fail when passed extra arg after context directory"
}

@test "bud with --no-hostname" {
  skip_if_no_runtime
  skip_if_chroot

  _prefetch alpine

  run_buildah build --no-cache -t testbud \
                  $WITH_POLICY_JSON $BUDFILES/no-hostname
  assert "${lines[2]}" != "localhost" "Should be set to something other then localhost"

  run_buildah build --no-hostname --no-cache -t testbud \
                  $WITH_POLICY_JSON \
		  $BUDFILES/no-hostname
  assert "${lines[2]}" == "localhost" "Should be set to localhost"

  run_buildah 1 build --network=none --no-hostname --no-cache -t testbud \
                  $WITH_POLICY_JSON \
		  -f $BUDFILES/no-hostname/Containerfile.noetc \
		  $BUDFILES/no-hostname
  assert "$output" =~ ".*ls: /etc: No such file or directory" "/etc/ directory should be gone"
}

@test "bud with --add-host" {
  skip_if_no_runtime

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
from alpine
run grep "myhostname" /etc/hosts
_EOF

  ip=123.45.67.$(( $RANDOM % 256 ))
  run_buildah build --add-host=myhostname:$ip -t testbud \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --from="${lines[2]}" --substring "^$ip\s+myhostname"

  run_buildah 125 build --no-cache --add-host=myhostname:$ip \
                  --no-hosts \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --substring "\-\-no-hosts and \-\-add-host conflict, can not be used together"

  run_buildah 1 build --no-cache --no-hosts \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --substring 'building at STEP "RUN grep "myhostname" /etc/hosts'
}

@test "bud with --add-host with host-gateway" {
  skip_if_no_runtime

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
from alpine
run cat /etc/hosts
_EOF

  run_buildah build --add-host=myhostname:host-gateway -t testbud \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile ${mytimedir}
  expect_output --substring "myhostname"
}

@test "bud with --cgroup-parent" {
  skip_if_rootless_environment
  skip_if_chroot

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN .linux.cgroupsPath
_EOF

  # with cgroup-parent
  run_buildah --cgroup-manager cgroupfs build --cgroupns=host --cgroup-parent test-cgroup -t with-flag \
                  --runtime ${DUMPSPEC_BINARY} $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --substring "test-cgroup"
  # without cgroup-parent
  run_buildah --cgroup-manager cgroupfs build -t without-flag \
                  --runtime ${DUMPSPEC_BINARY} $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  assert "$output" !~ test-cgroup
}

@test "bud with --cpu-period and --cpu-quota" {
  skip_if_chroot
  skip_if_rootless_environment
  skip_if_no_runtime

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}

  cat > $mytmpdir/Containerfile << _EOF
from alpine
run cat /sys/fs/cgroup/\$(awk -F: '{print \$NF}' /proc/self/cgroup)/cpu.max
_EOF

  run_buildah build --cpu-period=1234 --cpu-quota=5678 -t testcpu \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --from="${lines[2]}" "5678 1234"
}

@test "bud check mount /sys/fs/cgroup" {
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}

  cat > $mytmpdir/Containerfile << _EOF
from alpine
run ls /sys/fs/cgroup
_EOF
  run_buildah build $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --substring "cpu"
  expect_output --substring "memory"
}

@test "bud with --cpu-shares, checked" {
  skip_if_chroot
  skip_if_rootless_environment
  skip_if_no_runtime

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}

  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
RUN printf "weight " && cat /sys/fs/cgroup/\$(awk -F : '{print \$NF}' /proc/self/cgroup)/cpu.weight
_EOF

  for shares in 2 200 2000 12345 20000 200000 ; do
    local converted="$(convert_v1_shares_to_v2_weight ${shares})"
    local expect="(weight ${converted##* }|weight ${converted%% *})"

    echo requesting "${shares}" shares
    run_buildah build --cpu-shares=${shares} -t testcpu \
                    $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
    echo expected "${expect}"
    expect_output --from="${lines[2]}" --substring "${expect}"
  done
}

@test "bud with --cpuset-cpus" {
  skip_if_chroot
  skip_if_rootless_environment
  skip_if_no_runtime

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}

  cat > $mytmpdir/Containerfile << _EOF
from alpine
run printf "cpuset-cpus " && cat /sys/fs/cgroup/\$(awk -F : '{print \$NF}' /proc/self/cgroup)/cpuset.cpus
_EOF

  run_buildah build --cpuset-cpus=0 -t testcpuset \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --from="${lines[2]}" "cpuset-cpus 0"
}

@test "bud with --cpuset-mems" {
  skip_if_chroot
  skip_if_rootless_environment
  skip_if_no_runtime

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}

  cat > $mytmpdir/Containerfile << _EOF
from alpine
run printf "cpuset-mems " && cat /sys/fs/cgroup/\$(awk -F : '{print \$NF}' /proc/self/cgroup)/cpuset.mems
_EOF

  run_buildah build --cpuset-mems=0 -t testcpuset \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --from="${lines[2]}" "cpuset-mems 0"
}

@test "bud with --isolation" {
  skip_if_rootless_environment
  skip_if_no_runtime
  test -z "${BUILDAH_ISOLATION}" || skip "BUILDAH_ISOLATION=${BUILDAH_ISOLATION} overrides --isolation"

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
from alpine
run readlink /proc/self/ns/pid
_EOF

  run readlink /proc/self/ns/pid
  host_pidns=$output
  run_buildah build --isolation chroot -t testisolation --pid private \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  # chroot isolation doesn't make a new PID namespace.
  expect_output --from="${lines[2]}" "${host_pidns}"
}

@test "bud with --pull-always" {
  _prefetch docker.io/library/alpine
  run_buildah build --pull-always $WITH_POLICY_JSON -t testpull $BUDFILES/containerfile
  expect_output --substring "Trying to pull docker.io/library/alpine:latest..."
  run_buildah build --pull=always $WITH_POLICY_JSON -t testpull $BUDFILES/containerfile
  expect_output --substring "Trying to pull docker.io/library/alpine:latest..."
}

@test "bud with --memory and --memory-swap" {
  skip_if_chroot
  skip_if_no_runtime
  skip_if_rootless_environment

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}

  cat > $mytmpdir/Containerfile << _EOF
from alpine
run printf "memory-max=" && cat /sys/fs/cgroup/\$(awk -F : '{print \$NF}' /proc/self/cgroup)/memory.max
run printf "memory-swap-result=" && cat /sys/fs/cgroup/\$(awk -F : '{print \$NF}' /proc/self/cgroup)/memory.swap.max
_EOF

  run_buildah build --memory=40m --memory-swap=70m -t testmemory \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --from="${lines[2]}" "memory-max=41943040"
  expect_output --from="${lines[4]}" "memory-swap-result=31457280"
}

@test "bud with --shm-size" {
  skip_if_chroot
  skip_if_no_runtime

  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
from alpine
run df -h /dev/shm
_EOF

  run_buildah build --shm-size=80m -t testshm \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --from="${lines[3]}" --substring "shm\s+80.0M"
}

@test "bud with --ulimit" {
  _prefetch alpine

  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/Containerfile << _EOF
from alpine
run printf "ulimit=" && ulimit -t
_EOF

  run_buildah build --ulimit cpu=300 -t testulimit \
                  $WITH_POLICY_JSON --file ${mytmpdir}/Containerfile .
  expect_output --from="${lines[2]}" "ulimit=300"
}

@test "bud with .dockerignore #3" {
  run_buildah build -t test $WITH_POLICY_JSON $BUDFILES/copy-globs
  run_buildah build -t test2 -f Containerfile.missing $WITH_POLICY_JSON $BUDFILES/copy-globs

  run_buildah 125 build -t test3 -f Containerfile.bad $WITH_POLICY_JSON $BUDFILES/copy-globs
  expect_output --substring "matched nothing"
}

@test "bud with copy --exclude" {
  run_buildah build -t test $WITH_POLICY_JSON $BUDFILES/copy-exclude
  assert "$output" !~ "test1.txt"

  run_buildah build -t test2 -f Containerfile.missing $WITH_POLICY_JSON $BUDFILES/copy-exclude
  assert "$output" !~ "test2.txt"
}

@test "bud with copy --parents" {
  run_buildah build -t test $WITH_POLICY_JSON $BUDFILES/copy-parents
  assert "$output" =~ "\/no_parents\/a\.txt"
  assert "$output" =~ "\/parents\/x\/a\.txt"
  assert "$output" =~ "\/parents\/y\/a\.txt"
  assert "$output" =~ "\/parents_file_point\/y\/b\.txt"
  assert "$output" =~ "\/parents_file_point\/y\/a\.txt"
  assert "$output" =~ "\/parents_dir_point\/y\/b\.txt"
  assert "$output" =~ "\/parents_dir_point\/y\/a\.txt"
}

@test "bud-copy--parents-links" {
  skip_if_rootless_environment "unprivileged tests use sshfs which doesn't represent hard links"

  target=image
  run_buildah build $WITH_POLICY_JSON -t ${target} -f $BUDFILES/copy-parents/Containerfile-hardlinks $BUDFILES/copy-parents

  run_buildah from ${target}
  ctrid="$output"
  run_buildah_mount "$ctrid"
  root="$output"

  for dir in parents/x parents_dir_point ;do
    # Target of symlink-a.txt is not changed when using --parents with pivot point
    # Docker does not change the symlink target when using --parents with pivot point
    run stat -c "%N" ${root}/${dir}/z/symlink-b.txt
    expect_output "'${root}/${dir}/z/symlink-b.txt' -> '/x/z/b.txt'" "symlink-b.txt: symlink to b.txt"

    run stat -c "%d:%i" ${root}/${dir}/y/b.txt
    file_b1=$output
    run stat -c "%h" ${root}/${dir}/y/b.txt
    expect_output 2 "b.txt: number of hardlinks"

    run stat -c "%d:%i" ${root}/${dir}/z/hardlink-y-b.txt
    expect_output $file_b1 "stat(hardlink-y-b.txt) == stat(b.txt)"

    run stat -c "%d:%i" ${root}/${dir}/z/a.txt
    file_a1=$output
    run stat -c "%h" ${root}/${dir}/z/a.txt
    expect_output 2 "a.txt: number of hardlinks"

    run stat -c "%d:%i" ${root}/${dir}/z/hardlink-a.txt
    expect_output $file_a1 "stat(hardlink-a.txt) == stat(a.txt)"
  done
}

@test "bud with containerfile secret" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir1
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/mysecret << _EOF
SOMESECRETDATA
_EOF

  run_buildah build --secret=id=mysecret,src=${mytmpdir}/mysecret $WITH_POLICY_JSON  -t secretimg -f $BUDFILES/run-mounts/Dockerfile.secret $BUDFILES/run-mounts
  expect_output --substring "SOMESECRETDATA"

  run_buildah from secretimg
  run_buildah 1 run secretimg-working-container cat /run/secrets/mysecret
  expect_output --substring "cat: can't open '/run/secrets/mysecret': No such file or directory"
  run_buildah rm -a
}

@test "bud with containerfile secret and secret is accessed twice and build should be successful" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir1
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/mysecret << _EOF
SOMESECRETDATA
_EOF

  cat > $mytmpdir/Dockerfile << _EOF
FROM alpine

RUN --mount=type=secret,id=mysecret,dst=/home/root/mysecret cat /home/root/mysecret

RUN --mount=type=secret,id=mysecret,dst=/home/root/mysecret2 echo hello && cat /home/root/mysecret2
_EOF

  run_buildah build --secret=id=mysecret,src=${mytmpdir}/mysecret $WITH_POLICY_JSON  -t secretimg -f ${mytmpdir}/Dockerfile
  expect_output --substring "hello"
  expect_output --substring "SOMESECRETDATA"
}

@test "bud with containerfile secret accessed on second RUN" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir1
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/mysecret << _EOF
SOMESECRETDATA
_EOF

  run_buildah 1 bud --secret=id=mysecret,src=${mytmpdir}/mysecret $WITH_POLICY_JSON  -t secretimg -f $BUDFILES/run-mounts/Dockerfile.secret-access $BUDFILES/run-mounts
  expect_output --substring "SOMESECRETDATA"
  expect_output --substring "cat: can't open '/mysecret': No such file or directory"

  run_buildah 1 bud --no-cache --layers --secret=id=mysecret,src=${mytmpdir}/mysecret $WITH_POLICY_JSON  -t secretimg -f $BUDFILES/run-mounts/Dockerfile.secret-access $BUDFILES/run-mounts
  expect_output --substring "SOMESECRETDATA"
  expect_output --substring "cat: can't open '/mysecret': No such file or directory"
}

@test "bud with default mode perms" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir1
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/mysecret << _EOF
SOMESECRETDATA
_EOF

  run_buildah bud --secret=id=mysecret,src=${mytmpdir}/mysecret,type=file $WITH_POLICY_JSON  -t secretmode -f $BUDFILES/run-mounts/Dockerfile.secret-mode $BUDFILES/run-mounts
  expect_output --substring "400"
}

@test "bud with containerfile secret options" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir1
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/mysecret << _EOF
SOMESECRETDATA
_EOF

  run_buildah build --secret=id=mysecret,src=${mytmpdir}/mysecret $WITH_POLICY_JSON  -t secretopts -f $BUDFILES/run-mounts/Dockerfile.secret-options $BUDFILES/run-mounts
  expect_output --substring "444"
  expect_output --substring "1000"
  expect_output --substring "1001"
}

@test "bud with containerfile secret not required" {
  _prefetch alpine

  run_buildah build $WITH_POLICY_JSON  -t secretnotreq -f $BUDFILES/run-mounts/Dockerfile.secret-not-required $BUDFILES/run-mounts
  run_buildah 1 build $WITH_POLICY_JSON  -t secretnotreq -f $BUDFILES/run-mounts/Dockerfile.secret-required-false $BUDFILES/run-mounts
  expect_output --substring "No such file or directory"
  assert "$output" !~ "secret required but no secret with id mysecret found"
}

@test "bud with containerfile secret required" {
  _prefetch alpine

  run_buildah 125 build $WITH_POLICY_JSON  -t secretreq -f $BUDFILES/run-mounts/Dockerfile.secret-required $BUDFILES/run-mounts
  expect_output --substring 'secret required but no secret with id "mysecret" found'

  # Also test secret required without value
  run_buildah 125 build $WITH_POLICY_JSON  -t secretreq -f $BUDFILES/run-mounts/Dockerfile.secret-required-wo-value $BUDFILES/run-mounts
  expect_output --substring 'secret required but no secret with id "mysecret" found'
}

@test "bud with containerfile env secret" {
  _prefetch alpine
  export MYSECRET=SOMESECRETDATA
  run_buildah build --secret=id=mysecret,src=MYSECRET,type=env $WITH_POLICY_JSON  -t secretimg -f $BUDFILES/run-mounts/Dockerfile.secret $BUDFILES/run-mounts
  expect_output --substring "SOMESECRETDATA"

  run_buildah from secretimg
  run_buildah 1 run secretimg-working-container cat /run/secrets/mysecret
  expect_output --substring "cat: can't open '/run/secrets/mysecret': No such file or directory"
  run_buildah rm -a

  run_buildah build --secret=id=mysecret,env=MYSECRET $WITH_POLICY_JSON  -t secretimg -f $BUDFILES/run-mounts/Dockerfile.secret $BUDFILES/run-mounts
  expect_output --substring "SOMESECRETDATA"

  run_buildah from secretimg
  run_buildah 1 run secretimg-working-container cat /run/secrets/mysecret
  expect_output --substring "cat: can't open '/run/secrets/mysecret': No such file or directory"

  run_buildah 125 build --secret=id=mysecret2,env=MYSECRET,true=false $WITH_POLICY_JSON -f $BUDFILES/run-mounts/Dockerfile.secret $BUDFILES/run-mounts
  expect_output --substring "incorrect secret flag format"

  run_buildah rm -a
}

@test "bud with containerfile env secret priority" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir1
  mkdir -p ${mytmpdir}
  cat > $mytmpdir/mysecret << _EOF
SOMESECRETDATA
_EOF

  export mysecret=ENVDATA
  run_buildah build --secret=id=mysecret $WITH_POLICY_JSON  -t secretimg -f $BUDFILES/run-mounts/Dockerfile.secret $BUDFILES/run-mounts
  expect_output --substring "ENVDATA"
}

@test "bud with malformed --secret flag should fail not panic" {
  run_buildah 125 build --secret=id=,src $WITH_POLICY_JSON -f $BUDFILES/run-mounts/Dockerfile.secret $BUDFILES/run-mounts
  expect_output --substring "incorrect secret flag format"

  run_buildah 125 build --secret=id=mysecret,src $WITH_POLICY_JSON -f $BUDFILES/run-mounts/Dockerfile.secret $BUDFILES/run-mounts
  expect_output --substring "incorrect secret flag format"

  run_buildah 125 build --secret=src $WITH_POLICY_JSON -f $BUDFILES/run-mounts/Dockerfile.secret $BUDFILES/run-mounts
  expect_output --substring "incorrect secret flag format"

  run_buildah 125 build --secret=id $WITH_POLICY_JSON -f $BUDFILES/run-mounts/Dockerfile.secret $BUDFILES/run-mounts
  expect_output --substring "incorrect secret flag format"
}

@test "bud-multiple-platform-values" {
  skip "FIXME: #4396 - this test is broken, and is failing gating tests"
  outputlist=testlist
  # check if we can run a couple of 32-bit versions of an image, and if we can,
  # assume that emulation for other architectures is in place.
  os=`go env GOOS`
  run_buildah from $WITH_POLICY_JSON --name try-386 --platform=$os/386 alpine
  run_buildah '?' run try-386 true
  if test $status -ne 0 ; then
    skip "unable to run 386 container, assuming emulation is not available"
  fi
  run_buildah from $WITH_POLICY_JSON --name try-arm --platform=$os/arm alpine
  run_buildah '?' run try-arm true
  if test $status -ne 0 ; then
    skip "unable to run arm container, assuming emulation is not available"
  fi

  # build for those architectures - RUN gets exercised
  run_buildah build $WITH_POLICY_JSON --jobs=0 --platform=$os/arm,$os/386 --manifest $outputlist $BUDFILES/multiarch
  run_buildah manifest inspect $outputlist
  list="$output"
  run jq -r '.manifests[0].digest' <<< "$list"
  d1="$output"
  run jq -r '.manifests[1].digest' <<< "$list"
  d2="$output"
  assert "$d1" =~ "^sha256:[0-9a-f]{64}\$"
  assert "$d2" =~ "^sha256:[0-9a-f]{64}\$"
  assert "$d1" != "$d2" "digest(arm) should != digest(386)"
}

@test "bud-multiple-platform-no-partial-manifest-list" {
  outputlist=localhost/testlist
  run_buildah 1 bud $WITH_POLICY_JSON --platform=linux/arm,linux/amd64 --manifest $outputlist -f $BUDFILES/multiarch/Dockerfile.fail $BUDFILES/multiarch
  expect_output --substring "building at STEP \"RUN test .arch. = x86_64"
  run_buildah 125 manifest inspect $outputlist
  expect_output --substring "reading image .* pinging container registry"
}

@test "bud-multiple-platform-failure" {
  # check if we can run a couple of 32-bit versions of an image, and if we can,
  # assume that emulation for other architectures is in place.
  os=$(go env GOOS)
  if [[ "$os" != linux ]]; then
    skip "GOOS is '$os'; this test can only run on linux"
  fi
  run_buildah from $WITH_POLICY_JSON --name try-386 --platform=$os/386 alpine
  run_buildah '?' run try-386 true
  if test $status -ne 0 ; then
    skip "unable to run 386 container, assuming emulation is not available"
  fi
  run_buildah from $WITH_POLICY_JSON --name try-arm --platform=$os/arm alpine
  run_buildah '?' run try-arm true
  if test $status -ne 0 ; then
    skip "unable to run arm container, assuming emulation is not available"
  fi
  outputlist=localhost/testlist
  run_buildah 1 build $WITH_POLICY_JSON \
              --jobs=0 \
              --platform=linux/arm64,linux/amd64 \
              --manifest $outputlist \
              --build-arg SAFEIMAGE=$SAFEIMAGE \
              -f $BUDFILES/multiarch/Dockerfile.fail-multistage \
              $BUDFILES/multiarch
  expect_output --substring 'building at STEP "RUN false"'
}

@test "bud-multiple-platform-no-run" {
  outputlist=localhost/testlist
  run_buildah build $WITH_POLICY_JSON \
              --jobs=0 \
              --all-platforms \
              --manifest $outputlist \
              --build-arg SAFEIMAGE=$SAFEIMAGE \
              -f $BUDFILES/multiarch/Dockerfile.no-run \
              $BUDFILES/multiarch

  run_buildah manifest inspect $outputlist
  manifests=$(jq -r '.manifests[].platform.architecture' <<<"$output" |sort|fmt)
  assert "$manifests" = "amd64 arm64 ppc64le s390x" "arch list in manifest"
}

# attempts to resolve heading arg as base-image with --all-platforms
@test "bud-multiple-platform-with-base-as-default-arg" {
  outputlist=localhost/testlist
  run_buildah build $WITH_POLICY_JSON \
              --jobs=1 \
              --all-platforms \
              --manifest $outputlist \
              -f $BUDFILES/all-platform/Containerfile.default-arg \
              $BUDFILES/all-platform

  run_buildah manifest inspect $outputlist
  manifests=$(jq -r '.manifests[].platform.architecture' <<<"$output" |sort|fmt)
  assert "$manifests" = "386 amd64 arm arm arm64 ppc64le s390x" "arch list in manifest"
}

@test "bud-multiple-platform-with-base-as-arg-with-tag-and-digest" {
  outputlist=localhost/testlist
  run_buildah build $WITH_POLICY_JSON \
              --build-arg=foo=${SAFEIMAGE}@${SAFEIMAGE_DIGEST} \
              --all-platforms \
              --manifest $outputlist \
              -f $BUDFILES/all-platform/Containerfile.default-arg \
              $BUDFILES/all-platform

  run_buildah manifest inspect $outputlist
  manifests=$(jq -r '.manifests[].platform.architecture' <<< "$output" | sort | fmt)
  assert "$manifests" = "amd64 arm64 ppc64le s390x" "arch list in manifest did not match ${SAFEIMAGE}@${SAFEIMAGE_DIGEST}"
}

@test "bud-multiple-platform for --all-platform with additional-build-context" {
  outputlist=localhost/testlist
  local contextdir=${TEST_SCRATCH_DIR}/bud/platform
  mkdir -p $contextdir

cat > $contextdir/Dockerfile1 << _EOF
FROM busybox
_EOF

  # Pulled images must be $SAFEIMAGE since we configured --build-context
  run_buildah build $WITH_POLICY_JSON --all-platforms --build-context busybox=docker://$SAFEIMAGE --manifest $outputlist -f $contextdir/Dockerfile1
  # must contain pulling logs for $SAFEIMAGE instead of busybox
  expect_output --substring "STEP 1/1: FROM $SAFEIMAGE"
  assert "$output" =~ "\[linux/s390x\] COMMIT"
  assert "$output" =~ "\[linux/ppc64le\] COMMIT"
  assert "$output" !~ "busybox"

  # Confirm the manifests and their architectures. It is not possible for
  # this to change, unless we bump $SAFEIMAGE to a new versioned tag.
  run_buildah manifest inspect $outputlist
  manifests=$(jq -r '.manifests[].platform.architecture' <<<"$output" |sort|fmt)
  assert "$manifests" = "amd64 arm64 ppc64le s390x" "arch list in manifest"
}

@test "bud-targetplatform-as-build-arg" {
  outputlist=localhost/testlist
  for targetplatform in linux/arm64 linux/amd64 ; do
    run_buildah build $WITH_POLICY_JSON \
              --build-arg SAFEIMAGE=$SAFEIMAGE \
              --build-arg TARGETPLATFORM=$targetplatform \
              -f $BUDFILES/multiarch/Dockerfile.built-in-args \
              $BUDFILES/multiarch
    expect_output --substring "I'm compiling for $targetplatform"
  done
}

@test "build must reset platform for stages if needed" {
  run_buildah info --format '{{.host.arch}}'
  myarch="$output"
  otherarch="arm64"
  # just make sure that other arch is not equivalent to host arch
  if [[ "$otherarch" == "$myarch" ]]; then
    otherarch="amd64"
  fi
  run_buildah build $WITH_POLICY_JSON --build-arg FOREIGNARCH=$otherarch -f $BUDFILES/multiarch/Containerfile.reset-platform $BUDFILES/multiarch
  run_buildah build $WITH_POLICY_JSON --build-arg TARGETPLATFORM=linux/$myarch --build-arg FOREIGNARCH=$otherarch -f $BUDFILES/multiarch/Containerfile.reset-platform $BUDFILES/multiarch
}

# * Performs multi-stage build with label1=value1 and verifies
# * Relabels build with label1=value2 and verifies
# * Rebuild with label1=value1 and makes sure everything is used from cache
@test "bud-multistage-relabel" {
  _prefetch alpine busybox
  run_buildah inspect --format "{{.FromImageDigest}}" busybox
  fromDigest="$output"

  target=relabel
  run_buildah build --layers --label "label1=value1" $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds/Dockerfile.reused $BUDFILES/multi-stage-builds

  # Store base digest of first image
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' ${target}
  firstDigest="$output"

  # Store image id of first build
  run_buildah inspect --format '{{ .FromImageID }}' ${target}
  firstImageID="$output"

  # Label of first build must contain label1:value1
  run_buildah inspect --format '{{ .Docker.ContainerConfig.Labels }}' ${target}
  expect_output --substring "label1:value1"

  # Rebuild with new label
  run_buildah build --layers --label "label1=value2" $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds/Dockerfile.reused $BUDFILES/multi-stage-builds

  # Base digest should match with first build
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' ${target}
  expect_output "$firstDigest" "base digest from busybox"

  # Label of second build must contain label1:value2
  run_buildah inspect --format '{{ .Docker.ContainerConfig.Labels }}' ${target}
  expect_output --substring "label1:value2"

  # Rebuild everything with label1=value1 and everything should be cached from first image
  run_buildah build --layers --label "label1=value1" $WITH_POLICY_JSON -t ${target} -f $BUDFILES/multi-stage-builds/Dockerfile.reused $BUDFILES/multi-stage-builds

  # Entire image must be picked from cache
  run_buildah inspect --format '{{ .FromImageID }}' ${target}
  expect_output "$firstImageID" "Image ID cached from first build"
}


@test "bud-from-relabel" {
  _prefetch alpine busybox

  run_buildah inspect --format "{{.FromImageDigest}}" alpine
  alpineDigest="$output"

  run_buildah inspect --format "{{.FromImageDigest}}" busybox
  busyboxDigest="$output"

  target=relabel2
  run_buildah build --layers --label "label1=value1" --from=alpine -t ${target} $BUDFILES/from-scratch

  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' ${target}
  expect_output "$alpineDigest" "base digest from alpine"

  # Label of second build must contain label1:value1
  run_buildah inspect --format '{{ .Docker.ContainerConfig.Labels }}' ${target}
  expect_output --substring "label1:value1"


  run_buildah build --layers --label "label1=value2" --from=busybox -t ${target} $BUDFILES/from-scratch

  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' ${target}
  expect_output "$busyboxDigest" "base digest from busybox"

  # Label of second build must contain label1:value2
  run_buildah inspect --format '{{ .Docker.ContainerConfig.Labels }}' ${target}
  expect_output --substring "label1:value2"
}

@test "bud with run should not leave mounts behind cleanup test" {
  skip_if_in_container
  skip_if_no_podman
  _prefetch alpine

  # Create target dir where we will export tar
  target=cleanable
  local contextdir=${TEST_SCRATCH_DIR}/${target}
  mkdir $contextdir

  # Build and export container to tar
  run_buildah build --no-cache $WITH_POLICY_JSON -t ${target} -f $BUDFILES/containerfile/Containerfile.in $BUDFILES/containerfile
  podman export $(podman create --name ${target} --net=host ${target}) --output=$contextdir.tar

  # We are done exporting so remove images and containers which are not needed
  podman rm -f ${target}
  run_buildah rmi ${target}

  # Explode tar
  tar -xf $contextdir.tar -C $contextdir
  count=$(ls -A $contextdir/run | wc -l)
  ## exported /run should be empty
  assert "$count" == "0"
}

@test "bud with custom files in /run/ should persist cleanup test" {
  skip_if_in_container
  skip_if_no_podman
  _prefetch alpine

  # Create target dir where we will export tar
  target=cleanable
  local contextdir=${TEST_SCRATCH_DIR}/${target}
  mkdir $contextdir

  # Build and export container to tar
  run_buildah build --no-cache $WITH_POLICY_JSON -t ${target} -f $BUDFILES/add-run-dir/Dockerfile
  podman export $(podman create --name ${target} --net=host ${target}) --output=$contextdir.tar

  # We are done exporting so remove images and containers which are not needed
  podman rm -f ${target}
  run_buildah rmi ${target}

  # Explode tar
  tar -xf $contextdir.tar -C $contextdir
  count=$(ls -A $contextdir/run | wc -l)
  ## exported /run should not be empty
  assert "$count" == "1"
}

@test "bud-with-mount-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=$BUDFILES/buildkit-mount
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfile $contextdir/
  expect_output --substring "hello"
}

@test "bud-with-mount-no-source-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfile2 $contextdir/
  expect_output --substring "hello"
}

@test "bud-with-mount-with-only-target-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfile6 $contextdir/
  expect_output --substring "hello"
}

@test "bud-with-mount-no-subdir-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfile $contextdir/subdir/
  expect_output --substring "hello"
}

@test "bud-with-mount-relative-path-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfile4 $contextdir/
  expect_output --substring "hello"
}

@test "bud-with-mount-relative-path-like-buildkit-arg-in-source" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  run_buildah build --build-arg INPUTPATH_1=subdir -t testbud $WITH_POLICY_JSON -f $contextdir/Containerfile5 $contextdir/
  expect_output --substring "hello"
}

@test "bud-with-mount-with-rw-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  run_buildah build --isolation chroot -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfile3 $contextdir/subdir/
  expect_output --substring "world"
}

@test "bud-verify-if-we-dont-clean-preexisting-path" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine ubuntu
  run_buildah 1 build -t testbud $WITH_POLICY_JSON --secret id=secret-foo,src=$BUDFILES/verify-cleanup/secret1.txt $BUDFILES/verify-cleanup/
  expect_output --substring "hello"
  expect_output --substring "secrettext"
  expect_output --substring "Directory /tmp exists."
  expect_output --substring "Directory /var/tmp exists."
  expect_output --substring "Directory /testdir DOES NOT exist."
  expect_output --substring "Cache Directory /cachedir DOES NOT exist."
  expect_output --substring "Secret File secret1.txt DOES NOT exist."
  expect_output --substring "/tmp/hey: No such file or directory"
}

@test "bud-with-mount-with-tmpfs-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  # tmpfs mount: target should be available on container without creating any special directory on container
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfiletmpfs
}

@test "bud-with-mount-with-tmpfs-with-copyup-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfiletmpfscopyup
  expect_output --substring "certs"
}

@test "bud-with-mount-cache-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  # Use a private TMPDIR so type=cache tests can run in parallel
  # try writing something to persistent cache
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilecachewrite
  # try reading something from persistent cache in a different build
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t testbud2 $WITH_POLICY_JSON -f $contextdir/Dockerfilecacheread
  expect_output --substring "hello"
}

@test "bud-with-mount-cache-like-buildkit with buildah prune should clear the cache" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  # try writing something to persistent cache
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilecachewrite
  # prune the mount cache
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah prune
  # try reading something from persistent cache in a different build
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah 1 build -t testbud2 $WITH_POLICY_JSON -f $contextdir/Dockerfilecacheread
  expect_output --substring "No such file or directory"
}

@test "bud-with-mount-cache-like-buildkit-verify-default-selinux-option" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  # try writing something to persistent cache
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t testbud $WITH_POLICY_JSON -f $BUDFILES/buildkit-mount/Dockerfilecachewritewithoutz
  # try reading something from persistent cache in a different build
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t testbud2 $WITH_POLICY_JSON -f $BUDFILES/buildkit-mount/Dockerfilecachereadwithoutz
  buildah_cache_dir="${TEST_SCRATCH_DIR}/buildah-cache-$UID"
  # buildah cache parent must have been created for our uid specific to this test
  test -d "$buildah_cache_dir"
  expect_output --substring "hello"
}

@test "bud-with-mount-cache-like-buildkit-locked-across-steps" {
  # Note: this test is just testing syntax for sharing, actual behaviour test needs parallel build in order to test locking.
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  # try writing something to persistent cache
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilecachewritesharing
  expect_output --substring "world"
}

@test "bud-with-multiple-mount-keeps-default-bind-mount" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount
  cp -R $BUDFILES/buildkit-mount $contextdir
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilemultiplemounts $contextdir/
  expect_output --substring "hello"
}

@test "bud with user in groups" {
  target=bud-group
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/group
}

@test "build proxy" {
  _prefetch alpine
  mytmpdir=${TEST_SCRATCH_DIR}/my-dir
  mkdir -p $mytmpdir
  cat > $mytmpdir/Containerfile << _EOF
FROM alpine
run printenv
_EOF
  target=env-image
  check="FTP_PROXY="FTP" ftp_proxy=ftp http_proxy=http HTTPS_PROXY=HTTPS"
  bogus="BOGUS_PROXY=BOGUS"
  eval $check $bogus run_buildah build --unsetenv PATH $WITH_POLICY_JSON -t oci-${target} -f $mytmpdir/Containerfile .
  for i in $check; do
    expect_output --substring "$i" "Environment variables available within build"
  done
  if [ -n "$(grep "$bogus" <<< "$output")" ]; then
    die "Unexpected bogus environment."
  fi
}

@test "build proxy - ADD URL" {
  _prefetch alpine
  target=alpine-image
  http_proxy=http://127.0.0.1:47 HTTPS_PROXY=http://127.0.0.1:47 \
    run_buildah 125 build $WITH_POLICY_JSON -t alpine-image -f $BUDFILES/add-checksum/Containerfile $BUDFILES/add-checksum
  expect_output --substring "127.0.0.1:47" "connection to fake proxy must fail"
  expect_output --substring "connection refused" "connection to fake proxy must fail"
}

@test "bud-with-mount-bind-from-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount-from
  cp -R $BUDFILES/buildkit-mount-from $contextdir
  # build base image which we will use as our `from`
  run_buildah build -t buildkitbase $WITH_POLICY_JSON -f $contextdir/Dockerfilebuildkitbase $contextdir/
  # try reading something from another image in a different build
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilebindfrom
  expect_output --substring "hello"
}

@test "bud-with-writeable-mount-bind-from-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount-from
  cp -R $BUDFILES/buildkit-mount-from $contextdir
  # build base image which we will use as our `from`
  run_buildah build -t buildkitbase $WITH_POLICY_JSON -f $contextdir/Dockerfilebuildkitbase $contextdir/
  # try reading something from another image in a different build
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilebindfromwriteable
  expect_output --substring "world"
}

@test "bud-with-mount-bind-from-without-source-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount-from
  cp -R $BUDFILES/buildkit-mount-from $contextdir
  # build base image which we will use as our `from`
  run_buildah build -t buildkitbase $WITH_POLICY_JSON -f $contextdir/Dockerfilebuildkitbase $contextdir/
  # try reading something from another image in a different build
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilebindfromwithoutsource
  expect_output --substring "hello"
}

@test "bud-with-mount-bind-from-with-empty-from-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount-from
  cp -R $BUDFILES/buildkit-mount-from $contextdir
  # build base image which we will use as our `from`
  run_buildah build -t buildkitbase $WITH_POLICY_JSON -f $contextdir/Dockerfilebuildkitbase $contextdir/
  # try reading something from image in a different build
  run_buildah 125 build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilebindfromwithemptyfrom
  expect_output --substring "points to an empty value"
}

@test "bud-with-mount-cache-from-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount-from
  cp -R $BUDFILES/buildkit-mount-from $contextdir
  # try reading something from persistent cache in a different build
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilecachefrom $contextdir/
  expect_output --substring "hello"
}

@test "bud-with-mount-cache-image-from-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${BUDFILES}/buildkit-mount-from

  # build base image which we will use as our `from`
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t buildkitbase $WITH_POLICY_JSON -f $contextdir/Dockerfilebuildkitbase $contextdir/

  # try reading something from persistent cache in a different build
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilecachefromimage
}

@test "bud-with-mount-cache-multiple-from-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount-from
  cp -R $BUDFILES/buildkit-mount-from $contextdir
  # try reading something from persistent cache in a different build
  TMPDIR=${TEST_SCRATCH_DIR} run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilecachemultiplefrom $contextdir/
  expect_output --substring "hello"
  expect_output --substring "hello2"
}

@test "bud-with-mount-bind-from-relative-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount-from
  cp -R $BUDFILES/buildkit-mount-from $contextdir
  # build base image which we will use as our `from`
  run_buildah build -t buildkitbaserelative $WITH_POLICY_JSON -f $contextdir/Dockerfilebuildkitbaserelative $contextdir/
  # try reading something from image in a different build
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilebindfromrelative
  expect_output --substring "hello"
}

@test "bud-with-mount-bind-from-multistage-relative-like-buildkit" {
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount-from
  cp -R $BUDFILES/buildkit-mount-from $contextdir
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  # build base image which we will use as our `from`
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilemultistagefrom $contextdir/
  expect_output --substring "hello"
}

@test "bud-with-mount-bind-from-cache-multistage-relative-like-buildkit" {
  skip_if_no_runtime
  skip_if_in_container
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/buildkit-mount-from
  cp -R $BUDFILES/buildkit-mount-from $contextdir
  # build base image which we will use as our `from`
  run_buildah build -t testbud $WITH_POLICY_JSON -f $contextdir/Dockerfilemultistagefromcache $contextdir/
  expect_output --substring "hello"
  expect_output --substring "hello2"
}

@test "bud with network names" {
  skip_if_no_runtime
  skip_if_in_container
  skip_if_rootless_environment

  _prefetch alpine

  run_buildah 125 bud $WITH_POLICY_JSON --network notexists $BUDFILES/network
  expect_output --substring "network not found"

  if test "$BUILDAH_ISOLATION" = "oci"; then
    run_buildah bud $WITH_POLICY_JSON --network podman $BUDFILES/network
    # default subnet is 10.88.0.0/16
    expect_output --substring "10.88."
  fi
}

@test "bud with --network pasta" {
  skip_if_no_runtime
  skip_if_chroot
  skip_if_root_environment "pasta only works rootless"

  _prefetch alpine

  # pasta by default copies the host ip
  ip=$(hostname -I | cut -f 1 -d " ")

  run_buildah bud $WITH_POLICY_JSON --network pasta $BUDFILES/network
  assert "$output" =~ "$ip" "ip addr shows default subnet"

  # check some network options, it accepts raw pasta(1) arguments
  mac="9a:dd:31:ea:92:98"
  run_buildah bud $WITH_POLICY_JSON --network pasta:--mtu,2000,--ns-mac-addr,"$mac" $BUDFILES/network
  assert "$output" =~ "$mac" "ip addr shows custom mac address"
  assert "$output" =~ "mtu 2000" "ip addr shows mtu 2000"
}

@test "bud WORKDIR owned by USER" {
  _prefetch alpine
  target=alpine-image
  ctr=alpine-ctr
  run_buildah build $WITH_POLICY_JSON -t ${target} $BUDFILES/workdir-user
  expect_output --substring "1000:1000 /home/http/public"
}

function build_signalled {
  skip_if_no_runtime

  _prefetch alpine

  mkfifo ${TEST_SCRATCH_DIR}/pipe
  # start the build running in the background - don't use the function wrapper because that sets '$!' to a value that's not what we want
  ${BUILDAH_BINARY} ${BUILDAH_REGISTRY_OPTS} ${ROOTDIR_OPTS} $WITH_POLICY_JSON build $BUDFILES/long-sleep > ${TEST_SCRATCH_DIR}/pipe 2>&1 &
  buildah_pid="${!}"
  echo buildah is pid ${buildah_pid}
  # save what's written to the fifo to a plain file
  coproc cat ${TEST_SCRATCH_DIR}/pipe > ${TEST_SCRATCH_DIR}/log
  cat_pid="${COPROC_PID}"
  echo cat is pid ${cat_pid}
  # kill the buildah process early
  sleep 30
  kill -s ${1} "${buildah_pid}"
  # wait for output to stop getting written from anywhere
  wait "${buildah_pid}" "${cat_pid}"
  echo log:
  cat ${TEST_SCRATCH_DIR}/log
  echo checking:
  ! grep 'not fully killed' ${TEST_SCRATCH_DIR}/log
}

@test "build interrupted" {
  build_signalled SIGINT
}

@test "build terminated" {
  build_signalled SIGTERM
}

@test "build killed" {
  build_signalled SIGKILL
}

@test "build-multiple-parse" {
  _prefetch alpine
  echo 'FROM alpine' | tee ${TEST_SCRATCH_DIR}/Dockerfile1
  echo '# escape=|\nFROM alpine' | tee ${TEST_SCRATCH_DIR}/Dockerfile2
  run_buildah 125 build -f ${TEST_SCRATCH_DIR}/Dockerfile1 -f ${TEST_SCRATCH_DIR}/Dockerfile2 ${TEST_SCRATCH_DIR}
  assert "$output" =~ "parsing additional Dockerfile .*Dockerfile2: invalid ESCAPE"
}

@test "build-with-network-test" {
  skip_if_in_container # Test only works in OCI isolation, which doesn't work in CI/CD systems. Buildah defaults to chroot isolation

  image="quay.io/libpod/alpine_nginx:latest"
  _prefetch $image
  cat > ${TEST_SCRATCH_DIR}/Containerfile << _EOF
FROM $image
RUN curl -k -o /dev/null http://www.redhat.com:80
_EOF

  # curl results show success
  run_buildah build ${WITH_POLICY_JSON} ${TEST_SCRATCH_DIR}

  # A proper test would use ping or nc, and check for ENETUNREACH.
  # But in a tightly firewalled environment, even the expected-success
  # test will fail. A not-quite-equivalent workaround is to use curl
  # and hope that $http_proxy is set; we then rely on curl to fail
  # in a slightly different way
  expect_rc=6
  expect_err="Could not resolve host: www.redhat.com"
  if [[ $http_proxy != "" ]]; then
    expect_rc=5
    expect_err="Could not resolve proxy:"
  fi
  run_buildah $expect_rc build --network=none ${WITH_POLICY_JSON} ${TEST_SCRATCH_DIR}
  expect_output --substring "$expect_err"
}

@test "build-with-no-new-privileges-test" {
  _prefetch alpine
  cat > ${TEST_SCRATCH_DIR}/Containerfile << _EOF
FROM alpine
RUN grep NoNewPrivs /proc/self/status
_EOF

  run_buildah build --security-opt no-new-privileges $WITH_POLICY_JSON ${TEST_SCRATCH_DIR}
  expect_output --substring "NoNewPrivs:.*1"
}

@test "build --group-add" {
  skip_if_no_runtime
  id=$RANDOM

  _prefetch alpine
  cat > ${TEST_SCRATCH_DIR}/Containerfile << _EOF
FROM alpine
RUN id -G
_EOF

  run_buildah build --group-add $id $WITH_POLICY_JSON ${TEST_SCRATCH_DIR}
  expect_output --substring "$id"

  find_oci_runtime

  if is_rootless && has_supplemental_groups && ! [[ $OCI =~ runc ]]; then
     run_buildah build --group-add keep-groups $WITH_POLICY_JSON ${TEST_SCRATCH_DIR}
     expect_output --substring "65534"
  else
     skip "test is being run by root or without any supplemental groups ($(id))"
  fi
}

@test "build-env-precedence" {
  skip_if_no_runtime

  _prefetch alpine

  run_buildah build --no-cache --env E=F --env G=H --env I=J --env K=L -f ${BUDFILES}/env/Dockerfile.env-precedence ${BUDFILES}/env
  expect_output --substring "a=b c=d E=F G=H"
  expect_output --substring "a=b c=d E=E G=G"
  expect_output --substring "w=x y=z I=J K=L"
  expect_output --substring "w=x y=z I=I K=K"

  run_buildah build --no-cache --layers --env E=F --env G=H --env I=J --env K=L -f ${BUDFILES}/env/Dockerfile.env-precedence ${BUDFILES}/env
  expect_output --substring "a=b c=d E=F G=H"
  expect_output --substring "a=b c=d E=E G=G"
  expect_output --substring "w=x y=z I=J K=L"
  expect_output --substring "w=x y=z I=I K=K"
}

@test "build prints 12-digit hash" {
  run_buildah build -t test -f $BUDFILES/containerfile/Containerfile .
  regex='--> [0-9a-zA-Z]{12}'
  if ! [[ $output =~ $regex ]]; then
    false
  fi
}

@test "build with name path changes" {
  _prefetch busybox
  run_buildah build --no-cache --quiet --pull=false $WITH_POLICY_JSON -t foo/bar $BUDFILES/commit/name-path-changes/
  run_buildah build --no-cache --quiet --pull=false $WITH_POLICY_JSON -t bar $BUDFILES/commit/name-path-changes/
  run_buildah images
  expect_output --substring "localhost/foo/bar"
  expect_output --substring "localhost/bar"
}

@test "build test default ulimits" {
  skip_if_no_runtime
  skip "FIXME: we cannot rely on podman-run ulimits matching buildah-bud (see #5820)"
  _prefetch alpine

  run podman --events-backend=none run --rm alpine sh -c "echo -n Files=; awk '/open files/{print \$4 \"/\" \$5}' /proc/self/limits"
  podman_files=$output

  run podman --events-backend=none run --rm alpine sh -c "echo -n Processes=; awk '/processes/{print \$3 \"/\" \$4}' /proc/self/limits"
  podman_processes=$output

  CONTAINERS_CONF=/dev/null run_buildah build --no-cache --pull=false $WITH_POLICY_JSON -t foo/bar $BUDFILES/bud.limits
  expect_output --substring "$podman_files"
  expect_output --substring "$podman_processes"
}

@test "build no write file on host - CVE-2024-1753" {
  _prefetch alpine
  cat > ${TEST_SCRATCH_DIR}/Containerfile << _EOF
FROM alpine as base

RUN ln -s / /rootdir

FROM alpine

# With exploit show host root, not the container's root, and create /BIND_BREAKOUT in / on the host
RUN --mount=type=bind,from=base,source=/rootdir,destination=/exploit,rw ls -l /exploit; touch /exploit/BIND_BREAKOUT; ls -l /exploit

_EOF

  run_buildah build $WITH_POLICY_JSON ${TEST_SCRATCH_DIR}
  expect_output --substring "/BIND_BREAKOUT"

  run ls /BIND_BREAKOUT
  rm -f /BIND_BREAKOUT
  assert "$status" -eq 2 "exit code from ls"
  expect_output --substring "No such file or directory"
}

@test "pull policy" {
  echo FROM busybox > ${TEST_SCRATCH_DIR}/Containerfile
  arch=amd64
  if test $(arch) = x86_64 ; then
    arch=arm64
  fi
  # specifying the arch should trigger "just pull it anyway" in containers/common
  run_buildah build --pull=missing --arch $arch --iidfile ${TEST_SCRATCH_DIR}/image1.txt ${TEST_SCRATCH_DIR}
  # not specifying the arch should trigger "yeah, fine, whatever we already have is fine" in containers/common
  run_buildah build --pull=missing --iidfile ${TEST_SCRATCH_DIR}/image2.txt ${TEST_SCRATCH_DIR}
  # both of these should have just been the base image's ID, which shouldn't have changed the second time around
  cmp ${TEST_SCRATCH_DIR}/image1.txt ${TEST_SCRATCH_DIR}/image2.txt
}

# Verify: https://github.com/containers/buildah/issues/5185
@test "build-test --mount=type=secret test from env with chroot isolation" {
  skip_if_root_environment "Need to not be root for this test to work"
  local contextdir=$BUDFILES/secret-env
  export MYSECRET=SOMESECRETDATA
  run_buildah build $WITH_POLICY_JSON --no-cache --isolation chroot --secret id=MYSECRET -t test -f $contextdir/Dockerfile
  expect_output --substring "SOMESECRETDATA"
}

@test "build-logs-from-platform" {
  run_buildah info --format '{{.host.os}}/{{.host.arch}}{{if .host.variant}}/{{.host.variant}}{{ end }}'
  local platform="$output"
  echo FROM --platform=$platform busybox > ${TEST_SCRATCH_DIR}/Containerfile
  run_buildah build ${TEST_SCRATCH_DIR}
  expect_output --substring "\-\-platform=$platform"
}

@test "build add https retry ca" {
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  mkdir -p ${TEST_SCRATCH_DIR}/private
  starthttpd ${TEST_SCRATCH_DIR} "" ${TEST_SCRATCH_DIR}/localhost.crt ${TEST_SCRATCH_DIR}/private/localhost.key
  echo FROM scratch | tee ${TEST_SCRATCH_DIR}/Dockerfile
  echo ADD "https://localhost:${HTTP_SERVER_PORT}/randomfile" / | tee -a ${TEST_SCRATCH_DIR}/Dockerfile
  run_buildah build --retry-delay=0.142857s --retry=14 --cert-dir ${TEST_SCRATCH_DIR} ${TEST_SCRATCH_DIR}
  run_buildah build --retry-delay=0.142857s --retry=14 --tls-verify=false $cid ${TEST_SCRATCH_DIR}
  run_buildah 125 build --retry-delay=0.142857s --retry=14 $cid ${TEST_SCRATCH_DIR}
  assert "$output" =~ "x509: certificate signed by unknown authority"
  stophttpd
  run_buildah 125 build --retry-delay=0.142857s --retry=14 --cert-dir ${TEST_SCRATCH_DIR} $cid ${TEST_SCRATCH_DIR}
  assert "$output" =~ "retrying in 142.*ms .*14/14.*"
}

@test "bud with ADD with git repository source" {
  # quay.io/hummingbird/git is only available on these arches:
  skip_unless_arch amd64 arm64
  _prefetch quay.io/hummingbird/git # any image with git preinstalled would do

  local repodir=${TEST_SCRATCH_DIR}/repository
  mkdir -p ${repodir}/podman.git
  tar -C ${repodir}/podman.git -xz < ${TEST_SOURCES}/git-daemon/bare-podman-repo.tar.gz
  starthttpd /git/=${repodir}:"git http-backend":GIT_HTTP_EXPORT_ALL=1:GIT_PROJECT_ROOT=${repodir} ${repodir}

  local contextdir=${TEST_SCRATCH_DIR}/add-git
  mkdir -p $contextdir
  cat > $contextdir/Dockerfile << _EOF
FROM quay.io/hummingbird/git
USER 0:0
ADD http://0.0.0.0:${HTTP_SERVER_PORT}/git/podman.git#v5.0 /podman-branch
ADD http://0.0.0.0:${HTTP_SERVER_PORT}/git/podman.git#v5.0.0 /podman-tag
_EOF

  run_buildah build -f $contextdir/Dockerfile -t git-image $contextdir
  run_buildah from --quiet $WITH_POLICY_JSON --name testctr git-image

  run_buildah run --network=host testctr -- sh -c 'git -C /podman-branch rev-parse HEAD'
  local_head_hash=$output
  run_buildah run --network=host testctr -- sh -c 'git -C /podman-branch ls-remote origin v5.0 | cut -f1'
  assert "$output" = "$local_head_hash"

  run_buildah run --network=host testctr -- sh -c 'git -C /podman-tag rev-parse HEAD'
  local_head_hash=$output
  run_buildah run --network=host testctr -- sh -c 'git -C /podman-tag ls-remote --tags origin v5.0.0 | cut -f1'
  assert "$output" = "$local_head_hash"

  cat > $contextdir/Dockerfile << _EOF
FROM scratch
ADD http://0.0.0.0:${HTTP_SERVER_PORT}/git/podman.git#nosuchbranch /src
_EOF
  run_buildah 125 build -f $contextdir/Dockerfile -t git-image $contextdir
  expect_output --substring "couldn't find remote ref nosuchbranch"
}

@test "bud with ADD with git repository source - .containerignore not applied" {
  _prefetch busybox

  local repodir=${TEST_SCRATCH_DIR}/repository-add-git-ignoring-files
  mkdir -p ${repodir}/podman.git
  tar -C ${repodir}/podman.git -xz < ${TEST_SOURCES}/git-daemon/bare-podman-repo.tar.gz
  starthttpd /git/=${repodir}:"git http-backend":GIT_HTTP_EXPORT_ALL=1:GIT_PROJECT_ROOT=${repodir} ${repodir}

  local contextdir=${TEST_SCRATCH_DIR}/add-git-ignoring-files
  mkdir -p $contextdir
  cat > $contextdir/Dockerfile << _EOF
FROM busybox
ADD http://0.0.0.0:${HTTP_SERVER_PORT}/git/podman.git#v5.0.0 /podman-tag
RUN test -f /podman-tag/file.txt
_EOF

  cat > $contextdir/.containerignore << _EOF
file.txt
_EOF

  local tmpdir=${TEST_SCRATCH_DIR}/tmpdir
  mkdir -p $tmpdir
  TMPDIR=$tmpdir run_buildah build -f $contextdir/Dockerfile -t add-git-ignoring-files $contextdir
  assert_no_tempdir_leaks $tmpdir
}

@test "bud with ADD with git repository source escape directory" {
  _prefetch alpine

  local secretdir=${TEST_SCRATCH_DIR}/secretdir
  mkdir -p ${secretdir}
  echo mysecret > ${secretdir}/secretfile

  # Archive generated with:
  #   d=$(mktemp -d) && mkdir "$d/repo"
  #   git -C "$d/repo" init -b main
  #   git -C "$d/repo" config user.email "test@example.com"
  #   git -C "$d/repo" config user.name "Test User"
  #   ln -s / "$d/repo/proj"
  #   git -C "$d/repo" add proj
  #   git -C "$d/repo" commit -m "initial commit"
  #   git clone --bare "$d/repo" "$d/test-bug.git"
  #   tar czf tests/git-daemon/escape-directory.tar.gz --owner=root:0 --group=root:0 -C "$d/test-bug.git" .
  #   rm -rf "$d"
  local repodir=${TEST_SCRATCH_DIR}/repository
  mkdir -p ${repodir}/test-bug.git
  tar --no-same-owner -C ${repodir}/test-bug.git -xz < ${TEST_SOURCES}/git-daemon/escape-directory.tar.gz
  starthttpd /git/=${repodir}:"git http-backend":GIT_HTTP_EXPORT_ALL=1:GIT_PROJECT_ROOT=${repodir} ${repodir}

  local contextdir=${TEST_SCRATCH_DIR}/add-git
  mkdir -p $contextdir
  cat > $contextdir/Containerfile << _EOF
FROM alpine
ADD http://0.0.0.0:${HTTP_SERVER_PORT}/git/test-bug.git#main:proj${secretdir} /mydir
RUN cat /mydir/secretfile
_EOF

  local tmpdir=${TEST_SCRATCH_DIR}/tmpdir
  mkdir -p $tmpdir
  TMPDIR=$tmpdir run_buildah 125 build -f $contextdir/Containerfile -t escape-image --no-cache $contextdir
  assert "$output" !~ "mysecret"
  assert_no_tempdir_leaks $tmpdir
}

@test "bud with http context symlinked Dockerfile does not write through symlink on fallback" {
  local targetfile=${TEST_SCRATCH_DIR}/targetfile
  echo "ORIGINAL_CONTENT" > ${targetfile}

  # Create a tar with a symlink Dockerfile pointing to targetfile, then
  # append garbage so chrootarchive.Untar creates the symlink but fails.
  local tarsrc=${TEST_SCRATCH_DIR}/tarsrc
  mkdir -p ${tarsrc}
  ln -s ${targetfile} ${tarsrc}/Dockerfile
  local broken_tar=${TEST_SCRATCH_DIR}/broken.tar
  tar -cf ${broken_tar} -C ${tarsrc} Dockerfile
  dd if=/dev/urandom bs=512 count=4 >> ${broken_tar} 2>/dev/null

  # Start a Python HTTP server that serves the broken tar on the first
  # request (triggers Untar failure + symlink creation), then a malicious
  # Dockerfile on the second request (which would be written through the
  # symlink without the fix).
  local portfile=${TEST_SCRATCH_DIR}/port
  python3 -c "
import http.server, threading, sys

class Handler(http.server.BaseHTTPRequestHandler):
    count = 0
    def do_GET(self):
        Handler.count += 1
        self.send_response(200)
        self.end_headers()
        if Handler.count % 2 == 1:
            with open('${broken_tar}', 'rb') as f:
                self.wfile.write(f.read())
        else:
            self.wfile.write(b'FROM scratch\nRUN echo MALICIOUS_CONTENT\n')
    def log_message(self, *args):
        pass

srv = http.server.HTTPServer(('0.0.0.0', 0), Handler)
with open('${portfile}', 'w') as f:
    f.write(str(srv.server_address[1]))
srv.serve_forever()
" &
  local srv_pid=$!
  # Wait for the server to write its port.
  local waited=0
  while ! test -s ${portfile}; do
    sleep 0.1
    if test $((++waited)) -ge 50; then
      kill $srv_pid 2>/dev/null || true
      die "python http server did not start"
    fi
  done
  local port=$(< ${portfile})

  run_buildah 125 build $WITH_POLICY_JSON http://0.0.0.0:${port}/context.tar
  kill $srv_pid 2>/dev/null || true

  run cat ${targetfile}
  assert "$output" = "ORIGINAL_CONTENT"
}

@test "bud with stdin context symlinked Dockerfile does not write through symlink on fallback" {
  local targetfile=${TEST_SCRATCH_DIR}/targetfile
  echo "ORIGINAL_CONTENT" > ${targetfile}

  # Create a tar with a symlink Dockerfile pointing to targetfile, then
  # append garbage so chrootarchive.Untar fails after creating the symlink.
  local tarsrc=${TEST_SCRATCH_DIR}/tarsrc
  mkdir -p ${tarsrc}
  ln -s ${targetfile} ${tarsrc}/Dockerfile
  local broken_tar=${TEST_SCRATCH_DIR}/broken.tar
  tar -cf ${broken_tar} -C ${tarsrc} Dockerfile
  dd if=/dev/urandom bs=512 count=4 >> ${broken_tar} 2>/dev/null

  run_buildah 125 build $WITH_POLICY_JSON - < ${broken_tar}
  run cat ${targetfile}
  assert "$output" = "ORIGINAL_CONTENT"
}

@test "build-validates-bind-bind-propagation" {
  _prefetch alpine

  cat > ${TEST_SCRATCH_DIR}/Containerfile << _EOF
FROM alpine as base
FROM alpine
RUN --mount=type=bind,from=base,source=/,destination=/var/empty,rw,bind-propagation=suid pwd
_EOF

  run_buildah 125 build $WITH_POLICY_JSON ${TEST_SCRATCH_DIR}
  expect_output --substring "invalid mount option"
}

@test "build-validates-cache-bind-propagation" {
  _prefetch alpine

  cat > ${TEST_SCRATCH_DIR}/Containerfile << _EOF
FROM alpine
RUN --mount=type=cache,destination=/var/empty,rw,bind-propagation=suid pwd
_EOF

  run_buildah 125 build $WITH_POLICY_JSON ${TEST_SCRATCH_DIR}
  expect_output --substring "invalid mount option"
}

@test "build-check-cve-2024-9675" {
  _prefetch alpine

  touch ${TEST_SCRATCH_DIR}/file.txt

  cat > ${TEST_SCRATCH_DIR}/Containerfile <<EOF
FROM alpine
RUN --mount=type=cache,id=../../../../../../../../../../../$TEST_SCRATCH_DIR,target=/var/tmp \
ls -l /var/tmp && cat /var/tmp/file.txt
EOF

  run_buildah 1 build --no-cache ${TEST_SCRATCH_DIR}
  expect_output --substring "cat: can't open '/var/tmp/file.txt': No such file or directory"

  cat > ${TEST_SCRATCH_DIR}/Containerfile <<EOF
FROM alpine
RUN --mount=type=cache,source=../../../../../../../../../../../$TEST_SCRATCH_DIR,target=/var/tmp \
ls -l /var/tmp && cat /var/tmp/file.txt
EOF

  run_buildah 125 build --no-cache ${TEST_SCRATCH_DIR}
  expect_output --substring "no such file or directory"

  mkdir ${TEST_SCRATCH_DIR}/cve20249675
  cat > ${TEST_SCRATCH_DIR}/cve20249675/Containerfile <<EOF
FROM alpine
RUN --mount=type=cache,from=testbuild,source=../,target=/var/tmp \
ls -l /var/tmp && cat /var/tmp/file.txt
EOF

  mkdir ${TEST_SCRATCH_DIR}/cachedir

  run_buildah 1 build --security-opt label=disable --build-context testbuild=${TEST_SCRATCH_DIR}/cachedir/ --no-cache ${TEST_SCRATCH_DIR}/cve20249675/
  expect_output --substring "cat: can't open '/var/tmp/file.txt': No such file or directory"
}

@test "build-mounts-implicit-workdir" {
  base=busybox
  _prefetch $base
  run_buildah inspect --format '{{.Docker.Config.WorkingDir}}' --type=image $base
  expect_output "" "test base image needs to not have a default working directory defined in its configuration"
  # check that the target for a bind mount can be specified as a relative path even when there's no WorkingDir defined for it to be relative to
  echo FROM $base > ${TEST_SCRATCH_DIR}/Containerfile
  echo RUN --mount=type=bind,src=Containerfile,target=Containerfile test -s Containerfile >> ${TEST_SCRATCH_DIR}/Containerfile
  echo RUN --mount=type=cache,id=cash,target=cachesubdir truncate -s 1024 cachesubdir/cachefile >> ${TEST_SCRATCH_DIR}/Containerfile
  echo RUN --mount=type=cache,id=cash,target=cachesubdir2 test -s cachesubdir2/cachefile >> ${TEST_SCRATCH_DIR}/Containerfile
  echo RUN --mount=type=tmpfs,target=tmpfssubdir test '`stat -f -c %i .`' '!=' '`stat -f -c %i tmpfssubdir`' >> ${TEST_SCRATCH_DIR}/Containerfile
  run_buildah build --security-opt label=disable ${TEST_SCRATCH_DIR}
}

@test "build-security-opt-mask" {
  base=busybox
  _prefetch $base
  run_buildah build bud/masks
  expect_output --substring "masked" "Everything should be masked"
  run_buildah build --security-opt unmask=all bud/masks
  expect_output --substring "unmasked" "Everything should be masked"
  run_buildah build --security-opt unmask=/proc/* bud/masks
  expect_output --substring "unmasked" "Everything should be masked"
  run_buildah build --security-opt unmask=/proc/acpi bud/masks
  expect_output --substring "unmasked" "Everything should be masked"
}

@test "build-mounts-build-context-rw" {
  zflag=
  if which selinuxenabled > /dev/null 2> /dev/null ; then
    if selinuxenabled ; then
      zflag=,z
    fi
  fi
  base=busybox
  _prefetch $base
  mkdir -p ${TEST_SCRATCH_DIR}/buildcontext
  cat > ${TEST_SCRATCH_DIR}/buildcontext/Dockerfile << EOF
  FROM $base
  RUN --mount=type=bind,dst=/dst,source=/,rw${zflag} \
    mkdir /dst/subdir ; \
    chown 1000:1000 /dst/subdir ; \
    chmod 777 /dst/subdir ; \
    touch /dst/subdir/file-suid ; \
    chmod 4777 /dst/subdir/file-suid
EOF
  run_buildah build ${TEST_SCRATCH_DIR}/buildcontext
  run find ${TEST_SCRATCH_DIR}/buildcontext -name file-suid -ls
  find ${TEST_SCRATCH_DIR}/buildcontext -ls
  expect_output "" "build should not be able to write to build context"
}

@test "build-with-two-outputs" {
  _prefetch busybox
  mkdir -p "${TEST_SCRATCH_DIR}"/context
  cat > "${TEST_SCRATCH_DIR}"/context/Containerfile << _EOF
FROM busybox
RUN truncate -s1 /built.txt
_EOF
  run_buildah build --output type=local,dest=${TEST_SCRATCH_DIR}/output1 --output ${TEST_SCRATCH_DIR}/output2 $WITH_POLICY_JSON "${TEST_SCRATCH_DIR}"/context
  test -s "${TEST_SCRATCH_DIR}"/output1/built.txt
  test -s "${TEST_SCRATCH_DIR}"/output2/built.txt
}

@test "build-with-timestamp-applies-to-oci-archive" {
  local outpath="${TEST_SCRATCH_DIR}/timestamp-oci.tar"

  run_buildah build -f <(echo 'FROM scratch') --tag=oci-archive:${outpath}.a --timestamp 0
  run_buildah build -f <(echo 'FROM scratch') --tag=oci-archive:${outpath}.b --timestamp 1
  sleep 1.1 # sleep at least 1 second, so that timestamps are incremented
  run_buildah build -f <(echo 'FROM scratch') --tag=oci-archive:${outpath}.c --timestamp 1

  # should be different ( || false is due to bats weirdness )
  ! diff "${outpath}.a" "${outpath}.b" || false

  # should be the same
  diff "${outpath}.b" "${outpath}.c"
}

@test "build-with-timestamp-applies-to-oci-archive-with-base" {
  local outpath="${TEST_SCRATCH_DIR}/timestamp-oci.tar"

  run_buildah build -f <(echo 'FROM busybox') --tag=oci-archive:${outpath}.a --timestamp 0
  sleep 1.1 # sleep at least 1 second, so that timestamps are incremented
  run_buildah build -f <(echo 'FROM busybox') --tag=oci-archive:${outpath}.b --timestamp 0

  # should be the same
  diff "${outpath}.a" "${outpath}.b"
}

@test "bud-source-date-epoch-arg" {
  _prefetch busybox
  local timestamp=60
  mkdir -p $TEST_SCRATCH_DIR/context
  cat > $TEST_SCRATCH_DIR/context/Dockerfile <<EOF
  FROM busybox
  RUN echo \$SOURCE_DATE_EPOCH | tee /SOURCE_DATE_EPOCH_BEFORE
  ARG SOURCE_DATE_EPOCH
  RUN echo \$SOURCE_DATE_EPOCH | tee /SOURCE_DATE_EPOCH_AFTER
EOF
  # build twice with no cache, with both an explicit and implicitly-set source-date-epoch value
  run_buildah build --layers --no-cache --source-date-epoch=$timestamp -t dir:$TEST_SCRATCH_DIR/explicit $TEST_SCRATCH_DIR/context
  sleep 2 # if both of the flag and environment variable get ignored for some reason, this will force the test to fail
  export local SOURCE_DATE_EPOCH=$timestamp
  run_buildah build --layers --no-cache -t dir:$TEST_SCRATCH_DIR/implicit $TEST_SCRATCH_DIR/context
  unset SOURCE_DATE_EPOCH
  # build once without the override
  run_buildah build --layers --no-cache -t dir:$TEST_SCRATCH_DIR/default $TEST_SCRATCH_DIR/context
  assert "$output" !~ "missing.*SOURCE_DATE_EPOCH.*build argument"
  # extract the layers
  mkdir -p $TEST_SCRATCH_DIR/explicit-layer $TEST_SCRATCH_DIR/implicit-layer $TEST_SCRATCH_DIR/default-layer
  echo extracting next-to-last layer for build with explicit flag $(dir_image_diff $TEST_SCRATCH_DIR/explicit - 2)
  tar -C $TEST_SCRATCH_DIR/explicit-layer -xvf $TEST_SCRATCH_DIR/explicit/$(dir_image_diff $TEST_SCRATCH_DIR/explicit - 2)
  echo extracting next-to-last layer for build with environment flag $(dir_image_diff $TEST_SCRATCH_DIR/implicit - 2)
  tar -C $TEST_SCRATCH_DIR/implicit-layer -xvf $TEST_SCRATCH_DIR/implicit/$(dir_image_diff $TEST_SCRATCH_DIR/implicit - 2)
  echo extracting next-to-last layer for default $(dir_image_diff $TEST_SCRATCH_DIR/default - 2)
  tar -C $TEST_SCRATCH_DIR/default-layer -xvf $TEST_SCRATCH_DIR/default/$(dir_image_diff $TEST_SCRATCH_DIR/default - 2)
  echo extracting last layer for build with explicit flag $(dir_image_last_diff $TEST_SCRATCH_DIR/explicit)
  tar -C $TEST_SCRATCH_DIR/explicit-layer -xvf $TEST_SCRATCH_DIR/explicit/$(dir_image_last_diff $TEST_SCRATCH_DIR/explicit)
  echo extracting last layer for build with environment flag $(dir_image_last_diff $TEST_SCRATCH_DIR/implicit)
  tar -C $TEST_SCRATCH_DIR/implicit-layer -xvf $TEST_SCRATCH_DIR/implicit/$(dir_image_last_diff $TEST_SCRATCH_DIR/implicit)
  echo extracting last layer for default $(dir_image_last_diff $TEST_SCRATCH_DIR/default)
  tar -C $TEST_SCRATCH_DIR/default-layer -xvf $TEST_SCRATCH_DIR/default/$(dir_image_last_diff $TEST_SCRATCH_DIR/default)
  # check the contents of files that recorded $SOURCE_DATE_EPOCH before the ARG was declared, which all should have been empty
  assert "$(< $TEST_SCRATCH_DIR/explicit-layer/SOURCE_DATE_EPOCH_BEFORE)" = "" "SOURCE_DATE_EPOCH arg should not have been defined yet when --source-date-epoch was specified on the command line"
  assert "$(< $TEST_SCRATCH_DIR/implicit-layer/SOURCE_DATE_EPOCH_BEFORE)" = "" "SOURCE_DATE_EPOCH arg should not have been defined yet when \$SOURCE_DATE_EPOCH was set in the environment"
  assert "$(< $TEST_SCRATCH_DIR/default-layer/SOURCE_DATE_EPOCH_BEFORE)" = "" "SOURCE_DATE_EPOCH arg should not have been defined at all, since we didn't specify a source date epoch"
  # check the contents of files that recorded $SOURCE_DATE_EPOCH after the ARG was declared, which should contain a value if it was defined, or be empty
  assert "$(< $TEST_SCRATCH_DIR/explicit-layer/SOURCE_DATE_EPOCH_AFTER)" = "$timestamp" "SOURCE_DATE_EPOCH arg should have been correctly defined when --source-date-epoch was specified on the command line"
  assert "$(< $TEST_SCRATCH_DIR/implicit-layer/SOURCE_DATE_EPOCH_AFTER)" = "$timestamp" "SOURCE_DATE_EPOCH arg should have been correctly defined when \$SOURCE_DATE_EPOCH was set in the environment"
  assert "$(< $TEST_SCRATCH_DIR/default-layer/SOURCE_DATE_EPOCH_AFTER)" = "" "SOURCE_DATE_EPOCH arg should have not been defined at all, since we didn't specify a source date epoch"
}

@test "bud-with-source-date-epoch-env" {
  _prefetch busybox
  local timestamp=60
  local datestamp="1970-01-01T00:01:00Z"
  mkdir -p $TEST_SCRATCH_DIR/context
  createrandom $TEST_SCRATCH_DIR/context/randomfile1
  createrandom $TEST_SCRATCH_DIR/context/randomfile2
  createrandom $TEST_SCRATCH_DIR/context/randomfile3
  tar c --owner=root:0 --group=root:0 -C $TEST_SCRATCH_DIR/context -f $TEST_SCRATCH_DIR/context/archive randomfile3
  cat > $TEST_SCRATCH_DIR/context/Dockerfile <<EOF
  FROM busybox
  RUN echo \$SOURCE_DATE_EPOCH | tee /SOURCE_DATE_EPOCH_BEFORE
  ARG SOURCE_DATE_EPOCH
  RUN echo \$SOURCE_DATE_EPOCH | tee /SOURCE_DATE_EPOCH_AFTER
  ADD randomfile1 /
  COPY randomfile2 /
  ADD archive /
EOF
  # build three times with no cache, with both an explicit and implicitly-set source-date-epoch value rewriting timestamps, and once not rewriting
  run_buildah build --layers --no-cache --source-date-epoch=$timestamp --rewrite-timestamp -t oci:$TEST_SCRATCH_DIR/explicit $TEST_SCRATCH_DIR/context
  sleep 2 # if both of the flag and environment variable get ignored for some reason, this will force the test to fail
  export local SOURCE_DATE_EPOCH=$timestamp
  run_buildah build --layers --no-cache --rewrite-timestamp -t oci:$TEST_SCRATCH_DIR/implicit $TEST_SCRATCH_DIR/context
  unset SOURCE_DATE_EPOCH
  sleep 2 # if both of the flag and environment variable get ignored for some reason, this will force the test to fail
  run_buildah build --layers --no-cache --source-date-epoch=$timestamp --output $TEST_SCRATCH_DIR/output-dest -t oci:$TEST_SCRATCH_DIR/norewrite $TEST_SCRATCH_DIR/context
  # build once without the override
  run_buildah build --layers --no-cache -t oci:$TEST_SCRATCH_DIR/default $TEST_SCRATCH_DIR/context
  assert "$output" !~ "missing.*SOURCE_DATE_EPOCH.*build argument"

  # first, both of the builds which used the same forced timestamp should be completely identical
  run diff -u -N -r $TEST_SCRATCH_DIR/explicit $TEST_SCRATCH_DIR/implicit
  echo "$output"
  assert $status = 0 "diff should have exited with status 0 to indicate no differences between the two with rewrite-timestamp"
  assert "$output" = ""

  # check timestamps in the one we didn't rewrite: read the image creation date and history entries
  local config=$(oci_image_config $TEST_SCRATCH_DIR/norewrite)
  run jq -r '.created' $TEST_SCRATCH_DIR/norewrite/$config
  echo image creation date:
  echo "$output"
  assert $status = 0 "looking for the image creation date in the one without rewrite-timestamp"
  assert "$output" = "$datestamp" "unexpected creation date in the one without rewrite-timestamp"

  # check timestamps in the one we didn't rewrite: read the image history entry dates
  run jq -r '.history[-6:][].created' $TEST_SCRATCH_DIR/norewrite/$config
  echo history timestamps:
  echo "$output"
  assert $status = 0 "looking for the image history entries in the one without rewrite-timestamp"
  local layer=1
  for created in "${lines[@]}"; do
    assert "$created" = "$datestamp" "unexpected datestamp for history entry $layer in the image we built without rewrite-timestamp"
    layer=$((++layer))
  done

  # check timestamps in the --output output: even without rewrite-timestamp, it should have been fixed
  for file in $(find $TEST_SCRATCH_DIR/output-dest/* -print) ; do
    run stat -c %Y $file
    assert $status = 0 "checking datestamp on $file in --output from build without rewrite-timestamp"
    assert "$output" = "$timestamp" "unexpected datestamp on $file in --output from build without rewrite-timestamp"
    case $(basename $file) in
      SOURCE_DATE_EPOCH_BEFORE)
        run cat "$file"
        assert "$status" = 0 "checking contents of $file"
        assert "$output" = "" "checking contents of $file"
        ;;
      SOURCE_DATE_EPOCH_AFTER)
        run cat "$file"
        assert "$status" = 0 "checking contents of $file"
        assert "$output" = $timestamp "checking contents of $file"
        ;;
    esac
  done

  # check timestamps in the identical images: read the image creation date
  local config=$(oci_image_config $TEST_SCRATCH_DIR/explicit)
  run jq -r '.created' $TEST_SCRATCH_DIR/explicit/$config
  echo image creation date:
  echo "$output"
  assert $status = 0 "looking for the image creation date in an image built with rewrite-timestamp"
  assert "$output" = "$datestamp" "unexpected creation date for image built with rewrite-timestamp"

  # check timestamps in the identical images: read the image history entry dates
  run jq -r '.history[-6:][].created' $TEST_SCRATCH_DIR/explicit/$config
  echo history timestamps:
  echo "$output"
  assert $status = 0 "looking for the image history entries in an image built with rewrite-timestamp"
  local layer=1
  for created in "${lines[@]}"; do
    assert "$created" = "$datestamp" "unexpected datestamp for history entry $layer in an image built with rewrite-timestamp"
    layer=$((++layer))
  done

  # check timestamps in the identical images: find the layer blobs that we added
  run jq -r '.rootfs.diff_ids[-5:][]' $TEST_SCRATCH_DIR/explicit/$config
  echo layer diff_id values:
  echo "$output"
  assert $status = 0 "looking for the image layer blobs in an image built with rewrite-timestamp"
  local layer=1
  for diffID in "${lines[@]}"; do
    local digest="$diffID"
    local alg=${digest%%:*}
    local val=${digest##*:}
    mkdir -p $TEST_SCRATCH_DIR/layer$layer
    # list the layer blob contents for the benefit of troubleshooters
    echo "layer $layer:"
    tar t -v -f $TEST_SCRATCH_DIR/explicit/blobs/"$alg"/"$val"
    # extract the layer blob
    tar x -C $TEST_SCRATCH_DIR/layer$layer -f $TEST_SCRATCH_DIR/explicit/blobs/"$alg"/"$val"
    # walk the layer blob, checking timestamps
    for file in $(find $TEST_SCRATCH_DIR/layer$layer/* -print) ; do
      run stat -c %Y $file
      assert $status = 0 "checking datestamp on $file in layer $layer in an image built with rewrite-timestamp"
      assert "$output" = "$timestamp" "unexpected datestamp on $file in layer $layer in an image built with rewrite-timestamp"
      case $(basename $file) in
        SOURCE_DATE_EPOCH_BEFORE)
          run cat "$file"
          assert "$status" = 0 "checking contents of $file"
          assert "$output" = "" "checking contents of $file"
          ;;
        SOURCE_DATE_EPOCH_AFTER)
          run cat "$file"
          assert "$status" = 0 "checking contents of $file"
          assert "$output" = $timestamp "checking contents of $file"
          ;;
      esac
    done
    layer=$((++layer))
  done
}

@test "bud-with-timestamp-config-effects" {
  _prefetch busybox
  local timestamp=60
  mkdir -p $TEST_SCRATCH_DIR/context
  cat > $TEST_SCRATCH_DIR/context/Dockerfile << EOF
  FROM busybox
  RUN hostname | tee /hostname.txt
EOF
  local config
  local diff
  local hostname

  for cliflag in timestamp source-date-epoch ; do
    run_buildah build --"$cliflag"=$timestamp --layers --no-cache -t dir:$TEST_SCRATCH_DIR/docker-layers-$cliflag --format=docker $TEST_SCRATCH_DIR/context
    config=$(dir_image_config "$TEST_SCRATCH_DIR"/docker-layers-$cliflag)
    run jq -r .config.Hostname "$TEST_SCRATCH_DIR"/docker-layers-$cliflag/"$config"
    echo "$output"
    test $status -eq 0
    assert "$output" == sandbox
    run jq -r .config.Domainname "$TEST_SCRATCH_DIR"/docker-layers-$cliflag/"$config"
    echo "$output"
    test $status -eq 0
    assert "$output" == ""
    run jq -r .container "$TEST_SCRATCH_DIR"/docker-layers-$cliflag/"$config"
    echo "$output"
    test $status -eq 0
    assert "$output" == null
    mkdir "$TEST_SCRATCH_DIR"/diff-docker-layers-$cliflag
    diff=$(dir_image_last_diff "$TEST_SCRATCH_DIR"/docker-layers-$cliflag)
    tar -C "$TEST_SCRATCH_DIR"/docker-layers-$cliflag -xf "$TEST_SCRATCH_DIR"/docker-layers-$cliflag/"$diff"
    hostname=$(< "$TEST_SCRATCH_DIR"/docker-layers-$cliflag/hostname.txt)
    assert $hostname = sandbox "expected the hostname to be the static value 'sandbox'"

    run_buildah build --"$cliflag"=$timestamp -t dir:"$TEST_SCRATCH_DIR"/docker-squashed-$cliflag --format=docker $TEST_SCRATCH_DIR/context
    config=$(dir_image_config "$TEST_SCRATCH_DIR"/docker-squashed-$cliflag)
    run jq -r .config.Hostname "$TEST_SCRATCH_DIR"/docker-squashed-$cliflag/"$config"
    echo "$output"
    test $status -eq 0
    assert "$output" == sandbox
    run jq -r .config.Domainname "$TEST_SCRATCH_DIR"/docker-squashed-$cliflag/"$config"
    echo "$output"
    test $status -eq 0
    assert "$output" == ""
    run jq -r .container "$TEST_SCRATCH_DIR"/docker-squashed-$cliflag/"$config"
    echo "$output"
    test $status -eq 0
    assert "$output" == null
    mkdir "$TEST_SCRATCH_DIR"/diff-docker-squashed-$cliflag
    diff=$(dir_image_last_diff "$TEST_SCRATCH_DIR"/docker-squashed-$cliflag)
    tar -C "$TEST_SCRATCH_DIR"/docker-squashed-$cliflag -xf "$TEST_SCRATCH_DIR"/docker-squashed-$cliflag/"$diff"
    hostname=$(< "$TEST_SCRATCH_DIR"/docker-squashed-$cliflag/hostname.txt)
    assert $hostname = sandbox "expected the hostname to be the static value 'sandbox'"
  done
}

@test "bud-sets-created-annotation" {
  _prefetch busybox
  mkdir ${TEST_SCRATCH_DIR}/buildcontext
  cat > ${TEST_SCRATCH_DIR}/buildcontext/Dockerfile << _EOF
  FROM busybox
  RUN pwd
_EOF
  for flagdir in default: timestamp:--timestamp=0 sde:--source-date-epoch=0 suppressed:--unsetannotation=org.opencontainers.image.created specific:--created-annotation=false explicit:--created-annotation=true ; do
    local flag=${flagdir##*:}
    local subdir=${flagdir%%:*}
    run_buildah build --layers --no-cache $flag -t oci:${TEST_SCRATCH_DIR}/$subdir ${TEST_SCRATCH_DIR}/buildcontext
    local manifest=${TEST_SCRATCH_DIR}/$subdir/$(oci_image_manifest ${TEST_SCRATCH_DIR}/$subdir)
    run jq -r '.annotations["org.opencontainers.image.created"]' "$manifest"
    assert $status -eq 0
    echo "$output"
    local manifestcreated="$output"
    local config=${TEST_SCRATCH_DIR}/$subdir/$(oci_image_config ${TEST_SCRATCH_DIR}/$subdir)
    run jq -r '.created' "$config"
    assert $status -eq 0
    echo "$output"
    local configcreated="$output"
    if [[ "$flag" =~ "=0" ]]; then
      assert $manifestcreated = $configcreated "manifest and config disagree on the image's created-time"
      assert $manifestcreated = "1970-01-01T00:00:00Z"
    elif [[ "$flag" =~ "unsetannotation" ]]; then
      assert $configcreated != ""
      assert $manifestcreated = "null"
    elif [[ "$flag" =~ "created-annotation=false" ]]; then
      assert $configcreated != ""
      assert $manifestcreated = "null"
    else
      assert $manifestcreated = $configcreated "manifest and config disagree on the image's created-time"
      assert $manifestcreated != ""
      assert $manifestcreated != "1970-01-01T00:00:00Z"
    fi
  done
}

@test "bud-with-source-date-epoch-arg" {
  _prefetch busybox
  local timestamp=60
  local datestamp="1970-01-01T00:01:00Z"
  mkdir -p $TEST_SCRATCH_DIR/buildcontext
  cat > $TEST_SCRATCH_DIR/buildcontext/Dockerfile <<EOF
  FROM busybox
  RUN echo \$SOURCE_DATE_EPOCH | tee /SOURCE_DATE_EPOCH_BEFORE
  ARG SOURCE_DATE_EPOCH
  RUN echo \$SOURCE_DATE_EPOCH | tee /SOURCE_DATE_EPOCH_AFTER
EOF
  run_buildah build --layers --no-cache --build-arg=SOURCE_DATE_EPOCH=$timestamp --rewrite-timestamp -t target $TEST_SCRATCH_DIR/buildcontext
  run_buildah from target
  local cid="$output"
  run_buildah run "$cid" cat /SOURCE_DATE_EPOCH_BEFORE
  assert "$output" = "" "SOURCE_DATE_EPOCH wasn't empty before it was declared"
  run_buildah run "$cid" cat /SOURCE_DATE_EPOCH_AFTER
  assert "$output" = "$timestamp" "SOURCE_DATE_EPOCH was wrong after it was declared"
  run_buildah build --layers --no-cache --build-arg=SOURCE_DATE_EPOCH=$timestamp --rewrite-timestamp -t oci:$TEST_SCRATCH_DIR/layout $TEST_SCRATCH_DIR/buildcontext
  local manifest=${TEST_SCRATCH_DIR}/layout/$(oci_image_manifest ${TEST_SCRATCH_DIR}/layout)
  run jq -r '.annotations."org.opencontainers.image.created"' $manifest
  assert $status = 0 "error running jq"
  assert "$output" = "$datestamp" "SOURCE_DATE_EPOCH build arg didn't affect image creation date annotation"
  local config=${TEST_SCRATCH_DIR}/layout/$(oci_image_config ${TEST_SCRATCH_DIR}/layout)
  run jq -r .created $config
  assert $status = 0 "error running jq"
  assert "$output" = "$datestamp" "SOURCE_DATE_EPOCH build arg didn't affect image config creation date"
}

@test "bud source-date-epoch and timestamp inform cache checks" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir $contextdir

  # We'll build a multiple-layer image based on busybox.
  cat > $contextdir/Dockerfile <<-EOF
  FROM busybox
  ARG SOURCE_DATE_EPOCH
  COPY Dockerfile /root/Dockerfile
  RUN pwd > /pwd.txt
EOF

  run_buildah build --layers=true --iidfile ${TEST_SCRATCH_DIR}/baseiidfile ${contextdir}
  local baseiid=$(< ${TEST_SCRATCH_DIR}/baseiidfile) iid

  # Try an assortment of layer-mutating flags.
  run_buildah build --layers=true --iidfile ${TEST_SCRATCH_DIR}/iidfile-sde --source-date-epoch=86400 ${contextdir}
  local iidsde=$(< ${TEST_SCRATCH_DIR}/iidfile-sde)
  assert "$iidsde" != "$baseiid"

  run_buildah build --layers=true --iidfile ${TEST_SCRATCH_DIR}/iidfile-ts --timestamp=86400 ${contextdir}
  local iidts=$(< ${TEST_SCRATCH_DIR}/iidfile-ts)
  assert "$iidts" != "$baseiid"
  assert "$iidts" != "$basesde"

  run_buildah build --layers=true --iidfile ${TEST_SCRATCH_DIR}/iidfile-rt --source-date-epoch=86400 --rewrite-timestamp ${contextdir}
  local iidrt=$(< ${TEST_SCRATCH_DIR}/iidfile-rt)
  assert "$iidrt" != "$baseiid"
  assert "$iidrt" != "$basets"
  assert "$iidrt" != "$basesde"

  run_buildah build --layers=true --iidfile ${TEST_SCRATCH_DIR}/iidfile --source-date-epoch=86400 ${contextdir}
  iid=$(< ${TEST_SCRATCH_DIR}/iidfile)
  assert "$iid" == "$iidsde"

  run_buildah build --layers=true --iidfile ${TEST_SCRATCH_DIR}/iidfile --timestamp=86400 ${contextdir}
  iid=$(< ${TEST_SCRATCH_DIR}/iidfile)
  assert "$iid" == "$iidts"

  run_buildah build --layers=true --iidfile ${TEST_SCRATCH_DIR}/iidfile --source-date-epoch=86400 --rewrite-timestamp ${contextdir}
  iid=$(< ${TEST_SCRATCH_DIR}/iidfile)
  assert "$iid" == "$iidrt"
}

@test "bud unset/inherit=false for labels and annotations inform cache checks" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir $contextdir

  # We'll build a multiple-layer image based on busybox.
  cat > $contextdir/Dockerfile <<-EOF
  FROM busybox
  COPY Dockerfile /root/Dockerfile
  RUN pwd > /pwd.txt
EOF

  # Build an image with a label and an annotation that we don't want in the end.
  run_buildah build --layers=true --annotation=annotation1=annotationvalue1 --annotation=annotation2=annotationvalue2 --label=label1=labelvalue1 --label=label2=labelvalue2 --iidfile ${TEST_SCRATCH_DIR}/baseiidfile ${contextdir}
  local baseiid=$(< ${TEST_SCRATCH_DIR}/baseiidfile) iid
  baseiid="${baseiid##*:}"
  assert "${baseiid}" != ""
  # Double-check that the label and annotation made it in.
  run_buildah inspect -t image "${baseiid}"
  run jq -r .ImageAnnotations <<< "${output}"
  assert "${status}" == 0
  assert "${output}" =~ annotation1
  assert "${output}" =~ annotation2
  run_buildah inspect -t image "${baseiid}"
  run jq -r .OCIv1.config.Labels <<< "${output}"
  assert "${status}" == 0
  assert "${output}" =~ label1
  assert "${output}" =~ label2

  # Derive future images from the one we just built.
  cat > $contextdir/Dockerfile <<-EOF
  FROM ${baseiid}
  COPY Dockerfile /root/Dockerfile
  RUN pwd > /pwd.txt
EOF

  # Build a derived image without that annotation.
  run_buildah build --layers=true --unsetannotation=annotation1 --iidfile ${TEST_SCRATCH_DIR}/unsetaiidfile ${contextdir}
  local unsetaiid=$(< ${TEST_SCRATCH_DIR}/unsetaiidfile)
  unsetaiid="${unsetaiid##*:}"
  assert "${unsetaiid}" != "${baseiid}"
  # Try it again.  We should get cache hits all the way to the end.
  run_buildah build --layers=true --unsetannotation=annotation1 --iidfile ${TEST_SCRATCH_DIR}/iidfile ${contextdir}
  iid=$(< ${TEST_SCRATCH_DIR}/iidfile)
  iid="${iid##*:}"
  assert "${iid}" == "${unsetaiid}"
  # Double-check that the annotation that we wanted to discard was discarded.
  run_buildah inspect -t image "${iid}"
  run jq -r .ImageAnnotations <<< "${output}"
  assert "${status}" == 0
  assert "${output}" !~ annotation1
  assert "${output}" =~ annotation2

  # Build a derived image without that label.
  run_buildah build --layers=true --unsetlabel=label1 --iidfile ${TEST_SCRATCH_DIR}/unsetliidfile ${contextdir}
  local unsetliid=$(< ${TEST_SCRATCH_DIR}/unsetliidfile)
  unsetliid="${unsetliid##*:}"
  assert "${unsetliid}" != "${baseiid}"
  # Try it again.  We should get cache hits all the way to the end.
  run_buildah build --layers=true --unsetlabel=label1 --iidfile ${TEST_SCRATCH_DIR}/iidfile ${contextdir}
  iid=$(< ${TEST_SCRATCH_DIR}/iidfile)
  iid="${iid##*:}"
  assert "${iid}" == "${unsetliid}"
  # Double-check that the label that we wanted to discard was discarded.
  run_buildah inspect -t image "${iid}"
  run jq -r .OCIv1.config.Labels <<< "${output}"
  assert "${status}" == 0
  assert "${output}" !~ label1
  assert "${output}" =~ label2

  # Build a derived image without any inherited annotations.
  run_buildah build --layers=true --inherit-annotations=false --iidfile ${TEST_SCRATCH_DIR}/disavowaiidfile ${contextdir}
  local disavowaiid=$(< ${TEST_SCRATCH_DIR}/disavowaiidfile)
  disavowaiid="${disavowaiid##*:}"
  assert "${disavowaiid}" != "${baseiid}"
  # Try it again.  We should get cache hits all the way to the end.
  run_buildah build --layers=true --inherit-annotations=false --iidfile ${TEST_SCRATCH_DIR}/iidfile ${contextdir}
  iid=$(< ${TEST_SCRATCH_DIR}/iidfile)
  iid="${iid##*:}"
  assert "${iid}" == "${disavowaiid}"
  assert "${iid}" != "${unsetaiid}"
  # Double-check that the annotations were discarded.
  run_buildah inspect -t image "${iid}"
  run jq -r .ImageAnnotations <<< "${output}"
  assert "${status}" == 0
  assert "${output}" !~ annotation1
  assert "${output}" !~ annotation2

  # Build a derived image without any inherited labels.
  run_buildah build --layers=true --inherit-labels=false --iidfile ${TEST_SCRATCH_DIR}/disavowliidfile ${contextdir}
  local disavowliid=$(< ${TEST_SCRATCH_DIR}/disavowliidfile)
  disavowliid="${disavowliid##*:}"
  assert "${disavowliid}" != "${baseiid}"
  # Try it again.  We should get cache hits all the way to the end.
  run_buildah build --layers=true --inherit-labels=false --iidfile ${TEST_SCRATCH_DIR}/iidfile ${contextdir}
  iid=$(< ${TEST_SCRATCH_DIR}/iidfile)
  iid="${iid##*:}"
  assert "${iid}" == "${disavowliid}"
  assert "${iid}" != "${unsetliid}"
  # Double-check that the labels were discarded.
  run_buildah inspect -t image "${iid}"
  run jq -r .OCIv1.config.Labels <<< "${output}"
  assert "${status}" == 0
  assert "${output}" !~ label1
  assert "${output}" !~ label2
}

@test "bud --link consistent diffID with --no-cache" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/link-diffid-nocache
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << EOF
FROM alpine
RUN echo "before link layer" > /before.txt
COPY --link test.txt /test.txt
RUN echo "after link layer" > /after.txt
RUN ls -l /test.txt
RUN cat /test.txt
EOF

  echo "test content" > $contextdir/test.txt

  run_buildah build --no-cache --layers $WITH_POLICY_JSON -t oci:${TEST_SCRATCH_DIR}/oci-layout1 $contextdir

  run_buildah build --no-cache --layers $WITH_POLICY_JSON -t oci:${TEST_SCRATCH_DIR}/oci-layout2 $contextdir

  diffid1=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-layout1 2)
  diffid2=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-layout2 2)
  echo $diffid1
  echo $diffid2

  assert "$diffid1" = "$diffid2" "COPY --link should produce identical diffIDs with --no-cache"
}

@test "bud --link consistent diffID with different base images" {
  _prefetch alpine busybox
  local contextdir=${TEST_SCRATCH_DIR}/bud/link-diffid-bases
  mkdir -p $contextdir

  echo "shared content" > $contextdir/shared.txt

  cat > $contextdir/Containerfile.alpine << EOF
FROM alpine
RUN echo "alpine setup" > /setup.txt
COPY --link shared.txt /shared.txt
RUN echo "alpine complete" > /complete.txt
RUN cat /shared.txt
EOF

  cat > $contextdir/Containerfile.busybox << EOF
FROM busybox
RUN echo "busybox setup" > /setup.txt
COPY --link shared.txt /shared.txt
RUN echo "busybox complete" > /complete.txt
RUN cat /shared.txt
EOF

  run_buildah build --no-cache --layers $WITH_POLICY_JSON -f $contextdir/Containerfile.alpine -t oci:${TEST_SCRATCH_DIR}/oci-layout-alpine $contextdir

  run_buildah build --no-cache --layers $WITH_POLICY_JSON -f $contextdir/Containerfile.busybox -t oci:${TEST_SCRATCH_DIR}/oci-layout-busybox $contextdir

  diffid_alpine=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-layout-alpine 2)
  diffid_busybox=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-layout-busybox 2)

  assert "$diffid_alpine" = "$diffid_busybox" "COPY --link should produce identical diffIDs regardless of base image"
}

@test "bud --link consistent diffID with multiple files" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/link-diffid-multi
  mkdir -p $contextdir/subdir

  echo "file1" > $contextdir/file1.txt
  echo "file2" > $contextdir/file2.txt
  echo "subfile" > $contextdir/subdir/sub.txt

  cat > $contextdir/Dockerfile << EOF
FROM alpine
RUN echo "setup" > /setup.txt
COPY --link file1.txt file2.txt /files/
ADD --link subdir /subdir
RUN echo "complete" > /complete.txt
RUN ls -l /files/
EOF

  run_buildah build --no-cache --layers $WITH_POLICY_JSON -t oci:${TEST_SCRATCH_DIR}/oci-layout-multi1 $contextdir

  run_buildah build --no-cache --layers $WITH_POLICY_JSON -t oci:${TEST_SCRATCH_DIR}/oci-layout-multi2 $contextdir

  copy_diffid1=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-layout-multi1 2)
  copy_diffid2=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-layout-multi2 2)
  add_diffid1=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-layout-multi1 3)
  add_diffid2=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-layout-multi2 3)

  assert "$copy_diffid1" = "$copy_diffid2" "COPY --link with multiple files should have consistent diffID"
  assert "$add_diffid1" = "$add_diffid2" "ADD --link should have consistent diffID"
}

@test "bud --link with glob patterns consistent diffID" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/link-diffid-glob
  mkdir -p $contextdir

  echo "test1" > $contextdir/test1.txt
  echo "test2" > $contextdir/test2.txt
  echo "json1" > $contextdir/data1.json
  echo "json2" > $contextdir/data2.json

  cat > $contextdir/Dockerfile << EOF
FROM alpine
COPY --link test*.txt /tests/
COPY --link *.json /data/
RUN echo "globbing complete" > /complete.txt
RUN ls -l /tests/
RUN ls -l /data/
EOF

  run_buildah build --no-cache --layers $WITH_POLICY_JSON -t oci:${TEST_SCRATCH_DIR}/oci-glob-1 $contextdir

  run_buildah build --no-cache --layers $WITH_POLICY_JSON -t oci:${TEST_SCRATCH_DIR}/oci-glob-2 $contextdir

  glob_txt_diffid1=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-glob-1 1)
  glob_txt_diffid2=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-glob-2 1)
  glob_json_diffid1=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-glob-1 2)
  glob_json_diffid2=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-glob-2 2)

  assert "$glob_txt_diffid1" = "$glob_txt_diffid2" "COPY --link with glob *.txt should have consistent diffID"
  assert "$glob_json_diffid1" = "$glob_json_diffid2" "COPY --link with glob *.json should have consistent diffID"
}

@test "bud --link cache behavior basic" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/link-cache
  mkdir -p $contextdir

  echo "test content" > $contextdir/testfile.txt

  cat > $contextdir/Dockerfile << EOF
FROM alpine
COPY --link testfile.txt /testfile.txt
RUN echo "build complete" > /complete.txt
RUN cat /testfile.txt
EOF

  # First build
  run_buildah build --layers $WITH_POLICY_JSON -t link-cache1 $contextdir

  run_buildah build --layers $WITH_POLICY_JSON -t link-cache2 $contextdir
  assert "$output" =~ "Using cache"

  # Modify content
  echo "modified content" > $contextdir/testfile.txt

  run_buildah build --layers $WITH_POLICY_JSON -t link-cache3 $contextdir
  assert "$output" !~ "STEP 2/3: COPY --link testfile.txt /testfile.txt"$'\n'".*Using cache"
}

@test "bud --link with chmod and chown" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/link-perms
  mkdir -p $contextdir

  echo "test content" > $contextdir/testfile.txt

  cat > $contextdir/Dockerfile << EOF
FROM alpine
COPY --link --chmod=755 --chown=1000:1000 testfile.txt /testfile.txt
RUN stat -c '%u:%g %a' /testfile.txt
EOF

  run_buildah build --layers $WITH_POLICY_JSON -t link-perms $contextdir
  expect_output --substring "1000:1000 755"
}

@test "bud --link multi-stage build cache" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/link-multistage
  mkdir -p $contextdir

  echo "stage1 content" > $contextdir/stage1.txt
  echo "stage2 content" > $contextdir/stage2.txt

  cat > $contextdir/Dockerfile << EOF
FROM alpine AS stage1
ADD --link stage1.txt /stage1.txt
RUN echo "stage1 complete" > /complete.txt
RUN cat /stage1.txt

FROM alpine AS stage2
COPY --from=stage1 /stage1.txt /from-stage1.txt
ADD --link stage2.txt /stage2.txt
RUN echo "stage2 complete" > /complete.txt
RUN cat /stage2.txt

EOF

  run_buildah build --layers $WITH_POLICY_JSON -t link-multistage1 $contextdir

  run_buildah build --layers $WITH_POLICY_JSON -t link-multistage2 $contextdir
  assert "$output" =~ "Using cache"

  run_buildah from --name test-ctr link-multistage2
  run_buildah run test-ctr cat /from-stage1.txt
  expect_output "stage1 content"
  run_buildah run test-ctr cat /stage2.txt
  expect_output "stage2 content"
  run_buildah rm test-ctr
}

@test "bud --link ADD with remote URL consistent diffID" {
  _prefetch alpine
  local contentdir=${TEST_SCRATCH_DIR}/content
  mkdir -p $contentdir
  echo this is a readmin > ${contentdir}/README.md
  starthttpd ${contentdir}
  local contextdir=${TEST_SCRATCH_DIR}/bud/link-url
  mkdir -p $contextdir

  cat > $contextdir/Dockerfile << EOF
FROM alpine
ADD --link http://0.0.0.0:${HTTP_SERVER_PORT}/README.md /README.md
RUN echo "remote add complete" > /complete.txt
RUN cat /README.md
EOF

  run_buildah build --no-cache --layers $WITH_POLICY_JSON -t oci:${TEST_SCRATCH_DIR}/oci-url1 $contextdir
  run_buildah build --no-cache --layers $WITH_POLICY_JSON -t oci:${TEST_SCRATCH_DIR}/oci-url2 $contextdir

  diffid1=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-url1 1)
  diffid2=$(oci_image_diff_id ${TEST_SCRATCH_DIR}/oci-url2 1)

  assert "$diffid1" = "$diffid2" "ADD --link with URL should have consistent diffID"
}

@test "bud --link respects dockerignore" {
  _prefetch alpine
  local contextdir=${TEST_SCRATCH_DIR}/bud/link-ignore
  mkdir -p $contextdir

  echo "included" > $contextdir/included.txt
  echo "excluded" > $contextdir/excluded.txt

  echo "excluded.txt" > $contextdir/.dockerignore

  cat > $contextdir/Dockerfile << EOF
FROM alpine
RUN echo "Starting" > /start.txt
COPY --link *.txt /files/
RUN echo "Ending" > /end.txt
RUN ls -l /files/
EOF

  run_buildah build --layers $WITH_POLICY_JSON -t link-ignore $contextdir
  expect_output --substring "included.txt"
  assert "$output" !~ "excluded.txt"
}

@test "bud multi-platform --layers should create separate images for each platform" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/bud/multiplatform-layers
  mkdir -p $contextdir

  # Create a simple Dockerfile that starts from scratch to test the specific caching bug
  cat > $contextdir/hello.txt << _EOF
Hello from multi-platform build!
_EOF

  cat > $contextdir/Dockerfile << _EOF
FROM scratch
COPY hello.txt /hello.txt
CMD ["/hello.txt"]
_EOF

  # Build for multiple platforms with --layers and --manifest
  # This should create separate images for each platform, not reuse cached images incorrectly
  local manifestname="multiplatform-test-$(safename)"
  run_buildah build $WITH_POLICY_JSON --layers --platform linux/amd64,linux/arm64 --manifest "$manifestname" -f $contextdir/Dockerfile $contextdir

  # Verify that the build completed successfully
  expect_output --substring "linux/amd64"
  expect_output --substring "linux/arm64"

  # Check that we have a manifest list
  run_buildah manifest exists "$manifestname"

  # Inspect the manifest list to verify it contains entries for both platforms
  run_buildah manifest inspect "$manifestname"
  local manifest_content="$output"

  # Verify the manifest contains both amd64 and arm64 entries
  assert "$manifest_content" =~ "amd64" "manifest should contain amd64 platform"
  assert "$manifest_content" =~ "arm64" "manifest should contain arm64 platform"

  # Count the number of manifests - should have at least 2 (one for each platform)
  run jq -r '.manifests | length' <<< "$manifest_content"
  local manifest_count="$output"
  assert "$manifest_count" -ge 2 "manifest list should contain at least 2 platform-specific manifests"

  # Verify that the platforms are correctly specified in the manifest
  run jq -r '.manifests[].platform.architecture' <<< "$manifest_content"
  local architectures="$output"
  assert "$architectures" =~ "amd64" "manifest should list amd64 architecture"
  assert "$architectures" =~ "arm64" "manifest should list arm64 architecture"

  # Verify we have multiple different image IDs (not the same image reused)
  # This is the key test - before the fix, the same image would be reused for both platforms
  run jq -r '.manifests[].digest' <<< "$manifest_content"
  local digests=($output)

  # Each platform should have a unique digest - they should not be the same
  if [ "${#digests[@]}" -ge 2 ]; then
    local first_digest="${digests[0]}"
    local found_different=false
    for digest in "${digests[@]:1}"; do
      if [ "$digest" != "$first_digest" ]; then
        found_different=true
        break
      fi
    done
    assert "$found_different" = true "each platform should have a unique image digest, not reuse the same cached image"
  fi
}

@test "bud multi-platform --layers should not reuse different platform if default platform is being used" {
  run_buildah info --format '{{.host.arch}}'
  myarch="$output"
  otherarch="ppc64le"

  # just make sure that other arch is not equivalent to host arch
  if [[ "$otherarch" == "$myarch" ]]; then
    otherarch="amd64"
  fi
  run_buildah build --layers --platform linux/$otherarch -f $BUDFILES/from-scratch/Containerfile2 $BUDFILES/from-scratch/
  # Second build should not use cache at all
  run_buildah build --layers -f $BUDFILES/from-scratch/Containerfile2 $BUDFILES/from-scratch/
  assert "$output" !~ "Using cache"
}

@test "bud --layers should not include pulled up parent directories of mount points" {
  _prefetch quay.io/libpod/busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir $contextdir
  mkdir $contextdir/dev
  mkdir $contextdir/proc
  mkdir $contextdir/sys

  cat > $contextdir/Dockerfile << _EOF
FROM scratch
COPY / /
COPY --from=quay.io/libpod/busybox / /
RUN rm -f /Dockerfile
RUN --mount=type=bind,ro,src=/Dockerfile,target=/var/spool/mail/tmpfile touch /newfile
_EOF

  # the most recent layer should only have the one newly-added file in it
  run_buildah build --layers -t oci:${TEST_SCRATCH_DIR}/oci-image ${contextdir}
  lastlayer=$(oci_image_last_diff ${TEST_SCRATCH_DIR}/oci-image)
  run tar tf ${TEST_SCRATCH_DIR}/oci-image/"${lastlayer}"
  echo "$output"
  assert "$status" = "0"
  assert "${#lines[*]}" = "1"
}

@test "bud should include excluded pulled up parent directories in squashed images" {
  _prefetch quay.io/libpod/busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir $contextdir
  mkdir $contextdir/dev
  mkdir $contextdir/etc
  mkdir $contextdir/proc
  mkdir $contextdir/sys

  cat > $contextdir/Dockerfile << _EOF
FROM scratch
COPY / /
COPY --from=quay.io/libpod/busybox / /
RUN rm -f /Dockerfile
RUN --mount=type=bind,ro,src=/Dockerfile,target=/etc/removed-file --mount=type=bind,ro,src=/Dockerfile,target=/var/spool/mail/tmpfile touch /newfile
_EOF

  local source_date_epoch=$(date +%s)
  # build a copy of the image that's all squashed from the start
  run_buildah build --no-cache --squash=true  --source-date-epoch=$source_date_epoch --rewrite-timestamp -t dir:${TEST_SCRATCH_DIR}/squashed-image ${contextdir}
  # build a copy of the image where we only commit at the end
  run_buildah build --no-cache --layers=false --source-date-epoch=$source_date_epoch --rewrite-timestamp -t not-layered-image  ${contextdir}
  # build a copy of the image where we commit at every step
  run_buildah build --no-cache --layers=true  --source-date-epoch=$source_date_epoch --rewrite-timestamp -t layered-image ${contextdir}
  # now squash them, with no internal record of needing to exclude anything carrying over
  run_buildah from --name not-layered-container not-layered-image
  run_buildah commit --squash not-layered-container dir:${TEST_SCRATCH_DIR}/squashed-not-layered-image
  run_buildah from --name layered-container layered-image
  run_buildah commit --squash layered-container dir:${TEST_SCRATCH_DIR}/squashed-layered-image
  # find the diffs for the three versions, which should look the same thanks to the --source-date-epoch --rewrite-timestamp combo
  local squashed=${TEST_SCRATCH_DIR}/squashed-image/$(dir_image_last_diff ${TEST_SCRATCH_DIR}/squashed-image)
  local notlayered=${TEST_SCRATCH_DIR}/squashed-not-layered-image/$(dir_image_last_diff ${TEST_SCRATCH_DIR}/squashed-not-layered-image)
  local layered=${TEST_SCRATCH_DIR}/squashed-layered-image/$(dir_image_last_diff ${TEST_SCRATCH_DIR}/squashed-layered-image)
  tar tvf ${squashed} > ${TEST_SCRATCH_DIR}/squashed-image-rootfs.txt
  tar tvf ${notlayered} > ${TEST_SCRATCH_DIR}/squashed-not-layered-image-rootfs.txt
  tar tvf ${layered} > ${TEST_SCRATCH_DIR}/squashed-layered-image-rootfs.txt
  diff -u ${TEST_SCRATCH_DIR}/squashed-layered-image-rootfs.txt ${TEST_SCRATCH_DIR}/squashed-image-rootfs.txt
  diff -u ${TEST_SCRATCH_DIR}/squashed-layered-image-rootfs.txt ${TEST_SCRATCH_DIR}/squashed-not-layered-image-rootfs.txt
}

@test "bud COPY one file to ..../. creates the destination directory" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p ${contextdir}
  echo hello, this is a file > ${contextdir}/file.txt
  cat > ${contextdir}/Dockerfile << _EOF
FROM busybox
COPY /file.txt /app/.
WORKDIR /app
RUN pwd
_EOF
  run_buildah build --layers ${contextdir}
  run_buildah build ${contextdir}
}

@test "bud with a previously-used graphroot with base image in it used as imagestore" {
  # there are subtle differences between "this was always the imagestore" and
  # "this was a graphroot, but i'm using it as an imagestore now"
  case "${STORAGE_DRIVER}" in
    overlay) ;;
    *) skip "imagestore flag is compatible with overlay, but not ${STORAGE_DRIVER}" ;;
  esac

  _prefetch quay.io/libpod/alpine:latest
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p ${contextdir}
  cat > ${contextdir}/Dockerfile << _EOF
FROM quay.io/libpod/alpine:latest
RUN mkdir hello
_EOF

  run_buildah --root=${TEST_SCRATCH_DIR}/newroot --storage-opt=imagestore=${TEST_SCRATCH_DIR}/root build --pull=never --no-cache --layers=true ${contextdir}
  run_buildah --root=${TEST_SCRATCH_DIR}/newroot --storage-opt=imagestore=${TEST_SCRATCH_DIR}/root build --pull=never ${contextdir}
  run_buildah --root=${TEST_SCRATCH_DIR}/newroot --storage-opt=imagestore=${TEST_SCRATCH_DIR}/root build --pull=never --squash ${contextdir}
}

@test "bud with exec-form RUN instruction" {
  # This test requires a statically linked busybox which is not
  # the case for quay.io/libpod/busybox on ppc64le & s390x
  skip_unless_arch amd64 arm64
  baseimage=busybox
  _prefetch $baseimage
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"
  cat > "${contextdir}"/Dockerfile <<-EOF
  FROM scratch AS mkdir
  RUN --mount=type=bind,from="${baseimage}",destination=/usr ["busybox", "sh", "-x", "-c", "mkdir /brand-new-subdir"]
  FROM "${baseimage}"
  RUN --mount=type=bind,from=mkdir,destination=/mounted find /mounted -print
EOF
  run_buildah build --layers=true "${contextdir}"
  expect_output --substring /mounted/brand-new-subdir
  run_buildah build --layers=false "${contextdir}"
  expect_output --substring /mounted/brand-new-subdir
}

@test "bud with --iidfile" {
  target=scratch-image
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"
  cat > "${contextdir}"/Dockerfile <<-EOF
  FROM scratch
  COPY . .
EOF
  for layers in "--layers=true" "--layers=false" ; do
    for destination in "dir:${TEST_SCRATCH_DIR}/dir" "oci-archive:${TEST_SCRATCH_DIR}/oci-archive" "docker-archive:${TEST_SCRATCH_DIR}/docker-archive" "oci:${TEST_SCRATCH_DIR}/oci-layout" "local" ; do
      rm -f "${TEST_SCRATCH_DIR}"/iidfile
      fsname="${destination#*:}" # assume : is used in a non-containers-storage name rather than a repository name + tag combination
      if test "${fsname}" != "${destination}" ; then
        rm -fr "${fsname}"
      fi
      run_buildah build --iidfile "${TEST_SCRATCH_DIR}"/iidfile --no-cache "${layers}" -t "${destination}" "${contextdir}"
      local iid=$(< "${TEST_SCRATCH_DIR}"/iidfile)
      assert "${iid}" != ""
      assert "${iid#sha256:}" != ""
      if test "${fsname}" != "${destination}" ; then
        test -e "${fsname}"
      fi
    done
  done
}

@test "bud with --iidfile-raw" {
  target=scratch-image
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"
  cat > "${contextdir}"/Dockerfile <<-EOF
  FROM scratch
  COPY . .
EOF
  for layers in "--layers=true" "--layers=false" ; do
    for destination in "dir:${TEST_SCRATCH_DIR}/dir" "oci-archive:${TEST_SCRATCH_DIR}/oci-archive" "docker-archive:${TEST_SCRATCH_DIR}/docker-archive" "oci:${TEST_SCRATCH_DIR}/oci-layout" "local" ; do
      rm -f "${TEST_SCRATCH_DIR}"/iidfile-raw
      fsname="${destination#*:}" # assume : is used in a non-containers-storage name rather than a repository name + tag combination
      if test "${fsname}" != "${destination}" ; then
        rm -fr "${fsname}"
      fi
      run_buildah build --iidfile-raw "${TEST_SCRATCH_DIR}"/iidfile-raw --no-cache "${layers}" -t "${destination}" "${contextdir}"
      local iid_raw=$(< "${TEST_SCRATCH_DIR}"/iidfile-raw)
      # iidfile-raw should contain just the hash without sha256: prefix
      assert "${iid_raw}" != ""
      assert "${iid_raw}" =~ "^[0-9a-f]{64}$"
      # Verify it doesn't contain sha256: prefix
      assert "${iid_raw}" !~ "sha256:"
      if test "${fsname}" != "${destination}" ; then
        test -e "${fsname}"
      fi
    done
  done
}

@test "bud with --iidfile and --iidfile-raw comparison" {
  target=scratch-image
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"
  cat > "${contextdir}"/Dockerfile <<-EOF
  FROM scratch
  COPY . .
EOF
  # Build with both --iidfile and --iidfile-raw to verify they write the same image ID
  run_buildah build --iidfile "${TEST_SCRATCH_DIR}"/iidfile --iidfile-raw "${TEST_SCRATCH_DIR}"/iidfile-raw -t "${target}" "${contextdir}"
  local iid=$(< "${TEST_SCRATCH_DIR}"/iidfile)
  local iid_raw=$(< "${TEST_SCRATCH_DIR}"/iidfile-raw)

  # iid should have sha256: prefix, iid_raw should not
  assert "${iid}" =~ "^sha256:[0-9a-f]{64}$"
  assert "${iid_raw}" =~ "^[0-9a-f]{64}$"

  # The hash portion should be identical
  assert "${iid#sha256:}" == "${iid_raw}"
}

@test "bud with --raw-iidfile alias" {
  target=scratch-image
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"
  cat > "${contextdir}"/Dockerfile <<-EOF
  FROM scratch
  COPY . .
EOF
  # Test that --raw-iidfile works as an alias for --iidfile-raw
  run_buildah build --raw-iidfile "${TEST_SCRATCH_DIR}"/iidfile-alias -t "${target}" "${contextdir}"
  local iid_alias=$(< "${TEST_SCRATCH_DIR}"/iidfile-alias)

  # Should contain just the hash without sha256: prefix
  assert "${iid_alias}" != ""
  assert "${iid_alias}" =~ "^[0-9a-f]{64}$"
  assert "${iid_alias}" !~ "sha256:"
}

@test "build-oci-archive-switch" {
  local base=busybox
  _prefetch $base
  run_buildah from -q $base
  run_buildah inspect --format '{{.FromImageID}}' "$output"
  local imageID="$output"
  mkdir -p ${TEST_SCRATCH_DIR}/buildcontext
  copy containers-storage:$imageID oci-archive:${TEST_SCRATCH_DIR}/buildcontext/source-oci-archive.tar
  copy containers-storage:$imageID oci:${TEST_SCRATCH_DIR}/buildcontext/source-oci
  copy containers-storage:$imageID docker-archive:${TEST_SCRATCH_DIR}/buildcontext/source-docker-archive.tar
  copy containers-storage:$imageID dir:${TEST_SCRATCH_DIR}/buildcontext/source-dir
  pushd ${TEST_SCRATCH_DIR}
  cat > ${TEST_SCRATCH_DIR}/buildcontext/Dockerfile << EOF
  FROM $base
  RUN --mount=type=bind,rw,target=/bc mkdir /bc/oci-archive /bc/oci /bc/docker-archive /bc/dir
  RUN --mount=type=bind,rw,target=/bc cp /bc/source-oci-archive.tar /bc/oci-archive/archive.tar
  RUN --mount=type=bind,rw,target=/bc cp -a /bc/source-oci/* /bc/oci/
  RUN --mount=type=bind,rw,target=/bc cp /bc/source-docker-archive.tar /bc/docker-archive/archive.tar
  RUN --mount=type=bind,rw,target=/bc cp -a /bc/source-dir/* /bc/dir/
EOF
  local stage=0
  local mounts=
  for base in oci-archive:oci-archive/archive.tar oci:oci docker-archive:docker-archive/archive.tar dir:dir ; do
    local dirsuffix=
    case $base in
      dir:*|oci:*)
        dirsuffix=/
        ;;
    esac
    for prefix in "" / ./ ; do
      for suffix in "" ${dirsuffix} ; do
        local adjustedbase=${base/:/:${prefix}}${suffix}
        echo FROM $adjustedbase >> ${TEST_SCRATCH_DIR}/buildcontext/Dockerfile
        mounts="$mounts --mount=type=bind,from=$stage,target=/var/tmp/$stage"
        echo "RUN$mounts :" >> ${TEST_SCRATCH_DIR}/buildcontext/Dockerfile
        stage=$((stage+1))
      done
    done
  done
  run_buildah build ${TEST_SCRATCH_DIR}/buildcontext
}

@test "build-with-run-mount" {
  local out_path=${TEST_SCRATCH_DIR}/run-mount.tar

  # add an ephemeral mount
  FOO=world run_buildah build \
      --secret id=mysecret,env=FOO \
      --mount type=secret,id=mysecret,dst=/bar,required \
      --timestamp 0 \
      --tag oci-archive:"${out_path}.a" \
      -f <(printf 'FROM busybox\nRUN echo "hello $(cat /bar) welcome"')
  expect_output --substring "hello world welcome"

  # if we run the same, but no secret and no mount, we should get identical tarball
  run_buildah build \
      --timestamp 0 \
      --tag oci-archive:"${out_path}.b" \
      -f <(printf 'FROM busybox\nRUN echo "hello $(cat /bar) welcome"')
  expect_output --substring "hello  welcome"

  # should be the same, as the ephemeral run mount should not affect the result
  diff "${out_path}.a" "${out_path}.b"
}

@test "build-explicitly-from-storage" {
  _prefetch busybox
  run_buildah inspect --type=image --format '{{.FromImageID}}' busybox
  local iid="${output}"
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"
  cat > "${contextdir}"/Containerfile <<- _EOF
  ARG BASE=containers-storage:busybox
  FROM \$BASE
  RUN date | tee /root/date.txt
_EOF
  for base in containers-storage:docker.io/library/busybox containers-storage:docker.io/busybox containers-storage:busybox containers-storage:${iid}; do
    run_buildah build --build-arg BASE="${base}" "${contextdir}"
  done
}

@test "build-with-run-image-mount" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"
  cat > "${contextdir}"/Containerfile << _EOF
  FROM busybox
  RUN cat /alpine/etc/os-release | tee /alpine.os-release
_EOF
  run_buildah build --mount=type=bind,from=alpine,target=/alpine "${contextdir}"
}

@test "bud with FROM --after" {
  # This tests the 'FROM --after' workflow, i.e. that:
  # - --after is enough to pull in the builder stage as a dep
  # - even with --jobs=4, --after forces us to wait before even trying to import
  #   the image
  # - build arguments for --after are handled correctly
  # - the final "built" image matches the after stage output exactly
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"
  copy containers-storage:busybox oci-archive:"${contextdir}"/busybox.ociarchive
  # Re-import it so we have it in OCI format and not v2s2 so we can compare
  # image IDs later on.
  copy oci-archive:"${contextdir}"/busybox.ociarchive containers-storage:busybox-oci
  cat > "${contextdir}"/Containerfile << 'EOF'
ARG TESTARG=builder
FROM busybox AS builder
# copy it to a different name as proof that this RUN stage must've run
RUN --mount=type=bind,target=/src,rw cp /src/busybox.ociarchive /src/out.ociarchive

FROM --after=${TESTARG} oci-archive:out.ociarchive
EOF
  run_buildah build $WITH_POLICY_JSON --jobs=4 -t test-after "${contextdir}"
  # Verify the final image is identical to the OCI-converted busybox
  run_buildah inspect --format '{{.FromImageID}}' busybox-oci
  local busybox_id="$output"
  run_buildah inspect --format '{{.FromImageID}}' test-after
  local test_after_id="$output"
  assert "$busybox_id" == "$test_after_id"
}

@test "bud with FROM --after and COPY --from" {
  _prefetch busybox
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"
  cat > "${contextdir}"/Containerfile << 'EOF'
FROM busybox AS builder
RUN echo "Artifact Created" > /data.txt

FROM busybox AS verifier
COPY --from=builder /data.txt /data.txt
RUN echo "Artifact Appended" >> /data.txt

# this must wait until the builder stage is finished
FROM --after=builder busybox AS final
# this must wait until the verifier stage is finished
COPY --from=verifier /data.txt /root/data.txt
RUN grep "Artifact Created" /root/data.txt
RUN grep "Artifact Appended" /root/data.txt
EOF
  run_buildah build $WITH_POLICY_JSON --jobs=4 -t test-after-copy-from "${contextdir}"
}

@test "bud with FROM --after error cases" {
  local contextdir=${TEST_SCRATCH_DIR}/context
  mkdir -p "${contextdir}"

  # self-reference should fail
  cat > "${contextdir}"/Containerfile << 'EOF'
FROM --after=self scratch AS self
EOF
  run_buildah 125 build $WITH_POLICY_JSON "${contextdir}"
  expect_output --substring "FROM --after=self: cannot depend on later stage"

  # attempt a circular dependency
  cat > "${contextdir}"/Containerfile << 'EOF'
FROM --after=second busybox AS first
RUN echo first
FROM first AS second
RUN echo second
EOF
  run_buildah 125 build $WITH_POLICY_JSON "${contextdir}"
  expect_output --substring "FROM --after=second: stage \"second\" not found"
}

@test "bud-windows-host-process-container" {
  skip_if_rootless_environment
  # Test building a Windows container image on Linux
  # This validates that the Windows layer mutations are applied correctly
  _prefetch alpine

  local contextdir=${TEST_SCRATCH_DIR}/windows-context
  mkdir -p ${contextdir}

  # Create a test file to copy into the Windows image
  echo "test" > ${contextdir}/testfile

  # Create inline Dockerfile for Windows container
  cat > ${contextdir}/Dockerfile << _EOF
FROM alpine as build
RUN echo "test" > testfile

FROM --platform=windows/amd64 mcr.microsoft.com/oss/kubernetes/windows-host-process-containers-base-image:v1.0.0
COPY --from=build testfile /testfile
ENV PATH="C:\Windows\system32;C:\Windows;C:\WINDOWS\System32\WindowsPowerShell\v1.0\;"
USER ContainerAdministrator
_EOF

  # Build the Windows container image directly to OCI format
  local ocidir=${TEST_SCRATCH_DIR}/oci-windows
  run_buildah build $WITH_POLICY_JSON -t oci:${ocidir} ${contextdir}

  # Create a container from the OCI image and mount it
  run_buildah from --quiet oci:${ocidir}
  # Extract just the container ID (last line), ignoring warning messages
  cid=$(echo "$output" | tail -1)
  run_buildah_mount ${cid}
  root=$(echo "$output" | tail -1)

  # Verify the Windows-specific directory structure was created
  # The Windows mutator should create /Hives and /Files directories
  test -d ${root}/Hives/
  test -d ${root}/Files/

  # Verify that the copied testfile exists in the correct location
  # The COPY instruction copies to /testfile, but the Windows mutator
  # should relocate it to /Files/testfile
  test -f ${root}/Files/testfile

  # Verify Windows-specific PAX attributes are present in the layer
  # Get the last layer blob using helper function
  local layer_file=${ocidir}/$(oci_image_last_diff ${ocidir})

  # Decompress the layer and check for Windows-specific PAX headers
  # The layer contains PAX extended headers with MSWINDOWS.fileattr
  local layer_tar=${TEST_SCRATCH_DIR}/layer.tar
  gzip -dc ${layer_file} > ${layer_tar} 2>/dev/null || cat ${layer_file} > ${layer_tar}

  # Verify the layer contains the expected Windows structure
  run tar -tf ${layer_tar}
  expect_output --substring "Hives/"
  expect_output --substring "Files/testfile"

  # Check for Windows-specific PAX headers on all expected paths
  # Each file/directory should have: MSWINDOWS.fileattr, MSWINDOWS.rawsd, and LIBARCHIVE.creationtime
  # Use tar's verbose output which shows when it encounters these headers
  local required_paths=("Hives/" "Files/" "Files/testfile")

  # Run tar and capture its stderr which contains PAX header warnings
  local tar_output=${TEST_SCRATCH_DIR}/tar_output.txt
  tar -tvf ${layer_tar} 2>&1 | tee ${tar_output}

  # For each path, verify that the PAX headers appear immediately before it
  for path in "${required_paths[@]}"; do
    # Extract lines before this path entry (PAX warnings appear just before the file)
    # We need to find the block of PAX warnings that precede this specific path
    local line_num=$(grep -n "^.*${path}\$" ${tar_output} | head -1 | cut -d: -f1)

    if [ -z "${line_num}" ]; then
      echo "ERROR: Path ${path} not found in tar listing"
      false
    fi

    # Get the preceding 10 lines which should contain PAX header warnings
    local context_start=$((line_num - 10))
    if [ ${context_start} -lt 1 ]; then
      context_start=1
    fi
    local preceding_lines=$(sed -n "${context_start},$((line_num - 1))p" ${tar_output})

    # Check for required headers in the preceding PAX warnings
    if ! echo "${preceding_lines}" | grep -q "MSWINDOWS.fileattr" ; then
      echo "ERROR: MSWINDOWS.fileattr not found for ${path}"
      echo "Context around ${path}:"
      sed -n "${context_start},${line_num}p" ${tar_output}
      false
    fi

    if ! echo "${preceding_lines}" | grep -q "LIBARCHIVE.creationtime" ; then
      echo "ERROR: LIBARCHIVE.creationtime not found for ${path}"
      echo "Context around ${path}:"
      sed -n "${context_start},${line_num}p" ${tar_output}
      false
    fi

    # MSWINDOWS.rawsd should be present for all paths (files and directories)
    if ! echo "${preceding_lines}" | grep -q "MSWINDOWS.rawsd" ; then
      echo "ERROR: MSWINDOWS.rawsd not found for ${path}"
      echo "Context around ${path}:"
      sed -n "${context_start},${line_num}p" ${tar_output}
      false
    fi
  done

  # Cleanup
  run_buildah_umount ${cid}
  run_buildah rm ${cid}
}

@test "bud-windows-with-timestamp-reproducible" {
  skip_if_rootless_environment

  _prefetch alpine

  local contextdir=${TEST_SCRATCH_DIR}/windows-context
  mkdir -p ${contextdir}

  cat > ${contextdir}/Dockerfile << _EOF
FROM alpine as build
RUN echo "test" > testfile

FROM --platform=windows/amd64 mcr.microsoft.com/oss/kubernetes/windows-host-process-containers-base-image:v1.0.0
COPY --from=build testfile /testfile
_EOF

  # Build twice with the same timestamp - should produce identical results
  local timestamp=86400
  run_buildah build $WITH_POLICY_JSON --timestamp=${timestamp} -t oci:${TEST_SCRATCH_DIR}/windows-a ${contextdir}
  sleep 1.1 # sleep at least 1 second, so that timestamps are incremented
  run_buildah build $WITH_POLICY_JSON --timestamp=${timestamp} -t oci:${TEST_SCRATCH_DIR}/windows-b ${contextdir}

  # should be the same (all timestamps including Hives and Files directories should be identical)
  diff -r "${TEST_SCRATCH_DIR}/windows-a" "${TEST_SCRATCH_DIR}/windows-b"

  # Verify that all files in the layer have the correct timestamp
  local layer=${TEST_SCRATCH_DIR}/windows-a/$(oci_image_last_diff ${TEST_SCRATCH_DIR}/windows-a)
  mkdir -p ${TEST_SCRATCH_DIR}/layer
  gzip -dc ${layer} > ${TEST_SCRATCH_DIR}/layer.tar 2>/dev/null || cat ${layer} > ${TEST_SCRATCH_DIR}/layer.tar
  tar -C ${TEST_SCRATCH_DIR}/layer -xvf ${TEST_SCRATCH_DIR}/layer.tar

  # Check timestamps on all files including Hives and Files directories
  for file in $(find ${TEST_SCRATCH_DIR}/layer/* -print) ; do
    run stat -c %Y $file
    assert $status = 0 "checking datestamp on $file in layer"
    assert "$output" = "$timestamp" "unexpected datestamp on $file in layer (expected ${timestamp}, got ${output})"
  done
}

@test "bud-windows-with-source-date-epoch-reproducible" {
  skip_if_rootless_environment

  _prefetch alpine

  local contextdir=${TEST_SCRATCH_DIR}/windows-context
  mkdir -p ${contextdir}

  # Create files with different timestamps to test clamping behavior
  local old_timestamp=10000  # Older than source-date-epoch (should be preserved)
  local source_date_epoch=86400  # The clamping threshold

  # Create an old file (timestamp older than source-date-epoch)
  echo "old" > ${contextdir}/oldfile
  touch -d @${old_timestamp} ${contextdir}/oldfile

  # Create a new file (timestamp will be current time, newer than source-date-epoch)
  echo "new" > ${contextdir}/newfile

  cat > ${contextdir}/Dockerfile << _EOF
FROM --platform=windows/amd64 mcr.microsoft.com/oss/kubernetes/windows-host-process-containers-base-image:v1.0.0
COPY oldfile /oldfile
COPY newfile /newfile
_EOF

  # Build twice with the same source-date-epoch and rewrite-timestamp
  run_buildah build $WITH_POLICY_JSON --source-date-epoch=${source_date_epoch} --rewrite-timestamp -t oci:${TEST_SCRATCH_DIR}/windows-a ${contextdir}
  sleep 1.1 # sleep at least 1 second, so that timestamps are incremented
  run_buildah build $WITH_POLICY_JSON --source-date-epoch=${source_date_epoch} --rewrite-timestamp -t oci:${TEST_SCRATCH_DIR}/windows-b ${contextdir}

  # should be the same (reproducible builds)
  diff -r "${TEST_SCRATCH_DIR}/windows-a" "${TEST_SCRATCH_DIR}/windows-b"

  # Verify timestamp clamping behavior
  local layer=${TEST_SCRATCH_DIR}/windows-a/$(oci_image_last_diff ${TEST_SCRATCH_DIR}/windows-a)
  mkdir -p ${TEST_SCRATCH_DIR}/layer
  gzip -dc ${layer} > ${TEST_SCRATCH_DIR}/layer.tar 2>/dev/null || cat ${layer} > ${TEST_SCRATCH_DIR}/layer.tar
  tar -C ${TEST_SCRATCH_DIR}/layer -xvf ${TEST_SCRATCH_DIR}/layer.tar

  # Check that Hives and Files directories are clamped to source-date-epoch
  run stat -c %Y ${TEST_SCRATCH_DIR}/layer/Hives
  assert $status = 0 "checking datestamp on Hives directory"
  assert "$output" = "$source_date_epoch" "Hives directory should be clamped to source-date-epoch (expected ${source_date_epoch}, got ${output})"

  run stat -c %Y ${TEST_SCRATCH_DIR}/layer/Files
  assert $status = 0 "checking datestamp on Files directory"
  assert "$output" = "$source_date_epoch" "Files directory should be clamped to source-date-epoch (expected ${source_date_epoch}, got ${output})"

  # Check that oldfile preserves its old timestamp (older than source-date-epoch)
  run stat -c %Y ${TEST_SCRATCH_DIR}/layer/Files/oldfile
  assert $status = 0 "checking datestamp on oldfile"
  assert "$output" = "$old_timestamp" "oldfile should preserve old timestamp (expected ${old_timestamp}, got ${output})"

  # Check that newfile is clamped to source-date-epoch (was newer, got clamped)
  run stat -c %Y ${TEST_SCRATCH_DIR}/layer/Files/newfile
  assert $status = 0 "checking datestamp on newfile"
  assert "$output" = "$source_date_epoch" "newfile should be clamped to source-date-epoch (expected ${source_date_epoch}, got ${output})"
}

@test "use-secret-to-env-variable" {
  local outpath="${TEST_SCRATCH_DIR}/timestamp-after-secret.tar"

  BAR=baz run_buildah build --secret id=mysecret,env=BAR -f <(printf "FROM alpine\nRUN --mount=type=secret,id=mysecret,env=FOO,required sh -c 'echo "'"Hello $FOO"'"'") --tag=oci-archive:${outpath}.a --timestamp 0
  expect_output --substring "Hello baz"
  BAR=boz run_buildah build --secret id=mysecret,env=BAR -f <(printf "FROM alpine\nRUN --mount=type=secret,id=mysecret,env=FOO,required sh -c 'echo "'"Hello $FOO"'"'") --tag=oci-archive:${outpath}.b --timestamp 0
  expect_output --substring "Hello boz"

  # even though different secret was passed to each(baz vs boz), we expect the same result, ie should not affect build history
  diff "${outpath}.a" "${outpath}.b"
}

@test "use-secret-to-env-variable-and-file-path" {
  local outpath="${TEST_SCRATCH_DIR}/timestamp-after-secret-and-path.tar"

  # here we mount the secret as both an env variable AND at a file path
  # neither of which should affect the build history

  BAR=baz run_buildah build --secret id=mysecret,env=BAR -f <(printf "FROM alpine\nRUN --mount=type=secret,id=mysecret,env=FOO,target=/foo,required sh -c 'echo -n "'"Hello $FOO "'" && cat /foo'") --tag=oci-archive:${outpath}.a --timestamp 0
  expect_output --substring "Hello baz baz"

  BAR=boz run_buildah build --secret id=mysecret,env=BAR -f <(printf "FROM alpine\nRUN --mount=type=secret,id=mysecret,env=FOO,target=/foo,required sh -c 'echo -n "'"Hello $FOO "'" && cat /foo'") --tag=oci-archive:${outpath}.b --timestamp 0
  expect_output --substring "Hello boz boz"

  # even though different secret was passed to each(baz vs boz), we expect the same result, ie should not affect build history
  diff "${outpath}.a" "${outpath}.b"
}

@test "[test flags] --stage-labels without --save-stages should error" {
  _prefetch alpine
  target="stage-labels-error-test-$(safename)"

  run_buildah 125 build $WITH_POLICY_JSON --stage-labels -t ${target} -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  expect_output --substring '"--stage-labels" requires "--save-stages"'
}

@test "[test flags] bud with --save-stages=false does not save intermediate images" {
  _prefetch alpine
  target="save-stages-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages=false -t ${target}-without-save -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  run_buildah images -a
  baseline_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  baseline_count=$(echo "$baseline_images" | wc -w)
  assert "$baseline_count" == "0" "without save-stages should have 0 intermediate images"
}

@test "[test flags] --save-stages and --stage-labels=false does not label intermediate images" {
  _prefetch alpine
  target="save-stages-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels=false -t ${target} -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  run_buildah images -a
  intermediate_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  intermediate_count=$(echo "$intermediate_images" | wc -w)
  assert "$intermediate_count" == "1" "should have 1 intermediate image"

  for image_id in $intermediate_images; do
    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $image_id
    assert "$output" == "" "stage.name label should not be present when --stage-labels=false"

    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $image_id
    assert "$output" == "" "stage.base label should not be present when --stage-labels=false"
  done
}

@test "[test saving intermediate images] --save-stages on single-stage Dockerfile should not create intermediate images" {
  _prefetch alpine
  target="single-stage-test-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages -t ${target} -f $BUDFILES/save-stages/Dockerfile.simple $BUDFILES/save-stages

  run_buildah images -a
  intermediate_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  intermediate_count=$(echo "$intermediate_images" | wc -w)
  assert "$intermediate_count" == "0" "single-stage build should not create intermediate images"

  run_buildah rmi ${target}
}

@test "[test saving intermediate images] --save-stages preserves intermediate image and skips unused" {
  _prefetch alpine
  target="save-stages-$(safename)"

  # Build without --save-stages (baseline - should produce 0 intermediate images)
  run_buildah build $WITH_POLICY_JSON -t ${target}-no-save -f $BUDFILES/save-stages/Dockerfile.two-build-stages-one-unused $BUDFILES/save-stages
  run_buildah images -a
  baseline_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  baseline_count=$(echo "$baseline_images" | wc -w)
  assert "$baseline_count" == "0" "without save-stages should have 0 intermediate images"
  run_buildah rmi ${target}-no-save

  # Build with --save-stages (should produce 1 intermediate image: 'intermediate' stage)
  # Note: 'builder' stage is skipped due to --skip-unused-stages=true (default)
  run_buildah build $WITH_POLICY_JSON --save-stages -t ${target} -f $BUDFILES/save-stages/Dockerfile.two-build-stages-one-unused $BUDFILES/save-stages
  run_buildah images -a
  save_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  save_count=$(echo "$save_images" | wc -w)
  assert "$save_count" == "1" "with save-stages should have 1 intermediate image (intermediate stage)"
}

@test "[test saving intermediate images] --save-stages preserves intermediate images for all stages" {
  _prefetch alpine
  target="save-stages-all-test-$(safename)"

  # Build without --save-stages (baseline - should produce 0 intermediate images)
  run_buildah build $WITH_POLICY_JSON -t ${target}-no-save -f $BUDFILES/save-stages/Dockerfile.three-build-stages-parent-child-independent $BUDFILES/save-stages
  run_buildah images -a
  baseline_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  baseline_count=$(echo "$baseline_images" | wc -w)
  assert "$baseline_count" == "0" "without save-stages should have 0 intermediate images"
  run_buildah rmi ${target}-no-save

  # Build with --save-stages (should produce 3 intermediate images: parent, child, independent)
  # This demonstrates that intermediate images are saved for ALL stages: for each in parent-child chain and independent stage
  run_buildah build $WITH_POLICY_JSON --save-stages -t ${target} -f $BUDFILES/save-stages/Dockerfile.three-build-stages-parent-child-independent $BUDFILES/save-stages
  run_buildah images -a
  save_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  save_count=$(echo "$save_images" | wc -w)
  assert "$save_count" == "3" "with save-stages should have 3 intermediate images (parent, child, independent)"
}

@test "[test labeling intermediate images] --stage-labels adds metadata to config and history of the intermediate image and final image" {
  _prefetch alpine
  target="stage-labels-test-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target} -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  run_buildah images -a
  intermediate_image=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  intermediate_count=$(echo "$intermediate_image" | wc -w)

  assert "$intermediate_count" == "1" "should have exactly 1 intermediate image"

  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $intermediate_image
  assert "$output" "==" "intermediate" "stage.name should be 'intermediate'"

  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $intermediate_image
  assert "$output" "=~" "alpine" "stage.base should reference alpine image"

  # Verify LABEL instruction appears in history (not just in config.Labels)
  run_buildah inspect --format '{{range .OCIv1.History}}{{.CreatedBy}}{{"\n"}}{{end}}' $intermediate_image
  expect_output --substring "LABEL"

  # Verify exact label names and values in history
  expect_output --substring '"io.buildah.stage.name"="intermediate"' "history should contain exact stage.name label"
  expect_output --substring '"io.buildah.stage.base"="alpine"' "history should contain exact stage.base label"

  # Verify final image also has stage labels (stage position and base pullspec)
  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' ${target}
  assert "$output" "==" "1" "final stage.name should be stage position '1' (no alias)"

  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' ${target}
  assert "$output" "==" "alpine" "final stage.base should be 'alpine' pullspec"
}

@test "[test labeling intermediate images] chain of two stages - labels are indicating that one stage is using another stage as base" {
  _prefetch alpine
  target="chained-stages-test-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target} -f $BUDFILES/save-stages/Dockerfile.chained-two-build-stages $BUDFILES/save-stages

  run_buildah images -a
  all_intermediate_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')

  parent_image_id=""
  parent_name=""
  parent_base=""
  child_image_id=""
  child_name=""
  child_base=""

  for image_id in $all_intermediate_images; do
    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $image_id
    name="$output"

    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $image_id
    base="$output"

    # Check if base is in the list of intermediate images (stage is used by another stage as base)
    is_child=false
    for iid in $all_intermediate_images; do
      # base in label is full image ID, but all_intermediate_images contains short IDs, so check prefix
      if [[ "$base" == "$iid"* ]]; then
        is_child=true
        break
      fi
    done

    if [ "$is_child" = true ]; then
      # This is a child stage: its base is another intermediate image
      child_image_id="$image_id"
      child_name="$name"
      child_base="$base"
    else
      # This is a parent stage that serves as base for another stage
      parent_image_id="$image_id"
      parent_name="$name"
      parent_base="$base"
    fi
  done

  assert "$parent_image_id" "!=" "" "parent stage should be present"
  assert "$child_image_id" "!=" "" "child stage should be present"

  assert "$parent_name" "==" "parent-stage" "parent stage name should be 'parent-stage'"
  assert "$child_name" "==" "child-stage" "child stage name should be 'child-stage'"

  assert "$parent_base" "=~" "alpine" "parent stage.base should reference alpine image"

  # Verify child stage uses parent as base (child_base contains full ID, parent_image_id contains short ID)
  assert "$child_base" "=~" "^$parent_image_id" "child stage.base should start with parent image ID"
}

@test "[test labeling intermediate images] --jobs 1 and 2 correctly resolve stage references to imageID in labels" {
  _prefetch alpine

  for jobs in 1 2; do
    target="imageid-resolution-jobs${jobs}-$(safename)"

    # Build with specified --jobs value
    run_buildah build $WITH_POLICY_JSON --jobs $jobs --save-stages --stage-labels --layers -t ${target} -f $BUDFILES/save-stages/Dockerfile.chained-two-build-stages $BUDFILES/save-stages

    run_buildah images -a
    all_intermediate_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')

    # Find parent-stage and child-stage intermediate images
    parent_image=""
    child_image=""
    for image_id in $all_intermediate_images; do
      run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $image_id
      if [ "$output" = "parent-stage" ]; then
        parent_image="$image_id"
      elif [ "$output" = "child-stage" ]; then
        child_image="$image_id"
      fi
    done

    assert "$parent_image" "!=" "" "parent-stage intermediate image should exist (jobs=$jobs)"
    assert "$child_image" "!=" "" "child-stage intermediate image should exist (jobs=$jobs)"

    # Get child-stage base label value
    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $child_image
    child_base="$output"

    # Verify base label is an imageID from intermediate images (not alias "parent-stage")
    found_in_images=false
    for iid in $all_intermediate_images; do
      if [[ "$child_base" == "$iid"* ]]; then
        found_in_images=true
        break
      fi
    done
    assert "$found_in_images" == "true" "child stage.base should be imageID from intermediate images (jobs=$jobs)"

    run_buildah rmi -a -f
    _prefetch alpine
  done
}

@test "[test labeling intermediate images] two independent intermediate images are correctly labeled" {
  _prefetch alpine
  target="two-stages-test-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target} -f $BUDFILES/save-stages/Dockerfile.two-build-stages $BUDFILES/save-stages

  run_buildah images -a
  all_intermediate_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')

  intermediate_count=$(echo "$all_intermediate_images" | wc -w)
  assert "$intermediate_count" == "2" "should have exactly 2 intermediate images"

  stage_names=""
  for image_id in $all_intermediate_images; do
    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $image_id
    stage_name="$output"
    stage_names="$stage_names $stage_name"

    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $image_id
    assert "$output" "=~" "alpine" "stage base should be alpine"
  done

  assert "$stage_names" "=~" "alias1" "should have alias1 stage"
  assert "$stage_names" "=~" "alias2" "should have alias2 stage"
}

@test "[test labeling intermediate images] intermediate images from chain of three stages are correctly labeled" {
  _prefetch alpine
  target="chained-multiple-stages-test-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target} -f $BUDFILES/save-stages/Dockerfile.chained-three-build-stages $BUDFILES/save-stages
  run_buildah images -a
  all_intermediate_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')

  intermediate_count=$(echo "$all_intermediate_images" | wc -w)
  assert "$intermediate_count" == "3" "should have exactly 3 intermediate images in chain"

  declare -A stage_ids
  declare -A stage_bases

  for image_id in $all_intermediate_images; do
    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $image_id
    stage_name="$output"
    stage_ids[$stage_name]="$image_id"

    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $image_id
    stage_bases[$stage_name]="$output"
  done

  # Verify all 3 stages exist and have correct names
  assert "${stage_ids[base]}" "!=" "" "should have base stage"
  assert "${stage_ids[stage1]}" "!=" "" "should have stage1 stage"
  assert "${stage_ids[stage2]}" "!=" "" "should have stage2 stage"

  # Verify stage base references
  assert "${stage_bases[base]}" "=~" "alpine" "base should use alpine as base"
  # stage_bases contains full IDs, stage_ids contains short IDs - use prefix match
  assert "${stage_bases[stage1]}" "=~" "^${stage_ids[base]}" "stage1 base should start with base image ID"
  assert "${stage_bases[stage2]}" "=~" "^${stage_ids[stage1]}" "stage2 base should start with stage1 image ID"
}

@test "[test labeling intermediate images] intermediate images with ARG in stage names and bases in Containerfile are correctly labeled" {
  _prefetch alpine
  target="arg-stages-chained-stages-test-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target} -f $BUDFILES/save-stages/Dockerfile.arg-build-stages-and-chained-build-stages $BUDFILES/save-stages

  run_buildah images -a
  all_intermediate_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')

  intermediate_count=$(echo "$all_intermediate_images" | wc -w)
  assert "$intermediate_count" == "2" "should have exactly 2 intermediate images"

  declare -A stage_ids
  declare -A stage_bases
  declare -a found_stage_names

  for image_id in $all_intermediate_images; do
    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $image_id
    stage_name="$output"
    stage_ids[$stage_name]="$image_id"
    found_stage_names+=("$stage_name")

    assert "$stage_name" "!~" '\$\{' "stage name should not contain unresolved ARG placeholders like \${...}"

    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $image_id
    stage_base="$output"
    stage_bases[$stage_name]="$stage_base"

    assert "$stage_base" "!~" '\$\{' "stage base should not contain unresolved ARG placeholders like \${...}"
  done

  # Verify exact stage names are resolved from ARG variables (images returned newest first)
  assert "${found_stage_names[0]}" == "second-stage" "first image should be 'second-stage' (resolved from \${SECOND_STAGE})"
  assert "${found_stage_names[1]}" == "first-stage" "second image should be 'first-stage' (resolved from \${FIRST_STAGE})"

  # Verify stage bases are resolved correctly
  assert "${stage_bases[first-stage]}" == "alpine" "first-stage base should reference alpine image"
  assert "${stage_bases[second-stage]}" "=~" "^${stage_ids[first-stage]}" "second-stage base should reference first-stage image ID (resolved from \${FIRST_STAGE})"
}

@test "[test labeling intermediate images] intermediate images without aliases are correctly labeled" {
  _prefetch alpine
  target=save-stages-no-aliases-$(safename)

  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target} -f $BUDFILES/save-stages/Dockerfile.chained-two-build-stages-no-aliases $BUDFILES/save-stages

  run_buildah images -a
  intermediate_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  num_intermediate=$(echo "$intermediate_images" | wc -w)
  assert "$num_intermediate" "==" "2" "should have exactly 2 intermediate images (stage 0 and stage 1)"

  declare -A stage_ids
  for img_id in $intermediate_images; do
    run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $img_id
    stage_name="$output"
    stage_ids[$stage_name]="$img_id"
  done

  stage0_id="${stage_ids[0]}"
  assert "$stage0_id" "!=" "" "stage 0 should exist"
  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $stage0_id
  stage0_base="$output"
  assert "$stage0_base" "=~" "alpine" "stage 0 stage.base should reference alpine image"

  stage1_id="${stage_ids[1]}"
  assert "$stage1_id" "!=" "" "stage 1 should exist"
  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $stage1_id
  stage1_base="$output"
  assert "$stage1_base" "=~" "^$stage0_id" "stage 1 stage.base should reference stage 0 image ID"
}

@test "[test labeling intermediate images] empty intermediate stage (only FROM, no instructions) does not create labeled image" {
  _prefetch alpine
  target="empty-stage-$(safename)"

  # Build with --save-stages --stage-labels
  # The 'builder' stage has no instructions (only FROM alpine:latest AS builder)
  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target} -f $BUDFILES/save-stages/Dockerfile.empty-intermediate-build-stage $BUDFILES/save-stages

  # Check intermediate images
  run_buildah images -a
  intermediate_images=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  num_intermediate=$(echo "$intermediate_images" | wc -w)

  # Explanation: Empty stage (only FROM AS alias, no RUN/COPY/etc) is just an alias
  # for the base image. It doesn't need metadata labels or a separate image.
  # The final stage can reference it via COPY --from=builder, which resolves to alpine:latest.
  assert "$num_intermediate" "==" "0" "empty intermediate stage (only FROM) should not create intermediate image with labels"
}

@test "[test final image] --save-stages build has same final image RootFS.DiffIDs count as build without args (all parent layers and single layer squashing all final stage instructions)" {
  _prefetch alpine
  target_cache="save-stages-layer-count-$(safename)"
  target_normal="normal-layer-count-$(safename)"
  target_layers="with-layers-count-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target_cache} -f $BUDFILES/save-stages/Dockerfile.two-build-stages $BUDFILES/save-stages
  run_buildah inspect --format '{{len .OCIv1.RootFS.DiffIDs}}' ${target_cache}
  save_stages_count=$output

  run_buildah build $WITH_POLICY_JSON -t ${target_normal} -f $BUDFILES/save-stages/Dockerfile.two-build-stages $BUDFILES/save-stages
  run_buildah inspect --format '{{len .OCIv1.RootFS.DiffIDs}}' ${target_normal}
  normal_count=$output

  run_buildah build $WITH_POLICY_JSON --layers -t ${target_layers} -f $BUDFILES/save-stages/Dockerfile.two-build-stages $BUDFILES/save-stages
  run_buildah inspect --format '{{len .OCIv1.RootFS.DiffIDs}}' ${target_layers}
  layers_count=$output

  assert "$save_stages_count" == "2" "--save-stages build should have exactly 2 layers for given Containerfile"
  assert "$normal_count" == "2" "regular build should have exactly 2 layers for given Containerfile"
  assert "$layers_count" == "4" "--layers should create exactly 4 layers (one per instruction) for given Containerfile"
}

@test "[test final image] final stage uses previous stage as base, but does not inherit its labels - it has its own labels" {
  _prefetch alpine
  target="final-no-inherit-labels-$(safename)"

  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target} -f $BUDFILES/save-stages/Dockerfile.final-uses-build-stage $BUDFILES/save-stages

  run_buildah images -a
  intermediate_image=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')

  # Verify intermediate stage has stage labels
  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $intermediate_image
  assert "$output" "==" "intermediate" "intermediate stage should have stage.name label"

  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' $intermediate_image
  assert "$output" "=~" "alpine" "intermediate stage should have stage.base label referencing alpine"

  # Verify final image has its own labels
  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' ${target}
  assert "$output" "==" "1" "final image should have 1 as stage.name label"

  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.base"}}' ${target}
  assert "$output" "=~" "^$intermediate_image" "final image should have image ID of the previous stage as stage.base label"
}

@test "[test caching] Build 1: --layers --save-stages --stage-labels | Build 2: --layers --save-stages --stage-labels | Cache: layers can be reused" {
  _prefetch alpine
  target="cache-reuse-test-$(safename)"

  run_buildah build $WITH_POLICY_JSON --layers --save-stages --stage-labels -t ${target}-first -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  first_build_image_id=$(echo "$output" | tail -1)

  # Get all intermediate layers from first build
  run_buildah images -a
  all_intermediate_layers_first=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  first_intermediate_layers_count=$(echo "$all_intermediate_layers_first" | wc -w)
  assert "$first_intermediate_layers_count" -eq 4 "first build should create 4 intermediate layers (LABEL from --stage-labels for intermediate and final stage, RUN from intermediate stage, COPY from final stage)"

  # Second build should reuse the layers created by first build (cache hit)
  run_buildah build $WITH_POLICY_JSON --layers --save-stages --stage-labels -t ${target}-second -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  second_build_image_id=$(echo "$output" | tail -1)

  expect_output --substring "Using cache"

  # Verify cache was used 4 times (LABEL from --stage-labels (intermediate and final stage), RUN from intermediate stage, COPY from final stage, RUN from final stage)
  cache_count=$(echo "$output" | grep -c "Using cache")
  assert "$cache_count" -eq 5 "should use cache 5 times (LABEL (intermediate and final stage), RUN intermediate, COPY, RUN final)"

  # Get all intermediate layers from second build - second build should not add any more
  run_buildah images -a
  all_intermediate_layers_second=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  second_intermediate_layers_count=$(echo "$all_intermediate_layers_second" | wc -w)
  assert "$second_intermediate_layers_count" -eq 4 "second build should also have 4 intermediate layers"

  # Verify final image IDs match (complete cache hit)
  assert "$first_build_image_id" "==" "$second_build_image_id" "final image ID should match when cache is fully reused"
}

@test "[test caching] Build 1: --save-stages | Build 2: --save-stages | Cache: intermediate images cannot be reused" {
  _prefetch alpine
  target="cache-accumulate-$(safename)"

  # First build
  run_buildah build $WITH_POLICY_JSON --save-stages -t ${target}-first -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages

  run_buildah images -a
  intermediate_images_first=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_first=$(echo "$intermediate_images_first" | wc -w)
  assert "$count_first" -eq 1 "first build should create 1 intermediate image"

  # Second build - intermediate images accumulate (no cache reuse without --layers)
  run_buildah build $WITH_POLICY_JSON --save-stages -t ${target}-second -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  assert "$output" "!~" "Using cache" "cache should not be used without --layers"

  run_buildah images -a
  intermediate_images_second=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_second=$(echo "$intermediate_images_second" | wc -w)
  assert "$count_second" -eq 2 "second build should create another intermediate image (total 2)"

  # Third build - accumulate more
  run_buildah build $WITH_POLICY_JSON --save-stages -t ${target}-third -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  assert "$output" "!~" "Using cache" "cache should not be used without --layers"

  run_buildah images -a
  intermediate_images_third=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_third=$(echo "$intermediate_images_third" | wc -w)
  assert "$count_third" -eq 3 "third build should create another intermediate image (total 3)"
}

@test "[test caching] Build 1: --save-stages --stage-labels | Build 2: --save-stages --stage-labels | Cache: intermediate images cannot be reused" {
  _prefetch alpine
  target="cache-accumulate-labels-$(safename)"

  # First build
  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target}-first -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages

  run_buildah images -a
  intermediate_images_first=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_first=$(echo "$intermediate_images_first" | wc -w)
  assert "$count_first" -eq 1 "first build should create 1 intermediate image with labels"

  # Second build - intermediate images accumulate (no cache reuse without --layers)
  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target}-second -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  assert "$output" "!~" "Using cache" "cache should not be used without --layers (even with stage labels)"

  run_buildah images -a
  intermediate_images_second=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_second=$(echo "$intermediate_images_second" | wc -w)
  assert "$count_second" -eq 2 "second build should create another intermediate image (total 2)"

  # Third build - accumulate more
  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels -t ${target}-third -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  assert "$output" "!~" "Using cache" "cache should not be used without --layers (even with stage labels)"

  run_buildah images -a
  intermediate_images_third=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_third=$(echo "$intermediate_images_third" | wc -w)
  assert "$count_third" -eq 3 "third build should create another intermediate image (total 3)"
}

@test "[test caching] Build 1: --save-stages --layers | Build 2: --save-stages --layers | Cache: intermediate images can be reused" {
  _prefetch alpine
  target="cache-reuse-layers-$(safename)"

  # First build
  run_buildah build $WITH_POLICY_JSON --save-stages --layers -t ${target}-first -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  first_build_final_image_id=$(echo "$output" | tail -1)

  run_buildah images -a
  intermediate_images_first=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_first=$(echo "$intermediate_images_first" | wc -w)
  assert "$count_first" -eq 2 "first build should create 2 intermediate layers (RUN from intermediate stage, COPY from final stage)"

  # Build 2 should reuse all layers
  run_buildah build $WITH_POLICY_JSON --save-stages --layers -t ${target}-second -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages
  second_build_final_image_id=$(echo "$output" | tail -1)

  expect_output --substring "Using cache"

  # Verify cache was used 3 times (RUN from intermediate stage, COPY from final stage, RUN from final stage)
  cache_count=$(echo "$output" | grep -c "Using cache")
  assert "$cache_count" -eq 3 "should use cache 3 times (RUN intermediate, COPY, RUN final)"

  run_buildah images -a
  intermediate_images_second=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_second=$(echo "$intermediate_images_second" | wc -w)
  assert "$count_second" -eq 2 "second build should reuse layers, not create new ones (still 2 total)"

  # Full cache reuse - final image is same
  assert $first_build_final_image_id "==" $second_build_final_image_id
}

@test "[test caching] Build 1: --save-stages --stage-labels --layers | Build 2: --save-stages --layers | Cache: No cache reuse - missing labels are causing cache miss for each stage" {
  _prefetch alpine
  target="cache-no-reuse-$(safename)"

  # First build
  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels --layers -t ${target}-first -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages

  run_buildah images -a
  intermediate_images_first=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_first=$(echo "$intermediate_images_first" | wc -w)
  assert "$count_first" -eq 4 "first build should create 4 intermediate layers (LABEL (intermediate and final), RUN, COPY)"

  # Build 2 - no cache reuse
  run_buildah build $WITH_POLICY_JSON --save-stages --layers -t ${target}-second -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages

  # Verify cache was not used
  assert "$output" !~ "Using cache" "should NOT use cache because LABEL was added"

  run_buildah images -a
  intermediate_images_second=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_second=$(echo "$intermediate_images_second" | wc -w)
  assert "$count_second" -eq 6 "second build should have 6 intermediate layers (4 from first + 2 instruction layers from second)"
}

@test "[test caching] Build 1: --save-stages --layers | Build 2: --save-stages --stage-labels --layers | Cache: No cache reuse - added labels are causing cache miss for each stage" {
  _prefetch alpine
  target="cache-no-reuse-$(safename)"

  # First build
  run_buildah build $WITH_POLICY_JSON --save-stages --layers -t ${target}-first -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages

  run_buildah images -a
  intermediate_images_first=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_first=$(echo "$intermediate_images_first" | wc -w)
  assert "$count_first" -eq 2 "first build should create 2 intermediate layers (RUN from intermediate, COPY from final)"

  # Second build (adds LABEL layers)
  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels --layers -t ${target}-second -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages

  # Verify cache was not used
  assert "$output" !~ "Using cache" "should NOT use cache because LABEL was added"
  run_buildah images -a
  intermediate_images_second=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_second=$(echo "$intermediate_images_second" | wc -w)
  assert "$count_second" -eq 6 "second build should have 6 intermediate layers (2 from first + 4 from second)"
}

@test "[test caching] Build 1: --layers | Build 2: --save-stages --stage-labels --layers | Cache: No cache reuse - added labels are causing cache miss for each stage" {
  _prefetch alpine
  target="cache-no-reuse-$(safename)"

  # First build
  run_buildah build $WITH_POLICY_JSON --layers -t ${target}-first -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages

  run_buildah images -a
  intermediate_images_first=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_first=$(echo "$intermediate_images_first" | wc -w)
  assert "$count_first" -eq 2 "first build should create 2 intermediate layers (RUN, COPY)"

  # Second build (adds LABEL layer for interemediate and final stage)
  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels --layers -t ${target}-second -f $BUDFILES/save-stages/Dockerfile.single-build-stage $BUDFILES/save-stages

  # Verify cache was not used
  assert "$output" !~ "Using cache" "should NOT use cache because LABEL was added"

  run_buildah images -a
  intermediate_images_second=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_second=$(echo "$intermediate_images_second" | wc -w)
  assert "$count_second" -eq 6 "second build should have 6 intermediate layers (2 from first (instructions) + 4 from second (labels and instructions))"
}

@test "[test caching] Build 1: --save-stages --stage-labels --layers | Build 2: same flags, modified stage alias | Cache: intermediate stage rebuilt (name label changed)" {
  _prefetch alpine
  target="cache-miss-alias-change-$(safename)"

  # Build 1: with alias "intermediate"
  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels --layers -t ${target}-first -f $BUDFILES/save-stages/Dockerfile.single-build-stage-modifiable $BUDFILES/save-stages
  first_build_final_image_id=$(echo "$output" | tail -1)

  run_buildah images -a
  intermediate_images_first=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_first=$(echo "$intermediate_images_first" | wc -w)
  assert "$count_first" -eq 4 "first build should create 4 intermediate layers (LABEL (intermediate and final), RUN, COPY)"

  # Get last layer from first stage (intermediate stage image)
  first_intermediate_run_layer=$(echo "$intermediate_images_first" | awk 'NR==3')

  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $first_intermediate_run_layer
  assert "$output" "==" "intermediate" "first build intermediate stage image should have 'intermediate' as stage name"

  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $first_build_final_image_id
  assert "$output" "==" "1" "first build final image should have '1' index as stage name"

  # Build 2: with modified alias "intermediate-renamed" - same Containerfile but different stage name
  run_buildah build $WITH_POLICY_JSON --save-stages --stage-labels --layers -t ${target}-second -f $BUDFILES/save-stages/Dockerfile.single-build-stage-modifiable-renamed $BUDFILES/save-stages
  second_build_final_image_id=$(echo "$output" | tail -1)

  expect_output --substring "Using cache"

  # Verify cache was used 3 times
  cache_count=$(echo "$output" | grep -c "Using cache")
  assert "$cache_count" -eq 3 "should use cache 3 times (LABEL final, COPY final, RUN final)"

  run_buildah images -a
  intermediate_images_second=$(echo "$output" | awk '$1 == "<none>" && $2 == "<none>" {print $3}')
  count_second=$(echo "$intermediate_images_second" | wc -w)
  assert "$count_second" -eq 6 "second build should have 5 intermediate layers (4 from first + 2 new; LABEL intermediate and RUN intermediate)"

  # Get last layer from first stage (intermediate stage image)
  second_intermediate_run_layer=$(echo "$intermediate_images_second" | awk 'NR==1')

  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $second_intermediate_run_layer
  assert "$output" "==" "intermediate-renamed" "second build should have 'intermediate-renamed' as stage name"

  # Unchanged
  run_buildah inspect --format '{{index .OCIv1.Config.Labels "io.buildah.stage.name"}}' $second_build_final_image_id
  assert "$output" "==" "1" "second build final image should have '1' index as stage name"

  # Verify final image IDs match (complete cache hit)
  assert "$first_build_final_image_id" "==" "$second_build_final_image_id" "final image ID should match when cache is fully reused"
}

@test "build-with-additional-build-context URL temp dirs are cleaned up" {
  _prefetch alpine

  local tmpdir=${TEST_SCRATCH_DIR}/tmpdir
  mkdir -p $tmpdir
  local tarball_content=${TEST_SCRATCH_DIR}/tarball-src
  mkdir -p $tarball_content/myproject
  echo secret-content > $tarball_content/myproject/file.txt
  local tarball=${TEST_SCRATCH_DIR}/context.tar.gz
  tar -czf $tarball -C $tarball_content .
  local httpdir=${TEST_SCRATCH_DIR}/http
  mkdir -p $httpdir
  cp $tarball $httpdir/context.tar.gz

  starthttpd $httpdir
  local contextdir=${TEST_SCRATCH_DIR}/bud/context
  mkdir -p $contextdir
  cat > $contextdir/Dockerfile << _EOF
FROM alpine
COPY --from=ctx myproject/file.txt /file.txt
RUN cat /file.txt
RUN --mount=type=bind,src=myproject,from=ctx2,target=/mnt,z cmp /file.txt /mnt/file.txt
_EOF

  TMPDIR=$tmpdir run_buildah build $WITH_POLICY_JSON \
    --build-context ctx=http://0.0.0.0:${HTTP_SERVER_PORT}/context.tar.gz \
    --build-context ctx2=http://0.0.0.0:${HTTP_SERVER_PORT}/context.tar.gz \
    -t source -f $contextdir/Dockerfile $contextdir
  expect_output --substring "secret-content"
  assert_no_tempdir_leaks $tmpdir
}

@test "bud-duplicate-stage-names-target" {
  local contextdir="${TEST_SCRATCH_DIR}"/context
  mkdir -p $contextdir
  cat > $contextdir/Dockerfile << EOF
  FROM scratch AS stage0
  LABEL x="really stage 0"

  FROM scratch AS stage0
  LABEL x="actually stage 1"

  FROM stage0
EOF
  local target=image0
  for njobs in "" --jobs=3 ; do
    for layers in "" --layers ; do
      for through in "" --target=stage0 ; do
        echo building with options: $layers $through $njobs
        run_buildah build $WITH_POLICY_JSON --identity-label=false $layers $through $njobs -t "${target}" "${contextdir}"
        run_buildah inspect --format='{{.OCIv1.Config.Labels}}' "${target}"
        expect_output 'map[x:actually stage 1]'
        run_buildah rmi -a -f
      done
    done
  done
}

@test "bud-duplicate-stage-names-copy-from" {
  _prefetch busybox

  local target=image0

  local contextdir="${TEST_SCRATCH_DIR}"/context
  mkdir -p $contextdir
  cat > $contextdir/Dockerfile.1 <<_EOF
  FROM busybox AS stage0
  COPY <<EOF /stage.txt
  actually stage 0
EOF
  FROM busybox AS stage0
  COPY <<EOF /stage.txt
  actually stage 1
EOF
  FROM busybox
  COPY --from=stage0 /stage.txt /
_EOF
  cat > $contextdir/Dockerfile.2 <<_EOF
  FROM ${target}
  RUN cat /stage.txt
_EOF

  for njobs in "" --jobs=3 ; do
    for layers in "" "--layers --no-cache" ; do
      echo building with option: $layers
      run_buildah build $WITH_POLICY_JSON $layers $njobs -t "${target}" -f ${contextdir}/Dockerfile.1 "${contextdir}"
      run_buildah build $WITH_POLICY_JSON $layers $njobs -t "${target}"-derived -f ${contextdir}/Dockerfile.2 "${contextdir}"
      expect_output --substring "actually stage 1"
      run_buildah rmi "${target}"-derived
      run_buildah rmi "${target}"
    done
  done
}

@test "bud-duplicate-stage-names-mount-from" {
  _prefetch busybox

  local target=image0

  local contextdir="${TEST_SCRATCH_DIR}"/context
  mkdir -p $contextdir
  cat > $contextdir/Dockerfile <<_EOF
  FROM busybox AS stage0
  COPY <<EOF /stage.txt
  actually stage 0
EOF
  FROM busybox AS stage0
  COPY <<EOF /stage.txt
  actually stage 1
EOF
  FROM busybox AS stage0
  COPY <<EOF /expected.txt
  actually stage 1
EOF
  RUN --mount=from=stage0,target=/stage0 cmp /stage0/stage.txt /expected.txt && echo this build was ok
_EOF

  for njobs in "" --jobs=3 ; do
    for layers in "" "--layers --no-cache" ; do
      echo building with option: $layers
      run_buildah build $WITH_POLICY_JSON $layers $njobs -t "${target}" -f ${contextdir}/Dockerfile "${contextdir}"
      run_buildah rmi "${target}"
    done
  done
}
