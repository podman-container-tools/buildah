#!/usr/bin/env bash

# Directory in which tests live
TEST_SOURCES=${TEST_SOURCES:-$(dirname ${BASH_SOURCE})}

BUILDAH_BINARY=${BUILDAH_BINARY:-$TEST_SOURCES/../bin/buildah}
IMGTYPE_BINARY=${IMGTYPE_BINARY:-$TEST_SOURCES/../bin/imgtype}
COPY_BINARY=${COPY_BINARY:-$TEST_SOURCES/../bin/copy}
TUTORIAL_BINARY=${TUTORIAL_BINARY:-$TEST_SOURCES/../bin/tutorial}
INET_BINARY=${INET_BINARY:-$TEST_SOURCES/../bin/inet}
DUMPSPEC_BINARY=${DUMPSPEC_BINARY:-$TEST_SOURCES/../bin/dumpspec}
CRASH_BINARY=${CRASH_BINARY:-$TEST_SOURCES/../bin/crash}
WAIT_BINARY=${WAIT_BINARY:-$TEST_SOURCES/../bin/wait}
PASSWD_BINARY=${PASSWD_BINARY:-$TEST_SOURCES/../bin/passwd}
GRPCNOOP_BINARY=${GRPCNOOP_BINARY:-$TEST_SOURCES/../bin/grpcnoop}
STORAGE_DRIVER=${STORAGE_DRIVER:-vfs}
PATH=$(dirname ${BASH_SOURCE})/../bin:${PATH}
# Default timeout for a buildah command.
BUILDAH_TIMEOUT=${BUILDAH_TIMEOUT:-300}

# Safe reliable unchanging test image
SAFEIMAGE_REGISTRY=${SAFEIMAGE_REGISTRY:-quay.io}
SAFEIMAGE_USER=${SAFEIMAGE_USER:-libpod}
SAFEIMAGE_NAME=${SAFEIMAGE_NAME:-testimage}
SAFEIMAGE_TAG=${SAFEIMAGE_TAG:-20221018}
SAFEIMAGE_DIGEST=sha256:c782d03c968791b10300fb15478f7555be560329c5182ea27cba5fc299c98911
SAFEIMAGE="${SAFEIMAGE:-$SAFEIMAGE_REGISTRY/$SAFEIMAGE_USER/$SAFEIMAGE_NAME:$SAFEIMAGE_TAG}"

# Prompt to display when logging buildah commands; distinguish root/rootless
_LOG_PROMPT='$'
if [ $(id -u) -eq 0 ]; then
    _LOG_PROMPT='#'
fi

# Shortcut for directory containing Containerfiles for bud.bats
BUDFILES=${TEST_SOURCES}/bud

# Used hundreds of times throughout all the tests
WITH_POLICY_JSON="--signature-policy ${TEST_SOURCES}/policy.json"

# We don't invoke gnupg directly in many places, but this avoids ENOTTY errors
# when we invoke it directly in batch mode, and CI runs us without a terminal
# attached.
export GPG_TTY=/dev/null

function setup(){
    setup_tests
}

function setup_tests() {
    pushd "$(dirname "$(readlink -f "$BASH_SOURCE")")"

    # $TEST_SCRATCH_DIR is a custom scratch directory for each @test,
    # but it is NOT EMPTY! It is the caller's responsibility to make
    # empty subdirectories as needed. All of it will be deleted upon
    # test completion.
    #
    # buildah/podman: "repository name must be lowercase".
    # me: "but it's a local file path, not a repository name!"
    # buildah/podman: "i dont care. no caps anywhere!"
    TEST_SCRATCH_DIR=$(mktemp -d --dry-run --tmpdir=${BATS_TMPDIR:-${TMPDIR:-/tmp}} buildah_tests.XXXXXX | tr A-Z a-z)
    if test -z "${TEST_SCRATCH_DIR}" ; then
        die error creating temporary directory
    fi
    mkdir --mode=0700 $TEST_SCRATCH_DIR

    mkdir -p ${TEST_SCRATCH_DIR}/{root,runroot,sigstore,registries.d}
    cat >${TEST_SCRATCH_DIR}/registries.d/default.yaml <<EOF
default-docker:
  sigstore-staging: file://${TEST_SCRATCH_DIR}/sigstore
docker:
  registry.access.redhat.com:
    sigstore: https://access.redhat.com/webassets/docker/content/sigstore
  registry.redhat.io:
    sigstore: https://registry.redhat.io/containers/sigstore
EOF

    # Common options for all buildah and podman invocations
    ROOTDIR_OPTS="--root ${TEST_SCRATCH_DIR}/root --runroot ${TEST_SCRATCH_DIR}/runroot --storage-driver ${STORAGE_DRIVER}"

    # When running in CI, use a local registry for all image pulls
    local cached=
    if [[ -n "$CI_USE_REGISTRY_CACHE" ]]; then
        cached="-cached"
    fi
    regconfopt="--registries-conf ${TEST_SOURCES}/registries$cached.conf"
    regconfdir="--registries-conf-dir ${TEST_SCRATCH_DIR}/registries.d"
    BUILDAH_REGISTRY_OPTS="${regconfopt} ${regconfdir} --short-name-alias-conf ${TEST_SCRATCH_DIR}/cache/shortnames.conf"
    COPY_REGISTRY_OPTS="${BUILDAH_REGISTRY_OPTS}"
    PODMAN_REGISTRY_OPTS="${regconfopt}"
}

