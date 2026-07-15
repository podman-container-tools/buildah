#!/usr/bin/env bats

load helpers

@test "config-flags-order-verification" {
  run_buildah 125 config cnt1 --author=user1
  check_options_flag_err "--author=user1"

  run_buildah 125 config cnt1 --arch x86_54
  check_options_flag_err "--arch"

  run_buildah 125 config cnt1 --created-by buildahcli --cmd "/usr/bin/run.sh" --hostname "localhost1"
  check_options_flag_err "--created-by"

  run_buildah 125 config cnt1 --annotation=service=cache
  check_options_flag_err "--annotation=service=cache"
}

@test "config-flags-verification" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --label LABEL $cid
  run_buildah config --annotation ANNOTATION $cid

  run_buildah 125 config --healthcheck 'AB "CD' $cid
  expect_output --substring 'parsing --healthcheck "AB \\"CD": invalid command line string'

  run_buildah 125 config --healthcheck-interval ABCD $cid
  expect_output --substring 'parsing --healthcheck-interval "ABCD": time: invalid duration "?ABCD"?'

  run_buildah 125 config --cmd 'AB "CD' $cid
  expect_output --substring 'parsing --cmd "AB \\"CD": invalid command line string'

  run_buildah 125 config --env ENV $cid
  expect_output --substring 'setting env "ENV": no value given'

  run_buildah 125 config --shell 'AB "CD' $cid
  expect_output --substring 'parsing --shell "AB \\"CD": invalid command line string'
}

function check_matrix() {
  local setting=$1
  local expect=$2

  # matrix test: all permutations of .Docker.* and .OCIv1.* in all image types
  for image in docker oci; do
      for which in Docker OCIv1; do
	run_buildah inspect --type=image --format "{{.$which.$setting}}" scratch-image-$image
	expect_output "$expect"
    done
  done
}

@test "config entrypoint using single element in JSON array (exec form)" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --entrypoint '[ "/ENTRYPOINT" ]' $cid
  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  check_matrix "Config.Entrypoint" '[/ENTRYPOINT]'
}

@test "config entrypoint using multiple elements in JSON array (exec form)" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --entrypoint '[ "/ENTRYPOINT", "ELEMENT2" ]' $cid
  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  check_matrix 'Config.Entrypoint' '[/ENTRYPOINT ELEMENT2]'
}

@test "config entrypoint using string (shell form)" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --entrypoint /ENTRYPOINT $cid
  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  check_matrix 'Config.Entrypoint' '[/bin/sh -c /ENTRYPOINT]'
}

@test "config --unsetlabel" {
  base=quay.io/libpod/testimage:20241011
  _prefetch $base
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON $base
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid with-created-by-label
  run_buildah config --unsetlabel created_by $cid
  run_buildah commit $WITH_POLICY_JSON $cid without-created-by-label

  run_buildah inspect --format '{{ index .Docker.Config.Labels "created_by"}}' with-created-by-label
  assert "$output" != "" "label should be set in base image"
  run_buildah inspect --format '{{ index .Docker.Config.Labels "created_by"}}' without-created-by-label
  assert "$output" == "" "created_by label should be removed"
}

@test "config --unsetannotation" {
  base=quay.io/libpod/testimage:20241011
  _prefetch $base
  run_buildah from --quiet --pull=false $WITH_POLICY_JSON $base
  cid=$output
  run_buildah commit $WITH_POLICY_JSON $cid with-name-annotation
  run_buildah config --unsetannotation org.opencontainers.image.base.name $cid
  run_buildah commit $WITH_POLICY_JSON $cid without-name-annotation

  run_buildah inspect --format '{{ index .ImageAnnotations "org.opencontainers.image.base.name"}}' with-name-annotation
  assert "$output" != "" "annotation should be set in base image"
  run_buildah inspect --format '{{ index .ImageAnnotations "org.opencontainers.image.base.name"}}' without-name-annotation
  assert "$output" == "" "name annotation should be removed"
}

