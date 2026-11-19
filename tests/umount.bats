#!/usr/bin/env bats

load helpers

@test "umount-flags-order-verification" {
  run_buildah_umount 125 cnt1 -a
  check_options_flag_err "-a"

  run_buildah_umount 125 cnt1 --all cnt2
  check_options_flag_err "--all"

  run_buildah_umount 125 cnt1 cnt2 --all
  check_options_flag_err "--all"
}

@test "umount one image" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah_mount "$cid"
  run_buildah_umount "$cid"
}

@test "umount bad image" {
  run_buildah_umount 125 badcontainer
}

@test "umount multi images" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah_mount "$cid1"
  run_buildah from --quiet --pull-never $WITH_POLICY_JSON alpine
  cid2=$output
  run_buildah_mount "$cid2"
  run_buildah from --quiet --pull-never $WITH_POLICY_JSON alpine
  cid3=$output
  run_buildah_mount "$cid3"
  run_buildah_umount "$cid1" "$cid2" "$cid3"
}

@test "umount all images" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah_mount "$cid1"
  run_buildah from --quiet --pull-never $WITH_POLICY_JSON alpine
  cid2=$output
  run_buildah_mount "$cid2"
  run_buildah from --quiet --pull-never $WITH_POLICY_JSON alpine
  cid3=$output
  run_buildah_mount "$cid3"
  run_buildah_umount --all
  run_buildah_mount
  expect_output "" "no container should be mounted after buildah umount --all"
}

@test "umount multi images one bad" {
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah_mount "$cid1"
  run_buildah from --quiet --pull-never $WITH_POLICY_JSON alpine
  cid2=$output
  run_buildah_mount "$cid2"
  run_buildah from --quiet --pull-never $WITH_POLICY_JSON alpine
  cid3=$output
  run_buildah_mount "$cid3"
  run_buildah_umount 125 "$cid1" badcontainer "$cid2" "$cid3"
}

@test "umount --all=false w/o arguments error out" {
  skip_if_rootless_environment
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah_mount "$cid1"
  mnt1=$output

  run_buildah_umount 125 --all=false
  expect_output --substring "at least one container ID must be specified" "umount --all=false"
  run_buildah inspect --format '{{.MountPoint}}' $cid1
  expect_output "$mnt1" "$cid1 must stay mounted after umount --all=false failed because of bad arguments"

  run_buildah_umount 0 $cid1
}

@test "umount --all=false w container ID umount only that container" {
  skip_if_rootless_environment
  _prefetch alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah_mount "$cid1"
  run_buildah from --quiet --pull-never $WITH_POLICY_JSON alpine
  cid2=$output
  run_buildah_mount "$cid2"
  mnt2=$output

  run_buildah_umount --all=false $cid1
  run_buildah inspect --format '{{.MountPoint}}' $cid1
  expect_output "" "$cid1 must be umounted"
  run_buildah inspect --format '{{.MountPoint}}' $cid2
  expect_output "$mnt2" "$cid1 must stay mounted"

  run_buildah_umount $cid2
}