function starthttpd() { # directoryspecs [working-directory-or-"" [certfile, keyfile]]
# directoryspecs is a comma-separated list of one or more of:
# * default content root directory (required)
# * locationPattern=path (map a location to a path - patterns are passed to a net/http.ServeMux, see documentation associated with the type)
# * locationPattern=workingDirectory:cgiBinary (map a location to a CGI binary started in a working directory)
# * locationPattern=workingDirectory:cgiBinary:envVars (as above, but with "key=value" environment variables set, and "key" variables inherited)
    if test -n "$4" ; then
      if ! openssl req -newkey rsa:4096 -nodes -sha256 -keyout "$4" -x509 -days 2 -addext "subjectAltName = DNS:localhost,IP:0.0.0.0,IP:127.0.0.1" -out "$3" -subj "/CN=localhost" ; then
        die error creating new key and certificate
      fi
      chmod 644 "$3"
      chmod 600 "$4"
    fi
    pushd ${2:-${TEST_SCRATCH_DIR}} > /dev/null
    go build -o serve ${TEST_SOURCES}/serve/serve.go
    portfile=$(mktemp)
    if test -z "${portfile}"; then
        die error creating temporary file
    fi
    pidfile=$(mktemp)
    if test -z "${pidfile}"; then
        die error creating temporary file
    fi
    sh -c "./serve \"${1:-${BATS_TMPDIR}}\" 0 \"${portfile}\" \"${3}\" \"${4}\" ${pidfile} &"
    waited=0
    while ! test -s ${pidfile} ; do
        sleep 0.1
        if test $((++waited)) -ge 300 ; then
            die test http server did not write pid file within timeout
        fi
    done
    HTTP_SERVER_PID=$(< ${pidfile})
    rm -f ${pidfile}
    waited=0
    while ! test -s ${portfile} ; do
        sleep 0.1
        if test $((++waited)) -ge 300 ; then
            die test http server did not start listening within timeout
        fi
    done
    HTTP_SERVER_PORT=$(< ${portfile})
    rm -f ${portfile}
    popd > /dev/null
}

function stophttpd() {
    if test -n "$HTTP_SERVER_PID" ; then
        kill -HUP ${HTTP_SERVER_PID}
        unset HTTP_SERVER_PID
        unset HTTP_SERVER_PORT
    fi
    true
}

function teardown(){
    teardown_tests
}

function teardown_tests() {
    stophttpd
    stop_git_daemon
    stop_registry

    # Workaround for #1991 - buildah + overlayfs leaks mount points.
    # Many tests leave behind /var/tmp/.../root/overlay and sub-mounts;
    # let's find those and clean them up, otherwise 'rm -rf' fails.
    # 'sort -r' guarantees that we umount deepest subpaths first.
    mount |\
        awk '$3 ~ testdir { print $3 }' testdir="^${TEST_SCRATCH_DIR}/" |\
        sort -r |\
        xargs --no-run-if-empty --max-lines=1 umount

    rm -fr ${TEST_SCRATCH_DIR}

    popd
}

function normalize_image_name() {
    for img in "$@"; do
        if [[ "${img##*/}" == "$img" ]] ; then
            echo -n docker.io/library/"$img"
        elif [[ docker.io/"${img##*/}" == "$img" ]] ; then
            echo -n docker.io/library/"${img##*/}"
        else
            echo -n "$img"
        fi
    done
}

function _prefetch() {
    if [ -z "${_BUILDAH_IMAGE_CACHEDIR}" ]; then
        export _BUILDAH_IMAGE_CACHEDIR=${BATS_SUITE_TMPDIR}/buildah-image-cache
        mkdir -p ${_BUILDAH_IMAGE_CACHEDIR}

        # It's 700 by default; that prevents 'unshare' from reading cached images
        chmod 711 ${BATS_SUITE_TMPDIR:?is unset} ${BATS_SUITE_TMPDIR}/..
    fi

    local storage=
    for img in "$@"; do
        if [[ "$img" =~ '[vfs@' ]] ; then
            storage="$img"
            continue
        fi
        img=$(normalize_image_name "$img")
        echo "# [checking for: $img]" >&2
        fname=$(tr -c a-zA-Z0-9.- - <<< "$img")
        ( flock --timeout 300 9 || die "Could not flock"; _prefetch_locksafe $img $fname ) 9> $_BUILDAH_IMAGE_CACHEDIR/$fname.lock
    done
}

# DO NOT CALL THIS. EVER. This must only be called from _prefetch().
function _prefetch_locksafe() {
    local img="$1"
    local fname="$2"

    if [ -d $_BUILDAH_IMAGE_CACHEDIR/$fname ]; then
        echo "# [restoring from cache: $_BUILDAH_IMAGE_CACHEDIR / $img]" >&2
        copy dir:$_BUILDAH_IMAGE_CACHEDIR/$fname containers-storage:"$storage""$img"
    else
        rm -fr ${_BUILDAH_IMAGE_CACHEDIR:?THIS CAN NEVER HAPPEN}/$fname
        echo "# [copy docker://$img dir:$_BUILDAH_IMAGE_CACHEDIR/$fname]" >&2
        for attempt in $(seq 3) ; do
            if copy $COPY_REGISTRY_OPTS docker://"$img" dir:$_BUILDAH_IMAGE_CACHEDIR/$fname ; then
                break
            else
                # Failed. Clean up, so we don't leave incomplete remnants
                rm -fr ${_BUILDAH_IMAGE_CACHEDIR:?THIS CAN NEVER HAPPEN EITHER}/$fname
            fi
            sleep 5
        done
        echo "# [copy dir:$_BUILDAH_IMAGE_CACHEDIR/$fname containers-storage:$storage$img]" >&2
        copy dir:$_BUILDAH_IMAGE_CACHEDIR/$fname containers-storage:"$storage""$img"
    fi
}

