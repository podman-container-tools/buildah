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
  chown -R 1:1 ${TEST_SCRATCH_DIR}/chroot/merged/run ${TEST_SCRATCH_DIR}/chroot/merged/var/lib/containers
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
  cat /proc/self/uid_map
  cat /proc/self/gid_map
  mount --make-shared /
  /usr/local/bin/buildah ${BUILDAH_REGISTRY_OPTS} --root /var/lib/containers/storage --runroot /run/containers/storage --storage-driver ${STORAGE_DRIVER} ${storage_opts:+--storage-opt ${storage_opts}} pull dir:/base-image
  baseID=\$(jq -r .config.digest /base-image/manifest.json)
  /usr/local/bin/buildah ${BUILDAH_REGISTRY_OPTS} --root /var/lib/containers/storage --runroot /run/containers/storage --storage-driver ${STORAGE_DRIVER} ${storage_opts:+--storage-opt ${storage_opts}} tag \${baseID} docker.io/library/busybox
  /usr/local/bin/buildah ${BUILDAH_REGISTRY_OPTS} --root /var/lib/containers/storage --runroot /run/containers/storage --storage-driver ${STORAGE_DRIVER} ${storage_opts:+--storage-opt ${storage_opts}} from --name ctrid --pull=never --quiet docker.io/library/busybox
  /usr/local/bin/buildah ${BUILDAH_REGISTRY_OPTS} --root /var/lib/containers/storage --runroot /run/containers/storage --storage-driver ${STORAGE_DRIVER} ${storage_opts:+--storage-opt ${storage_opts}} run --isolation=chroot ctrid pwd
EOF
  chmod +x ${TEST_SCRATCH_DIR}
  chmod +rx ${TEST_SCRATCH_DIR}/script1 ${TEST_SCRATCH_DIR}/script2
  env -i unshare -inCfpm bash ${TEST_SCRATCH_DIR}/script1
}
