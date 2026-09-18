#!/usr/bin/env bats

load helpers

@test "images-flags-order-verification" {
  run_buildah images --all

  run_buildah 125 images img1 -n
  check_options_flag_err "-n"

  run_buildah 125 images img1 --filter="service=redis" img2
  check_options_flag_err "--filter=service=redis"

  run_buildah 125 images img1 img2 img3 -q
  check_options_flag_err "-q"
}

@test "images" {
  _prefetch alpine busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid2=$output
  run_buildah images
  expect_line_count 3
}

@test "images all test" {
  _prefetch alpine
  contentdir=${TEST_SCRATCH_DIR}/content
  mkdir -p ${contentdir}
  echo legend has it this counts as a README file > ${contentdir}/README.md
  starthttpd ${contentdir}
  run_buildah bud $WITH_POLICY_JSON --build-arg=HTTP_SERVER_PORT=${HTTP_SERVER_PORT} --layers -t test $BUDFILES/use-layers
  run_buildah images
  expect_line_count 3

  run_buildah images -a
  expect_line_count 9

  # create a no name image which should show up when doing buildah images without the --all flag
  run_buildah bud $WITH_POLICY_JSON --build-arg=HTTP_SERVER_PORT=${HTTP_SERVER_PORT} $BUDFILES/use-layers
  run_buildah images
  expect_line_count 4
}

@test "images filter test" {
  _prefetch quay.io/libpod/k8s-pause:3.5 busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON quay.io/libpod/k8s-pause:3.5
  cid1=$output
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid2=$output

  run_buildah 125 images --noheading --filter since quay.io/libpod/k8s-pause:3.5
  expect_output 'Error: invalid image filter "since": must be in the format "filter=value or filter!=value"'


  run_buildah images --noheading --filter since=quay.io/libpod/k8s-pause:3.5
  expect_line_count 1

  # pause* and u* should only give us pause image not busybox since its a AND between
  # two filters
  run_buildah images --noheading --filter "reference=pause*" --filter "reference=u*"
  expect_line_count 1
}

@test "images format test" {
  _prefetch alpine busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid2=$output
  run_buildah images --format "{{.Name}}"
  expect_line_count 2
}

@test "images noheading test" {
  _prefetch alpine busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid2=$output
  run_buildah images --noheading
  expect_line_count 2
}

@test "images quiet test" {
  _prefetch alpine busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid2=$output
  run_buildah images --quiet
  expect_line_count 2
}

@test "images no-trunc test" {
  _prefetch alpine busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid2=$output
  run_buildah images -q --no-trunc
  expect_line_count 2
  expect_output --substring --from="${lines[0]}" "sha256"
}

@test "images json test" {
  _prefetch alpine busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox

  for img in '' alpine busybox; do
      run_buildah images --json $img
      run jq -re 'length > 0 and
        all(.[]; has("id")
             and has("names")
             and has("digest")
             and has("createdat")
             and has("size")
             and has("readonly")
             and has("history"))' <<<"$output"
      expect_output "true"
      assert "$status" -eq 0 "status from jq for $img"
  done
}

@test "images json dup test" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid test
  run_buildah tag test new-name

  run_buildah images --json
  expect_output --substring '"id": '
}

@test "images json valid" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid1=$output
  run_buildah from $WITH_POLICY_JSON scratch
  cid2=$output
  run_buildah commit $WITH_POLICY_JSON $cid1 test
  run_buildah commit $WITH_POLICY_JSON $cid2 test2

  run_buildah images --json
  run python3 -m json.tool <<< "$output"
  assert "$status" -eq 0 "status from python json.tool"
}

@test "specify an existing image" {
  _prefetch alpine busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  cid1=$output
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox
  cid2=$output
  run_buildah images alpine
  expect_line_count 2
}

@test "specify a nonexistent image" {
  run_buildah 125 images alpine
  expect_output --from="${lines[0]}" "Error: alpine: image not known"
  expect_line_count 1
}

