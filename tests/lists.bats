#!/usr/bin/env bats

load helpers

IMAGE_LIST=docker://registry.k8s.io/pause:3.1
IMAGE_LIST_DIGEST=docker://registry.k8s.io/pause@sha256:f78411e19d84a252e53bff71a4407a5686c46983a2c2eeed83929b888179acea
IMAGE_LIST_INSTANCE=docker://registry.k8s.io/pause@sha256:f365626a556e58189fc21d099fc64603db0f440bff07f77c740989515c544a39
IMAGE_LIST_AMD64_INSTANCE_DIGEST=sha256:59eec8837a4d942cc19a52b8c09ea75121acc38114a2c68b98983ce9356b8610
IMAGE_LIST_ARM_INSTANCE_DIGEST=sha256:c84b0a3a07b628bc4d62e5047d0f8dff80f7c00979e1e28a821a033ecda8fe53
IMAGE_LIST_ARM64_INSTANCE_DIGEST=sha256:f365626a556e58189fc21d099fc64603db0f440bff07f77c740989515c544a39
IMAGE_LIST_PPC64LE_INSTANCE_DIGEST=sha256:bcf9771c0b505e68c65440474179592ffdfa98790eb54ffbf129969c5e429990
IMAGE_LIST_S390X_INSTANCE_DIGEST=sha256:882a20ee0df7399a445285361d38b711c299ca093af978217112c73803546d5e

@test "manifest-create" {
    _prefetch busybox
    run_buildah inspect -f '{{ .FromImageDigest }}' busybox
    imagedigest="$output"
    run_buildah manifest create foo
    listid="$output"
    run_buildah 125 manifest create foo
    assert "$output" =~ "that name is already in use"
    run_buildah manifest create --amend foo
    assert "$output" == "$listid"
    run_buildah manifest create --amend --annotation red=blue --annotation green=yellow,orange,purple foo busybox
    assert "$output" == "$listid"
    run_buildah manifest inspect foo
    assert "$output" =~ '"red": "blue"'
    assert "$output" =~ '"green": "yellow,orange,purple"'
    assert "$output" =~ "${imagedigest}"
    # since manifest exists in local storage this should exit with `0`
    run_buildah manifest exists foo
    # since manifest does not exist in local storage this should exit with `1`
    run_buildah 1 manifest exists foo2
}

@test "manifest-create-amend-nonlist" {
    run_buildah bud $WITH_POLICY_JSON -t amend-test:v1 $BUDFILES/from-scratch
    run_buildah images -q amend-test:v1
    originalid="$output"

    # --amend should convert a non-list image into a manifest list
    run_buildah manifest create --amend amend-test:v1
    listid="$output"
    # the list ID should differ from the original image
    assert "$listid" != "$originalid" "list ID should differ from original image ID"
    run_buildah manifest inspect amend-test:v1
    # the original image should be the sole entry in the manifest list
    nmanifests=$(jq '.manifests | length' <<< "$output")
    assert "$nmanifests" -eq 1 "expected exactly 1 manifest entry"
}

@test "manifest-create-amend-nonlist-with-images" {
    run_buildah bud $WITH_POLICY_JSON -t amend-add-test:v1 $BUDFILES/from-scratch
    run_buildah bud $WITH_POLICY_JSON -t scratch-for-amend $BUDFILES/from-scratch

    # --amend with a non-list image and additional images to add
    run_buildah manifest create --amend amend-add-test:v1 scratch-for-amend
    run_buildah manifest inspect amend-add-test:v1
    # should contain the converted original + the added scratch image
    nmanifests=$(jq '.manifests | length' <<< "$output")
    assert "$nmanifests" -ge 2 "expected at least 2 manifest entries (converted + added)"
}

@test "manifest-create-replace" {
    run_buildah bud $WITH_POLICY_JSON -t replace-test:v1 $BUDFILES/from-scratch
    run_buildah images -q replace-test:v1
    originalid="$output"

    # --replace should untag the existing image and create a fresh empty list
    run_buildah manifest create --replace replace-test:v1
    listid="$output"
    assert "$listid" != "$originalid" "list ID should differ from original image ID"
    run_buildah manifest inspect replace-test:v1
    # the new list should have no entries
    nmanifests=$(jq '.manifests | length' <<< "$output")
    assert "$nmanifests" -eq 0 "expected empty manifest list"
    # the original image should still exist (just untagged from replace-test:v1)
    run_buildah inspect "$originalid"
}

