#!/usr/bin/env bats

load helpers

@test "add-flags-order-verification" {
  run_buildah 125 add container1 -q /tmp/container1
  check_options_flag_err "-q"

  run_buildah 125 add container1 --chown /tmp/container1 --quiet
  check_options_flag_err "--chown"

  run_buildah 125 add container1 /tmp/container1 --quiet
  check_options_flag_err "--quiet"
}

@test "add-local-plain" {
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  createrandom ${TEST_SCRATCH_DIR}/other-randomfile

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah_mount $cid
  root=$output
  mkdir $root/subdir $root/other-subdir
  # Copy a file to the working directory
  run_buildah config --workingdir=/ $cid
  run_buildah_umount $cid
  run_buildah add --retry 4 --retry-delay 4s $cid ${TEST_SCRATCH_DIR}/randomfile
  # Copy a file to a specific subdirectory
  run_buildah add $cid ${TEST_SCRATCH_DIR}/randomfile /subdir
  # Copy two files to a specific subdirectory
  run_buildah add $cid ${TEST_SCRATCH_DIR}/randomfile ${TEST_SCRATCH_DIR}/other-randomfile /other-subdir
  # Copy two files to a specific location, which succeeds because we can create it as a directory.
  run_buildah add $cid ${TEST_SCRATCH_DIR}/randomfile ${TEST_SCRATCH_DIR}/other-randomfile /notthereyet-subdir
  # Copy two files to a specific location, which fails because it's not a directory.
  run_buildah 125 add $cid ${TEST_SCRATCH_DIR}/randomfile ${TEST_SCRATCH_DIR}/other-randomfile /randomfile
  # Copy a file to a different working directory
  run_buildah config --workingdir=/cwd $cid
  run_buildah add $cid ${TEST_SCRATCH_DIR}/randomfile
  run_buildah commit $WITH_POLICY_JSON $cid containers-storage:new-image
  run_buildah rm $cid

  run_buildah from $WITH_POLICY_JSON new-image
  newcid=$output
  run_buildah_mount $newcid
  newroot=$output
  test -s $newroot/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $newroot/randomfile
  test -s $newroot/subdir/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $newroot/subdir/randomfile
  test -s $newroot/other-subdir/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $newroot/other-subdir/randomfile
  test -s $newroot/other-subdir/other-randomfile
  cmp ${TEST_SCRATCH_DIR}/other-randomfile $newroot/other-subdir/other-randomfile
  test -d $newroot/cwd
  test -s $newroot/cwd/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $newroot/cwd/randomfile
}

