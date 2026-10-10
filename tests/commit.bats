#!/usr/bin/env bats

load helpers

@test "commit-flags-order-verification" {
  run_buildah 125 commit cnt1 --tls-verify
  check_options_flag_err "--tls-verify"

  run_buildah 125 commit cnt1 -q
  check_options_flag_err "-q"

  run_buildah 125 commit cnt1 -f=docker --quiet --creds=bla:bla
  check_options_flag_err "-f=docker"

  run_buildah 125 commit cnt1 --creds=bla:bla
  check_options_flag_err "--creds=bla:bla"
}

@test "commit" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid alpine-image
  run_buildah images alpine-image
}

# Mainly this test is added for rootless setups where XDG_RUNTIME_DIR
# is not set and we end up setting incorrect runroot at various steps
# Use case is typically seen on environments where current session
# is invalid login session.
@test "commit image on rootless setup with mount" {
  unset XDG_RUNTIME_DIR
  run dd if=/dev/zero of=${TEST_SCRATCH_DIR}/file count=1 bs=10M
  run_buildah from scratch
  CONT=$output
  unset XDG_RUNTIME_DIR
  run_buildah_mount $CONT
  MNT=$output
  run cp ${TEST_SCRATCH_DIR}/file $MNT/file
  run_buildah_umount $CONT
  run_buildah commit $CONT foo
  run_buildah images foo
  expect_output --substring "10.5 MB"
}

@test "commit-with-identity-label" {
  run_buildah from scratch
  cid=$output
  touch $TEST_SCRATCH_DIR/content.txt
  run_buildah add $cid $TEST_SCRATCH_DIR/content.txt /
  run_buildah commit $cid scratch-image-1
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-1
  assert "$output" != "map[]"
  run_buildah commit --identity-label=true $cid scratch-image-2
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-2
  assert "$output" != "map[]"
}

@test "commit-without-identity-label" {
  run_buildah from scratch
  cid=$output
  touch $TEST_SCRATCH_DIR/content.txt
  run_buildah add $cid $TEST_SCRATCH_DIR/content.txt /
  run_buildah commit --identity-label=false $WITH_POLICY_JSON $cid scratch-image
  run_buildah images scratch-image
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image
  assert "$output" = "map[]"
}

@test "commit-suppressed-identity-label" {
  run_buildah from scratch
  cid=$output
  touch $TEST_SCRATCH_DIR/content.txt
  run_buildah add $cid $TEST_SCRATCH_DIR/content.txt /

  run_buildah commit --source-date-epoch=60 $WITH_POLICY_JSON $cid scratch-image-1
  run_buildah images scratch-image-1
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-1
  assert "$output" = "map[]"

  export SOURCE_DATE_EPOCH=90
  run_buildah commit $WITH_POLICY_JSON $cid scratch-image-2
  unset SOURCE_DATE_EPOCH
  run_buildah images scratch-image-2
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-2
  assert "$output" = "map[]"

  run_buildah commit --timestamp=60 $WITH_POLICY_JSON $cid scratch-image-3
  run_buildah images scratch-image-3
  run_buildah inspect --format '{{printf "%q" .Docker.Config.Labels}}' scratch-image-3
  assert "$output" = "map[]"
}

@test "commit format test" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid alpine-image-oci
  run_buildah commit --format docker --disable-compression=false $WITH_POLICY_JSON $cid alpine-image-docker

  run_buildah inspect --type=image --format '{{.Manifest}}' alpine-image-oci
  mediatype=$(jq -r '.layers[0].mediaType' <<<"$output")
  expect_output --from="$mediatype" "application/vnd.oci.image.layer.v1.tar"
  run_buildah inspect --type=image --format '{{.Manifest}}' alpine-image-docker
  mediatype=$(jq -r '.layers[1].mediaType' <<<"$output")
  expect_output --from="$mediatype" "application/vnd.docker.image.rootfs.diff.tar.gzip"
}