@test "config --annotation layer:" {
  # Build a base image with per-layer annotations.
  local base_image=$(build_oci_image_with_layer_annotations \
    '"base1": "baseval1",' \
    '"base2": "baseval2"')

  run_buildah from --quiet $WITH_POLICY_JSON ${base_image}
  local cid=$output

  # Read the base layer annotations and save them for verification.
  run_buildah inspect --type=image --format '{{.Manifest}}' ${base_image}
  local base_annotations=$(jq '[.layers[].annotations]' <<<"$output")
  local base_layer_count=$(jq '.layers | length' <<<"$output")

  # Set 2 top-layer annotations.
  run_buildah config \
    --annotation layer:initial1=initialval1 \
    --annotation layer:initial2=initialval2 \
    $cid

  # Verify they are both set.
  run_buildah inspect --format '{{index .TopLayerAnnotations "initial1"}}' $cid
  expect_output "initialval1"
  run_buildah inspect --format '{{index .TopLayerAnnotations "initial2"}}' $cid
  expect_output "initialval2"

  # Clear all top-layer annotations.
  run_buildah config --annotation layer:- $cid
  run_buildah inspect --format '{{.TopLayerAnnotations}}' $cid
  expect_output "map[]"

  # Add 2 key-value pairs and 2 key-only annotations.
  run_buildah config \
    --annotation layer:removekv=removeval \
    --annotation layer:persistkv=persistval \
    $cid
  run_buildah config \
    --annotation layer:removek \
    --annotation layer:persistk \
    $cid

  # Verify all four are set.
  run_buildah inspect --format '{{index .TopLayerAnnotations "removekv"}}' $cid
  expect_output "removeval"
  run_buildah inspect --format '{{index .TopLayerAnnotations "persistkv"}}' $cid
  expect_output "persistval"
  run_buildah inspect --format '{{index .TopLayerAnnotations "removek"}}' $cid
  expect_output ""
  run_buildah inspect --format '{{index .TopLayerAnnotations "persistk"}}' $cid
  expect_output ""

  # Unset one key=value and one key-only.
  run_buildah config \
    --annotation layer:removekv- \
    --annotation layer:removek- \
    $cid

  # Verify remaining annotations and their values.
  run_buildah inspect --format '{{index .TopLayerAnnotations "persistkv"}}' $cid
  expect_output "persistval"
  run_buildah inspect --format '{{index .TopLayerAnnotations "persistk"}}' $cid
  expect_output ""
  run_buildah inspect --format '{{.TopLayerAnnotations}}' $cid
  assert "$output" != *"removekv"* "removekv should be removed"
  assert "$output" != *"removek"* "removek should be removed"

  # Add content so the new layer is not empty, then commit.
  run_buildah copy $cid /dev/null /testfile
  run_buildah commit $WITH_POLICY_JSON $cid committed-layer-annot

  # Verify that base layer annotations are preserved.
  run_buildah inspect --type=image --format '{{.Manifest}}' committed-layer-annot
  local committed_manifest="$output"

  for ((i = 0; i < base_layer_count; i++)); do
    local expected
    expected=$(jq -c ".[$i]" <<<"$base_annotations")
    run jq -c ".layers[$i].annotations" <<<"$committed_manifest"
    assert "$output" = "$expected" \
      "layer $i annotations should be preserved after commit"
  done

  # Verify that annotations not removed were committed on the top-most layer.
  run jq -r '.layers[1].annotations["persistkv"]' <<<"$committed_manifest"
  assert "$output" = "persistval" "top layer should have persistkv=persistval"
  run jq -r '.layers[1].annotations["persistk"]' <<<"$committed_manifest"
  assert "$output" = "" "top layer should have persistk with empty value"
  run jq '.layers[1].annotations | has("removekv")' <<<"$committed_manifest"
  assert "$output" = "false" "top layer should not have removekv (removed)"
  run jq '.layers[1].annotations | has("removek")' <<<"$committed_manifest"
  assert "$output" = "false" "top layer should not have removek (removed)"
}

@test "config set empty entrypoint doesn't wipe cmd" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --cmd "command" $cid
  run_buildah config --entrypoint "" $cid
  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  check_matrix 'Config.Cmd' '[command]'
}

@test "config cmd without entrypoint" {
  run_buildah from --pull-never $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config \
   --cmd COMMAND-OR-ARGS \
  $cid
  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  check_matrix 'Config.Cmd' '[COMMAND-OR-ARGS]'
  check_matrix 'Config.Entrypoint' '[]'
}

