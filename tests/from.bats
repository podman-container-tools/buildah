#!/usr/bin/env bats

load helpers

@test "from-flags-order-verification" {
  run_buildah 125 from scratch -q
  check_options_flag_err "-q"

  run_buildah 125 from scratch --pull
  check_options_flag_err "--pull"

  run_buildah 125 from scratch --ulimit=1024
  check_options_flag_err "--ulimit=1024"

  run_buildah 125 from scratch --name container-name-irrelevant
  check_options_flag_err "--name"

  run_buildah 125 from scratch --cred="fake fake" --name small
  check_options_flag_err "--cred=fake fake"
}

@test "from-with-digest" {
  _prefetch alpine
  run_buildah inspect --format "{{.FromImageID}}" alpine
  digest=$output

  run_buildah from "sha256:$digest"
  run_buildah rm $output

  run_buildah 125 from sha256:1111111111111111111111111111111111111111111111111111111111111111
  expect_output --substring "1111111111111111111111111111111111111111111111111111111111111111: image not known"
}

@test "commit-to-from-elsewhere" {
  elsewhere=${TEST_SCRATCH_DIR}/elsewhere-img
  mkdir -p ${elsewhere}

  run_buildah from --retry 4 --retry-delay 4s --pull $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid dir:${elsewhere}
  run_buildah rm $cid

  run_buildah from --quiet --pull=false $WITH_POLICY_JSON dir:${elsewhere}
  expect_output "dir-working-container"
  run_buildah rm $output

  run_buildah from --quiet --pull-always $WITH_POLICY_JSON dir:${elsewhere}
  expect_output "dir-working-container"

  run_buildah from --pull $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid oci-archive:${elsewhere}.oci
  run_buildah rm $cid

  run_buildah from --quiet --pull=false $WITH_POLICY_JSON oci-archive:${elsewhere}.oci
  expect_output "oci-archive-working-container"
  run_buildah rm $output

  run_buildah from --pull $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid docker-archive:${elsewhere}.docker
  run_buildah rm $cid

  run_buildah from --quiet --pull=false $WITH_POLICY_JSON docker-archive:${elsewhere}.docker
  expect_output "docker-archive-working-container"
  run_buildah rm $output
}

@test "from-tagged-image" {
  # GitHub #396: Make sure the container name starts with the correct image even when it's tagged.
  run_buildah from --pull=false $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah commit $WITH_POLICY_JSON "$cid" scratch2
  # Also check for base-image annotations.
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' scratch2
  expect_output "" "no base digest for scratch"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.name" }}' scratch2
  expect_output "" "no base name for scratch"
  run_buildah rm $cid
  run_buildah tag scratch2 scratch3
  # Set --pull=false to prevent looking for a newer scratch3 image.
  run_buildah from --pull=false $WITH_POLICY_JSON scratch3
  expect_output --substring "scratch3-working-container"
  run_buildah rm $output
  # Set --pull=never to prevent looking for a newer scratch3 image.
  run_buildah from --pull=never $WITH_POLICY_JSON scratch3
  expect_output --substring "scratch3-working-container"
  run_buildah rm $output
  run_buildah rmi scratch2 scratch3

  # GitHub https://github.com/containers/buildah/issues/396#issuecomment-360949396
  run_buildah from --quiet --pull=true $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah rm $cid
  run_buildah tag alpine alpine2
  run_buildah from --quiet $WITH_POLICY_JSON localhost/alpine2
  expect_output "alpine2-working-container"
  run_buildah rm $output
  tmp=$RANDOM
  run_buildah from --suffix $tmp --quiet $WITH_POLICY_JSON localhost/alpine2
  expect_output "alpine2-$tmp"
  run_buildah rm $output
  run_buildah rmi alpine alpine2

  run_buildah from --quiet --pull=true $WITH_POLICY_JSON docker.io/alpine
  run_buildah rm $output
  run_buildah rmi docker.io/alpine

  run_buildah from --quiet --pull=true $WITH_POLICY_JSON docker.io/alpine:latest
  run_buildah rm $output
  run_buildah rmi docker.io/alpine:latest

  # FIXME FIXME FIXME: I don't see the point of these. Any reason not to delete?
#  run_buildah from --quiet --pull=true $WITH_POLICY_JSON docker.io/centos:7
#  run_buildah rm $output
#  run_buildah rmi docker.io/centos:7

#  run_buildah from --quiet --pull=true $WITH_POLICY_JSON docker.io/centos:latest
#  run_buildah rm $output
#  run_buildah rmi docker.io/centos:latest
}