@test "commit --unsetenv PATH" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah commit --unsetenv PATH $WITH_POLICY_JSON $cid alpine-image-oci
  run_buildah commit --unsetenv PATH --format docker --disable-compression=false $WITH_POLICY_JSON $cid alpine-image-docker

  run_buildah inspect --type=image --format '{{.OCIv1.Config.Env}}' alpine-image-oci
  expect_output "[]" "No Path should be defined"
  run_buildah inspect --type=image --format '{{.Docker.Config.Env}}' alpine-image-docker
  expect_output "[]" "No Path should be defined"
}

@test "commit quiet test" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah commit --iidfile /dev/null $WITH_POLICY_JSON -q $cid alpine-image
  expect_output ""
}

@test "commit iidfile" {
  _prefetch busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah commit --iidfile ${TEST_SCRATCH_DIR}/iidfile.txt $cid
  test -s ${TEST_SCRATCH_DIR}/iidfile.txt
}

@test "commit metadata-file" {
  _prefetch busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah commit --metadata-file ${TEST_SCRATCH_DIR}/metadata.txt $cid
  jq . ${TEST_SCRATCH_DIR}/metadata.txt
  test -s ${TEST_SCRATCH_DIR}/metadata.txt
  assert $(wc -c < ${TEST_SCRATCH_DIR}/metadata.txt) -gt 2
}

@test "commit rm test" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah commit $WITH_POLICY_JSON --rm $cid alpine-image
  run_buildah 125 rm $cid
  expect_output --substring "removing container \"alpine-working-container\": container not known"
}

@test "commit-alternate-storage" {
  _prefetch alpine
  echo FROM
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  echo COMMIT
  run_buildah commit $WITH_POLICY_JSON $cid "containers-storage:[vfs@${TEST_SCRATCH_DIR}/root2+${TEST_SCRATCH_DIR}/runroot2]newimage"
  echo FROM
  run_buildah --storage-driver vfs --root ${TEST_SCRATCH_DIR}/root2 --runroot ${TEST_SCRATCH_DIR}/runroot2 from $WITH_POLICY_JSON newimage
}

@test "commit-rejected-name" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah 125 commit $WITH_POLICY_JSON $cid ThisNameShouldBeRejected
  expect_output --substring "must be lower"
}

@test "commit-no-empty-created-by" {
  if ! python3 -c 'import json, sys' 2> /dev/null ; then
    skip "python interpreter with json module not found"
  fi
  target=new-image
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output

  run_buildah config --created-by "untracked actions" $cid
  run_buildah commit $WITH_POLICY_JSON $cid ${target}
  run_buildah inspect --format '{{.Config}}' ${target}
  config="$output"
  run python3 -c 'import json, sys; config = json.load(sys.stdin); print(config["history"][len(config["history"])-1]["created_by"])' <<< "$config"
  echo "$output"
  assert "$status" -eq 0 "status from python command 1"
  expect_output "untracked actions"

  run_buildah config --created-by "" $cid
  run_buildah commit $WITH_POLICY_JSON $cid ${target}
  run_buildah inspect --format '{{.Config}}' ${target}
  config="$output"
  run python3 -c 'import json, sys; config = json.load(sys.stdin); print(config["history"][len(config["history"])-1]["created_by"])' <<< "$config"
  echo "$output"
  assert "$status" -eq 0 "status from python command 2"
  expect_output "/bin/sh"
}

@test "commit-no-name" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid
}

@test "commit should fail with nonexistent authfile" {
  _prefetch alpine
  run_buildah from --quiet --pull $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah 125 commit --authfile /tmp/nonexistent $WITH_POLICY_JSON $cid alpine-image
}

@test "commit-builder-identity" {
	_prefetch alpine
	run_buildah from --quiet --pull $WITH_POLICY_JSON alpine
	cid=$output
	run_buildah commit $WITH_POLICY_JSON $cid alpine-image

	run_buildah --version
        local -a output_fields=($output)
	buildah_version=${output_fields[2]}

	run_buildah inspect --format '{{ index .Docker.Config.Labels "io.buildah.version"}}' alpine-image
        expect_output "$buildah_version"
}