@test "add-local-archive" {
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  createrandom ${TEST_SCRATCH_DIR}/other-randomfile

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output

  dd if=/dev/urandom bs=1024 count=4 of=${TEST_SCRATCH_DIR}/random1
  dd if=/dev/urandom bs=1024 count=4 of=${TEST_SCRATCH_DIR}/random2
  tar -c -C ${TEST_SCRATCH_DIR}    -f ${TEST_SCRATCH_DIR}/tarball1.tar random1 random2
  mkdir ${TEST_SCRATCH_DIR}/tarball2
  dd if=/dev/urandom bs=1024 count=4 of=${TEST_SCRATCH_DIR}/tarball2/tarball2.random1
  dd if=/dev/urandom bs=1024 count=4 of=${TEST_SCRATCH_DIR}/tarball2/tarball2.random2
  tar -c -C ${TEST_SCRATCH_DIR} -z -f ${TEST_SCRATCH_DIR}/tarball2.tar.gz  tarball2
  mkdir ${TEST_SCRATCH_DIR}/tarball3
  dd if=/dev/urandom bs=1024 count=4 of=${TEST_SCRATCH_DIR}/tarball3/tarball3.random1
  dd if=/dev/urandom bs=1024 count=4 of=${TEST_SCRATCH_DIR}/tarball3/tarball3.random2
  tar -c -C ${TEST_SCRATCH_DIR} -j -f ${TEST_SCRATCH_DIR}/tarball3.tar.bz2 tarball3
  mkdir ${TEST_SCRATCH_DIR}/tarball4
  dd if=/dev/urandom bs=1024 count=4 of=${TEST_SCRATCH_DIR}/tarball4/tarball4.random1
  dd if=/dev/urandom bs=1024 count=4 of=${TEST_SCRATCH_DIR}/tarball4/tarball4.random2
  tar -c -C ${TEST_SCRATCH_DIR} -j -f ${TEST_SCRATCH_DIR}/tarball4.tar.bz2 tarball4
  # Add the files to the working directory, which should extract them all.
  run_buildah config --workingdir=/ $cid
  run_buildah add $cid ${TEST_SCRATCH_DIR}/tarball1.tar
  run_buildah add $cid ${TEST_SCRATCH_DIR}/tarball2.tar.gz
  run_buildah add $cid ${TEST_SCRATCH_DIR}/tarball3.tar.bz2
  run_buildah add $cid ${TEST_SCRATCH_DIR}/tarball4.tar.bz2
  run_buildah commit $WITH_POLICY_JSON $cid containers-storage:new-image
  run_buildah rm $cid

  run_buildah from $WITH_POLICY_JSON new-image
  newcid=$output
  run_buildah_mount $newcid
  newroot=$output
  test -s $newroot/random1
  cmp ${TEST_SCRATCH_DIR}/random1 $newroot/random1
  test -s $newroot/random2
  cmp ${TEST_SCRATCH_DIR}/random2 $newroot/random2
  test -s $newroot/tarball2/tarball2.random1
  cmp ${TEST_SCRATCH_DIR}/tarball2/tarball2.random1 $newroot/tarball2/tarball2.random1
  test -s $newroot/tarball2/tarball2.random2
  cmp ${TEST_SCRATCH_DIR}/tarball2/tarball2.random2 $newroot/tarball2/tarball2.random2
  test -s $newroot/tarball3/tarball3.random1
  cmp ${TEST_SCRATCH_DIR}/tarball3/tarball3.random1 $newroot/tarball3/tarball3.random1
  test -s $newroot/tarball3/tarball3.random2
  cmp ${TEST_SCRATCH_DIR}/tarball3/tarball3.random2 $newroot/tarball3/tarball3.random2
  test -s $newroot/tarball4/tarball4.random1
  cmp ${TEST_SCRATCH_DIR}/tarball4/tarball4.random1 $newroot/tarball4/tarball4.random1
  test -s $newroot/tarball4/tarball4.random2
  cmp ${TEST_SCRATCH_DIR}/tarball4/tarball4.random2 $newroot/tarball4/tarball4.random2
}

@test "add single file creates absolute path with correct permissions" {
  _prefetch ubuntu
  imgName=ubuntu-image
  createrandom ${TEST_SCRATCH_DIR}/distutils.cfg
  permission=$(stat -c "%a" ${TEST_SCRATCH_DIR}/distutils.cfg)

  run_buildah from --quiet $WITH_POLICY_JSON ubuntu
  cid=$output
  run_buildah add $cid ${TEST_SCRATCH_DIR}/distutils.cfg /usr/lib/python3.7/distutils
  run_buildah run $cid stat -c "%a" /usr/lib/python3.7/distutils
  expect_output $permission
  run_buildah commit $WITH_POLICY_JSON $cid containers-storage:${imgName}
  run_buildah rm $cid

  run_buildah from --quiet $WITH_POLICY_JSON ${imgName}
  newcid=$output
  run_buildah run $newcid stat -c "%a" /usr/lib/python3.7/distutils
  expect_output $permission
}

@test "add single file creates relative path with correct permissions" {
  _prefetch ubuntu
  imgName=ubuntu-image
  createrandom ${TEST_SCRATCH_DIR}/distutils.cfg
  permission=$(stat -c "%a" ${TEST_SCRATCH_DIR}/distutils.cfg)

  run_buildah from --quiet $WITH_POLICY_JSON ubuntu
  cid=$output
  run_buildah add $cid ${TEST_SCRATCH_DIR}/distutils.cfg lib/custom
  run_buildah run $cid stat -c "%a" lib/custom
  expect_output $permission
  run_buildah commit $WITH_POLICY_JSON $cid containers-storage:${imgName}
  run_buildah rm $cid

  run_buildah from --quiet $WITH_POLICY_JSON ${imgName}
  newcid=$output
  run_buildah run $newcid stat -c "%a" lib/custom
  expect_output $permission
}

