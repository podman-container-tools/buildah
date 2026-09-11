#!/usr/bin/env bats

load helpers

@test "chroot mount flags" {
  skip_if_no_unshare
  if ! test -e /etc/subuid ; then
    skip "we can't bind mount over /etc/subuid during the test if there is no /etc/subuid file"
  fi
  if ! test -e /etc/subgid ; then
    skip "we can't bind mount over /etc/subgid during the test if there is no /etc/subgid file"
  fi
  # whom should we map to root in a nested namespace?
  if is_rootless ; then
    subid=128
    rangesize=1024
  else
    subid=1048576
    rangesize=16384
  fi
  # we're going to have to prefetch into storage used by someone else image
  # chosen because its rootfs doesn't have any uid/gid ownership above
  # $rangesize, because the nested namespace needs to be able to represent all
  # of them
  baseimage=registry.access.redhat.com/ubi10:latest
  _prefetch $baseimage
  baseimagef=$(tr -c a-zA-Z0-9.- - <<< "$baseimage")
  # create the directories that we need
  tmpfs=${TEST_SCRATCH_DIR}/tmpfs
  mkdir $tmpfs
  context=${TEST_SCRATCH_DIR}/context
  mkdir $context
  storagedir=${TEST_SCRATCH_DIR}/storage
  mkdir $storagedir
  rootdir=${storagedir}/rootdir
  mkdir $rootdir
  runrootdir=${storagedir}/runrootdir
  mkdir $runrootdir
  xdgruntimedir=${storagedir}/xdgruntime
  mkdir $xdgruntimedir
  xdgconfighome=${storagedir}/xdgconfighome
  mkdir $xdgconfighome
  xdgdatahome=${storagedir}/xdgdatahome
  mkdir $xdgdatahome
  storageopts="--storage-driver vfs --root $rootdir --runroot $runrootdir"
  # our temporary parent directory might not be world-searchable, which will
  # cause someone in the nested user namespace to hit permissions issues even
  # looking for $storagedir, so tweak perms to let them do at least that much
  fixupdir=$storagedir
  while test $(stat -c %d:%i $fixupdir) != $(stat -c %d:%i /) ; do
    # walk up to root, or the first parent that we don't own
    if test $(stat -c %u $fixupdir) -ne $(id -u) ; then
      break
    fi
    chmod +x $fixupdir
    fixupdir=$fixupdir/..
  done
  # start writing the script to run in the nested user namespace
  cp -v ${TEST_SOURCES}/containers.conf ${TEST_SCRATCH_DIR}/containers.conf
  chmod ugo+r ${TEST_SCRATCH_DIR}/containers.conf
  echo set -e > ${TEST_SCRATCH_DIR}/script.sh
  echo export XDG_RUNTIME_DIR=$xdgruntimedir >> ${TEST_SCRATCH_DIR}/script.sh
  echo export XDG_CONFIG_HOME=$xdgconfighome >> ${TEST_SCRATCH_DIR}/script.sh
  echo export XDG_DATA_HOME=$xdgdatahome >> ${TEST_SCRATCH_DIR}/script.sh
  echo export CONTAINERS_CONF=${TEST_SCRATCH_DIR}/containers.conf >> ${TEST_SCRATCH_DIR}/script.sh
  echo export CONTAINERS_STORAGE_CONF=/dev/null >> ${TEST_SCRATCH_DIR}/script.sh
  # give our would-be user ownership of that directory
  echo chown --recursive ${subid}:${subid} ${storagedir} >> ${TEST_SCRATCH_DIR}/script.sh
  # make newuidmap/newgidmap, invoked by unshare even for uid=0, happy
  echo root:0:4294967295 > ${TEST_SCRATCH_DIR}/subid
  echo mount --bind -r ${TEST_SCRATCH_DIR}/subid /etc/subuid >> ${TEST_SCRATCH_DIR}/script.sh
  echo mount --bind -r ${TEST_SCRATCH_DIR}/subid /etc/subgid >> ${TEST_SCRATCH_DIR}/script.sh
  # don't get tripped up by ${TEST_SCRATCH_DIR} potentially being on a filesystem with non-default mount flags
  echo mount -t tmpfs -o size=256K tmpfs $tmpfs >> ${TEST_SCRATCH_DIR}/script.sh
  # mount a small tmpfs with every mount flag combination that concerns us, and
  # be ready to tell buildah to mount everything conservatively, to mirror the
  # TransientMounts API being used to nodev/noexec/nosuid/ro bind in a source
  # that doesn't necessarily have those flags already set on it
  for d in dev nodev ; do
    for e in exec noexec ; do
      for s in suid nosuid ; do
        for r in ro rw ; do
          subdir=$tmpfs/d-$d-$e-$s-$r
          echo mkdir ${subdir} >> ${TEST_SCRATCH_DIR}/script.sh
          echo mount -t tmpfs -o size=256K,$d,$e,$s,$r tmpfs ${subdir} >> ${TEST_SCRATCH_DIR}/script.sh
          mounts="${mounts:+${mounts} }--volume ${subdir}:/mounts/d-$d-$e-$s-$r:nodev,noexec,nosuid,ro"
        done
      done
    done
  done
  # copy binaries to a location where parent directory permissions are less
  # likely to interfere with running them from a different UID
  cp ${COPY_BINARY} ${TEST_SCRATCH_DIR}/copy
  cp ${BUILDAH_BINARY} ${TEST_SCRATCH_DIR}/buildah
  # make sure that RUN doesn't just break when we try to use volume mounts with
  # flags set that we're not allowed to modify
  echo FROM $baseimage > $context/Dockerfile
  echo RUN cat /proc/mounts >> $context/Dockerfile
  # copy in the prefetched image
  # unshare from util-linux 2.39 also accepts INNER:OUTER:SIZE for --map-users
  # and --map-groups, but fedora 37's is too old, so the older OUTER,INNER,SIZE
  # (using commas instead of colons as field separators) will have to do
  echo "env | sort" >> ${TEST_SCRATCH_DIR}/script.sh
  echo "env _CONTAINERS_USERNS_CONFIGURED=done unshare -Umpf --mount-proc --setuid 0 --setgid 0 --map-users=${subid},0,${rangesize} --map-groups=${subid},0,${rangesize} ${TEST_SCRATCH_DIR}/copy $WITH_POLICY_JSON ${storageopts} dir:$_BUILDAH_IMAGE_CACHEDIR/$baseimagef containers-storage:$baseimage" >> ${TEST_SCRATCH_DIR}/script.sh
  # try to do a build with all of the volume mounts
  echo "env _CONTAINERS_USERNS_CONFIGURED=done unshare -Umpf --mount-proc --setuid 0 --setgid 0 --map-users=${subid},0,${rangesize} --map-groups=${subid},0,${rangesize} ${TEST_SCRATCH_DIR}/buildah ${BUILDAH_REGISTRY_OPTS} ${storageopts} build --isolation chroot --pull=never $mounts $context" >> ${TEST_SCRATCH_DIR}/script.sh
  # run that whole script in a nested mount namespace with no $XDG_...
  # variables leaked into it
  if is_rootless ; then
    run_buildah unshare env -i bash -x ${TEST_SCRATCH_DIR}/script.sh
  else
    unshare -mpf --mount-proc env -i bash -x ${TEST_SCRATCH_DIR}/script.sh
  fi
}