@test "commit-container-id" {
  _prefetch alpine
  run_buildah from --quiet --pull $WITH_POLICY_JSON alpine

  # There is exactly one container. Get its ID.
  run_buildah containers --format '{{.ContainerID}}'
  cid=$output

  run_buildah commit $WITH_POLICY_JSON --format docker $cid alpine-image
  run_buildah inspect --format '{{.Docker.Container}}' alpine-image
  expect_output "$cid" "alpine-image -> .Docker.Container"
}

@test "commit with name" {
  _prefetch busybox
  run_buildah from --quiet $WITH_POLICY_JSON --name busyboxc busybox
  expect_output "busyboxc"

  # Commit with a new name
  newname="commitbyname/busyboxname"
  run_buildah commit $WITH_POLICY_JSON busyboxc $newname

  run_buildah from $WITH_POLICY_JSON localhost/$newname
  expect_output "busyboxname-working-container"

  cname=$output
  run_buildah inspect --format '{{.FromImage}}' $cname
  expect_output "localhost/$newname:latest"
}

@test "commit to docker-distribution" {
  _prefetch busybox
  run_buildah from $WITH_POLICY_JSON --name busyboxc busybox
  start_registry
  run_buildah commit $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword busyboxc docker://localhost:${REGISTRY_PORT}/commit/busybox
  run_buildah from $WITH_POLICY_JSON --name fromdocker --tls-verify=false --creds testuser:testpassword docker://localhost:${REGISTRY_PORT}/commit/busybox
}

