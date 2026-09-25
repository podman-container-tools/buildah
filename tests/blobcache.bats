#!/usr/bin/env bats

load helpers

# The 'rm cachefile' in the "blobs must be reused" test
# causes flakes when parallelizing
function setup_file() {
    export BATS_NO_PARALLELIZE_WITHIN_FILE=true
}

@test "blobcache-pull" {
	blobcachedir=${TEST_SCRATCH_DIR}/cache
	mkdir -p ${blobcachedir}
	# Pull an image using a fresh directory for the blob cache.
	run_buildah pull --blob-cache=${blobcachedir} $WITH_POLICY_JSON quay.io/libpod/k8s-pause:3.5
	# Check that we dropped some files in there.
	run find ${blobcachedir} -type f
	echo "$output"
	[ "$status" -eq 0 ]
	[ "${#lines[@]}" -gt 0 ]
}

@test "blobcache-from" {
	blobcachedir=${TEST_SCRATCH_DIR}/cache
	mkdir -p ${blobcachedir}
	# Pull an image using a fresh directory for the blob cache.
	run_buildah from --blob-cache=${blobcachedir} $WITH_POLICY_JSON quay.io/libpod/k8s-pause:3.5
	# Check that we dropped some files in there.
	run find ${blobcachedir} -type f
	echo "$output"
	[ "$status" -eq 0 ]
	[ "${#lines[*]}" -gt 0 ]
}