function createrandom() {
    dd if=/dev/urandom bs=1 count=${2:-256} of=${1:-${BATS_TMPDIR}/randomfile} status=none
}

###################
#  random_string  #  Returns a pseudorandom human-readable string
###################
#
# Numeric argument, if present, is desired length of string
#
function random_string() {
    local length=${1:-10}

    head /dev/urandom | tr -dc a-zA-Z0-9 | head -c$length
}

##############
#  safename  #  Returns a pseudorandom string suitable for container/image/etc names
##############
#
# Name will include the bats test number and a pseudorandom element,
# eg "t123-xyz123". safename() will return the same string across
# multiple invocations within a given test; this makes it easier for
# a maintainer to see common name patterns.
#
# String is lower-case so it can be used as an image name
#
function safename() {
    safenamepath=$BATS_SUITE_TMPDIR/.safename.$BATS_SUITE_TEST_NUMBER
    if [[ ! -e $safenamepath ]]; then
        echo -n "t${BATS_SUITE_TEST_NUMBER}-$(random_string 8 | tr A-Z a-z)" >$safenamepath
    fi
    cat $safenamepath
}

function buildah() {
    ${BUILDAH_BINARY} ${BUILDAH_REGISTRY_OPTS} ${ROOTDIR_OPTS} "$@"
}

function imgtype() {
    ${IMGTYPE_BINARY} ${ROOTDIR_OPTS} "$@"
}

function copy() {
    ${COPY_BINARY} --max-parallel-downloads=1 ${ROOTDIR_OPTS} ${BUILDAH_REGISTRY_OPTS} "$@"
}

function podman() {
    command ${PODMAN_BINARY:-podman} ${PODMAN_REGISTRY_OPTS} ${ROOTDIR_OPTS} "$@"
}

# There are various scenarios where we would like to execute `tests` as rootless user, however certain commands like `buildah mount`
# do not work in rootless session since a normal user cannot mount a filesystem unless they're in a user namespace along with its
# own mount namespace. In order to run such specific commands from a rootless session we must perform `buildah unshare`.
# Following function makes sure that invoked command is triggered inside a `buildah unshare` session if env is rootless.
function run_unshared() {
    if is_rootless; then
        $BUILDAH_BINARY unshare "$@"
    else
        command "$@"
    fi
}

function mkdir() {
    run_unshared mkdir "$@"
}

function touch() {
    run_unshared touch "$@"
}

function cp() {
    run_unshared cp "$@"
}

function rm() {
    run_unshared rm "$@"
}

#################
#  run_with_log  #  Logs command before running it
#################
#
function run_with_log() {
    local expected_rc=0
    local retry=1
    local cmd="$*"
    case "$1" in
        [0-9])           expected_rc=$1; shift;;
        [1-9][0-9])      expected_rc=$1; shift;;
        [12][0-9][0-9])  expected_rc=$1; shift;;
        '?')             expected_rc=  ; shift;;  # ignore exit code
        --retry)         retry=3;        shift;;  # retry with sleep of 1 sec
    esac
    while [ $retry -gt 0 ]; do
        retry=$(( retry - 1 ))
        echo "$_LOG_PROMPT $cmd"
        run "$@"
        echo "$output"
        if [ "$status" -ne 0 ]; then
            echo -n "[ rc=$status ";
            if [ -n "$expected_rc" ]; then
                if [ "$status" -eq "$expected_rc" ]; then
                    echo -n "(expected) ";
                else
                    echo -n "(** EXPECTED $expected_rc **) ";
                fi
            fi
            echo "]"
        fi
        if [ -n "$expected_rc" ]; then
            if [ "$status" -eq "$expected_rc" ]; then
                return
            elif [ $retry -gt 0 ]; then
                echo "[ RETRYING ]" >&2
                sleep 1
            else
                die "exit code is $status; expected $expected_rc"
            fi
        fi
    done
}