@test "add with chown" {
  _prefetch busybox
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah add --chown bin:bin $cid ${TEST_SCRATCH_DIR}/randomfile /tmp/random
  run_buildah run $cid ls -l /tmp/random

  expect_output --substring bin.*bin
}

@test "add with chmod" {
  _prefetch busybox
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output

  run_buildah add --chmod 777 $cid ${TEST_SCRATCH_DIR}/randomfile /tmp/random
  run_buildah run $cid ls -l /tmp/random
  expect_output --substring rwxrwxrwx

  run_buildah add --chmod 0755 $cid ${TEST_SCRATCH_DIR}/randomfile /tmp/random2
  run_buildah run $cid ls -l /tmp/random2
  expect_output --substring rwxr-xr-x

  run_buildah add --chmod u=rX,go= $cid ${TEST_SCRATCH_DIR}/randomfile /tmp/random3
  run_buildah run $cid ls -l /tmp/random3
  expect_output --substring r--------

  run_buildah add --chmod u=rwx,go=u-w $cid ${TEST_SCRATCH_DIR}/randomfile /tmp/random4
  run_buildah run $cid ls -l /tmp/random4
  expect_output --substring rwxr-xr-x

  mkdir -p ${TEST_SCRATCH_DIR}/dir1/dir2
  createrandom ${TEST_SCRATCH_DIR}/dir1/dir2/randomfile
  run_buildah add --chmod u=rwX,go=X $cid ${TEST_SCRATCH_DIR}/dir1 /tmp/dir1
  run_buildah run $cid ls -l /tmp/dir1
  expect_output --substring drwx--x--x
  run_buildah run $cid ls -l /tmp/dir1/dir2
  expect_output --substring rw-------
}

@test "add url" {
  _prefetch busybox
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah add $cid https://github.com/containers/buildah/raw/main/README.md
  run_buildah run $cid ls /README.md

  run_buildah add $cid https://github.com/containers/buildah/raw/main/README.md /home
  run_buildah run $cid ls /home/README.md
}

@test "add relative" {
  # make sure we don't get thrown by relative source locations
  _prefetch busybox
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output

  run_buildah add $cid deny.json /
  run_buildah run $cid ls /deny.json

  run_buildah add $cid ./docker.json /
  run_buildah run $cid ls /docker.json

  run_buildah add $cid tools/Makefile /
  run_buildah run $cid ls /Makefile
}

@test "add --ignorefile" {
  mytest=${TEST_SCRATCH_DIR}/mytest
  mkdir -p ${mytest}
  touch ${mytest}/mystuff
  touch ${mytest}/source.go
  mkdir -p ${mytest}/notmystuff
  touch ${mytest}/notmystuff/notmystuff
  cat > ${mytest}/.ignore << _EOF
*.go
.ignore
notmystuff
_EOF

expect="
stuff
stuff/mystuff"

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output

  run_buildah 125 copy --ignorefile ${mytest}/.ignore $cid ${mytest} /stuff
  expect_output -- "Error: --ignorefile option requires that you specify a context dir using --contextdir" "container file list"

  run_buildah add --contextdir=${mytest} --ignorefile ${mytest}/.ignore $cid ${mytest} /stuff

  run_buildah_mount $cid
  mnt=$output
  run find $mnt -printf "%P\n"
  filelist=$(LC_ALL=C sort <<<"$output")
  run_buildah_umount $cid
  expect_output --from="$filelist" "$expect" "container file list"
}