@test "from the following transports: docker-archive, oci-archive, and dir" {
  _prefetch alpine
  run_buildah from --quiet --pull=true $WITH_POLICY_JSON alpine
  run_buildah rm $output

  # #2205: The important thing here is differentiating 'docker:latest'
  # (the image) from 'docker:/path' ('docker' as a protocol identifier).
  # This is a parsing fix so we don't actually need to pull the image.
  run_buildah 125 from --quiet --pull=false $WITH_POLICY_JSON docker:latest
  assert "$output" = "Error: docker:latest: image not known"

  run_buildah push $WITH_POLICY_JSON alpine docker-archive:${TEST_SCRATCH_DIR}/docker-alp.tar:alpine
  run_buildah push $WITH_POLICY_JSON alpine    oci-archive:${TEST_SCRATCH_DIR}/oci-alp.tar:alpine
  run_buildah push $WITH_POLICY_JSON alpine            dir:${TEST_SCRATCH_DIR}/alp-dir
  run_buildah rmi alpine

  run_buildah from --quiet $WITH_POLICY_JSON docker-archive:${TEST_SCRATCH_DIR}/docker-alp.tar
  expect_output "alpine-working-container"
  run_buildah rm ${output}
  run_buildah rmi alpine

  run_buildah from --quiet $WITH_POLICY_JSON oci-archive:${TEST_SCRATCH_DIR}/oci-alp.tar
  expect_output "alpine-working-container"
  run_buildah rm ${output}
  run_buildah rmi alpine

  run_buildah from --quiet $WITH_POLICY_JSON dir:${TEST_SCRATCH_DIR}/alp-dir
  expect_output "dir-working-container"
}

@test "from the following transports: docker-archive and oci-archive with no image reference" {
  _prefetch alpine
  run_buildah from --quiet --pull=true $WITH_POLICY_JSON alpine
  run_buildah rm $output

  run_buildah push $WITH_POLICY_JSON alpine docker-archive:${TEST_SCRATCH_DIR}/docker-alp.tar
  run_buildah push $WITH_POLICY_JSON alpine    oci-archive:${TEST_SCRATCH_DIR}/oci-alp.tar
  run_buildah rmi alpine

  run_buildah from --quiet $WITH_POLICY_JSON docker-archive:${TEST_SCRATCH_DIR}/docker-alp.tar
  expect_output "alpine-working-container"
  run_buildah rm $output
  run_buildah rmi -a

  run_buildah from --quiet $WITH_POLICY_JSON oci-archive:${TEST_SCRATCH_DIR}/oci-alp.tar
  expect_output "oci-archive-working-container"
  run_buildah rm $output
  run_buildah rmi -a
}

@test "from cpu-period test" {
  skip_if_rootless_environment
  skip_if_chroot
  skip_if_no_runtime

  _prefetch alpine
  run_buildah from --quiet --cpu-period=5000 --pull $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid /bin/sh -c "cut -d ' ' -f 2 /sys/fs/cgroup/\$(awk -F: '{print \$NF}' /proc/self/cgroup)/cpu.max"
  expect_output "5000"
}

@test "from cpu-quota test" {
  skip_if_rootless_environment
  skip_if_chroot
  skip_if_no_runtime

  _prefetch alpine
  run_buildah from --quiet --cpu-quota=5000 --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid /bin/sh -c "cut -d ' ' -f 1 /sys/fs/cgroup/\$(awk -F: '{print \$NF}' /proc/self/cgroup)/cpu.max"
  expect_output "5000"
}