@test "chroot with overlay root" {
  if test `uname` != Linux ; then
    skip "not meaningful except on Linux"
  fi
  skip_if_no_unshare
  if [ "$(id -u)" -ne 0 ]; then
    skip "expects to already be root"
  fi
  _prefetch docker.io/library/busybox
  cp -v ${TEST_SOURCES}/containers.conf ${TEST_SCRATCH_DIR}/containers.conf
  chmod ugo+r ${TEST_SCRATCH_DIR}/containers.conf
  cp -v ${TEST_SOURCES}/policy.json ${TEST_SCRATCH_DIR}/policy.json
  chmod ugo+r ${TEST_SCRATCH_DIR}/policy.json
  mkdir -p ${TEST_SCRATCH_DIR}/chroot
  ${COPY_BINARY} containers-storage:[${STORAGE_DRIVER}@${TEST_SCRATCH_DIR}/root+${TEST_SCRATCH_DIR}/runroot]docker.io/library/busybox:latest dir:${TEST_SCRATCH_DIR}/base-image
  chown -R 1:1 ${TEST_SCRATCH_DIR}/root ${TEST_SCRATCH_DIR}/runroot ${TEST_SCRATCH_DIR}/chroot
  if test ${STORAGE_DRIVER} = overlay ; then
    if test -x /usr/bin/fuse-overlayfs ; then
      local storage_opts="overlay.mount_program=/usr/bin/fuse-overlayfs"
    else
      skip "trying to use overlay on top of overlay, but fuse-overlayfs is not present"
    fi
  fi
  # a script that runs inside of a new mount namespace and mounts the current
  # rootfs as the "lower" for an overlay, then pivots into it
  cat > ${TEST_SCRATCH_DIR}/script1 <<- EOF
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin${PATH:+:$PATH}
  set -e
  set -x
  if test \$(stat -f -c %T "${TEST_SCRATCH_DIR}/chroot") = overlayfs ; then
    mount -t tmpfs -o size=16M none ${TEST_SCRATCH_DIR}/chroot
  fi
  mkdir -p ${TEST_SCRATCH_DIR}/chroot/workdir
  mkdir -p ${TEST_SCRATCH_DIR}/chroot/upperdir
  mkdir -p ${TEST_SCRATCH_DIR}/chroot/merged
  mount -t overlay overlay -o upperdir=${TEST_SCRATCH_DIR}/chroot/upperdir,workdir=${TEST_SCRATCH_DIR}/chroot/workdir,lowerdir=/ ${TEST_SCRATCH_DIR}/chroot/merged
  mount -t proc proc ${TEST_SCRATCH_DIR}/chroot/merged/proc
  mount -t sysfs sysfs ${TEST_SCRATCH_DIR}/chroot/merged/sys
  mount --bind /dev ${TEST_SCRATCH_DIR}/chroot/merged/dev
  mount --bind /etc ${TEST_SCRATCH_DIR}/chroot/merged/etc
  echo build > ${TEST_SCRATCH_DIR}/chroot/hostname
  chmod 644 ${TEST_SCRATCH_DIR}/chroot/hostname
  mount --bind ${TEST_SCRATCH_DIR}/chroot/hostname ${TEST_SCRATCH_DIR}/chroot/merged/etc/hostname
  touch ${TEST_SCRATCH_DIR}/chroot/hosts
  chmod 644 ${TEST_SCRATCH_DIR}/chroot/hosts
  mount --bind ${TEST_SCRATCH_DIR}/chroot/hosts ${TEST_SCRATCH_DIR}/chroot/merged/etc/hosts
  touch ${TEST_SCRATCH_DIR}/chroot/resolv.conf
  chmod 644 ${TEST_SCRATCH_DIR}/chroot/resolv.conf
  mount --bind ${TEST_SCRATCH_DIR}/chroot/resolv.conf ${TEST_SCRATCH_DIR}/chroot/merged/etc/resolv.conf
  mount --bind /tmp ${TEST_SCRATCH_DIR}/chroot/merged/tmp
  mkdir -p ${TEST_SCRATCH_DIR}/chroot/merged/var/tmp
  chmod 1777 ${TEST_SCRATCH_DIR}/chroot/merged/var/tmp
  if test -d /var/tmp; then
    mount --bind /var/tmp ${TEST_SCRATCH_DIR}/chroot/merged/var/tmp
  fi
  mkdir -p ${TEST_SCRATCH_DIR}/chroot/merged/run
  mount -t tmpfs -o size=1024k none ${TEST_SCRATCH_DIR}/chroot/merged/run
  chmod 755 ${TEST_SCRATCH_DIR}/chroot/merged/run
  mkdir -p ${TEST_SCRATCH_DIR}/chroot/merged/run/containers/storage
  chmod 755 ${TEST_SCRATCH_DIR}/chroot/merged/run/containers/storage
  mkdir -p ${TEST_SCRATCH_DIR}/chroot/merged/var/lib/containers/storage
  chmod 755 ${TEST_SCRATCH_DIR}/chroot/merged/var/lib/containers/storage
  # https://github.com/podman-container-tools/buildah/issues/6967
  # chown -R is not safe against concurrent removal, it will exit 1 when
  # it happens but still walks all files so we can ignore the error here.
  # Bug: https://bugs.gnu.org/81444
  # Only once the fix landed in our test distro images coreutils version this workaround can be removed.
  # Because this is a flake as simple remove and test is not enough to confirm. Please actually verify
  # it using the reproducer on the issue to ensure the flake is not added back.
  chown -R 1:1 ${TEST_SCRATCH_DIR}/chroot/merged/run ${TEST_SCRATCH_DIR}/chroot/merged/var/lib/containers || true
  mount --bind ${TEST_SCRATCH_DIR} ${TEST_SCRATCH_DIR}/chroot/merged/${TEST_SCRATCH_DIR}
  mkdir -p ${TEST_SCRATCH_DIR}/chroot/merged/usr/local/bin
  chmod 755 ${TEST_SCRATCH_DIR}/chroot/merged/usr/local/bin
  touch ${TEST_SCRATCH_DIR}/chroot/merged/usr/local/bin/buildah
  mount --bind ${BUILDAH_BINARY:-$TEST_SOURCES/../bin/buildah} ${TEST_SCRATCH_DIR}/chroot/merged/usr/local/bin/buildah
  cd ${TEST_SCRATCH_DIR}/chroot/merged
  ${COPY_BINARY} --root ${TEST_SCRATCH_DIR}/root --runroot ${TEST_SCRATCH_DIR}/runroot --storage-driver ${STORAGE_DRIVER} ${storage_opts:+--storage-opt ${storage_opts}} dir:${TEST_SCRATCH_DIR}/base-image dir:${TEST_SCRATCH_DIR}/chroot/merged/base-image
  pivot_root . tmp
  mount --make-rslave tmp
  umount -f -l tmp
  mount -o remount --make-rshared /
  grep ' / / ' /proc/self/mountinfo
  # unshare from util-linux 2.39 also accepts INNER:OUTER:SIZE for --map-users
  # and --map-groups, but fedora 37's is too old, so the older OUTER,INNER,SIZE
  # (using commas instead of colons as field separators) will have to do
  unshare --setuid 0 --setgid 0 --map-users=1,0,1024 --map-users=1025,65534,2 --map-groups=1,0,1024 --map-groups=1025,65534,2 -UinCfpm bash ${TEST_SCRATCH_DIR}/script2
EOF
  # a script that runs inside of a new user namespace with an unprivileged ID
  # mapped to root, which is expected to be able to run, with the proper
  # configuration options, on top of that overlay filesystem
  cat > ${TEST_SCRATCH_DIR}/script2 <<- EOF
  set -e
  set -x
  export _CONTAINERS_USERNS_CONFIGURED=done
  export CONTAINERS_CONF=${TEST_SCRATCH_DIR}/containers.conf
  export CONTAINERS_STORAGE_CONF=/dev/null # needed to avoid file lookup permission errors under /root/...
  cat /proc/self/uid_map
  cat /proc/self/gid_map
  mount --make-shared /
  /usr/local/bin/buildah ${BUILDAH_REGISTRY_OPTS} --root /var/lib/containers/storage --runroot /run/containers/storage --storage-driver ${STORAGE_DRIVER} ${storage_opts:+--storage-opt ${storage_opts}} pull --signature-policy ${TEST_SCRATCH_DIR}/policy.json dir:/base-image
  baseID=\$(jq -r .config.digest /base-image/manifest.json)
  /usr/local/bin/buildah ${BUILDAH_REGISTRY_OPTS} --root /var/lib/containers/storage --runroot /run/containers/storage --storage-driver ${STORAGE_DRIVER} ${storage_opts:+--storage-opt ${storage_opts}} tag \${baseID} docker.io/library/busybox
  /usr/local/bin/buildah ${BUILDAH_REGISTRY_OPTS} --root /var/lib/containers/storage --runroot /run/containers/storage --storage-driver ${STORAGE_DRIVER} ${storage_opts:+--storage-opt ${storage_opts}} from --signature-policy ${TEST_SCRATCH_DIR}/policy.json --name ctrid --pull=never --quiet docker.io/library/busybox
  /usr/local/bin/buildah ${BUILDAH_REGISTRY_OPTS} --root /var/lib/containers/storage --runroot /run/containers/storage --storage-driver ${STORAGE_DRIVER} ${storage_opts:+--storage-opt ${storage_opts}} run --isolation=chroot ctrid pwd
EOF
  chmod +x ${TEST_SCRATCH_DIR}
  chmod +rx ${TEST_SCRATCH_DIR}/script1 ${TEST_SCRATCH_DIR}/script2
  env -i unshare -inCfpm bash ${TEST_SCRATCH_DIR}/script1
}