@test "add quietly" {
  _prefetch busybox
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah add --quiet $cid ${TEST_SCRATCH_DIR}/randomfile /tmp/random
  expect_output ""
  run_buildah_mount $cid
  croot=$output
  cmp ${TEST_SCRATCH_DIR}/randomfile ${croot}/tmp/random
}

@test "add from container" {
  _prefetch busybox
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  from=$output
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah add --quiet $from ${TEST_SCRATCH_DIR}/randomfile /tmp/random
  expect_output ""
  run_buildah add --quiet $WITH_POLICY_JSON --from $from $cid /tmp/random /tmp/random # absolute path
  expect_output ""
  run_buildah add --quiet $WITH_POLICY_JSON --from $from $cid  tmp/random /tmp/random2 # relative path
  expect_output ""
  run_buildah_mount $cid
  croot=$output
  cmp ${TEST_SCRATCH_DIR}/randomfile ${croot}/tmp/random
  cmp ${TEST_SCRATCH_DIR}/randomfile ${croot}/tmp/random2
}

@test "add from image" {
  _prefetch busybox ubuntu
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah add --quiet $WITH_POLICY_JSON --from ubuntu $cid /etc/passwd /tmp/passwd # should pull the image, absolute path
  expect_output ""
  run_buildah add --quiet $WITH_POLICY_JSON --from ubuntu $cid  etc/passwd /tmp/passwd2 # relative path
  expect_output ""
  run_buildah from --quiet $WITH_POLICY_JSON ubuntu
  ubuntu=$output
  run_buildah_mount $cid
  croot=$output
  run_buildah_mount $ubuntu
  ubuntu=$output
  cmp $ubuntu/etc/passwd ${croot}/tmp/passwd
  cmp $ubuntu/etc/passwd ${croot}/tmp/passwd2
}

@test "add url with checksum flag" {
  _prefetch busybox
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah add --checksum=sha256:4fd3aed66b5488b45fe83dd11842c2324fadcc38e1217bb45fbd28d660afdd39 $cid https://raw.githubusercontent.com/containers/buildah/bf3b55ba74102cc2503eccbaeffe011728d46b20/README.md /
  run_buildah run $cid ls /README.md
}

@test "add url with bad checksum" {
  _prefetch busybox
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah 125 add --checksum=sha256:0000000000000000000000000000000000000000000000000000000000000000 $cid https://raw.githubusercontent.com/containers/buildah/bf3b55ba74102cc2503eccbaeffe011728d46b20/README.md /
  expect_output --substring "unexpected response digest for \"https://raw.githubusercontent.com/containers/buildah/bf3b55ba74102cc2503eccbaeffe011728d46b20/README.md\": sha256:4fd3aed66b5488b45fe83dd11842c2324fadcc38e1217bb45fbd28d660afdd39, want sha256:0000000000000000000000000000000000000000000000000000000000000000"
}

@test "add path with checksum flag" {
  _prefetch busybox
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah 125 add --checksum=sha256:0000000000000000000000000000000000000000000000000000000000000000 $cid ${TEST_SCRATCH_DIR}/randomfile /
  expect_output --substring "checksum flag is not supported for local sources"
}

@test "add https retry ca" {
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  mkdir -p ${TEST_SCRATCH_DIR}/private
  starthttpd ${TEST_SCRATCH_DIR} "" ${TEST_SCRATCH_DIR}/localhost.crt ${TEST_SCRATCH_DIR}/private/localhost.key
  run_buildah from --quiet scratch
  cid=$output
  run_buildah add --retry-delay=0.142857s --retry=14 --cert-dir ${TEST_SCRATCH_DIR} $cid https://localhost:${HTTP_SERVER_PORT}/randomfile
  run_buildah add --retry-delay=0.142857s --retry=14 --tls-verify=false $cid https://localhost:${HTTP_SERVER_PORT}/randomfile
  run_buildah 125 add --retry-delay=0.142857s --retry=14 $cid https://localhost:${HTTP_SERVER_PORT}/randomfile
  assert "$output" =~ "x509: certificate signed by unknown authority"
  stophttpd
  run_buildah 125 add --retry-delay=0.142857s --retry=14 --cert-dir ${TEST_SCRATCH_DIR} $cid https://localhost:${HTTP_SERVER_PORT}/randomfile
  assert "$output" =~ "retrying in 142.*ms .*14/14.*"
}