@test "from cpu-shares test" {
  skip_if_rootless_environment
  skip_if_chroot
  skip_if_no_runtime

  _prefetch alpine
  for shares in 2 200 2000 12345 20000 200000 ; do
    run_buildah from --quiet --cpu-shares=${shares} --pull $WITH_POLICY_JSON alpine
    cid=$output
    local converted="$(convert_v1_shares_to_v2_weight ${shares})"
    local expect="(weight ${converted##* }|weight ${converted%% *})"
    run_buildah run $cid /bin/sh -c "echo -n 'weight '; cat /sys/fs/cgroup/\$(awk -F : '{print \$NF}' /proc/self/cgroup)/cpu.weight"
    echo expected "${expect}"
    expect_output --substring "${expect}"
  done
}

@test "from cpuset-cpus test" {
  skip_if_rootless_environment
  skip_if_chroot
  skip_if_no_runtime

  _prefetch alpine
  run_buildah from --quiet --cpuset-cpus=0 --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid /bin/sh -c "cat /sys/fs/cgroup/\$(awk -F : '{print \$NF}' /proc/self/cgroup)/cpuset.cpus"
  expect_output "0"
}

@test "from cpuset-mems test" {
  skip_if_rootless_environment
  skip_if_chroot
  skip_if_no_runtime

  _prefetch alpine
  run_buildah from --quiet --cpuset-mems=0 --pull $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid /bin/sh -c "cat /sys/fs/cgroup/\$(awk -F : '{print \$NF}' /proc/self/cgroup)/cpuset.mems"
  expect_output "0"
}

@test "from memory test" {
  skip_if_rootless_environment
  skip_if_chroot
  _prefetch alpine
  run_buildah from --quiet --memory=40m --memory-swap=70m --pull=false $WITH_POLICY_JSON alpine
  cid=$output

  mpath="/sys/fs/cgroup\$(awk -F: '{print \$3}' /proc/self/cgroup)/memory.max"
  spath="/sys/fs/cgroup\$(awk -F: '{print \$3}' /proc/self/cgroup)/memory.swap.max"
  run_buildah run $cid sh -c "cat $mpath"
  expect_output "41943040" "$mpath"
  run_buildah run $cid sh -c "cat $spath"
  expect_output "31457280" "$spath"
}

@test "from volume test" {
  skip_if_no_runtime

  _prefetch alpine
  run_buildah from --quiet --volume=${TEST_SCRATCH_DIR}:/myvol --pull $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid -- cat /proc/mounts
  expect_output --substring " /myvol "
}

@test "from volume ro test" {
  skip_if_chroot
  skip_if_no_runtime

  _prefetch alpine
  run_buildah from --quiet --volume=${TEST_SCRATCH_DIR}:/myvol:ro --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid -- cat /proc/mounts
  expect_output --substring " /myvol "
}

@test "from --volume with U flag" {
  skip_if_rootless_environment
  skip_if_no_runtime

  # Check if we're running in an environment that can even test this.
  run readlink /proc/self/ns/user
  echo "readlink /proc/self/ns/user -> $output"
  [ $status -eq 0 ] || skip "user namespaces not supported"

  # Generate mappings for using a user namespace.
  uidbase=$((${RANDOM}+1024))
  gidbase=$((${RANDOM}+1024))
  uidsize=$((${RANDOM}+1024))
  gidsize=$((${RANDOM}+1024))

  # Create source volume.
  mkdir ${TEST_SCRATCH_DIR}/testdata
  touch ${TEST_SCRATCH_DIR}/testdata/testfile1.txt

  # Create a container that uses that mapping and U volume flag.
  _prefetch alpine
  run_buildah from --pull=false $WITH_POLICY_JSON --userns-uid-map 0:$uidbase:$uidsize --userns-gid-map 0:$gidbase:$gidsize --volume ${TEST_SCRATCH_DIR}/testdata:/mnt:z,U alpine
  ctr="$output"

  # Test mounted volume has correct UID and GID ownership.
  run_buildah run "$ctr" stat -c "%u:%g" /mnt/testfile1.txt
  expect_output "0:0"

  # Test user can create file in the mounted volume.
  run_buildah run "$ctr" touch /mnt/testfile2.txt

  # Test created file has correct UID and GID ownership.
  run_buildah run "$ctr" stat -c "%u:%g" /mnt/testfile2.txt
  expect_output "0:0"
}