#################
#  run_buildah  #  Invoke buildah, with timeout, using BATS 'run'
#################
#
# This is the preferred mechanism for invoking buildah:
#
#  * we use 'timeout' to abort (with a diagnostic) if something
#    takes too long; this is preferable to a CI hang.
#  * we log the command run and its output. This doesn't normally
#    appear in BATS output, but it will if there's an error.
#  * we check exit status. Since the normal desired code is 0,
#    that's the default; but the first argument can override:
#
#     run_buildah 125  nonexistent-subcommand
#     run_buildah '?'  some-other-command       # let our caller check status
#
# Since we use the BATS 'run' mechanism, $output and $status will be
# defined for our caller.
#
function run_buildah() {
    # Number as first argument = expected exit code; default 0
    # --retry as first argument = retry 3 times on error (eg registry flakes)
    local expected_rc=0
    local retry=1
    case "$1" in
        [0-9])           expected_rc=$1; shift;;
        [1-9][0-9])      expected_rc=$1; shift;;
        [12][0-9][0-9])  expected_rc=$1; shift;;
        '?')             expected_rc=  ; shift;;  # ignore exit code
        --retry)         retry=3;        shift;;  # retry network flakes
    esac

    # Remember command args, for possible use in later diagnostic messages
    MOST_RECENT_BUILDAH_COMMAND="buildah $*"

    # If session is rootless and `buildah mount` is invoked, perform unshare,
    # since normal user cannot mount a filesystem unless they're in a user namespace along with its own mount namespace.
    if is_rootless; then
        if [[ "$1" =~ mount ]]; then
            set "unshare" "$BUILDAH_BINARY" ${BUILDAH_REGISTRY_OPTS} ${ROOTDIR_OPTS} "$@"
        fi
    fi

    while [ $retry -gt 0 ]; do
        retry=$(( retry - 1 ))

        # stdout is only emitted upon error; this echo is to help a debugger
        echo "${_LOG_PROMPT} ${BUILDAH_BINARY} $*"
        run env CONTAINERS_CONF=${CONTAINERS_CONF:-$(dirname ${BASH_SOURCE})/containers.conf} timeout --foreground --kill=10 $BUILDAH_TIMEOUT ${BUILDAH_BINARY} ${BUILDAH_REGISTRY_OPTS} ${ROOTDIR_OPTS} "$@"
        # without "quotes", multiple lines are glommed together into one
        if [ -n "$output" ]; then
            echo "$output"
        fi
        if [ "$status" -ne 0 ]; then
            echo -n "[ rc=$status ";
            if [ -n "$expected_rc" ]; then
                if [ "$status" -eq "$expected_rc" ]; then
                    echo -n "(expected) ";
                else
                    echo -n "(** EXPECTED $expected_rc **) ";
                fi
            fi
            echo "]"
        fi

        if [ "$status" -eq 124 -o "$status" -eq 137 ]; then
            # FIXME: 'timeout -v' requires coreutils-8.29; travis seems to have
            #        an older version. If/when travis updates, please add -v
            #        to the 'timeout' command above, and un-comment this out:
            # if expr "$output" : ".*timeout: sending" >/dev/null; then
            echo "*** TIMED OUT ***"
            # This does not get the benefit of a retry
            false
        fi

        if [ -n "$expected_rc" ]; then
            if [ "$status" -eq "$expected_rc" ]; then
                return
            elif [ $retry -gt 0 ]; then
                echo "[ RETRYING ]" >&2
                sleep 30
            else
                die "exit code is $status; expected $expected_rc"
            fi
        fi
    done
}

######################
#  find_oci_runtime  #  Sets $OCI to the command for the OCI runtime, for invoking it directly
######################
function find_oci_runtime() {
    run_buildah info --format '{{.host.OCIRuntime}}'
    OCI="$output"
}

#########
#  die  #  Abort with helpful message
#########
function die() {
    echo "#/vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv"  >&2
    echo "#| FAIL: $*"                                           >&2
    echo "#\\^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^" >&2
    false
}

############
#  assert  #  Compare actual vs expected string; fail if mismatch
############
#
# Compares string (default: $output) against the given string argument.
# By default we do an exact-match comparison against $output, but there
# are two different ways to invoke us, each with an optional description:
#
#      assert               "EXPECT" [DESCRIPTION]
#      assert "RESULT" "OP" "EXPECT" [DESCRIPTION]
#
# The first form (one or two arguments) does an exact-match comparison
# of "$output" against "EXPECT". The second (three or four args) compares
# the first parameter against EXPECT, using the given OPerator. If present,
# DESCRIPTION will be displayed on test failure.
#
# Examples:
#
#   assert "this is exactly what we expect"
#   assert "${lines[0]}" =~ "^abc"  "first line begins with abc"
#
function assert() {
    local actual_string="$output"
    local operator='=='
    local expect_string="$1"
    local testname="$2"

    case "${#*}" in
        0)   die "Internal error: 'assert' requires one or more arguments" ;;
        1|2) ;;
        3|4) actual_string="$1"
             operator="$2"
             expect_string="$3"
             testname="$4"
             ;;
        *)   die "Internal error: too many arguments to 'assert" ;;
    esac

    # Comparisons.
    # Special case: there is no !~ operator, so fake it via '! x =~ y'
    local not=
    local actual_op="$operator"
    if [[ $operator == '!~' ]]; then
        not='!'
        actual_op='=~'
    fi
    if [[ $operator == '=' || $operator == '==' ]]; then
        # Special case: we can't use '=' or '==' inside [[ ... ]] because
        # the right-hand side is treated as a pattern... and '[xy]' will
        # not compare literally. There seems to be no way to turn that off.
        if [ "$actual_string" = "$expect_string" ]; then
            return
        fi
    elif [[ $operator == '!=' ]]; then
        # Same special case as above
        if [ "$actual_string" != "$expect_string" ]; then
            return
        fi
    else
        if eval "[[ $not \$actual_string $actual_op \$expect_string ]]"; then
            return
        elif [ $? -gt 1 ]; then
            die "Internal error: could not process 'actual' $operator 'expect'"
        fi
    fi

    # Test has failed. Get a descriptive test name.
    if [ -z "$testname" ]; then
        testname="${MOST_RECENT_BUILDAH_COMMAND:-[no test name given]}"
    fi

    # Display optimization: the typical case for 'expect' is an
    # exact match ('='), but there are also '=~' or '!~' or '-ge'
    # and the like. Omit the '=' but show the others; and always
    # align subsequent output lines for ease of comparison.
    local op=''
    local ws=''
    if [ "$operator" != '==' ]; then
        op="$operator "
        ws=$(printf "%*s" ${#op} "")
    fi

    # This is a multi-line message, which may in turn contain multi-line
    # output, so let's format it ourself, readably
    local actual_split
    IFS=$'\n' read -rd '' -a actual_split <<<"$actual_string" || true
    printf "#/vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv\n"    >&2
    printf "#|     FAIL: %s\n" "$testname"                        >&2
    printf "#| expected: %s'%s'\n" "$op" "$expect_string"         >&2
    printf "#|   actual: %s'%s'\n" "$ws" "${actual_split[0]}"     >&2
    local line
    for line in "${actual_split[@]:1}"; do
        printf "#|         > %s'%s'\n" "$ws" "$line"              >&2
    done
    printf "#\\^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^\n"   >&2
    false
}

###################
#  expect_output  #  [obsolete; kept for compatibility]
###################
#
# An earlier version of assert().
#
function expect_output() {
    # By default we examine $output, the result of run_buildah
    local actual="$output"
    local operator='=='

    # option processing: recognize --from="...", --substring
    local opt
    for opt; do
        local value=$(expr "$opt" : '[^=]*=\(.*\)')
        case "$opt" in
            --from=*)       actual="$value";   shift;;
            --substring)    operator='=~';     shift;;
            --)             shift; break;;
            -*)             die "Invalid option '$opt'" ;;
            *)              break;;
        esac
    done

    assert "$actual" "$operator" "$@"
}