@test "config entrypoint with cmd" {
  run_buildah from --pull-never $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config \
   --entrypoint /ENTRYPOINT \
   --cmd COMMAND-OR-ARGS \
  $cid
  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  check_matrix 'Config.Cmd' '[COMMAND-OR-ARGS]'

  run_buildah from --pull-never $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config \
   --entrypoint /ENTRYPOINT \
  $cid

  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  check_matrix 'Config.Cmd' '[]'

  run_buildah config \
   --entrypoint /ENTRYPOINT \
   --cmd COMMAND-OR-ARGS \
  $cid

  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci
  check_matrix 'Config.Cmd' '[COMMAND-OR-ARGS]'

  run_buildah config \
   --entrypoint /ENTRYPOINT \
   --cmd '[ "/COMMAND", "ARG1", "ARG2"]' \
  $cid

  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci
  check_matrix 'Config.Cmd' '[/COMMAND ARG1 ARG2]'
}

@test "config remove all" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config \
   --port 12345 \
   --annotation ANNOTATION=VALUE1,VALUE2 \
   --env VARIABLE=VALUE1,VALUE2 \
   --volume /VOLUME \
   --label LABEL=VALUE \
  $cid
  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  run_buildah inspect --type=image --format '{{index .ImageAnnotations "ANNOTATION"}}' scratch-image-oci
  expect_output "VALUE1,VALUE2"
  run_buildah inspect              --format '{{index .ImageAnnotations "ANNOTATION"}}' $cid
  expect_output "VALUE1,VALUE2"
  check_matrix 'Config.ExposedPorts' 'map[12345:{}]'
  check_matrix 'Config.Env'          '[VARIABLE=VALUE1,VALUE2]'
  check_matrix 'Config.Labels.LABEL' 'VALUE'

  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config \
   --port - \
   --annotation - \
   --env - \
   --volume - \
   --label - \
  $cid

  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --created-annotation=false --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  run_buildah inspect --type=image --format '{{.ImageAnnotations}}'                      scratch-image-oci
  expect_output "map[]"
  run_buildah inspect              --format '{{.ImageAnnotations}}'                      $cid
  expect_output "map[]"
  check_matrix 'Config.ExposedPorts' 'map[]'
  check_matrix 'Config.Env'          '[]'
  check_matrix 'Config.Labels.LABEL' '<no value>'
}