@test "manifest-create-replace-existing-list" {
    _prefetch busybox
    # create a list with an entry so it's non-empty
    run_buildah manifest create replace-list-test:v1
    originalid="$output"
    run_buildah manifest add replace-list-test:v1 busybox
    run_buildah manifest inspect replace-list-test:v1
    noriginal=$(jq '.manifests | length' <<< "$output")
    assert "$noriginal" -gt 0 "setup: original list should have entries"

    # --replace should discard the old list and create a fresh empty one
    run_buildah manifest create --replace replace-list-test:v1
    newid="$output"
    assert "$newid" != "$originalid" "new list ID should differ from original"
    run_buildah manifest inspect replace-list-test:v1
    nmanifests=$(jq '.manifests | length' <<< "$output")
    assert "$nmanifests" -eq 0 "expected empty manifest list after replace"
}

@test "manifest-create-amend-replace-conflict" {
    # --amend and --replace together should fail
    run_buildah 125 manifest create --amend --replace fail-test:v1
    assert "$output" =~ "mutually exclusive"
}

@test "manifest-inspect-id" {
    run_buildah manifest create foo
    cid=$output
    run_buildah manifest inspect $cid
}

@test "manifest-add" {
    run_buildah manifest create foo
    run_buildah manifest add foo ${IMAGE_LIST}
    # since manifest exists in local storage this should exit with `0`
    run_buildah manifest exists foo
    # since manifest does not exist in local storage this should exit with `1`
    run_buildah 1 manifest exists foo2
    run_buildah manifest rm foo
}

@test "manifest-add artifact" {
    _prefetch busybox
    createrandom $TEST_SCRATCH_DIR/randomfile2
    createrandom $TEST_SCRATCH_DIR/randomfile
    run sha256sum $TEST_SCRATCH_DIR/randomfile
    blobencoded="${output%% *}"
    run_buildah manifest create foo
    run_buildah manifest add --artifact --artifact-type image/jpeg --artifact-layer-type image/not-validated --artifact-config-type text/x-not-really --artifact-subject busybox --artifact-annotation up=down foo $TEST_SCRATCH_DIR/randomfile2
    run_buildah manifest add --artifact --artifact-type image/png --artifact-layer-type image/not-validated --artifact-config-type text/x-not-really --artifact-subject busybox --artifact-annotation left=right foo $TEST_SCRATCH_DIR/randomfile
    digest="${output##* }"
    alg="${digest%%:*}"
    encoded="${digest##*:}"
    run_buildah manifest annotate --annotation red=blue --annotation green=yellow,orange,purple foo $TEST_SCRATCH_DIR/randomfile
    run_buildah manifest inspect foo
    assert "$output" =~ '"image/png"'
    assert "$output" =~ '"red": "blue"'
    assert "$output" =~ '"green": "yellow,orange,purple"'
    run_buildah manifest push --all foo oci:$TEST_SCRATCH_DIR/pushed
    run cmp $TEST_SCRATCH_DIR/randomfile $TEST_SCRATCH_DIR/pushed/blobs/sha256/$blobencoded
    assert "$status" -eq 0 "pushed copy of random file did not match original"
    run cat $TEST_SCRATCH_DIR/pushed/blobs/$alg/$encoded
    assert "$status" -eq 0 "artifact manifest not found in expected location"
    assert "$output" =~ '"artifactType":"image/png"' "cat $TEST_SCRATCH_DIR/pushed/blobs/$alg/$encoded"
    assert "$output" =~ '"mediaType":"image/not-validated"' "cat $TEST_SCRATCH_DIR/pushed/blobs/$alg/$encoded"
    assert "$output" =~ '"mediaType":"text/x-not-really"' "cat $TEST_SCRATCH_DIR/pushed/blobs/$alg/$encoded"
    assert "$output" =~ '"annotations":\{"left":"right"\}' "cat $TEST_SCRATCH_DIR/pushed/blobs/$alg/$encoded"
    run_buildah manifest rm foo
}