#######################
#  expect_line_count  #  Check the expected number of output lines
#######################
#
# ...from the most recent run_buildah command
#
function expect_line_count() {
    local expect="$1"
    local testname="${2:-${MOST_RECENT_BUILDAH_COMMAND:-[no test name given]}}"

    local actual="${#lines[@]}"
    if [ "$actual" -eq "$expect" ]; then
        return
    fi

    printf "#/vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv\n"          >&2
    printf "#| FAIL: $testname\n"                                       >&2
    printf "#| Expected %d lines of output, got %d\n" $expect $actual   >&2
    printf "#| Output was:\n"                                           >&2
    local line
    for line in "${lines[@]}"; do
        printf "#| >%s\n" "$line"                                       >&2
    done
    printf "#\\^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^\n"         >&2
    false
}

function check_options_flag_err() {
    flag="$1"
    [ "$status" -eq 125 ]
    [[ $output = *"no options ($flag) can be specified after"* ]]
}

#################
#  is_rootless  #  Check if we run as normal user
#################
function is_rootless() {
    [ "$(id -u)" -ne 0 ]
}

#############################
#  has_supplemental_groups  #  Check that account has additional groups
#############################
function has_supplemental_groups() {
    [ "$(id -g)" != "$(id -G)" ]
}

#################################
#  skip_if_rootless_environment # `mount` or its variant needs unshare
#################################
function skip_if_rootless_environment() {
    if is_rootless; then
        skip "${1:-test is being invoked from rootless environment and might need unshare}"
    fi
}

#################################
#  skip_if_root_environment     #
#################################
function skip_if_root_environment() {
    if ! is_rootless; then
        skip "${1:-test is being invoked from root environment}"
    fi
}

####################
#  skip_if_chroot  #
####################
function skip_if_chroot() {
    if test "$BUILDAH_ISOLATION" = "chroot"; then
        skip "${1:-test does not work when \$BUILDAH_ISOLATION = chroot}"
    fi
}

######################
#  skip_if_rootless  #
######################
function skip_if_rootless() {
    if test "$BUILDAH_ISOLATION" = "rootless"; then
        skip "${1:-test does not work when \$BUILDAH_ISOLATION = rootless}"
    fi
}


########################
#  skip_if_no_runtime  #  'buildah run' can't work without a runtime
########################
function skip_if_no_runtime() {
    find_oci_runtime

    if type -p "${OCI}" &> /dev/null; then
        return
    fi

    skip "runtime \"$OCI\" not found"
}

#######################
#  skip_if_no_podman  #  we need 'podman' to test how we interact with podman
#######################
function skip_if_no_podman() {
    run which ${PODMAN_BINARY:-podman}
    if [[ $status -ne 0 ]]; then
        skip "podman is not installed"
    fi
}


##########################
#  skip_if_in_container  #
##########################
function skip_if_in_container() {
    if test "$CONTAINER" = "podman"; then
        skip "This test is not working inside a container"
    fi
}

#######################
#  skip_if_no_docker  #
#######################
function skip_if_no_docker() {
  which docker                  || skip "docker is not installed"
  systemctl -q is-active docker || skip "docker.service is not active"

  # Confirm that this is really truly docker, not podman.
  docker_version=$(docker --version)
  if [[ $docker_version =~ podman ]]; then
    skip "this test needs actual docker, not podman-docker"
  fi
}

########################
#  skip_if_no_unshare  #
########################
function skip_if_no_unshare() {
  run which ${UNSHARE_BINARY:-unshare}
  if [[ $status -ne 0 ]]; then
    skip "unshare is not installed"
  fi
  if ! unshare -Ur true ; then
    skip "unshare was not able to create a user namespace"
  fi
  if ! unshare -Urm true ; then
    skip "unshare was not able to create a mount namespace"
  fi
  if ! unshare -Urmpf true ; then
    skip "unshare was not able to create a pid namespace"
  fi
  if ! unshare -U --map-users $(id -u),0,1 true ; then
    skip "unshare does not support --map-users"
  fi
  if ! unshare -Ur --setuid 0 true ; then
    skip "unshare does not support --setuid"
  fi
}

######################
#  skip_unless_arch  #
######################
function skip_unless_arch() {
  local goarch
  goarch="$(go env GOARCH 2>/dev/null)"

  [ -z "$goarch" ] && return 0

  for arch in "$@"; do
    if [ "$arch" = "$goarch" ]; then
      return 0
    fi
  done

  skip "test requires one of these architectures: $* (current: $goarch)"
}