@test "Test dangling images" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid test
  run_buildah commit $WITH_POLICY_JSON $cid test
  run_buildah images
  expect_line_count 3

  run_buildah images --filter dangling=true
  expect_output --substring " <none> "
  expect_line_count 2

  run_buildah images --filter dangling=false
  expect_output --substring " latest "
  expect_line_count 2
}

@test "image digest test" {
  _prefetch busybox
  run_buildah pull $WITH_POLICY_JSON busybox
  run_buildah images --digests
  expect_output --substring "sha256:"
}

@test "images in OCI format with no creation dates" {
  mkdir -p $TEST_SCRATCH_DIR/blobs/sha256

  # Create a layer.
  dd if=/dev/zero bs=512 count=2 of=$TEST_SCRATCH_DIR/blob
  layerdigest=$(sha256sum $TEST_SCRATCH_DIR/blob | awk '{print $1}')
  layersize=$(stat -c %s $TEST_SCRATCH_DIR/blob)
  mv $TEST_SCRATCH_DIR/blob $TEST_SCRATCH_DIR/blobs/sha256/${layerdigest}

  # Create a configuration blob that doesn't include a "created" date.
  now=$(TZ=UTC date +%Y-%m-%dT%H:%M:%S.%NZ)
  arch=$(go env GOARCH)
  cat > $TEST_SCRATCH_DIR/blob << EOF
  {
    "architecture": "$arch",
    "os": "linux",
    "config": {
        "Env": [
            "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
        ],
        "Cmd": [
            "sh"
        ]
    },
    "rootfs": {
        "type": "layers",
        "diff_ids": [
            "sha256:${layerdigest}"
        ]
    },
    "history": [
        {
            "created": "${now}",
            "created_by": "/bin/sh -c #(nop) ADD file:${layerdigest} in / "
        }
    ]
  }
EOF
  configdigest=$(sha256sum $TEST_SCRATCH_DIR/blob | awk '{print $1}')
  configsize=$(stat -c %s $TEST_SCRATCH_DIR/blob)
  mv $TEST_SCRATCH_DIR/blob $TEST_SCRATCH_DIR/blobs/sha256/${configdigest}

  # Create a manifest for that configuration blob and layer.
  cat > $TEST_SCRATCH_DIR/blob << EOF
  {
    "schemaVersion": 2,
    "config": {
        "mediaType": "application/vnd.oci.image.config.v1+json",
        "digest": "sha256:${configdigest}",
        "size": ${configsize}
    },
    "layers": [
        {
            "mediaType": "application/vnd.oci.image.layer.v1.tar",
            "digest": "sha256:${layerdigest}",
            "size": ${layersize}
        }
    ]
  }
EOF
  manifestdigest=$(sha256sum $TEST_SCRATCH_DIR/blob | awk '{print $1}')
  manifestsize=$(stat -c %s $TEST_SCRATCH_DIR/blob)
  mv $TEST_SCRATCH_DIR/blob $TEST_SCRATCH_DIR/blobs/sha256/${manifestdigest}

  # Add the manifest to the image index.
  cat > $TEST_SCRATCH_DIR/index.json << EOF
  {
    "schemaVersion": 2,
    "manifests": [
        {
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "digest": "sha256:${manifestdigest}",
            "size": ${manifestsize}
        }
    ]
  }
EOF

  # Mark the directory as a layout directory.
  echo -n '{"imageLayoutVersion": "1.0.0"}' > $TEST_SCRATCH_DIR/oci-layout

  # Import the image.
  run_buildah pull oci:$TEST_SCRATCH_DIR

  # Inspect the image.  We shouldn't crash.
  run_buildah inspect ${configdigest}
  # List images.  We shouldn't crash.
  run_buildah images
}


@test "Test two image names" {
  _prefetch alpine busybox
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON alpine
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON busybox

  run_buildah 125 images --filter dangling=true alpine busybox
  expect_output "Error: 'buildah images' requires at most 1 argument"

  run_buildah 125 images alpine busybox
  expect_output "Error: 'buildah images' requires at most 1 argument"

  run_buildah 125 images --noheading alpine busybox
  expect_output "Error: 'buildah images' requires at most 1 argument"
}