@test "manifest-add-multiple-artifacts" {
    run_buildah manifest create foo
    createrandom $TEST_SCRATCH_DIR/randomfile4
    createrandom $TEST_SCRATCH_DIR/randomfile3
    run_buildah manifest add --artifact foo $TEST_SCRATCH_DIR/randomfile3 $TEST_SCRATCH_DIR/randomfile4
    run_buildah manifest push --all foo oci:$TEST_SCRATCH_DIR/pushed
}

@test "manifest-add local image" {
    target=scratch-image
    run_buildah bud $WITH_POLICY_JSON -t ${target} $BUDFILES/from-scratch
    run_buildah manifest create foo
    run_buildah manifest add foo ${target}
    run_buildah manifest rm foo
}

@test "manifest-add-one" {
    # This test must be run on amd64 to get an error when pulling an image from a different platform
    skip_unless_arch amd64
    run_buildah manifest create foo
    run_buildah manifest add --arch=arm64 foo ${IMAGE_LIST_INSTANCE}
    run_buildah manifest inspect foo
    expect_output --substring ${IMAGE_LIST_ARM64_INSTANCE_DIGEST}
    run_buildah 125 inspect --type image foo
    expect_output --substring "no image found"
    run_buildah inspect foo
    expect_output --substring ${IMAGE_LIST_ARM64_INSTANCE_DIGEST}
}

@test "manifest-add-all" {
    run_buildah manifest create foo
    run_buildah manifest add --all foo ${IMAGE_LIST}
    run_buildah manifest inspect foo
    expect_output --substring ${IMAGE_LIST_AMD64_INSTANCE_DIGEST}
    expect_output --substring ${IMAGE_LIST_ARM_INSTANCE_DIGEST}
    expect_output --substring ${IMAGE_LIST_ARM64_INSTANCE_DIGEST}
    expect_output --substring ${IMAGE_LIST_PPC64LE_INSTANCE_DIGEST}
    expect_output --substring ${IMAGE_LIST_S390X_INSTANCE_DIGEST}
}

@test "manifest-annotate global annotation" {
    _prefetch busybox
    run_buildah manifest create foo
    run_buildah manifest add foo busybox
    run_buildah manifest annotate --index --annotation red=blue --annotation green=yellow,orange,purple foo
    run_buildah manifest inspect foo
    assert "$output" =~ '"red": "blue"'
    assert "$output" =~ '"green": "yellow,orange,purple"'
}

@test "manifest-annotate instance annotation" {
    _prefetch busybox
    run_buildah manifest create foo
    run_buildah manifest add foo busybox
    instance="${output##* }"
    run_buildah manifest annotate --annotation red=blue --annotation green=yellow,orange,purple foo "${instance}"
    run_buildah manifest annotate --os OperatingSystem foo "${instance}"
    run_buildah manifest annotate --arch aRCHITECTURE foo "${instance}"
    run_buildah manifest annotate --variant vARIANT foo "${instance}"
    run_buildah manifest annotate --features FEATURE1 --features FEATURE2 foo "${instance}"
    run_buildah manifest annotate --os-features OSFEATURE1 --os-features OSFEATURE2 foo "${instance}"
    run_buildah manifest inspect foo
    assert "$output" =~ '"red": "blue"'
    assert "$output" =~ '"green": "yellow,orange,purple"'
    assert "$output" =~ '"os": "OperatingSystem"'
    assert "$output" =~ '"architecture": "aRCHITECTURE"'
    assert "$output" =~ '"variant": "vARIANT"'
}

@test "manifest-annotate subject" {
    _prefetch busybox "${IMAGE_LIST_INSTANCE##*://}"
    run_buildah manifest create foo
    run_buildah manifest add foo busybox
    run_buildah manifest annotate --subject "${IMAGE_LIST_INSTANCE##*://}" foo
    run_buildah inspect -f '{{ .FromImageDigest }}' "${IMAGE_LIST_INSTANCE##*://}"
    imagedigest="$output"
    run_buildah manifest inspect foo
    assert "$output" =~ "$imagedigest"
}