@test "chroot RUN succeeds when setgroups is denied" {
  skip_if_no_unshare
  if test `uname` != Linux ; then
    skip "not meaningful except on Linux"
  fi

  baseimage=registry.access.redhat.com/ubi10:latest
  _prefetch $baseimage
  baseimagef=$(tr -c a-zA-Z0-9.- - <<< "$baseimage")

  storagedir=${TEST_SCRATCH_DIR}/storage
  mkdir $storagedir
  rootdir=${storagedir}/rootdir
  mkdir $rootdir
  runrootdir=${storagedir}/runrootdir
  mkdir $runrootdir
  # ignore_chown_errors lets the base image extract in the single-ID namespace,
  # where high gids in the rootfs can't be represented; unrelated to the bug.
  storageopts="--storage-driver vfs --storage-opt vfs.ignore_chown_errors=true --root $rootdir --runroot $runrootdir"

  context=${TEST_SCRATCH_DIR}/context ; mkdir $context
  echo "FROM $baseimage" > $context/Dockerfile
  echo "RUN echo regression-marker-ran" >> $context/Dockerfile

  cp ${COPY_BINARY} ${TEST_SCRATCH_DIR}/copy
  cp ${BUILDAH_BINARY} ${TEST_SCRATCH_DIR}/buildah

  echo set -e > ${TEST_SCRATCH_DIR}/script.sh
  echo "mount -t tmpfs tmpfs /run/lock" >> ${TEST_SCRATCH_DIR}/script.sh
  # confirm we really are in a setgroups-denied namespace -- the bug's precondition
  echo "test \"\$(cat /proc/self/setgroups)\" = deny" >> ${TEST_SCRATCH_DIR}/script.sh
  # seed the store from the prefetched cache so the build needs no network
  echo "${TEST_SCRATCH_DIR}/copy $WITH_POLICY_JSON ${storageopts} dir:\$_BUILDAH_IMAGE_CACHEDIR/$baseimagef containers-storage:$baseimage" >> ${TEST_SCRATCH_DIR}/script.sh
  # the actual regression: this build used to fail at the RUN step with EPERM
  echo "${TEST_SCRATCH_DIR}/buildah ${BUILDAH_REGISTRY_OPTS} ${storageopts} build --isolation chroot --pull=never $context" >> ${TEST_SCRATCH_DIR}/script.sh

  run unshare --user --map-root-user --mount bash ${TEST_SCRATCH_DIR}/script.sh
  echo "$output"
  assert "$status" -eq 0
  expect_output --substring "regression-marker-ran"
}