@test "from shm-size test" {
  skip_if_chroot
  skip_if_no_runtime

  _prefetch alpine
  run_buildah from --quiet --shm-size=80m --pull $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid -- df -h /dev/shm
  expect_output --substring " 80.0M "
}

@test "from add-host test" {
  skip_if_no_runtime

  _prefetch alpine
  run_buildah from --quiet --add-host=localhost:127.0.0.1 --pull $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run --net=container $cid -- cat /etc/hosts
  expect_output --substring "127.0.0.1[[:blank:]]*localhost"
}

@test "from name test" {
  _prefetch alpine
  container_name=mycontainer
  run_buildah from --quiet --name=${container_name} --pull $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah inspect --format '{{.Container}}' ${container_name}
}

@test "from cidfile test" {
  _prefetch alpine
  run_buildah from --cidfile ${TEST_SCRATCH_DIR}/output.cid --pull=false $WITH_POLICY_JSON alpine
  cid=$(< ${TEST_SCRATCH_DIR}/output.cid)
  run_buildah containers -f id=${cid}
}

@test "from pull never" {
  run_buildah 125 from $WITH_POLICY_JSON --pull-never busybox
  echo "$output"
  expect_output --substring "busybox: image not known"

  run_buildah 125 from $WITH_POLICY_JSON --pull=false busybox
  echo "$output"
  expect_output --substring "busybox: image not known"

  run_buildah from $WITH_POLICY_JSON --pull=ifmissing busybox
  echo "$output"
  expect_output --substring "busybox-working-container"

  run_buildah from $WITH_POLICY_JSON --pull=never busybox
  echo "$output"
  expect_output --substring "busybox-working-container"
}

@test "from pull false no local image" {
  _prefetch busybox
  target=my-busybox
  run_buildah from $WITH_POLICY_JSON --pull=false busybox
  echo "$output"
  expect_output --substring "busybox-working-container"
}

@test "from with nonexistent authfile: fails" {
  run_buildah 125 from --authfile /no/such/file --pull $WITH_POLICY_JSON alpine
  assert "$output" =~ "Error: credential file is not accessible: (faccessat|stat) /no/such/file: no such file or directory"
}

@test "from --pull-always: emits 'Getting' even if image is cached" {
  _prefetch docker.io/busybox
  run_buildah inspect --format "{{.FromImageDigest}}" docker.io/busybox
  fromDigest="$output"
  run_buildah pull $WITH_POLICY_JSON docker.io/busybox
  run_buildah from $WITH_POLICY_JSON --name busyboxc --pull docker.io/busybox
  expect_output --substring "Getting"
  run_buildah rm busyboxc
  run_buildah from $WITH_POLICY_JSON --name busyboxc --pull=true docker.io/busybox
  expect_output --substring "Getting"
  run_buildah rm busyboxc
  run_buildah from $WITH_POLICY_JSON --name busyboxc --pull-always docker.io/busybox
  expect_output --substring "Getting"
  run_buildah commit $WITH_POLICY_JSON busyboxc fakename-img
  run_buildah 125 from $WITH_POLICY_JSON --pull=always fakename-img

  # Also check for base-image annotations.
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.digest" }}' fakename-img
  expect_output "$fromDigest" "base digest from busybox"
  run_buildah inspect --format '{{index .ImageAnnotations "org.opencontainers.image.base.name" }}' fakename-img
  expect_output "docker.io/library/busybox:latest" "base name from busybox"
}

@test "from --quiet: should not emit progress messages" {
  # Force a pull. Normally this would say 'Getting image ...' and other
  # progress messages. With --quiet, we should see only the container name.
  run_buildah '?' rmi busybox
  run_buildah from $WITH_POLICY_JSON --quiet docker.io/busybox
  expect_output "busybox-working-container"
}