@test "add file with IMA xattr" {
    if ! getfattr -d -n 'security.ima' /usr/libexec/catatonit/catatonit | grep -q ima; then
	skip "catatonit does not have IMA xattr, cannot perform test"
    fi

    run_buildah from --quiet scratch
    cid=$output

    # We do not care if the attribute was actually added, as rootless is allowed to discard it.
    # Only that the add was actually successful.
    run_buildah add $cid /usr/libexec/catatonit/catatonit /catatonit
}

@test "add-with-timestamp" {
  _prefetch busybox
  url=https://raw.githubusercontent.com/containers/buildah/main/tests/bud/from-scratch/Dockerfile
  timestamp=60
  mkdir -p $TEST_SCRATCH_DIR/context
  createrandom $TEST_SCRATCH_DIR/context/randomfile1
  createrandom $TEST_SCRATCH_DIR/context/randomfile2
  run_buildah from -q busybox
  cid="$output"
  # Add the content with more or less contemporary timestamps.
  run_buildah copy "$cid" $TEST_SCRATCH_DIR/context/randomfile* /default
  # Add a second copy that should get the same contemporary timestamps.
  run_buildah copy "$cid" $TEST_SCRATCH_DIR/context/randomfile* /default2
  # Add a third copy that we explicitly force timestamps for.
  run_buildah copy --timestamp=$timestamp "$cid" $TEST_SCRATCH_DIR/context/randomfile* /explicit
  run_buildah add --timestamp=$timestamp "$cid" "$url" /explicit
  # Add a fourth copy that we forced the timestamps for out of band.
  cp -v "${BUDFILES}"/from-scratch/Dockerfile $TEST_SCRATCH_DIR/context/
  tar -cf $TEST_SCRATCH_DIR/tarball -C $TEST_SCRATCH_DIR/context randomfile1 randomfile2 Dockerfile
  touch -d @$timestamp $TEST_SCRATCH_DIR/context/*
  run_buildah copy "$cid" $TEST_SCRATCH_DIR/context/* /touched
  # Add a fifth copy that we forced the timestamps for, from an archive.
  run_buildah add --timestamp=$timestamp "$cid" $TEST_SCRATCH_DIR/tarball /archive
  # Build the script to verify this inside of the rootfs.
  cat > $TEST_SCRATCH_DIR/context/check-dates.sh <<-EOF
  # Okay, at this point, default, default2, explicit, touched, and archive
  # should all contain randomfile1, randomfile2, and Dockerfile.
  # The copies in default and default2 should have contemporary timestamps for
  # the random files, and a server-supplied timestamp or the epoch for the
  # Dockerfile.
  # The copies in explicit, touched, and archive should all have the same
  # very old timestamps.
  touch -d @$timestamp /tmp/reference-file
  for f in /default/* /default2/* ; do
    if test \$f -ot /tmp/reference-file ; then
      echo expected \$f to be newer than /tmp/reference-file, but it was not
      ls -l \$f /tmp/reference-file
      exit 1
    fi
  done
  for f in /explicit/* /touched/* /archive/* ; do
    if test \$f -nt /tmp/reference-file ; then
      echo expected \$f and /tmp/reference-file to have the same datestamp
      ls -l \$f /tmp/reference-file
      exit 1
    fi
    if test \$f -ot /tmp/reference-file ; then
      echo expected \$f and /tmp/reference-file to have the same datestamp
      ls -l \$f /tmp/reference-file
      exit 1
    fi
  done
  exit 0
EOF
  run_buildah copy --chmod=0755 "$cid" $TEST_SCRATCH_DIR/context/check-dates.sh /
  run_buildah run "$cid" sh -x /check-dates.sh
}

@test "add-link-flag" {
  createrandom ${TEST_SCRATCH_DIR}/randomfile
  createrandom ${TEST_SCRATCH_DIR}/other-randomfile

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah_mount $cid
  root=$output
  mkdir $root/subdir
  run_buildah_umount $cid

  run_buildah config --workingdir=/ $cid

  # Test 1: Simple add
  run_buildah add --link $cid ${TEST_SCRATCH_DIR}/randomfile

  # Test 2: Add with rename (file to file with different name)
  run_buildah add --link $cid ${TEST_SCRATCH_DIR}/randomfile /renamed-file

  # Test 3: Multiple files to directory
  run_buildah add --link $cid ${TEST_SCRATCH_DIR}/randomfile ${TEST_SCRATCH_DIR}/other-randomfile /subdir

  run_buildah commit $WITH_POLICY_JSON $cid add-link-image

  run_buildah inspect --type=image add-link-image
  layers=$(echo "$output" | jq -r '.OCIv1.rootfs.diff_ids | length')
  if [ "$layers" -lt 3 ]; then
    echo "Expected at least 3 layers from 3 --link operations, but found $layers"
    echo "Layers found:"
    echo "$output" | jq -r '.OCIv1.rootfs.diff_ids[]'
    exit 1
  fi

  run_buildah from $WITH_POLICY_JSON add-link-image
  newcid=$output
  run_buildah_mount $newcid
  newroot=$output

  test -s $newroot/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $newroot/randomfile

  test -s $newroot/renamed-file
  cmp ${TEST_SCRATCH_DIR}/randomfile $newroot/renamed-file

  test -s $newroot/subdir/randomfile
  cmp ${TEST_SCRATCH_DIR}/randomfile $newroot/subdir/randomfile
  test -s $newroot/subdir/other-randomfile
  cmp ${TEST_SCRATCH_DIR}/other-randomfile $newroot/subdir/other-randomfile
}

@test "add-link-archive" {
  createrandom ${TEST_SCRATCH_DIR}/file1
  createrandom ${TEST_SCRATCH_DIR}/file2

  tar -c -C ${TEST_SCRATCH_DIR} -f ${TEST_SCRATCH_DIR}/archive.tar file1 file2

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output

  run_buildah config --workingdir=/ $cid

  run_buildah add --link $cid ${TEST_SCRATCH_DIR}/archive.tar

  run_buildah add --link $cid ${TEST_SCRATCH_DIR}/archive.tar /destdir/

  run_buildah commit $WITH_POLICY_JSON $cid add-link-archive-image

  run_buildah inspect --type=image add-link-archive-image
  layers=$(echo "$output" | jq -r '.OCIv1.rootfs.diff_ids | length')
  if [ "$layers" -lt 2 ]; then
    echo "Expected at least 2 layers from 2 --link operations, but found $layers"
    exit 1
  fi

  run_buildah from $WITH_POLICY_JSON add-link-archive-image
  newcid=$output
  run_buildah_mount $newcid
  newroot=$output

  test -s $newroot/file1
  cmp ${TEST_SCRATCH_DIR}/file1 $newroot/file1
  test -s $newroot/file2
  cmp ${TEST_SCRATCH_DIR}/file2 $newroot/file2

  test -s $newroot/destdir/file1
  cmp ${TEST_SCRATCH_DIR}/file1 $newroot/destdir/file1
  test -s $newroot/destdir/file2
  cmp ${TEST_SCRATCH_DIR}/file2 $newroot/destdir/file2
}

@test "add-link-directory" {
  mkdir -p ${TEST_SCRATCH_DIR}/testdir/subdir
  createrandom ${TEST_SCRATCH_DIR}/testdir/file1
  createrandom ${TEST_SCRATCH_DIR}/testdir/subdir/file2

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output

  run_buildah config --workingdir=/ $cid

  run_buildah add --link $cid ${TEST_SCRATCH_DIR}/testdir /testdir

  run_buildah commit $WITH_POLICY_JSON $cid add-link-dir-image

  run_buildah from $WITH_POLICY_JSON add-link-dir-image
  newcid=$output
  run_buildah_mount $newcid
  newroot=$output

  test -d $newroot/testdir
  test -s $newroot/testdir/file1
  test -s $newroot/testdir/subdir/file2

  cmp ${TEST_SCRATCH_DIR}/testdir/file1 $newroot/testdir/file1
  cmp ${TEST_SCRATCH_DIR}/testdir/subdir/file2 $newroot/testdir/subdir/file2
}

@test "add-allow-wildcard" {
  createrandom ${TEST_SCRATCH_DIR}/file-a
  createrandom ${TEST_SCRATCH_DIR}/file-b

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --workingdir / $cid

  # Default (allow-wildcard=true): glob expands, adds both files
  run_buildah add $cid "${TEST_SCRATCH_DIR}/file-*" /dest/
  run_buildah_mount $cid
  root=$output
  test -f $root/dest/file-a
  test -f $root/dest/file-b
  cmp ${TEST_SCRATCH_DIR}/file-a $root/dest/file-a
  cmp ${TEST_SCRATCH_DIR}/file-b $root/dest/file-b
  run_buildah_umount $cid

  # allow-wildcard=false: glob patterns are rejected
  run_buildah 125 add --allow-wildcard=false $cid "${TEST_SCRATCH_DIR}/file-*" /dest2/
  expect_output --substring "wildcards are not allowed"
}

@test "add-allow-empty-wildcard" {
  createrandom ${TEST_SCRATCH_DIR}/file-a

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --workingdir / $cid

  # Default (allow-empty-wildcard=false): non-matching glob should fail
  run_buildah 125 add $cid "${TEST_SCRATCH_DIR}/nonexistent-*" /dest/
  expect_output --substring "matched nothing"

  # Explicit true: non-matching glob should succeed
  run_buildah add --allow-empty-wildcard=true $cid "${TEST_SCRATCH_DIR}/nonexistent-*" /dest2/
  run_buildah_mount $cid
  root=$output
  test ! -d $root/dest2
  run_buildah_umount $cid

  # Existing files still add normally
  run_buildah add $cid ${TEST_SCRATCH_DIR}/file-a /dest3/
  run_buildah_mount $cid
  root=$output
  test -f $root/dest3/file-a
  run_buildah_umount $cid

  # Literal non-existent file should still error even with allow-empty-wildcard=true
  run_buildah 125 add --allow-empty-wildcard=true $cid ${TEST_SCRATCH_DIR}/no-such-file /dest4/
  expect_output --substring "no such file or directory"
}

@test "add-symlink-root-follow-default" {
  createrandom ${TEST_SCRATCH_DIR}/file
  ln -s ./file ${TEST_SCRATCH_DIR}/symlink

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output

  run_buildah add $cid ${TEST_SCRATCH_DIR}/symlink /dest

  run_buildah_mount $cid
  root=$output
  ls -lahR $root
  cmp ${TEST_SCRATCH_DIR}/file $root/dest
  test -f $root/dest
}

@test "add-symlink-root-no-follow" {
  createrandom ${TEST_SCRATCH_DIR}/file
  ln -s ./file ${TEST_SCRATCH_DIR}/symlink

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output

  # The symlink needs to point to something existing
  run_buildah add --no-follow-symlinks $cid ${TEST_SCRATCH_DIR}/file /file
  run_buildah add --no-follow-symlinks $cid ${TEST_SCRATCH_DIR}/symlink /dest

  run_buildah_mount $cid
  root=$output
  ls -lahR $root
  cmp ${TEST_SCRATCH_DIR}/file $root/dest
  test -L $root/dest
  test "$(readlink $root/dest)" = "./file"
}

@test "add-symlink-child-follow-default" {
  mkdir ${TEST_SCRATCH_DIR}/src
  createrandom ${TEST_SCRATCH_DIR}/src/file
  ln -s ./file ${TEST_SCRATCH_DIR}/src/symlink

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output

  run_buildah add $cid ${TEST_SCRATCH_DIR}/src /dest

  run_buildah_mount $cid
  root=$output
  ls -lahR $root
  cmp ${TEST_SCRATCH_DIR}/src/file $root/dest/symlink
  test -f $root/dest/symlink
}

@test "add-symlink-child-no-follow" {
  mkdir ${TEST_SCRATCH_DIR}/src
  createrandom ${TEST_SCRATCH_DIR}/src/file
  ln -s ./file ${TEST_SCRATCH_DIR}/src/symlink

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output

  run_buildah add --no-follow-symlinks $cid ${TEST_SCRATCH_DIR}/src /dest

  run_buildah_mount $cid
  root=$output
  ls -lahR $root
  test -L $root/dest/symlink
  test "$(readlink $root/dest/symlink)" = "./file"
}

@test "add-from-image" {
  _prefetch busybox
  run_buildah from --quiet $WITH_POLICY_JSON busybox
  cid=$output
  run_buildah add $WITH_POLICY_JSON --quiet --from ubuntu $cid /etc/passwd /tmp/passwd # should pull the image, absolute path
  expect_output ""
  run_buildah add --quiet $WITH_POLICY_JSON --from ubuntu $cid  etc/passwd /tmp/passwd2 # relative path
  expect_output ""
  run_buildah from --quiet $WITH_POLICY_JSON ubuntu
  ubuntu=$output
  run_buildah_mount $cid
  croot=$output
  run_buildah_mount $ubuntu
  ubuntu=$output
  cmp $ubuntu/etc/passwd ${croot}/tmp/passwd
  cmp $ubuntu/etc/passwd ${croot}/tmp/passwd2
}

@test "add --include" {
  mytest=${TEST_SCRATCH_DIR}/mytest
  mkdir -p ${mytest}/subdir
  touch ${mytest}/source.go
  touch ${mytest}/readme.md
  touch ${mytest}/subdir/nested.go
  touch ${mytest}/subdir/nested.md

expect="
stuff
stuff/source.go
stuff/subdir
stuff/subdir/nested.go"

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah add --include="**/*.go" $cid ${mytest} /stuff

  run_buildah_mount $cid
  mnt=$output
  run find $mnt -printf "%P\n"
  filelist=$(LC_ALL=C sort <<<"$output")
  run_buildah_umount $cid
  expect_output --from="$filelist" "$expect" "add recursive include"

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah add --include="**/*.go" $cid ${mytest}/source.go /stuff2/

  run_buildah_mount $cid
  mnt=$output
  run find $mnt -printf "%P\n"
  filelist=$(LC_ALL=C sort <<<"$output")
  run_buildah_umount $cid
  expect_output --from="$filelist" --substring "source.go" "add include single file"

  # include + exclude: exclude wins when both match
expect="
stuff
stuff/source.go
stuff/subdir"

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah add --include="**/*.go" --exclude="**/nested.go" $cid ${mytest} /stuff
  run_buildah_mount $cid
  mnt=$output
  run find $mnt -printf "%P\n"
  filelist=$(LC_ALL=C sort <<<"$output")
  run_buildah_umount $cid
  expect_output --from="$filelist" "$expect" "include with exclude"

  # negation pattern listed after the include it carves out: match is excluded
expect="
stuff
stuff/source.go
stuff/subdir"

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah add --include="**/*.go" --include="!**/nested.go" $cid ${mytest} /stuff
  run_buildah_mount $cid
  mnt=$output
  run find $mnt -printf "%P\n"
  filelist=$(LC_ALL=C sort <<<"$output")
  run_buildah_umount $cid
  expect_output --from="$filelist" "$expect" "negation after positive excludes the match"

  # same patterns, reversed order: the later positive pattern overrides the negation
expect="
stuff
stuff/source.go
stuff/subdir
stuff/subdir/nested.go"

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah add --include="!**/nested.go" --include="**/*.go" $cid ${mytest} /stuff
  run_buildah_mount $cid
  mnt=$output
  run find $mnt -printf "%P\n"
  filelist=$(LC_ALL=C sort <<<"$output")
  run_buildah_umount $cid
  expect_output --from="$filelist" "$expect" "later positive pattern overrides earlier negation"
}
