#!/usr/bin/env bats

load helpers

@test "overlay specific level" {
  if test \! -e /usr/bin/fuse-overlayfs -a "$BUILDAH_ISOLATION" = "rootless"; then
    skip "BUILDAH_ISOLATION = $BUILDAH_ISOLATION" and no /usr/bin/fuse-overlayfs present
  elif test "$STORAGE_DRIVER" = "vfs"; then
    skip "skipping overlay test because \$STORAGE_DRIVER = $STORAGE_DRIVER"
  fi
  image=alpine
  _prefetch $image
  mkdir ${TEST_SCRATCH_DIR}/lower
  touch ${TEST_SCRATCH_DIR}/lower/foo

  run_buildah from --quiet -v ${TEST_SCRATCH_DIR}/lower:/lower:O --quiet $WITH_POLICY_JSON $image
  cid=$output

  # This should succeed
  run_buildah run $cid ls /lower/foo

  # Create and remove content in the overlay directory, should succeed,
  # resetting the contents between each run.
  run_buildah run $cid touch /lower/bar
  run_buildah run $cid rm /lower/foo

  # This should fail, second runs of containers go back to original
  run_buildah 1 run $cid ls /lower/bar

  # This should fail
  run ls ${TEST_SCRATCH_DIR}/lower/bar
  assert "$status" -ne 0 "status of ls ${TEST_SCRATCH_DIR}/lower/bar"
}

@test "overlay source permissions and owners" {
  if test \! -e /usr/bin/fuse-overlayfs -a "$BUILDAH_ISOLATION" = "rootless"; then
    skip "BUILDAH_ISOLATION = $BUILDAH_ISOLATION" and no /usr/bin/fuse-overlayfs present
  elif test "$STORAGE_DRIVER" = "vfs"; then
    skip "skipping overlay test because \$STORAGE_DRIVER = $STORAGE_DRIVER"
  fi
  image=alpine
  _prefetch $image
  mkdir -m 770 ${TEST_SCRATCH_DIR}/lower
  chown 1:1 ${TEST_SCRATCH_DIR}/lower
  permission=$(stat -c "%a %u %g" ${TEST_SCRATCH_DIR}/lower)
  run_buildah from --quiet -v ${TEST_SCRATCH_DIR}/lower:/tmp/test:O --quiet $WITH_POLICY_JSON $image
  cid=$output

  # This should succeed
  run_buildah run $cid sh -c 'stat -c "%a %u %g" /tmp/test'
  expect_output "$permission"

  # Create and remove content in the overlay directory, should succeed
  touch ${TEST_SCRATCH_DIR}/lower/foo
  run_buildah run $cid touch /tmp/test/bar
  run_buildah run $cid rm /tmp/test/foo

  # This should fail, second runs of containers go back to original
  run_buildah 1 run $cid ls /tmp/test/bar

  # This should fail since /tmp/test was an overlay, not a bind mount
  run ls ${TEST_SCRATCH_DIR}/lower/bar
  assert "$status" -ne 0 "status of ls ${TEST_SCRATCH_DIR}/lower/bar"
}

@test "overlay path contains colon" {
  if test \! -e /usr/bin/fuse-overlayfs -a "$BUILDAH_ISOLATION" = "rootless"; then
    skip "BUILDAH_ISOLATION = $BUILDAH_ISOLATION" and no /usr/bin/fuse-overlayfs present
  elif test "$STORAGE_DRIVER" = "vfs"; then
    skip "skipping overlay test because \$STORAGE_DRIVER = $STORAGE_DRIVER"
  fi
  image=alpine
  _prefetch $image
  mkdir ${TEST_SCRATCH_DIR}/a:lower
  touch ${TEST_SCRATCH_DIR}/a:lower/foo

  if test $(stat -f -c %T ${TEST_SCRATCH_DIR}/a:lower) = overlayfs; then
    # we'll try to use fuse-overlayfs, which at least through 1.13
    # can't accept ":" in layer locations, escaped or not, so bail
    # now instead of breaking the whole thing
    skip "unable to test bind from an overlay location that includes colon characters"
  fi

  # This should succeed.
  # Add double backslash, because shell will escape.
  run_buildah from --quiet -v ${TEST_SCRATCH_DIR}/a\\:lower:/a\\:lower:O --quiet $WITH_POLICY_JSON $image
  cid=$output

  # This should succeed
  run_buildah run $cid ls /a:lower/foo

  # Mount volume when run
  run_buildah run -v ${TEST_SCRATCH_DIR}/a\\:lower:/b\\:lower:O $cid ls /b:lower/foo

  # Create and remove content in the overlay directory, should succeed,
  # resetting the contents between each run.
  run_buildah run $cid touch /a:lower/bar
  run_buildah run $cid rm /a:lower/foo

  # This should fail, second runs of containers go back to original
  run_buildah 1 run $cid ls /a:lower/bar

  # This should fail
  run ls ${TEST_SCRATCH_DIR}/a:lower/bar
  assert "$status" -ne 0 "status of ls ${TEST_SCRATCH_DIR}/a:lower/bar"
}