@test "commit encrypted local oci image" {
  skip_if_rootless_environment
  _prefetch busybox
  mkdir ${TEST_SCRATCH_DIR}/tmp
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey.pem 1024
  openssl rsa -in ${TEST_SCRATCH_DIR}/tmp/mykey.pem -pubout > ${TEST_SCRATCH_DIR}/tmp/mykey.pub
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah commit --iidfile /dev/null $WITH_POLICY_JSON --encryption-key jwe:${TEST_SCRATCH_DIR}/tmp/mykey.pub -q $cid oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc
  imgtype  -show-manifest oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc | grep "+encrypted"
  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "commit oci encrypt to registry" {
  _prefetch busybox
  mkdir ${TEST_SCRATCH_DIR}/tmp
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey.pem 1024
  openssl rsa -in ${TEST_SCRATCH_DIR}/tmp/mykey.pem -pubout > ${TEST_SCRATCH_DIR}/tmp/mykey.pub
  start_registry
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah commit --iidfile /dev/null --tls-verify=false --creds testuser:testpassword $WITH_POLICY_JSON --encryption-key jwe:${TEST_SCRATCH_DIR}/tmp/mykey.pub -q $cid docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest
  # this test, just checks the ability to commit an image to a registry
  # there is no good way to test the details of the image unless with ./buildah pull, test will be in pull.bats
  rm -rf ${TEST_SCRATCH_DIR}/tmp

  # verify that encrypted layers are not cached or reused for an non-encrypted image (See containers/image#1533)
  run_buildah commit --iidfile /dev/null --tls-verify=false --creds testuser:testpassword $WITH_POLICY_JSON -q $cid docker://localhost:${REGISTRY_PORT}/buildah/busybox_not_encrypted:latest
  run_buildah from $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword docker://localhost:${REGISTRY_PORT}/buildah/busybox_not_encrypted:latest
}

@test "commit omit-timestamp" {
  _prefetch busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah run $cid touch /test
  run_buildah commit $WITH_POLICY_JSON --omit-timestamp -q $cid omit
  run_buildah inspect --format '{{ .Docker.Created }}' omit
  expect_output --substring "1970-01-01"
  run_buildah inspect --format '{{ .OCIv1.Created }}' omit
  expect_output --substring "1970-01-01"


  run_buildah from --quiet --pull=false $WITH_POLICY_JSON omit
  cid=$output
  run_buildah run $cid ls -l /test
  expect_output --substring "1970"

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "commit timestamp" {
  _prefetch busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah run $cid touch /test
  run_buildah commit $WITH_POLICY_JSON --timestamp 0 -q $cid omit
  run_buildah inspect --format '{{ .Docker.Created }}' omit
  expect_output --substring "1970-01-01"
  run_buildah inspect --format '{{ .OCIv1.Created }}' omit
  expect_output --substring "1970-01-01"


  run_buildah from --quiet --pull=false $WITH_POLICY_JSON omit
  cid=$output
  run_buildah run $cid ls -l /test
  expect_output --substring "1970"

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "commit with authfile" {
  _prefetch busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah run $cid touch /test

  start_registry
  run_buildah login --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword --tls-verify=false localhost:${REGISTRY_PORT}
  run_buildah commit --authfile ${TEST_SCRATCH_DIR}/test.auth $WITH_POLICY_JSON --tls-verify=false $cid docker://localhost:${REGISTRY_PORT}/buildah/my-busybox
  expect_output --substring "Writing manifest to image destination"
}

@test "commit-without-names" {
  _prefetch busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah run $cid touch /testfile
  run_buildah run $cid chown $(id -u):$(id -g) /testfile
  run_buildah commit $cid dir:${TEST_SCRATCH_DIR}/new-image
  config=$(jq -r .config.digest ${TEST_SCRATCH_DIR}/new-image/manifest.json)
  echo "config blob is $config"
  diffid=$(jq -r '.rootfs.diff_ids[-1]' ${TEST_SCRATCH_DIR}/new-image/${config##*:})
  echo "new layer is $diffid"
  run_buildah copy $cid ${TEST_SCRATCH_DIR}/new-image/${diffid##*:} /testdiff.tar
  # use in-container version of tar to avoid worrying about differences in
  # output formats between tar implementations
  run_buildah run $cid tar tvf /testdiff.tar testfile
  echo "new file looks like [$output]"
  # ownership information should be forced to be in number/number format
  # instead of name/name because the names are gone
  assert "$output" =~ $(id -u)/$(id -g)
}

@test "commit-with-extra-files" {
  _prefetch busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid=$output
  createrandom ${TEST_SCRATCH_DIR}/randomfile1
  createrandom ${TEST_SCRATCH_DIR}/randomfile2

  for method in --squash=false --squash=true ; do
    run_buildah commit $method --add-file ${TEST_SCRATCH_DIR}/randomfile1:/randomfile1 $cid with-random-1
    run_buildah commit $method --add-file ${TEST_SCRATCH_DIR}/randomfile2:/in-a-subdir/randomfile2 $cid with-random-2
    run_buildah commit $method --add-file ${TEST_SCRATCH_DIR}/randomfile1:/randomfile1 --add-file ${TEST_SCRATCH_DIR}/randomfile2:/in-a-subdir/randomfile2 $cid with-random-both

    # first one should have the first file and not the second, and the shell should be there
    run_buildah from --quiet --pull=false $WITH_POLICY_JSON with-random-1
    cid=$output
    run_buildah_mount $cid
    mountpoint=$output
    test -s $mountpoint/bin/sh || test -L $mountpoint/bin/sh
    cmp ${TEST_SCRATCH_DIR}/randomfile1 $mountpoint/randomfile1
    run stat -c %u:%g $mountpoint
    [ $status -eq 0 ]
    rootowner=$output
    run stat -c %u:%g:%A $mountpoint/randomfile1
    [ $status -eq 0 ]
    assert ${rootowner}:-rw-r--r--
    ! test -f $mountpoint/randomfile2

    # second one should have the second file and not the first, and the shell should be there
    run_buildah from --quiet --pull=false $WITH_POLICY_JSON with-random-2
    cid=$output
    run_buildah_mount $cid
    mountpoint=$output
    test -s $mountpoint/bin/sh || test -L $mountpoint/bin/sh
    cmp ${TEST_SCRATCH_DIR}/randomfile2 $mountpoint/in-a-subdir/randomfile2
    run stat -c %u:%g $mountpoint
    [ $status -eq 0 ]
    rootowner=$output
    run stat -c %u:%g:%A $mountpoint/in-a-subdir/randomfile2
    [ $status -eq 0 ]
    assert ${rootowner}:-rw-r--r--
    ! test -f $mountpoint/randomfile1

    # third one should have both files, and the shell should be there
    run_buildah from --quiet --pull=false $WITH_POLICY_JSON with-random-both
    cid=$output
    run_buildah_mount $cid
    mountpoint=$output
    test -s $mountpoint/bin/sh || test -L $mountpoint/bin/sh
    cmp ${TEST_SCRATCH_DIR}/randomfile1 $mountpoint/randomfile1
    run stat -c %u:%g $mountpoint
    [ $status -eq 0 ]
    rootowner=$output
    run stat -c %u:%g:%A $mountpoint/randomfile1
    [ $status -eq 0 ]
    assert ${rootowner}:-rw-r--r--
    cmp ${TEST_SCRATCH_DIR}/randomfile2 $mountpoint/in-a-subdir/randomfile2
    run stat -c %u:%g:%A $mountpoint/in-a-subdir/randomfile2
    [ $status -eq 0 ]
    assert ${rootowner}:-rw-r--r--
  done
}

@test "commit with insufficient disk space" {
  skip_if_rootless_environment
  _prefetch busybox
  local tmp=$TEST_SCRATCH_DIR/buildah-test
  mkdir -p $tmp
  mount -t tmpfs -o size=4M tmpfs $tmp
  # Create a temporary file which should not be easy to compress,
  # which we'll add to our container for committing, but which is
  # larger than the filesystem where the layer blob that would
  # contain it, compressed or not, would be written during commit.
  run dd if=/dev/urandom of=$TEST_SCRATCH_DIR/8M bs=1M count=8
  # Create a working container.
  run_buildah from --pull=never $WITH_POLICY_JSON busybox
  ctrID="$output"
  # Copy the file into the working container.
  run_buildah copy $ctrID $TEST_SCRATCH_DIR/8M /8M
  # Try to commit the image.  The temporary copy of the layer diff should
  # require more space than is available where we're telling it to store
  # temporary things.
  TMPDIR=$tmp run_buildah '?' commit $ctrID
  umount $tmp
  expect_output --substring "no space left on device"
}

@test "commit-with-source-date-epoch" {
  _prefetch busybox
  local url=https://raw.githubusercontent.com/containers/buildah/main/tests/bud/from-scratch/Dockerfile
  local timestamp=60
  local datestamp="1970-01-01T00:01:00Z"
  mkdir -p $TEST_SCRATCH_DIR/context
  createrandom $TEST_SCRATCH_DIR/context/randomfile1
  createrandom $TEST_SCRATCH_DIR/context/randomfile2
  run_buildah from -q busybox
  local cid="$output"
  run_buildah add --add-history "$cid" $TEST_SCRATCH_DIR/context/* /context
  # commit using defaults
  run_buildah commit "$cid" oci:$TEST_SCRATCH_DIR/default
  # commit with an implicitly-provided timestamp
  export local SOURCE_DATE_EPOCH=$timestamp
  run_buildah commit "$cid" oci:$TEST_SCRATCH_DIR/implicit
  run_buildah commit --rewrite-timestamp "$cid" oci:$TEST_SCRATCH_DIR/implicit-rewritten
  unset SOURCE_DATE_EPOCH
  # commit with an explicity-provided timestamp
  run_buildah commit --source-date-epoch=$timestamp "$cid" oci:$TEST_SCRATCH_DIR/explicit
  run_buildah commit --source-date-epoch=$timestamp --rewrite-timestamp "$cid" oci:$TEST_SCRATCH_DIR/explicit-rewritten

  # check timestamps in the ones we forced: find the manifest's and config's digests
  manifestdigest=$(oci_image_manifest_digest "$TEST_SCRATCH_DIR"/explicit)
  manifestalg=${manifestdigest%%:*}
  manifestval=${manifestdigest##*:}
  configdigest=$(oci_image_config_digest "$TEST_SCRATCH_DIR"/explicit)
  configalg=${configdigest%%:*}
  configval=${configdigest##*:}
  # check timestamps in the ones we forced: read the image creation date
  config="$TEST_SCRATCH_DIR"/explicit/$(oci_image_config "$TEST_SCRATCH_DIR"/explicit)
  run jq -r '.created' "$config"
  echo "$output"
  assert $status = 0 "looking for the image creation date"
  assert "$output" = "$datestamp" "unexpected creation date for image"
  # check timestamps in the ones we forced: read the image history entry dates
  run jq -r '.history[-2].created' "$config"
  echo "$output"
  assert $status = 0 "looking for the image history entries"
  jq '.history' "$config"
  for line in "$lines[@]"; do
    assert "$output" = "$datestamp" "unexpected datestamp for history entry"
  done
  # check timestamps in the ones we forced: extract the layer blob
  layer="$TEST_SCRATCH_DIR"/explicit/$(oci_image_last_diff "$TEST_SCRATCH_DIR"/explicit)
  mkdir -p "$TEST_SCRATCH_DIR"/layer
  tar -C "$TEST_SCRATCH_DIR"/layer -xvf "$layer"
  # check timestamps in the ones we forced: walk the layer blob, checking
  # timestamps
  for file in $(find $TEST_SCRATCH_DIR/layer/* -print) ; do
    run stat -c %Y $file
    assert $status = 0 "checking datestamp on $file in layer"
    assert "$output" -gt "$timestamp" "unexpected datestamp on $file in layer"
  done
  # check timestamps in the ones we forced: check that we have an image config
  # and manifest with the same digests when we set the source date epoch
  # implicitly as we did when we forced them explicitly
  test -s $TEST_SCRATCH_DIR/implicit/blobs/"$manifestalg"/"$manifestval"
  test -s $TEST_SCRATCH_DIR/implicit/blobs/"$configalg"/"$configval"

  # check timestamps in the ones we forced and rewrote timestamps in: the
  # version where we rewrote timestamps in the layer should have produced
  # different diffIDs, and thus a different config blob, and a different
  # manifest, so the ones we just looked at shouldn't _also_ be in there
  ! test -s $TEST_SCRATCH_DIR/explicit-rewritten/blobs/"$manifestalg"/"$manifestval"
  ! test -s $TEST_SCRATCH_DIR/explicit-rewritten/blobs/"$configalg"/"$configval"

  # check timestamps in the ones we forced and rewrote timestamps in: find the
  # manifest's and config's digests
  manifestdigest=$(oci_image_manifest_digest "$TEST_SCRATCH_DIR"/explicit-rewritten)
  manifestalg=${manifestdigest%%:*}
  manifestval=${manifestdigest##*:}
  configdigest=$(oci_image_config_digest "$TEST_SCRATCH_DIR"/explicit-rewritten)
  configalg=${configdigest%%:*}
  configval=${configdigest##*:}
  # check timestamps in the ones we forced and rewrote timestamps in: read the
  # image creation date
  config="$TEST_SCRATCH_DIR"/explicit-rewritten/$(oci_image_config "$TEST_SCRATCH_DIR"/explicit-rewritten)
  run jq -r '.created' "$config"
  echo "$output"
  assert $status = 0 "looking for the image creation date"
  assert "$output" = "$datestamp" "unexpected creation date for image"
  # check timestamps in the ones we forced and rewrote timestamps in: read the
  # image history entry dates
  run jq -r '.history[-2].created' "$config"
  echo "$output"
  assert $status = 0 "looking for the image history entries"
  jq '.history' "$config"
  for line in "$lines[@]"; do
    assert "$output" = "$datestamp" "unexpected datestamp for history entry"
  done
  # check timestamps in the ones we forced and rewrote timestamps in: extract
  # the layer blob
  layer="$TEST_SCRATCH_DIR"/explicit-rewritten/$(oci_image_last_diff "$TEST_SCRATCH_DIR"/explicit-rewritten)
  rm -fr $TEST_SCRATCH_DIR/layer; mkdir -p $TEST_SCRATCH_DIR/layer
  tar -C $TEST_SCRATCH_DIR/layer -xvf "$layer"
  # check timestamps in the ones we forced and rewrote timestamps in: walk the
  # layer blob, checking timestamps
  for file in $(find $TEST_SCRATCH_DIR/layer/* -print) ; do
    run stat -c %Y $file
    assert $status = 0 "checking datestamp on $file in layer"
    assert "$output" -le "$timestamp" "unexpected datestamp on $file in layer"
  done
  # check timestamps in the ones we forced and rewrote timestamps in: check
  # that we have an image config and manifest with the same digests when we set
  # the source date epoch implicitly as we did when we forced them explicitly
  test -s $TEST_SCRATCH_DIR/implicit-rewritten/blobs/"$manifestalg"/"$manifestval"
  test -s $TEST_SCRATCH_DIR/implicit-rewritten/blobs/"$configalg"/"$configval"

  # check timestamps in the one we didn't force: find the manifest's and config's digests
  manifestdigest=$(oci_image_manifest_digest "$TEST_SCRATCH_DIR"/default)
  manifestalg=${manifestdigest%%:*}
  manifestval=${manifestdigest##*:}
  configdigest=$(oci_image_config_digest "$TEST_SCRATCH_DIR"/default)
  configalg=${configdigest%%:*}
  configval=${configdigest##*:}
  # check timestamps in the one we didn't force: read the image creation date
  config="$TEST_SCRATCH_DIR"/default/$(oci_image_config "$TEST_SCRATCH_DIR"/default)
  run jq -r '.created' "$config"
  echo "$output"
  assert $status = 0 "looking for the image creation date"
  assert "$output" != "$datestamp" "unexpected creation date for image"
  # check timestamps in the one we didn't force: read the image history entry dates
  run jq -r '.history[-2].created' "$config"
  echo "$output"
  assert $status = 0 "looking for the image history entries"
  jq '.history' "$config"
  for line in "$lines[@]"; do
    assert "$output" != "$datestamp" "unexpected datestamp for history entry"
  done
  # check timestamps in the ones we didn't force: extract the layer blob
  layer="$TEST_SCRATCH_DIR"/default/$(oci_image_last_diff "$TEST_SCRATCH_DIR"/default)
  rm -fr $TEST_SCRATCH_DIR/layer; mkdir -p $TEST_SCRATCH_DIR/layer
  tar -C $TEST_SCRATCH_DIR/layer -xvf "$layer"
  # check timestamps in the ones we didn't force: walk the layer blob, checking timestamps
  for file in $(find $TEST_SCRATCH_DIR/layer/* -print) ; do
    run stat -c %Y $file
    assert $status = 0 "checking datestamp on $file in layer"
    assert "$output" != "$timestamp" "unexpected datestamp on $file in layer"
  done
}

@test "commit-sets-created-annotation" {
  _prefetch busybox
  run_buildah from -q busybox
  local cid="$output"
  for annotation in a=b c=d ; do
    local subdir=${annotation%%=*}
    run_buildah commit --annotation $annotation "$cid" oci:${TEST_SCRATCH_DIR}/$subdir
    local manifest=${TEST_SCRATCH_DIR}/$subdir/$(oci_image_manifest ${TEST_SCRATCH_DIR}/$subdir)
    run jq -r '.annotations["'$subdir'"]' "$manifest"
    assert $status -eq 0
    echo "$output"
    assert "$output" = ${annotation##*=}
  done
  for flagdir in default: timestamp:--timestamp=0 sde:--source-date-epoch=0 suppressed:--unsetannotation=org.opencontainers.image.created specific:--created-annotation=false explicit:--created-annotation=true ; do
    local flag=${flagdir##*:}
    local subdir=${flagdir%%:*}
    run_buildah commit $flag "$cid" oci:${TEST_SCRATCH_DIR}/$subdir
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

@test "commit --disable-compression conflicts with compression flags" {
  _prefetch alpine
  run_buildah from $WITH_POLICY_JSON alpine
  cid=$output

  run_buildah 125 commit \
    $WITH_POLICY_JSON \
    --disable-compression \
    --compression-format zstd \
    $cid \
    dir:${TEST_SCRATCH_DIR}/should-fail
  expect_output --substring "cannot be used together"

  run_buildah 125 commit \
    $WITH_POLICY_JSON \
    --disable-compression \
    --force-compression \
    $cid \
    dir:${TEST_SCRATCH_DIR}/should-fail
  expect_output --substring "cannot be used together"
}

@test "commit --compression-format zstd to dir, oci, and oci-archive" {
  which skopeo || skip "skopeo is not installed"
  _prefetch alpine
  run_buildah from $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid touch /testfile

  for transport in dir oci oci-archive; do
    local dest=${TEST_SCRATCH_DIR}/commit-zstd-${transport}

    run_buildah commit \
      $WITH_POLICY_JSON \
      --compression-format zstd \
      --disable-compression=false \
      $cid \
      ${transport}:$dest

    run skopeo inspect --raw ${transport}:${dest}
    assert $status -eq 0 "$transport: skopeo inspect should succeed"
    expect_output --substring "zstd" \
      "$transport: manifest should reference zstd-compressed layers"
    assert "$output" !~ "gzip" \
      "$transport: manifest should NOT reference gzip layers"
  done
}

@test "commit should respect compression_format from containers.conf" {
  which skopeo || skip "skopeo is not installed"
  _prefetch alpine
  run_buildah from $WITH_POLICY_JSON alpine
  local cid=$output
  run_buildah run $cid touch /testfile

  local confdir=${TEST_SCRATCH_DIR}/commit-conf-zstd
  mkdir -p $confdir
  cat > $confdir/containers.conf << _EOF
[engine]
compression_format="zstd"
_EOF

  local dest=${TEST_SCRATCH_DIR}/commit-conf-zstd-output

  CONTAINERS_CONF=$confdir/containers.conf \
    run_buildah commit \
      $WITH_POLICY_JSON \
      --disable-compression=false \
      $cid \
      dir:$dest

  run skopeo inspect --raw dir:$dest
  assert $status -eq 0 "skopeo inspect should succeed"
  expect_output --substring "zstd" \
    "manifest should reference zstd-compressed layers per containers.conf"
  assert "$output" !~ "gzip" \
    "manifest should NOT reference gzip layers when containers.conf specifies zstd"
}

@test "commit preserves layer annotations" {
  local base_image=$(build_oci_image_with_layer_annotations \
    '"test.annotation.first": "value1",' \
    '"test.annotation.second": "value2"')

  # Start with an image with layer annotations present.
  run_buildah from --quiet $WITH_POLICY_JSON ${base_image}
  local cid=$output

  # Read the layer annotations and save them for verification.
  run_buildah inspect --type=image --format '{{.Manifest}}' ${base_image}
  local base_annotations=$(jq '[.layers[].annotations]' <<<"$output")
  local base_layer_count=$(jq '.layers | length' <<<"$output")
  local annotations_count=$(jq '[.layers[].annotations | select(. != null)] | length' <<<"$output")
  assert "$annotations_count" -ge 1 "base image should have at least one layer with annotations"

  # Add a layer and commit.
  run_buildah copy $cid /dev/null /testfile
  run_buildah commit $WITH_POLICY_JSON $cid committed-img

  # Verify that layer annotations are preserved in the committed image.
  run_buildah inspect --type=image --format '{{.Manifest}}' committed-img
  local committed_manifest="$output"
  for ((i = 0; i < base_layer_count; i++)); do
    local expected
    expected=$(jq -c ".[$i]" <<<"$base_annotations")
    run jq -c ".layers[$i].annotations" <<<"$committed_manifest"
    assert "$output" = "$expected" \
      "layer $i annotations should be preserved after commit"
  done

  # Verify that the newly added layer has no annotations.
  run jq -c ".layers[$base_layer_count].annotations" <<<"$committed_manifest"
  assert "$status" -eq 0 "jq failed parsing new layer annotations"
  assert "$output" = "null" \
    "new layer should not have base-image annotations"
}