@test "manifest-remove" {
    run_buildah manifest create foo
    run_buildah manifest add --all foo ${IMAGE_LIST}
    run_buildah manifest inspect foo
    expect_output --substring ${IMAGE_LIST_ARM64_INSTANCE_DIGEST}
    run_buildah manifest remove foo ${IMAGE_LIST_ARM64_INSTANCE_DIGEST}
    run_buildah manifest inspect foo
    expect_output --substring ${IMAGE_LIST_AMD64_INSTANCE_DIGEST}
    expect_output --substring ${IMAGE_LIST_ARM_INSTANCE_DIGEST}
    expect_output --substring ${IMAGE_LIST_PPC64LE_INSTANCE_DIGEST}
    expect_output --substring ${IMAGE_LIST_S390X_INSTANCE_DIGEST}

    # ARM64 should now be gone
    arm64=$(grep ${IMAGE_LIST_ARM64_INSTANCE_DIGEST} <<< "$output" || true)
    assert "$arm64" = "" "arm64 instance digest found in manifest list"
}

@test "manifest-remove-not-found" {
    run_buildah manifest create foo
    run_buildah manifest add foo ${IMAGE_LIST}
    run_buildah 125 manifest remove foo sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
}

@test "manifest-rm failures" {
    run_buildah 125 manifest rm foo1
    expect_output --substring "foo1: image not known"
}

@test "manifest-push-with-timestamp" {
    outpath=${TEST_SCRATCH_DIR}/out

    run_buildah manifest create foo
    run_buildah manifest add --all foo ${IMAGE_LIST}

    run_buildah manifest push --timestamp=1 foo oci-archive:${outpath}.a
    run_buildah manifest push --timestamp=0 foo oci-archive:${outpath}.b
    sleep 1.1
    run_buildah manifest push --timestamp=0 foo oci-archive:${outpath}.c

    run_buildah manifest rm foo

    # should be different ( || false is due to bats weirdness )
    ! diff "${outpath}.a" "${outpath}.b" || false

    # should be the same
    diff "${outpath}.b" "${outpath}.c"
}

@test "manifest-push" {
    run_buildah manifest create foo
    run_buildah manifest add --all foo ${IMAGE_LIST}
    run_buildah manifest push $WITH_POLICY_JSON foo dir:${TEST_SCRATCH_DIR}/pushed
    case "$(go env GOARCH 2> /dev/null)" in
	    amd64) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_AMD64_INSTANCE_DIGEST} ;;
	    arm64) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_ARM64_INSTANCE_DIGEST} ;;
	    arm) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_ARM_INSTANCE_DIGEST} ;;
	    ppc64le) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_PPC64LE_INSTANCE_DIGEST} ;;
	    s390x) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_S390X_INSTANCE_DIGEST} ;;
	    *) skip "current arch \"$(go env GOARCH 2> /dev/null)\" not present in manifest list" ;;
    esac

    run grep ${IMAGE_LIST_EXPECTED_INSTANCE_DIGEST##sha256} ${TEST_SCRATCH_DIR}/pushed/manifest.json
    assert "$status" -eq 0 "status code of grep for expected instance digest"
}

@test "manifest-push with retry" {
    run_buildah manifest create foo
    run_buildah manifest add --all foo ${IMAGE_LIST}
    run_buildah manifest push --retry 4 --retry-delay 4s $WITH_POLICY_JSON foo dir:${TEST_SCRATCH_DIR}/pushed
    case "$(go env GOARCH 2> /dev/null)" in
	    amd64) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_AMD64_INSTANCE_DIGEST} ;;
	    arm64) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_ARM64_INSTANCE_DIGEST} ;;
	    arm) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_ARM_INSTANCE_DIGEST} ;;
	    ppc64le) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_PPC64LE_INSTANCE_DIGEST} ;;
	    s390x) IMAGE_LIST_EXPECTED_INSTANCE_DIGEST=${IMAGE_LIST_S390X_INSTANCE_DIGEST} ;;
	    *) skip "current arch \"$(go env GOARCH 2> /dev/null)\" not present in manifest list" ;;
    esac

    run grep ${IMAGE_LIST_EXPECTED_INSTANCE_DIGEST##sha256} ${TEST_SCRATCH_DIR}/pushed/manifest.json
    assert "$status" -eq 0 "status code of grep for expected instance digest"
}