######################
#  start_git_daemon  #
######################
function start_git_daemon() {
  daemondir=${TEST_SCRATCH_DIR}/git-daemon
  mkdir -p ${daemondir}/repo
  gzip -dc < ${1:-${TEST_SOURCES}/git-daemon/repo.tar.gz} | tar x -C ${daemondir}/repo

  # git >=2.45 aborts with "dubious ownership" error if serving other user's files as root
  if ! is_rootless; then
      chown -R root:root ${daemondir}/repo
  fi

  ${INET_BINARY} -port-file ${TEST_SCRATCH_DIR}/git-daemon/port -pid-file=${TEST_SCRATCH_DIR}/git-daemon/pid -- git daemon --inetd --base-path=${daemondir} ${daemondir} &

  local waited=0
  while ! test -s ${TEST_SCRATCH_DIR}/git-daemon/pid ; do
    sleep 0.1
    if test $((++waited)) -ge 300 ; then
      die test git server did not write pid file within timeout
    fi
  done
  GITPORT=$(< ${TEST_SCRATCH_DIR}/git-daemon/port)
}

#####################
#  stop_git_daemon  #
#####################
function stop_git_daemon() {
  if test -s ${TEST_SCRATCH_DIR}/git-daemon/pid ; then
    kill $(< ${TEST_SCRATCH_DIR}/git-daemon/pid)
    rm -f ${TEST_SCRATCH_DIR}/git-daemon/pid
  fi
}

####################
#  start_registry  #
####################
# Bring up a registry server using buildah with vfs and chroot as a cheap
# substitute for podman, accessible only to user $1 using password $2 on the
# local system at a dynamically-allocated port.
# Requires openssl.
# A user name and password can be supplied as the two parameters, or default
# values of "testuser" and "testpassword" will be used.
# Sets REGISTRY_PID, REGISTRY_PORT (to append to "localhost:"), and
# REGISTRY_DIR (where the CA cert can be found) on success.
function start_registry() {
  local testuser="${1:-testuser}"
  local testpassword="${2:-testpassword}"
  local REGISTRY_IMAGE=quay.io/libpod/registry:2.8.2
  local config='
version: 0.1
log:
  fields:
    service: registry
storage:
  cache:
    blobdescriptor: inmemory
  filesystem:
    rootdirectory: /var/lib/registry
http:
  addr: :0
  headers:
    X-Content-Type-Options: [nosniff]
  tls:
    certificate: /etc/docker/registry/localhost.crt
    key: /etc/docker/registry/localhost.key
health:
  storagedriver:
    enabled: true
    interval: 10s
    threshold: 3
auth:
  htpasswd:
    realm: buildah-realm
    path: /etc/docker/registry/htpasswd
'
  # roughly equivalent to "htpasswd -nbB testuser testpassword", the registry uses
  # the same package this does for verifying passwords against hashes in htpasswd files
  htpasswd=${testuser}:$(${PASSWD_BINARY} ${testpassword})

  # generate the htpasswd and config.yml files for the registry
  mkdir -p "${TEST_SCRATCH_DIR}"/registry/root "${TEST_SCRATCH_DIR}"/registry/run "${TEST_SCRATCH_DIR}"/registry/certs "${TEST_SCRATCH_DIR}"/registry/config
  cat > "${TEST_SCRATCH_DIR}"/registry/config/htpasswd <<< "$htpasswd"
  cat > "${TEST_SCRATCH_DIR}"/registry/config/config.yml <<< "$config"
  chmod 644 "${TEST_SCRATCH_DIR}"/registry/config/htpasswd "${TEST_SCRATCH_DIR}"/registry/config/config.yml

  # generate a new key and certificate
  if ! openssl req -newkey rsa:4096 -nodes -sha256 -keyout "${TEST_SCRATCH_DIR}"/registry/certs/localhost.key -x509 -days 2 -addext "subjectAltName = DNS:localhost" -out "${TEST_SCRATCH_DIR}"/registry/certs/localhost.crt -subj "/CN=localhost" ; then
    die error creating new key and certificate
  fi
  chmod 644 "${TEST_SCRATCH_DIR}"/registry/certs/localhost.crt
  chmod 600 "${TEST_SCRATCH_DIR}"/registry/certs/localhost.key
  # use a copy of the server's certificate for validation from a client
  cp "${TEST_SCRATCH_DIR}"/registry/certs/localhost.crt "${TEST_SCRATCH_DIR}"/registry/

  # create a container in its own storage
  _prefetch "[vfs@${TEST_SCRATCH_DIR}/registry/root+${TEST_SCRATCH_DIR}/registry/run]" ${REGISTRY_IMAGE}
  ctr=$(${BUILDAH_BINARY} --storage-driver vfs --root "${TEST_SCRATCH_DIR}"/registry/root --runroot "${TEST_SCRATCH_DIR}"/registry/run from --quiet --pull-never ${REGISTRY_IMAGE})
  ${BUILDAH_BINARY} --storage-driver vfs --root "${TEST_SCRATCH_DIR}"/registry/root --runroot "${TEST_SCRATCH_DIR}"/registry/run copy $ctr "${TEST_SCRATCH_DIR}"/registry/config/htpasswd "${TEST_SCRATCH_DIR}"/registry/config/config.yml "${TEST_SCRATCH_DIR}"/registry/certs/localhost.key "${TEST_SCRATCH_DIR}"/registry/certs/localhost.crt /etc/docker/registry/

  # fire it up
  coproc ${BUILDAH_BINARY} --storage-driver vfs --root "${TEST_SCRATCH_DIR}"/registry/root --runroot "${TEST_SCRATCH_DIR}"/registry/run run --net host "$ctr" /entrypoint.sh /etc/docker/registry/config.yml 2> "${TEST_SCRATCH_DIR}"/registry/registry.log

  # record the coprocess's ID and try to parse the listening port from the log
  # we're separating all of this from the storage for any test that might call
  # this function and using vfs to minimize the cleanup required
  REGISTRY_PID="${COPROC_PID}"
  REGISTRY_DIR="${TEST_SCRATCH_DIR}"/registry
  REGISTRY_PORT=
  local waited=0
  while [ -z "${REGISTRY_PORT}" ] ; do
    if [ $waited -ge $BUILDAH_TIMEOUT ] ; then
      echo Could not determine listening port from log:
      sed -e 's/^/  >/' ${TEST_SCRATCH_DIR}/registry/registry.log
      stop_registry
      false
    fi
    waited=$((waited+1))
    sleep 1
    REGISTRY_PORT=$(sed -ne 's^.*listening on.*:\([0-9]\+\),.*^\1^p' ${TEST_SCRATCH_DIR}/registry/registry.log)
  done

  # push the registry image we just started... to itself, as a confidence check
  if ! ${BUILDAH_BINARY} --storage-driver vfs --root "${REGISTRY_DIR}"/root --runroot "${REGISTRY_DIR}"/run push --cert-dir "${REGISTRY_DIR}" --creds "${testuser}":"${testpassword}" "${REGISTRY_IMAGE}" localhost:"${REGISTRY_PORT}"/registry; then
    echo error pushing to /registry repository at localhost:$REGISTRY_PORT
    stop_registry
    false
  fi
}