function _check_matches() {
	local destdir="$1"
	local blobcachedir="$2"

	# Look for layer blobs in the destination that match the ones in the cache.
	local matched=0
	local unmatched=0
	for content in ${destdir}/* ; do
		match=false
		for blob in ${blobcachedir}/* ; do
			if cmp -s ${content} ${blob} ; then
				echo $(file ${blob}) and ${content} have the same contents, was cached
				match=true
				break
			fi
		done
		if ${match} ; then
			matched=$(( ${matched} + 1 ))
		else
			unmatched=$(( ${unmatched} + 1 ))
			echo ${content} was not cached
		fi
	done

        expect_output --from="$matched"   "$3"  "$4 should match"
        expect_output --from="$unmatched" "$5"  "$6 should not match"
}

# Integration test for https://github.com/containers/image/pull/1645
@test "blobcache: blobs must be reused when pushing across registry" {
	blobcachedir=${TEST_SCRATCH_DIR}/cachereuse
	mkdir -p ${blobcachedir}
	start_registry

	imgname=blobimg-$(random_string | tr A-Z a-z)
	run_buildah login --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth --username testuser --password testpassword localhost:${REGISTRY_PORT}
	outputdir=${TEST_SCRATCH_DIR}/outputdir
	mkdir -p ${outputdir}
	podman run --rm --mount type=bind,src=${TEST_SCRATCH_DIR}/test.auth,target=/test.auth,Z --mount type=bind,src=${outputdir},target=/output,Z --net host quay.io/skopeo/stable copy --preserve-digests --authfile=/test.auth --tls-verify=false docker://quay.io/libpod/alpine:3.10.2 dir:/output
	run_buildah rmi --all -f
	run_buildah pull --blob-cache=${blobcachedir} dir:${outputdir}
	run_buildah images -a --format '{{.ID}}'
	cid=$output
	run_buildah --log-level debug push --blob-cache=${blobcachedir} --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth $cid docker://localhost:${REGISTRY_PORT}/$imgname
	# must not contain "Skipping blob" since push must happen
	assert "$output" !~ "Skipping blob"

	# Clear local image and c/image's blob-info-cache
	run_buildah rmi --all -f
	cachedir=/var/lib
	if is_rootless;
	then
		cachedir=$HOME/.local/share
	fi
	run rm -rf $blobcachedir/*
	assert "$status" -eq 0 "status of `run rm $cachefile` must be 0"

	# In first push blob must be skipped after vendoring https://github.com/containers/image/pull/1645
	run_buildah pull dir:${outputdir}
	run_buildah images -a --format '{{.ID}}'
	cid=$output
	run_buildah --log-level debug push --blob-cache=${blobcachedir} --tls-verify=false --authfile ${TEST_SCRATCH_DIR}/test.auth $cid docker://localhost:${REGISTRY_PORT}/$imgname
	expect_output --substring "Skipping blob"
}

@test "blobcache-commit" {
	blobcachedir=${TEST_SCRATCH_DIR}/cache
	mkdir -p ${blobcachedir}
	# Pull an image using a fresh directory for the blob cache.
	run_buildah from --quiet --cidfile ${TEST_SCRATCH_DIR}/cid --blob-cache=${blobcachedir} $WITH_POLICY_JSON quay.io/libpod/k8s-pause:3.5
	ctr="$(< ${TEST_SCRATCH_DIR}/cid)"
	run_buildah add ${ctr} $BUDFILES/add-file/file /
	# Commit the image without using the blob cache, using compression so that uncompressed blobs
	# in the cache which we inherited from our base image won't be matched.
	doomeddir=${TEST_SCRATCH_DIR}/doomed
	mkdir -p ${doomeddir}
	run_buildah commit $WITH_POLICY_JSON --disable-compression=false ${ctr} dir:${doomeddir}
        _check_matches $doomeddir $blobcachedir \
                       0 "nothing" \
                       5 "everything"

	# Commit the image using the blob cache, again using compression.  We'll have recorded the
	# compressed digests that match the uncompressed digests the last time around, so we should
	# get some matches this time.
	destdir=${TEST_SCRATCH_DIR}/dest
	mkdir -p ${destdir}
	ls -l ${blobcachedir}
	run_buildah commit $WITH_POLICY_JSON --blob-cache=${blobcachedir} --disable-compression=false ${ctr} dir:${destdir}
	_check_matches $destdir $blobcachedir \
                       4 "base layers, new layer, config, and manifest" \
                       1 "version"
}

@test "blobcache-push" {
	target=targetimage
	blobcachedir=${TEST_SCRATCH_DIR}/cache
	mkdir -p ${blobcachedir}
	# Pull an image using a fresh directory for the blob cache.
	run_buildah from --quiet --cidfile ${TEST_SCRATCH_DIR}/cid --blob-cache=${blobcachedir} $WITH_POLICY_JSON quay.io/libpod/k8s-pause:3.5
	ctr="$(< ${TEST_SCRATCH_DIR}/cid)"
	run_buildah add ${ctr} $BUDFILES/add-file/file /
	# Commit the image using the blob cache.
	ls -l ${blobcachedir}
	run_buildah commit $WITH_POLICY_JSON --blob-cache=${blobcachedir} --disable-compression=false ${ctr} ${target}
	# Try to push the image without the blob cache.
	doomeddir=${TEST_SCRATCH_DIR}/doomed
	mkdir -p ${doomeddir}
	ls -l ${blobcachedir}
	run_buildah push $WITH_POLICY_JSON ${target} dir:${doomeddir}
        _check_matches $doomeddir $blobcachedir \
                       2 "only config and new layer" \
                       3 "version, manifest, base layers"

	# Now try to push the image using the blob cache.
	destdir=${TEST_SCRATCH_DIR}/dest
	mkdir -p ${destdir}
	ls -l ${blobcachedir}

	run_buildah push $WITH_POLICY_JSON --blob-cache=${blobcachedir} ${target} dir:${destdir}
        _check_matches $destdir $blobcachedir \
                       4 "base image layers, new layer, config, and manifest" \
                       1 "version"
}

@test "blobcache-build-compressed-using-dockerfile-explicit-push" {
	blobcachedir=${TEST_SCRATCH_DIR}/cache
	mkdir -p ${blobcachedir}
	target=new-image
	# Build an image while pulling the base image.  Compress the layers so that they get added
	# to the blob cache in their compressed forms.
	run_buildah build-using-dockerfile -t ${target} --pull-always $WITH_POLICY_JSON --blob-cache=${blobcachedir} --disable-compression=false $BUDFILES/add-file
	# Now try to push the image using the blob cache.  The blob cache will only suggest the
	# compressed version of a blob if it's been told that we want to compress things, so
	# we also request compression here to avoid having the copy logic just compress the
	# uncompressed copy again.
	destdir=${TEST_SCRATCH_DIR}/dest
	mkdir -p ${destdir}
	run_buildah push $WITH_POLICY_JSON --blob-cache=${blobcachedir} --disable-compression=false ${target} dir:${destdir}
        _check_matches $destdir $blobcachedir \
                       4 "config, base layer, new layer, and manifest" \
                       1 "version"
}

@test "blobcache-build-uncompressed-using-dockerfile-explicit-push" {
	blobcachedir=${TEST_SCRATCH_DIR}/cache
	mkdir -p ${blobcachedir}
	target=new-image
	# Build an image while pulling the base image.
	run_buildah build-using-dockerfile -t ${target} -D --pull-always --blob-cache=${blobcachedir} $WITH_POLICY_JSON $BUDFILES/add-file
	# Now try to push the image using the blob cache.
	destdir=${TEST_SCRATCH_DIR}/dest
	mkdir -p ${destdir}
	run_buildah push $WITH_POLICY_JSON --blob-cache=${blobcachedir} ${target} dir:${destdir}
	_check_matches $destdir $blobcachedir \
                       2 "config and previously-compressed base layer" \
                       3 "version, new layer, and manifest"
}

@test "blobcache-build-compressed-using-dockerfile-implicit-push" {
	blobcachedir=${TEST_SCRATCH_DIR}/cache
	mkdir -p ${blobcachedir}
	target=new-image
	destdir=${TEST_SCRATCH_DIR}/dest
	mkdir -p ${destdir}
	# Build an image while pulling the base image, implicitly pushing while writing.
	run_buildah build-using-dockerfile -t dir:${destdir} --pull-always --blob-cache=${blobcachedir} $WITH_POLICY_JSON $BUDFILES/add-file
        _check_matches $destdir $blobcachedir \
                       4 "base image, layer, config, and manifest" \
                       1 "version"
}

@test "blobcache-build-uncompressed-using-dockerfile-implicit-push" {
	blobcachedir=${TEST_SCRATCH_DIR}/cache
	mkdir -p ${blobcachedir}
	target=new-image
	destdir=${TEST_SCRATCH_DIR}/dest
	mkdir -p ${destdir}
	# Build an image while pulling the base image, implicitly pushing while writing.
	run_buildah build-using-dockerfile -t dir:${destdir} -D --pull-always --blob-cache=${blobcachedir} $WITH_POLICY_JSON $BUDFILES/add-file
        _check_matches $destdir $blobcachedir \
                       4 "base image, our layer, config, and manifest" \
                       1 "version"
}
