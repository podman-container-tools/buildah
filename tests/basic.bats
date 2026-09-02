#!/usr/bin/env bats

load helpers

@test "from" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah rm $cid
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah rm $cid
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON --name i-love-naming-things alpine
  cid=$output
  run_buildah rm i-love-naming-things
}

@test "from-defaultpull" {
  _prefetch alpine
  run_buildah from --quiet $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah rm $cid
}

@test "from-scratch" {
  run_buildah from --pull=false $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah rm $cid
  run_buildah from --pull=true  $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah rm $cid
}

@test "from-nopull" {
  run_buildah 125 from --pull-never $WITH_POLICY_JSON alpine
}

@test "mount" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah_mount $cid
  root=$output
  run_buildah_umount $cid
  run_buildah_mount $cid
  root=$output
  touch $root/foobar
  run_buildah_umount $cid
  run_buildah rm $cid
}

@test "by-name" {
  run_buildah from $WITH_POLICY_JSON --name scratch-working-image-for-test scratch
  cid=$output
  run_buildah_mount scratch-working-image-for-test
  root=$output
  run_buildah_umount scratch-working-image-for-test
  run_buildah rm scratch-working-image-for-test
}

@test "commit" {
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  createrandom ${TEST_SCRATCH_DIR}/other-randomfile

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah_mount $cid
  root=$output
  cp ${TEST_SCRATCH_DIR}/randomfile $root/randomfile
  run_buildah_umount $cid
  run_buildah commit --iidfile ${TEST_SCRATCH_DIR}/output.iid $WITH_POLICY_JSON $cid containers-storage:new-image
  iid=$(< ${TEST_SCRATCH_DIR}/output.iid)
  assert "$iid" =~ "sha256:[0-9a-f]{64}"
  run_buildah rmi $iid
  run_buildah commit $WITH_POLICY_JSON $cid containers-storage:new-image
  run_buildah rm $cid
  run_buildah from --quiet $WITH_POLICY_JSON new-image
  newcid=$output
  run_buildah_mount $newcid
  newroot=$output
  test -s $newroot/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $newroot/randomfile
  cp ${TEST_SCRATCH_DIR}/other-randomfile $newroot/other-randomfile
  run_buildah commit $WITH_POLICY_JSON $newcid containers-storage:other-new-image
  # Not an allowed ordering of arguments and flags.  Check that it's rejected.
  run_buildah 125 commit $newcid $WITH_POLICY_JSON containers-storage:rejected-new-image
  run_buildah commit $WITH_POLICY_JSON $newcid containers-storage:another-new-image
  run_buildah commit $WITH_POLICY_JSON $newcid yet-another-new-image
  run_buildah commit $WITH_POLICY_JSON $newcid containers-storage:gratuitous-new-image
  run_buildah_umount $newcid
  run_buildah rm $newcid

  run_buildah from --quiet $WITH_POLICY_JSON other-new-image
  othernewcid=$output
  run_buildah_mount $othernewcid
  othernewroot=$output
  test -s $othernewroot/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $othernewroot/randomfile
  test -s $othernewroot/other-randomfile
  cmp ${TEST_SCRATCH_DIR}/other-randomfile $othernewroot/other-randomfile
  run_buildah_umount $othernewcid
  run_buildah rm $othernewcid

  run_buildah from --quiet $WITH_POLICY_JSON another-new-image
  anothernewcid=$output
  run_buildah_mount $anothernewcid
  anothernewroot=$output
  test -s $anothernewroot/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $anothernewroot/randomfile
  test -s $anothernewroot/other-randomfile
  cmp ${TEST_SCRATCH_DIR}/other-randomfile $anothernewroot/other-randomfile
  run_buildah_umount $anothernewcid
  run_buildah rm $anothernewcid

  run_buildah from --quiet $WITH_POLICY_JSON yet-another-new-image
  yetanothernewcid=$output
  run_buildah_mount $yetanothernewcid
  yetanothernewroot=$output
  test -s $yetanothernewroot/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $yetanothernewroot/randomfile
  test -s $yetanothernewroot/other-randomfile
  cmp ${TEST_SCRATCH_DIR}/other-randomfile $yetanothernewroot/other-randomfile
  run_buildah_umount $yetanothernewcid
  run_buildah delete $yetanothernewcid

  run_buildah from --quiet $WITH_POLICY_JSON new-image
  newcid=$output
  run_buildah commit --rm $WITH_POLICY_JSON $newcid containers-storage:remove-container-image
  run_buildah_mount 125 $newcid

  run_buildah rmi remove-container-image
  run_buildah rmi containers-storage:other-new-image
  run_buildah rmi another-new-image
  run_buildah images -q
  assert "$output" != "" "images -q"
  run_buildah rmi -a
  run_buildah images -q
  expect_output ""
}

@test "release" {
  [[ "${RELEASE_TESTING:-false}" == "true" ]] || \
    skip "Release testing may be enabled by setting \$RELEASE_TESTING = 'true'."

  run_buildah --version

  assert "$output" "!~" "dev" "The Buildah version string does not mention 'dev'."
}

@test "experimental-timeout" {
  _prefetch busybox
  run_buildah 125 --experimental-timeout=1ns from busybox
  expect_output --substring "context deadline exceeded"
}