@test "manifest-push-all" {
    run_buildah manifest create foo
    run_buildah manifest add --all foo ${IMAGE_LIST}
    run_buildah manifest push $WITH_POLICY_JSON --all foo dir:${TEST_SCRATCH_DIR}/pushed
    run sha256sum ${TEST_SCRATCH_DIR}/pushed/*
    expect_output --substring ${IMAGE_LIST_AMD64_INSTANCE_DIGEST##sha256:}
    expect_output --substring ${IMAGE_LIST_ARM_INSTANCE_DIGEST##sha256:}
    expect_output --substring ${IMAGE_LIST_ARM64_INSTANCE_DIGEST##sha256:}
    expect_output --substring ${IMAGE_LIST_PPC64LE_INSTANCE_DIGEST##sha256:}
    expect_output --substring ${IMAGE_LIST_S390X_INSTANCE_DIGEST##sha256:}
}

@test "manifest-push-all-default-true" {
    run_buildah manifest push --help
    expect_output --substring "all.*\(default true\).*authfile"
}

@test "manifest-push-purge" {
    run_buildah manifest create foo
    run_buildah manifest add --arch=arm64 foo ${IMAGE_LIST}
    run_buildah manifest inspect foo
    run_buildah manifest push $WITH_POLICY_JSON --purge foo dir:${TEST_SCRATCH_DIR}/pushed
    run_buildah 125 manifest inspect foo
}

@test "manifest-push-rm" {
    run_buildah manifest create foo
    run_buildah manifest add --arch=arm64 foo ${IMAGE_LIST}
    run_buildah manifest inspect foo
    run_buildah manifest push $WITH_POLICY_JSON --rm foo dir:${TEST_SCRATCH_DIR}/pushed
    run_buildah 125 manifest inspect foo
}

@test "manifest-push should fail with nonexistent authfile" {
    run_buildah manifest create foo
    run_buildah manifest add --arch=arm64 foo ${IMAGE_LIST}
    run_buildah manifest inspect foo
    run_buildah 125 manifest push --authfile /tmp/nonexistent $WITH_POLICY_JSON --purge foo dir:${TEST_SCRATCH_DIR}/pushed

}

@test "manifest-from-tag" {
    run_buildah from $WITH_POLICY_JSON --name test-container ${IMAGE_LIST}
    run_buildah inspect --format '{{.OCIv1.Architecture}}' ${IMAGE_LIST#docker://}
    expect_output --substring amd64
    run_buildah inspect --format '{{.OCIv1.Architecture}}' test-container
    expect_output --substring amd64
}

@test "manifest-from-digest" {
    run_buildah from $WITH_POLICY_JSON --name test-container ${IMAGE_LIST_DIGEST}
    run_buildah inspect --format '{{.OCIv1.Architecture}}' ${IMAGE_LIST_DIGEST#docker://}
    expect_output --substring amd64
    run_buildah inspect --format '{{.OCIv1.Architecture}}' test-container
    expect_output --substring amd64
}

@test "manifest-from-instance" {
    run_buildah from $WITH_POLICY_JSON --name test-container ${IMAGE_LIST_INSTANCE}
    run_buildah inspect --format '{{.OCIv1.Architecture}}' ${IMAGE_LIST_INSTANCE#docker://}
    expect_output --substring amd64
    run_buildah inspect --format '{{.OCIv1.Architecture}}' test-container
    expect_output --substring amd64
}

@test "manifest-no-matching-instance" {
    # Check that local images which we can't load the config and history for
    # don't just break multi-layer builds.
    #
    # Create a test list with some stuff in it.
    run_buildah manifest create test-list
    run_buildah manifest add --all test-list ${IMAGE_LIST}
    # Remove the entry for the current arch from the list.
    arch=$(go env GOARCH)
    run_buildah manifest inspect test-list
    archinstance=$(jq -r '.manifests|map(select(.platform.architecture=="'$arch'"))[].digest' <<< "$output")
    run_buildah manifest remove test-list $archinstance
    # Try to build using the build cache.
    mkdir ${TEST_SCRATCH_DIR}/build
    echo 'much content, wow.' > ${TEST_SCRATCH_DIR}/build/content.txt
    echo 'FROM scratch' > ${TEST_SCRATCH_DIR}/build/Dockerfile
    echo 'ADD content.txt /' >> ${TEST_SCRATCH_DIR}/build/Dockerfile
    run_buildah bud --layers --iidfile ${TEST_SCRATCH_DIR}/image-id.txt ${TEST_SCRATCH_DIR}/build
    # Make sure we can add the new image to the list.
    run_buildah manifest add test-list $(< ${TEST_SCRATCH_DIR}/image-id.txt)
}

@test "manifest-add-to-list-from-storage" {
    run_buildah pull --arch=amd64 busybox
    run_buildah tag busybox test:amd64
    run_buildah pull --arch=arm64 busybox
    run_buildah tag busybox test:arm64
    run_buildah manifest create test
    run_buildah manifest add test test:amd64
    run_buildah manifest add --variant=variant-something test test:arm64
    run_buildah manifest inspect test
    # must contain amd64
    expect_output --substring "amd64"
    # must contain arm64
    expect_output --substring "arm64"
    # must contain variant v8
    expect_output --substring "variant-something"
}

@test "manifest-create-list-from-storage" {
    run_buildah from --quiet --arch amd64 busybox
    cid=$output
    run_buildah commit $cid "$cid-committed:latest"
    run_buildah manifest create test:latest "$cid-committed:latest"
    run_buildah manifest inspect test
    # must contain amd64
    expect_output --substring "amd64"
    # since manifest exists in local storage this should exit with `0`
    run_buildah manifest exists test:latest
    # since manifest does not exist in local storage this should exit with `1`
    run_buildah 1 manifest exists test2
}

@test "manifest-skip-some-base-images-with-all-platforms" {
    start_registry
    run_buildah manifest create localhost:"${REGISTRY_PORT}"/base
    run_buildah manifest add --all localhost:"${REGISTRY_PORT}"/base ${IMAGE_LIST}
    # get a count of how many "real" base images there are
    run_buildah manifest inspect localhost:"${REGISTRY_PORT}"/base
    nbaseplatforms=$(grep '"platform"' <<< "$output" | wc -l)
    echo $nbaseplatforms base platforms
    # add some trash that we expect to skip in a --all-platforms build
    run_buildah build --manifest localhost:"${REGISTRY_PORT}"/base --platform unknown/unknown --no-cache -f $BUDFILES/from-scratch/Containerfile2 $BUDFILES/from-scratch
    run_buildah build --manifest localhost:"${REGISTRY_PORT}"/base --platform linux/unknown --no-cache -f $BUDFILES/from-scratch/Containerfile2 $BUDFILES/from-scratch
    run_buildah build --manifest localhost:"${REGISTRY_PORT}"/base --platform unknown/amd64p32 --no-cache -f $BUDFILES/from-scratch/Containerfile2 $BUDFILES/from-scratch
    # add a known combination of OS/arch that we can be pretty sure wasn't already there
    run_buildah build --manifest localhost:"${REGISTRY_PORT}"/base --platform linux/amd64p32 --no-cache -f $BUDFILES/from-scratch/Containerfile2 $BUDFILES/from-scratch
    # push the list to the local registry and clean up our local copy
    run_buildah manifest push --tls-verify=false --creds testuser:testpassword --all localhost:"${REGISTRY_PORT}"/base
    run_buildah rmi localhost:"${REGISTRY_PORT}"/base
    # build a new list based on the valid base images in the list we just pushed
    run_buildah build --tls-verify=false --creds testuser:testpassword --manifest derived --all-platforms --from localhost:"${REGISTRY_PORT}"/base $BUDFILES/from-base
    run_buildah manifest inspect derived
    nderivedplatforms=$(grep '"platform"' <<< "$output" | wc -l)
    echo $nderivedplatforms derived platforms
    nexpectedderivedplatforms=$((nbaseplatforms+1))
    echo expected $nexpectedderivedplatforms derived platforms
    [[ $nderivedplatforms -eq $nexpectedderivedplatforms ]]
}
