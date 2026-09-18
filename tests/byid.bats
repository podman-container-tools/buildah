#!/usr/bin/env bats

load helpers

@test "from-by-id" {
  image=busybox
  _prefetch $image

  # Pull down the image, if we have to.
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON $image
  expect_output "$image-working-container"
  cid=$output
  run_buildah rm $cid

  # Get the image's ID.
  run_buildah images -q $image
  expect_line_count 1
  iid="$output"

  # Use the image's ID to create a container.
  run_buildah from --pull=false $WITH_POLICY_JSON ${iid}
  expect_line_count 1
  cid="$output"
  run_buildah rm $cid

  # Use a truncated form of the image's ID to create a container.
  run_buildah from --pull=false $WITH_POLICY_JSON ${iid:0:6}
  expect_line_count 1
  cid="$output"
  run_buildah rm $cid

  run_buildah rmi $iid
}

@test "inspect-by-id" {
  image=busybox
  _prefetch $image

  # Pull down the image, if we have to.
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON $image
  expect_output "$image-working-container"
  cid=$output
  run_buildah rm $cid

  # Get the image's ID.
  run_buildah images -q $image
  expect_line_count 1
  iid="$output"

  # Use the image's ID to inspect it.
  run_buildah inspect --type=image ${iid}

  # Use a truncated copy of the image's ID to inspect it.
  run_buildah inspect --type=image ${iid:0:6}

  run_buildah rmi $iid
}

@test "push-by-id" {
  for image in busybox quay.io/libpod/k8s-pause:3.5 ; do
    echo pulling/pushing image $image
    _prefetch $image

    TARGET=${TEST_SCRATCH_DIR}/subdir-$(basename $image)
    mkdir -p $TARGET $TARGET-truncated

    # Pull down the image, if we have to.
    run_buildah from --quiet --cidfile ${TEST_SCRATCH_DIR}/cid --pull=false $WITH_POLICY_JSON $image
    cid="$(< ${TEST_SCRATCH_DIR}/cid)"
    run_buildah rm $cid

    # Get the image's ID.
    run_buildah images -q $image
    expect_output --substring '^[0-9a-f]{12,64}$'
    iid="$output"

    # Use the image's ID to push it.
    run_buildah push $WITH_POLICY_JSON $iid dir:$TARGET

    # Use a truncated form of the image's ID to push it.
    run_buildah push $WITH_POLICY_JSON ${iid:0:6} dir:$TARGET-truncated

    # Use the image's complete ID to remove it.
    run_buildah rmi $iid
  done
}

@test "rmi-by-id" {
  image=busybox
  _prefetch $image

  # Pull down the image, if we have to.
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON $image
  expect_output "$image-working-container"
  run_buildah rm $output

  # Get the image's ID.
  run_buildah images -q $image
  expect_output --substring '^[0-9a-f]{12,64}$'
  iid="$output"

  # Use a truncated copy of the image's ID to remove it.
  run_buildah rmi ${iid:0:6}
}