@test "config" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config \
   --author TESTAUTHOR \
   --created-by COINCIDENCE \
   --arch amd64 \
   --os linux \
   --variant abc \
   --user likes:things \
   --port 12345 \
   --env VARIABLE=VALUE1,VALUE2 \
   --entrypoint /ENTRYPOINT \
   --cmd COMMAND-OR-ARGS \
   --comment INFORMATIVE \
   --history-comment PROBABLY-EMPTY \
   --volume /VOLUME \
   --workingdir /tmp \
   --label LABEL=VALUE \
   --label exec='podman run -it --mount=type=bind,bind-propagation=Z,source=foo,destination=bar /script buz'\
   --stop-signal SIGINT \
   --annotation ANNOTATION=VALUE1,VALUE2 \
   --shell /bin/arbitrarysh \
   --domainname mydomain.local \
   --hostname cleverhostname \
   --healthcheck "CMD /bin/true" \
   --healthcheck-start-period 5s \
   --healthcheck-start-interval 30s \
   --healthcheck-interval 6s \
   --healthcheck-timeout 7s \
   --healthcheck-retries 8 \
   --onbuild "RUN touch /foo" \
   --os-version "1.0" \
   --os-feature dynamic --os-feature - --os-feature removed --os-feature removed- --os-feature win32k \
  $cid

  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci

  check_matrix 'Author'       'TESTAUTHOR'
  check_matrix 'Architecture' 'amd64'
  check_matrix 'OS'           'linux'
  check_matrix 'Variant'      'abc'
  check_matrix 'OSVersion'    '1.0'
  check_matrix 'OSFeatures'   '[win32k]'

  run_buildah inspect --format '{{.ImageCreatedBy}}' $cid
  expect_output "COINCIDENCE"

  check_matrix 'Config.Cmd'          '[COMMAND-OR-ARGS]'
  check_matrix 'Config.Entrypoint'   '[/bin/sh -c /ENTRYPOINT]'
  check_matrix 'Config.Env'          '[VARIABLE=VALUE1,VALUE2]'
  check_matrix 'Config.ExposedPorts' 'map[12345:{}]'
  check_matrix 'Config.Labels.exec'  'podman run -it --mount=type=bind,bind-propagation=Z,source=foo,destination=bar /script buz'
  check_matrix 'Config.Labels.LABEL' 'VALUE'
  check_matrix 'Config.StopSignal'   'SIGINT'
  check_matrix 'Config.User'         'likes:things'
  check_matrix 'Config.Volumes'      "map[/VOLUME:{}]"
  check_matrix 'Config.WorkingDir'   '/tmp'

  run_buildah inspect --type=image --format '{{(index .Docker.History 0).Comment}}' scratch-image-docker
  expect_output "PROBABLY-EMPTY"
  run_buildah inspect --type=image --format '{{(index .OCIv1.History 0).Comment}}' scratch-image-docker
  expect_output "PROBABLY-EMPTY"
  run_buildah inspect --type=image --format '{{(index .Docker.History 0).Comment}}' scratch-image-oci
  expect_output "PROBABLY-EMPTY"
  run_buildah inspect --type=image --format '{{(index .OCIv1.History 0).Comment}}' scratch-image-oci
  expect_output "PROBABLY-EMPTY"

  # The following aren't part of the Docker v2 spec, so they're discarded when we save to Docker format.
  run_buildah inspect --type=image --format '{{index .ImageAnnotations "ANNOTATION"}}'   scratch-image-oci
  expect_output "VALUE1,VALUE2"
  run_buildah inspect              --format '{{index .ImageAnnotations "ANNOTATION"}}'   $cid
  expect_output "VALUE1,VALUE2"
  run_buildah inspect --type=image --format '{{.Docker.Comment}}'                        scratch-image-docker
  expect_output "INFORMATIVE"
  run_buildah inspect --type=image --format '{{.Docker.Config.Domainname}}'              scratch-image-docker
  expect_output "mydomain.local"
  run_buildah inspect --type=image --format '{{.Docker.Config.Hostname}}'                scratch-image-docker
  expect_output "cleverhostname"
  run_buildah inspect --type=image --format '{{.Docker.Config.Shell}}'                   scratch-image-docker
  expect_output "[/bin/arbitrarysh]"
  run_buildah inspect               -f      '{{.Docker.Config.Healthcheck.Test}}'        scratch-image-docker
  expect_output "[CMD /bin/true]"
  run_buildah inspect               -f      '{{.Docker.Config.Healthcheck.StartPeriod}}' scratch-image-docker
  expect_output "5s"
  run_buildah inspect               -f      '{{.Docker.Config.Healthcheck.StartInterval}}' scratch-image-docker
  expect_output "30s"
  run_buildah inspect               -f      '{{.Docker.Config.Healthcheck.Interval}}'    scratch-image-docker
  expect_output "6s"
  run_buildah inspect               -f      '{{.Docker.Config.Healthcheck.Timeout}}'     scratch-image-docker
  expect_output "7s"
  run_buildah inspect               -f      '{{.Docker.Config.Healthcheck.Retries}}'     scratch-image-docker
  expect_output "8"
  run_buildah inspect               -f      '{{.Docker.Config.OnBuild}}'                 scratch-image-docker
  expect_output "[RUN touch /foo]"
  rm -rf /VOLUME
}

@test "config env using local environment" {
  export foo=bar
  run_buildah from --pull-never $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --env 'foo' $cid
  run_buildah commit --format docker $WITH_POLICY_JSON $cid env-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid env-image-oci

  run_buildah inspect --type=image --format '{{.Docker.Config.Env}}' env-image-docker
  expect_output --substring "foo=bar"

  run_buildah inspect --type=image --format '{{.OCIv1.Config.Env}}' env-image-docker
  expect_output --substring "foo=bar"
}