###################
#  stop_registry  #
###################
function stop_registry() {
  if test -n "${REGISTRY_PID}" ; then
    kill "${REGISTRY_PID}"
    wait "${REGISTRY_PID}" || true
  fi
  unset REGISTRY_PID
  unset REGISTRY_PORT
  if test -n "${REGISTRY_DIR}" ; then
    ${BUILDAH_BINARY} --storage-driver vfs --root "${REGISTRY_DIR}"/root --runroot "${REGISTRY_DIR}"/run rmi -a -f
    rm -fr "${REGISTRY_DIR}"
  fi
  unset REGISTRY_DIR
}

###############################
#  oci_image_manifest_digest  #
###############################
# prints the digest of the form "sha256:xxx" of the manifest for the main image
# in an OCI layout in "$1"
function oci_image_manifest_digest() {
  run jq -r '.manifests[0].digest' "$1"/index.json
  assert $status = 0 "looking for the digest of the image manifest"
  assert "$output" != ""
  echo "$output"
}

########################
#  oci_image_manifest  #
########################
# prints the relative path of the manifest for the main image in an OCI
# layout in "$1"
function oci_image_manifest() {
  local diff_id=$(oci_image_manifest_digest "$@")
  local alg=${diff_id%%:*}
  local val=${diff_id##*:}
  echo blobs/"$alg"/"$val"
}

#############################
#  oci_image_config_digest  #
#############################
# prints the digest of the form "sha256:xxx" of the config blob for the main
# image in an OCI layout in "$1"
function oci_image_config_digest() {
  local digest=$(oci_image_manifest_digest "$1")
  local alg=${digest%%:*}
  local val=${digest##*:}
  run jq -r '.config.digest' "$1"/blobs/"$alg"/"$val"
  assert $status = 0 "looking for the digest of the image config"
  assert "$output" != ""
  echo "$output"
}

######################
#  oci_image_config  #
######################
# prints the relative path of the config blob for the main image in an OCI
# layout in "$1"
function oci_image_config() {
  local diff_id=$(oci_image_config_digest "$@")
  local alg=${diff_id%%:*}
  local val=${diff_id##*:}
  echo blobs/"$alg"/"$val"
}

########################
#  oci_image_diff_ids  #
########################
# prints the list of digests of the diff IDs for the main image in an OCI
# layout in "$1"
function oci_image_diff_ids() {
  local digest=$(oci_image_config_digest "$1")
  local alg=${digest%%:*}
  local val=${digest##*:}
  run jq -r '.rootfs.diff_ids[]' "$1"/blobs/"$alg"/"$val"
  assert $status = 0 "looking for the diff IDs in the image config"
  assert "$output" != ""
  echo "$output"
}

#######################
#  oci_image_diff_id  #
#######################
# prints a single diff ID for the main image in an OCI layout in "$1", choosing
# which one to print based on an index and arithmetic operands passed in
# subsequent arguments
function oci_image_diff_id() {
  local diff_ids=($(oci_image_diff_ids "$1"))
  shift
  case "$*" in
      -*) echo ${diff_ids[$((${#diff_ids[@]} "$@"))]} ;;
      *) echo ${diff_ids[$(("$@"))]} ;;
  esac
}

############################
#  oci_image_last_diff_id  #
############################
# prints the diff ID of the most recent layer for the main image in an OCI
# layout in "$1"
function oci_image_last_diff_id() {
  local diff_id=($(oci_image_diff_id "$1" - 1))
  echo "$diff_id"
}

####################
#  oci_image_diff  #
####################
# prints the relative path of a single layer diff for the main image in an OCI
# layout in "$1", choosing which one to print based on an index and arithmetic
# operands passed in subsequent arguments
function oci_image_diff() {
  local diff_id=$(oci_image_diff_id "$@")
  local alg=${diff_id%%:*}
  local val=${diff_id##*:}
  echo blobs/"$alg"/"$val"
}

#########################
#  oci_image_last_diff  #
#########################
# prints the relative path of the most recent layer for the main image in an
# OCI layout in "$1"
function oci_image_last_diff() {
  local output=$(oci_image_diff "$1" - 1)
  echo "$output"
}

#############################
#  dir_image_config_digest  #
#############################
# prints the digest of the form "sha256:xxx" of the config blob for the "dir"
# image in "$1"
function dir_image_config_digest() {
  run jq -r '.config.digest' "$1"/manifest.json
  assert $status = 0 "looking for the digest of the image config"
  assert "$output" != ""
  echo "$output"
}

########################
#  dir_image_diff_ids  #
########################
# prints the list of digests of the diff IDs for the "dir" image in "$1"
function dir_image_diff_ids() {
  local digest=$(dir_image_config_digest "$1")
  local alg=${digest%%:*}
  local val=${digest##*:}
  run jq -r '.rootfs.diff_ids[]' "$1"/"$val"
  assert $status = 0 "looking for the diff IDs in the image config"
  assert "$output" != ""
  echo "$output"
}

#######################
#  dir_image_diff_id  #
#######################
# prints a single diff ID for the "dir" image in "$1", choosing which one to
# print based on an index and arithmetic operands passed in subsequent
# arguments
function dir_image_diff_id() {
  local diff_ids=($(dir_image_diff_ids "$1"))
  shift
  case "$*" in
      -*) echo ${diff_ids[$((${#diff_ids[@]} "$@"))]} ;;
      *) echo ${diff_ids[$(("$@"))]} ;;
  esac
}

############################
#  dir_image_last_diff_id  #
############################
# prints the diff ID of the most recent layer for "dir" image in "$1"
function dir_image_last_diff_id() {
  local diff_id=($(dir_image_diff_id "$1" - 1))
  echo "$diff_id"
}

######################
#  dir_image_config  #
######################
# prints the relative path of the config blob for the "dir" image in "$1"
function dir_image_config() {
  local diff_id=$(dir_image_config_digest "$@")
  local alg=${diff_id%%:*}
  local val=${diff_id##*:}
  echo "$val"
}

####################
#  dir_image_diff  #
####################
# prints the relative path of a single layer diff for the "dir" image in "$1",
# choosing which one to print based on an index and arithmetic operands passed
# in subsequent arguments
function dir_image_diff() {
  local diff_id=$(dir_image_diff_id "$@")
  local alg=${diff_id%%:*}
  local val=${diff_id##*:}
  echo "$val"
}

#########################
#  dir_image_last_diff  #
#########################
# prints the relative path of the most recent layer for "dir" image in "$1"
function dir_image_last_diff() {
  local output=$(dir_image_diff "$1" - 1)
  echo "$output"
}

####################################
#  convert_v1_shares_to_v2_weight  #
####################################
function convert_v1_shares_to_v2_weight() {
  # https://kubernetes.io/blog/2026/01/30/new-cgroup-v1-to-v2-cpu-conversion-formula/
  # there's an old way to convert the value, and a new way to convert the value, and we
  # don't know which one our runtime is using, so return the values we would get from
  # using both methods
  local shares="$1"
  local oldconverted="$((1 + ((${shares} - 2) * 9999) / 262142))"
  test -n "$oldconverted"
  local newconverted=$(awk '{if ($1 <= 2) { print "1"} else if ($1 >= 262144) {print "10000"} else {l=log($1)/log(2); e=((((l+125)*l)/612.0) - 7.0/34.0); p = exp(e*log(10)); if ( p == int(p) ) {print p} else { print int(p+1) }}}' <<< "${shares}")
  test -n "$newconverted"
  echo "$oldconverted" "$newconverted"
}

################################################
#  build_oci_image_with_layer_annotations      #
################################################
# Build a minimal single-layer OCI image with per-layer annotations,
# pull it into storage, and echo the image ID (configdigest).
# Usage: local img=$(build_oci_image_with_layer_annotations '"key1": "val1", "key2": "val2"')
function build_oci_image_with_layer_annotations() {
  local annotations="$*"
  local imgdir=${TEST_SCRATCH_DIR}/oci-image
  mkdir -p ${imgdir}/blobs/sha256

  # Create a layer.
  dd if=/dev/zero bs=512 count=2 of=${imgdir}/blob 2>/dev/null
  local layerdigest=$(sha256sum ${imgdir}/blob | awk '{print $1}')
  local layersize=$(stat -c %s ${imgdir}/blob)
  mv ${imgdir}/blob ${imgdir}/blobs/sha256/${layerdigest}

  # Create a configuration blob.
  local arch=$(go env GOARCH)
  cat > ${imgdir}/blob << EOF
  {
    "architecture": "$arch",
    "os": "linux",
    "config": {},
    "rootfs": {
        "type": "layers",
        "diff_ids": [
            "sha256:${layerdigest}"
        ]
    },
    "history": [
        {
            "created_by": "test layer"
        }
    ]
  }
EOF
  local configdigest=$(sha256sum ${imgdir}/blob | awk '{print $1}')
  local configsize=$(stat -c %s ${imgdir}/blob)
  mv ${imgdir}/blob ${imgdir}/blobs/sha256/${configdigest}

  # Create a manifest with layer annotations.
  cat > ${imgdir}/blob << EOF
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
            "size": ${layersize},
            "annotations": {
                ${annotations}
            }
        }
    ]
  }
EOF
  local manifestdigest=$(sha256sum ${imgdir}/blob | awk '{print $1}')
  local manifestsize=$(stat -c %s ${imgdir}/blob)
  mv ${imgdir}/blob ${imgdir}/blobs/sha256/${manifestdigest}

  # Add the manifest to the image index.
  cat > ${imgdir}/index.json << EOF
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

  echo -n '{"imageLayoutVersion": "1.0.0"}' > ${imgdir}/oci-layout

  run_buildah pull $WITH_POLICY_JSON oci:${imgdir} >&2
  assert $status = 0 "pulling OCI image with layer annotations"
  echo "${configdigest}"
}