@test "chroot run denies group access to a member when group bits are restrictive" {
  skip_if_no_unshare
  if test `uname` != Linux ; then
    skip "not meaningful except on Linux"
  fi
  if ! test -e /etc/subgid ; then
    skip "no /etc/subgid allocation to set up the probe"
  fi
  if ! test -e /etc/subuid ; then
    skip "no /etc/subuid allocation to set up the probe"
  fi

  baseimage=registry.access.redhat.com/ubi10:latest
  _prefetch $baseimage
  baseimagef=$(tr -c a-zA-Z0-9.- - <<< "$baseimage")

  storagedir=${TEST_SCRATCH_DIR}/storage
  mkdir $storagedir
  rootdir=${storagedir}/rootdir
  mkdir $rootdir
  runrootdir=${storagedir}/runrootdir
  mkdir $runrootdir
  storageopts="--storage-driver vfs --storage-opt vfs.ignore_chown_errors=true --root $rootdir --runroot $runrootdir"

  probe=${TEST_SCRATCH_DIR}/probe
  mkdir $probe

  subgidline=$(grep "^$(id -un):" /etc/subgid | head -1)
  substart=$(echo "$subgidline" | cut -d: -f2)
  subsize=$(echo "$subgidline" | cut -d: -f3)
  if test -z "$substart" || test -z "$subsize" ; then
    skip "no subgid range for $(id -un) to set up the probe"
  fi

  subuidline=$(grep "^$(id -un):" /etc/subuid | head -1)
  subuidstart=$(echo "$subuidline" | cut -d: -f2)
  subuidsize=$(echo "$subuidline" | cut -d: -f3)
  if test -z "$subuidstart" || test -z "$subuidsize" ; then
    skip "no subuid range for $(id -un) to set up the probe"
  fi

  # The namespaces below map inner gid 0 to our real egid, and inner gids
  # 1..${subsize} to our subordinate gid range.  The innermost namespace, where
  # the build runs, maps only our real uid/gid to 0, so:
  #   okgid   -- inner 0 -> our real egid, which is also the RUN process's
  #              primary gid, so the RUN process is a member of it.
  #   notmine -- an inner gid from the subordinate range, which the build's
  #              namespace does not map and the RUN process does not hold.
  okgid=0
  notmine=4242
  if test "$notmine" -lt 1 || test "$notmine" -gt "$subsize" ; then
    skip "gid $notmine is outside the mappable subordinate range 1..$subsize"
  fi

  # member_denied: group okgid, mode 0004 (owner ---, group ---, other r).  The
  # RUN process is a member of the group, so the group bits must deny it even
  # though "other" would allow.  This checks in_group_p()'s fsgid path.
  echo secret_member > $probe/member_denied
  # nonmember_allowed: group notmine, mode 0004.  The RUN process is not a
  # member, so it falls through to "other" (r) and is allowed.  Control for the
  # above: if both were denied, member_denied would prove nothing.
  echo ok_nonmember > $probe/nonmember_allowed

  # Set ownership and bits from a namespace that maps our subordinate ranges, so
  # no sudo is needed.  Files are chown'ed to inner uid 1 (our subordinate
  # range) so the RUN process is not their owner and cannot use CAP_DAC_OVERRIDE
  # on them -- capable_wrt_inode_uidgid() requires the inode's uid to be mapped,
  # and inner uid 1 is not mapped in the build's namespace.
  unshare --user --mount \
    --map-users=$(id -u),0,1 --map-users=${subuidstart},1,1 \
    --map-groups=$(id -g),0,1 --map-groups=${substart},1,${subsize} \
    sh -c "
    set -e
    chown 1:$okgid   $probe/member_denied     && chmod 0004 $probe/member_denied
    chown 1:$notmine $probe/nonmember_allowed && chmod 0004 $probe/nonmember_allowed
  " || skip "user namespace could not map the uid/gids for the probe"

  cp ${COPY_BINARY} ${TEST_SCRATCH_DIR}/copy
  cp ${BUILDAH_BINARY} ${TEST_SCRATCH_DIR}/buildah

  echo set -e > ${TEST_SCRATCH_DIR}/script.sh
  echo "mount -t tmpfs tmpfs /run/lock" >> ${TEST_SCRATCH_DIR}/script.sh
  # setgroups-denied single-ID namespace -- same environment as #6947
  echo "test \"\$(cat /proc/self/setgroups)\" = deny" >> ${TEST_SCRATCH_DIR}/script.sh
  echo "${TEST_SCRATCH_DIR}/copy $WITH_POLICY_JSON ${storageopts} dir:\$_BUILDAH_IMAGE_CACHEDIR/$baseimagef containers-storage:$baseimage" >> ${TEST_SCRATCH_DIR}/script.sh
  echo "ctr=\$(${TEST_SCRATCH_DIR}/buildah ${BUILDAH_REGISTRY_OPTS} ${storageopts} from --pull=never -q $baseimage)" >> ${TEST_SCRATCH_DIR}/script.sh
  echo "${TEST_SCRATCH_DIR}/buildah ${BUILDAH_REGISTRY_OPTS} ${storageopts} run --isolation chroot -v $probe:/probe:z \"\$ctr\" -- sh -c 'echo setgroups:\$(cat /proc/self/setgroups); stat -c \"member_denied=%u:%g:%a\" /probe/member_denied; stat -c \"nonmember_allowed=%u:%g:%a\" /probe/nonmember_allowed; grep ^Gid: /proc/self/status; cat /probe/nonmember_allowed && ! cat /probe/member_denied'" >> ${TEST_SCRATCH_DIR}/script.sh

  # Two namespaces, mirroring the reported environment: the outer one is mapped
  # via newuidmap/newgidmap, which leaves setgroups() permitted, so we can drop
  # our supplemental groups the way the container runtime would have; the inner
  # one is the single-ID namespace, where the kernel requires setgroups to be
  # denied before it will accept the mappings.
  run unshare --user --mount \
    --map-users=$(id -u),0,1 --map-users=${subuidstart},1,${subuidsize} \
    --map-groups=$(id -g),0,1 --map-groups=${substart},1,${subsize} \
    sh -c "setpriv --groups 0 -- unshare --user --map-root-user --mount bash ${TEST_SCRATCH_DIR}/script.sh"
  echo "$output"
  assert "$status" -eq 0
  # the probes have to run inside buildah run, in the denied namespace
  expect_output --substring "setgroups:deny"
  # both files must be owned by an unmapped uid (65534), so neither the owner
  # bits nor CAP_DAC_OVERRIDE apply, and both must still be mode 0004
  expect_output --substring "member_denied=65534:${okgid}:4"
  expect_output --substring "nonmember_allowed=65534:65534:4"
  # okgid must be the RUN process's primary gid, otherwise member_denied is not
  # exercising group-bit enforcement against a member
  expect_output --substring "Gid:	${okgid}"
  expect_output --substring "ok_nonmember"
  assert "$output" !~ "secret_member"
}