@test "docker formatted builds must inherit healthcheck from base image" {
  _prefetch busybox
  ctxdir=${TEST_SCRATCH_DIR}/bud
  mkdir -p $ctxdir
  cat >$ctxdir/Dockerfile <<EOF
FROM busybox
HEALTHCHECK CMD curl --fail http://localhost:3000 || exit 1
EOF

  run_buildah build --format docker $WITH_POLICY_JSON -t test ${ctxdir}

  cat >$ctxdir/Dockerfile <<EOF
FROM test
RUN echo hello
EOF

  run_buildah build --format docker $WITH_POLICY_JSON -t test2 ${ctxdir}
  run_buildah inspect --type=image --format '{{.Docker.ContainerConfig.Healthcheck.Test}}' test2
  expect_output --substring "localhost:3000"
}

@test "config env using --env expansion" {
  run_buildah from --pull-never $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config --env 'foo=bar' --env 'foo1=bar1' $cid
  run_buildah config --env 'combined=$foo/${foo1}' $cid
  run_buildah commit --format docker $WITH_POLICY_JSON $cid env-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid env-image-oci

  run_buildah inspect --type=image --format '{{.Docker.Config.Env}}' env-image-docker
  expect_output --substring "combined=bar/bar1"

  run_buildah inspect --type=image --format '{{.OCIv1.Config.Env}}' env-image-docker
  expect_output --substring "combined=bar/bar1"
}

@test "user" {
  _prefetch alpine
  run_buildah from --quiet --pull $WITH_POLICY_JSON alpine
  cid=$output
  run_buildah run $cid grep CapBnd /proc/self/status
  bndoutput=$output
  run_buildah config --user 1000 $cid
  run_buildah run $cid id -u
  expect_output "1000"

  run_buildah run $cid sh -c "grep CapEff /proc/self/status | cut -f2"
  expect_output "0000000000000000"

  run_buildah run $cid grep CapBnd /proc/self/status
  expect_output "$bndoutput"
}

@test "remove configs using '-' syntax" {
  run_buildah from $WITH_POLICY_JSON scratch
  cid=$output
  run_buildah config \
   --created-by COINCIDENCE \
   --volume /VOLUME \
   --env VARIABLE=VALUE1,VALUE2 \
   --label LABEL=VALUE \
   --port 12345 \
   --annotation ANNOTATION=VALUE1,VALUE2 \
  $cid

  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci
  run_buildah inspect --format '{{.ImageCreatedBy}}' $cid
  expect_output "COINCIDENCE"

  check_matrix 'Config.Volumes'      "map[/VOLUME:{}]"
  check_matrix 'Config.Env'          '[VARIABLE=VALUE1,VALUE2]'
  check_matrix 'Config.Labels.LABEL' 'VALUE'
  check_matrix 'Config.ExposedPorts' 'map[12345:{}]'
  run_buildah inspect --type=image --format '{{index .ImageAnnotations "ANNOTATION"}}' scratch-image-oci
  expect_output "VALUE1,VALUE2"
  run_buildah inspect              --format '{{index .ImageAnnotations "ANNOTATION"}}' $cid
  expect_output "VALUE1,VALUE2"

  run_buildah config \
   --created-by COINCIDENCE \
   --volume /VOLUME- \
   --env VARIABLE- \
   --label LABEL- \
   --port 12345- \
   --annotation ANNOTATION- \
  $cid

  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci
  run_buildah inspect --format '{{.ImageCreatedBy}}' $cid
  expect_output "COINCIDENCE"
  check_matrix 'Config.Volumes'      'map[]'
  check_matrix 'Config.Env'          '[]'
  check_matrix 'Config.Labels.LABEL' '<no value>'
  check_matrix 'Config.ExposedPorts' 'map[]'
  run_buildah inspect --type=image --format '{{index .ImageAnnotations "ANNOTATION"}}' scratch-image-oci
  expect_output ""
  run_buildah inspect              --format '{{index .ImageAnnotations "ANNOTATION"}}' $cid
  expect_output ""

  run_buildah config \
   --created-by COINCIDENCE \
   --volume /VOLUME- \
   --env VARIABLE=VALUE1,VALUE2 \
   --label LABEL=VALUE \
   --annotation ANNOTATION=VALUE1,VALUE2 \
  $cid

  run_buildah commit --format docker $WITH_POLICY_JSON $cid scratch-image-docker
  run_buildah commit --format oci $WITH_POLICY_JSON $cid scratch-image-oci
  run_buildah inspect --format '{{.ImageCreatedBy}}' $cid
  expect_output "COINCIDENCE"

  check_matrix 'Config.Volumes'      "map[]"
}