@test "from encrypted local image" {
  _prefetch busybox
  mkdir ${TEST_SCRATCH_DIR}/tmp
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey.pem 1024
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey2.pem 1024
  openssl rsa -in ${TEST_SCRATCH_DIR}/tmp/mykey.pem -pubout > ${TEST_SCRATCH_DIR}/tmp/mykey.pub
  run_buildah push $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword --encryption-key jwe:${TEST_SCRATCH_DIR}/tmp/mykey.pub busybox oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc

  # Try encrypted image without key should fail
  run_buildah 125 from oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc
  expect_output --substring "does not match config's DiffID"

  # Try encrypted image with wrong key should fail
  run_buildah 125 from --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey2.pem oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc
  expect_output --substring "decrypting layer .* no suitable key unwrapper found or none of the private keys could be used for decryption"

  # Providing the right key should succeed
  run_buildah from  --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey.pem oci:${TEST_SCRATCH_DIR}/tmp/busybox_enc

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "from encrypted registry image" {
  _prefetch busybox
  mkdir ${TEST_SCRATCH_DIR}/tmp
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey.pem 2048
  openssl genrsa -out ${TEST_SCRATCH_DIR}/tmp/mykey2.pem 2048
  openssl rsa -in ${TEST_SCRATCH_DIR}/tmp/mykey.pem -pubout > ${TEST_SCRATCH_DIR}/tmp/mykey.pub
  start_registry
  run_buildah push $WITH_POLICY_JSON --tls-verify=false --creds testuser:testpassword --encryption-key jwe:${TEST_SCRATCH_DIR}/tmp/mykey.pub busybox docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest

  # Try encrypted image without key should fail
  run_buildah 125 from --tls-verify=false --creds testuser:testpassword docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest
  expect_output --substring "does not match config's DiffID"

  # Try encrypted image with wrong key should fail
  run_buildah 125 from --tls-verify=false --creds testuser:testpassword --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey2.pem docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest
  expect_output --substring "decrypting layer .* no suitable key unwrapper found or none of the private keys could be used for decryption"

  # Providing the right key should succeed
  run_buildah from --tls-verify=false --creds testuser:testpassword --decryption-key ${TEST_SCRATCH_DIR}/tmp/mykey.pem docker://localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest
  run_buildah rm -a
  run_buildah rmi localhost:${REGISTRY_PORT}/buildah/busybox_encrypted:latest

  rm -rf ${TEST_SCRATCH_DIR}/tmp
}

@test "from with non buildah container" {
  skip_if_in_container
  skip_if_no_podman

  _prefetch busybox
  podman create --net=host --name busyboxc-podman busybox top
  run_buildah from $WITH_POLICY_JSON --name busyboxc busybox
  expect_output --substring "busyboxc"
  podman rm -f busyboxc-podman
  run_buildah rm busyboxc
}

@test "from --arch test" {
  skip_if_no_runtime

  _prefetch alpine
  run_buildah from --quiet --pull $WITH_POLICY_JSON --arch=arm64 alpine
  other=$output
  run_buildah from --quiet --pull $WITH_POLICY_JSON --arch=$(go env GOARCH) alpine
  cid=$output
  run_buildah copy --from $other $cid /etc/apk/arch /root/other-arch
  run_buildah run $cid cat /root/other-arch
  expect_output "aarch64"

  run_buildah from --quiet --pull $WITH_POLICY_JSON --arch=s390x alpine
  other=$output
  run_buildah copy --from $other $cid /etc/apk/arch /root/other-arch
  run_buildah run $cid cat /root/other-arch
  expect_output "s390x"
}

@test "from --platform test" {
  skip_if_no_runtime

  run_buildah version
  platform=$(grep ^BuildPlatform: <<< "$output")
  echo "$platform"
  platform=${platform##* }
  echo "$platform"

  _prefetch alpine
  run_buildah from --quiet --pull $WITH_POLICY_JSON --platform=linux/arm64 alpine
  other=$output
  run_buildah from --quiet --pull $WITH_POLICY_JSON --platform=${platform} alpine
  cid=$output
  run_buildah copy --from $other $cid /etc/apk/arch /root/other-arch
  run_buildah run $cid cat /root/other-arch
  expect_output "aarch64"

  run_buildah from --quiet --pull $WITH_POLICY_JSON --platform=linux/s390x alpine
  other=$output
  run_buildah copy --from $other $cid /etc/apk/arch /root/other-arch
  run_buildah run $cid cat /root/other-arch
  expect_output "s390x"
}

@test "from --authfile test" {
  _prefetch busybox
  start_registry
  run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}
  run_buildah push $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth busybox docker://localhost:${REGISTRY_PORT}/buildah/busybox:latest
  target=busybox-image
  run_buildah from -q $WITH_POLICY_JSON --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth docker://localhost:${REGISTRY_PORT}/buildah/busybox:latest
  run_buildah rm $output
  run_buildah rmi localhost:${REGISTRY_PORT}/buildah/busybox:latest
}

@test "from --cap-add/--cap-drop test" {
  _prefetch alpine
  CAP_DAC_OVERRIDE=2  # unlikely to change

  # Try with default caps.
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid awk '/^CapEff/{print $2;}' /proc/self/status
  defaultcaps="$output"
  run_buildah rm $cid

  if ((0x$defaultcaps & 0x$CAP_DAC_OVERRIDE)); then
    run_buildah from --quiet --cap-drop CAP_DAC_OVERRIDE --pull=false $WITH_POLICY_JSON alpine
    cid=$output
    run_buildah run $cid awk '/^CapEff/{print $2;}' /proc/self/status
    droppedcaps="$output"
    run_buildah rm $cid
    if ((0x$droppedcaps & 0x$CAP_DAC_OVERRIDE)); then
      die "--cap-drop did not drop DAC_OVERRIDE: $droppedcaps"
    fi
  else
    run_buildah from --quiet --cap-add CAP_DAC_OVERRIDE --pull=false $WITH_POLICY_JSON alpine
    cid=$output
    run_buildah run $cid awk '/^CapEff/{print $2;}' /proc/self/status
    addedcaps="$output"
    run_buildah rm $cid
    if (( !(0x$addedcaps & 0x$CAP_DAC_OVERRIDE) )); then
      die "--cap-add did not add DAC_OVERRIDE: $addedcaps"
    fi
  fi
}

@test "from ulimit test" {
  _prefetch alpine
  run_buildah from -q --ulimit cpu=300 $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid /bin/sh -c "ulimit -t"
  expect_output "300" "ulimit -t"
}

@test "from isolation test" {
  skip_if_rootless_environment
  _prefetch alpine
  run_buildah from -q --isolation chroot $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah inspect $cid
  expect_output --substring '"Isolation": "chroot"'

  if [ -z "${BUILDAH_ISOLATION}" ]; then
    run readlink /proc/self/ns/pid
    host_pidns=$output
    run_buildah run --pid private $cid readlink /proc/self/ns/pid
    # chroot isolation doesn't make a new PID namespace.
    expect_output "${host_pidns}"
  fi
}

@test "from cgroup-parent test" {
  skip_if_rootless_environment
  skip_if_chroot

  _prefetch alpine
  # with cgroup-parent
  run_buildah from -q --cgroupns=host --cgroup-parent test-cgroup $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah --cgroup-manager cgroupfs run $cid /bin/sh -c 'cat /proc/$$/cgroup'
  expect_output --substring "test-cgroup"

  # without cgroup-parent
  run_buildah from -q $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah --cgroup-manager cgroupfs run $cid /bin/sh -c 'cat /proc/$$/cgroup'
  if [ -n "$(grep "test-cgroup" <<< "$output")" ]; then
    die "Unexpected cgroup."
  fi
}

@test "from-image-with-zstd-compression" {
  copy --format oci --dest-compress --dest-compress-format zstd docker://quay.io/libpod/alpine_nginx:latest dir:${TEST_SCRATCH_DIR}/base-image
  run_buildah from dir:${TEST_SCRATCH_DIR}/base-image
}

@test "from proxy test" {
  skip_if_no_runtime

  _prefetch alpine
  tmp=$RANDOM
  run_buildah from --quiet --pull $WITH_POLICY_JSON alpine
  cid=$output
  FTP_PROXY=$tmp run_buildah run $cid printenv FTP_PROXY
  expect_output "$tmp"
  ftp_proxy=$tmp run_buildah run $cid printenv ftp_proxy
  expect_output "$tmp"
  HTTP_PROXY=$tmp run_buildah run $cid printenv HTTP_PROXY
  expect_output "$tmp"
  https_proxy=$tmp run_buildah run $cid printenv https_proxy
  expect_output "$tmp"
  BOGUS_PROXY=$tmp run_buildah 1 run $cid printenv BOGUS_PROXY
}

@test "from-image-by-id" {
  skip_if_chroot
  skip_if_no_runtime

  _prefetch busybox
  run_buildah from --cidfile ${TEST_SCRATCH_DIR}/cid busybox
  cid=$(< ${TEST_SCRATCH_DIR}/cid)
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  run_buildah copy ${cid} ${TEST_SCRATCH_DIR}/randomfile /
  run_buildah commit --iidfile ${TEST_SCRATCH_DIR}/iid ${cid}
  iid=$(< ${TEST_SCRATCH_DIR}/iid)
  run_buildah from --cidfile ${TEST_SCRATCH_DIR}/cid2 ${iid}
  cid2=$(< ${TEST_SCRATCH_DIR}/cid2)
  run_buildah run ${cid2} cat /etc/hosts
  truncated=${iid##*:}
  truncated="${truncated:0:12}"
  expect_output --substring ${truncated}-working-container
  run_buildah run ${cid2} hostname -f
  expect_output "${cid2:0:12}"
}

@test "from-onbuild-copy-chown-chmod" {
  mkdir -p ${TEST_SCRATCH_DIR}/onbuild-src
  echo test > ${TEST_SCRATCH_DIR}/onbuild-src/file1
  echo test > ${TEST_SCRATCH_DIR}/onbuild-src/file2

  run_buildah from --quiet $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --onbuild "COPY --chown=1:1 file1 /file1" --onbuild "COPY --chmod=0640 file2 /file2" $cid
  run_buildah commit $WITH_POLICY_JSON --format docker $cid onbuild-copy-flags
  run_buildah rm $cid

  pushd ${TEST_SCRATCH_DIR}/onbuild-src > /dev/null
  run_buildah from --quiet --cidfile ${TEST_SCRATCH_DIR}/cid $WITH_POLICY_JSON onbuild-copy-flags
  popd > /dev/null
  cid=$(< ${TEST_SCRATCH_DIR}/cid)

  run_buildah_mount ${cid}
  root=$output
  run stat -c '%u:%g' ${root}/file1
  expect_output "1:1"
  run stat -c '%a' ${root}/file2
  expect_output "640"
  run_buildah_umount ${cid}
}

@test "from-onbuild-add-honors-chown" {
  mkdir -p ${TEST_SCRATCH_DIR}/onbuild-src
  echo test > ${TEST_SCRATCH_DIR}/onbuild-src/file1

  run_buildah from --quiet $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --onbuild "ADD --chown=1:1 file1 /file1" $cid
  run_buildah commit $WITH_POLICY_JSON --format docker $cid onbuild-add-chown
  run_buildah rm $cid

  pushd ${TEST_SCRATCH_DIR}/onbuild-src > /dev/null
  run_buildah from --quiet --cidfile ${TEST_SCRATCH_DIR}/cid $WITH_POLICY_JSON onbuild-add-chown
  popd > /dev/null
  cid=$(< ${TEST_SCRATCH_DIR}/cid)

  run_buildah_mount ${cid}
  root=$output
  test -e ${root}/file1
  run stat -c '%u:%g' ${root}/file1
  expect_output "1:1"
  run_buildah_umount ${cid}
}
