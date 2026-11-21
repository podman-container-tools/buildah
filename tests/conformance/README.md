![buildah logo](https://cdn.rawgit.com/containers/buildah/main/logos/buildah-logo_large.png)

# Buildah/Docker Conformance Test Suite

The conformance test for buildah is used to verify the images built with Buildah are equivalent to those built by Docker.  It does this by building an image using the version of buildah library that's being tested, building what should be the same image using the docker engine's build API, and comparing them.

## Installing dependencies

The additional dependencies for conformance testing are:
  * docker
  * docker-buildx-plugin (or an otherwise working `docker buildx` for tests that use `dockerBuildCLI`)

### Install Docker CE

Conformance tests use Docker CE to build images to be compared with images built with Buildah.  Install Docker CE with dnf, yum or apt-get, based on your distribution and verify that the `docker` service is started.  In Fedora, RHEL and CentOS `docker` or `moby-engine` rather than Docker CE may be installed by default.  In Debian or Ubuntu you may instead have the `docker.io` package.  Please verify that you install at least version 19.03.

Most cases call the Docker engine API through go-dockerclient and send a client-side context tar packed with `github.com/moby/go-archive` (same as go-dockerclient’s `ContextDir` helper).  A subset of `.dockerignore` regression tests set `dockerBuildCLI` and run `docker build` on the context directory with BuildKit (`DOCKER_BUILDKIT=1`), which needs the buildx component on current Docker packages.  Those cases do not depend on that client-side tar filter.  Verify with:

```
docker buildx version
```

CI installs the cached `docker-buildx-plugin` package when running conformance on Debian (`hack/ci/runner.sh`).

## Run conformance tests

These are the base images used by various conformance tests:
```
bash
docker pull mirror.gcr.io/golang
docker pull mirror.gcr.io/alpine
docker pull mirror.gcr.io/busybox
docker pull quay.io/fedora/python-311:latest
docker pull quay.io/libpod/centos:7
docker pull quay.io/libpod/ubuntu:latest
docker pull quay.io/libpod/busybox@sha256:32968e717e29f79e5b889721908b3be6d1573992e1f3ba4c9a41718e2728458c
docker pull quay.io/libpod/busybox@sha256:1faaf7a75319417261267b3dcef6a2b14c8c5122d7fcc7abeb07a32bc19728fd
```

This test program is used as input in a few of the conformance tests:
```
make tests/conformance/testdata/mount-targets/true
```

You can run all of the tests with go test (and under `buildah unshare` or `podman unshare` if you're not root):
```
go test -v -timeout=30m -tags "$(./btrfs_installed_tag.sh)" ./tests/conformance
```

If you want to run one of the test cases you can use the "-run" flag:
```
go test -v -timeout=30m -tags "$(./btrfs_installed_tag.sh)" -run TestConformance/shell ./tests/conformance
```

To run only the `.dockerignore` wildcard-negation conformance cases (API + `dockerBuildCLI`):
```
go test -v -count=1 -timeout=30m -tags "$(./btrfs_installed_tag.sh)" ./tests/conformance -run \
'TestConformance/copy-integration1$|TestConformance/dockerignore-allowlist-wildcard-negation$|TestConformance/dockerignore-exceptions-skip$|TestConformance/dockerignore-exclude-kind-of-deep-subdir|TestConformance/dockerignore-exclude-deep-subdir'
```

From the `tests/conformance` directory, drop the `./tests/conformance` path and run `go test` in that directory instead (same `-run` pattern).

If you also want to build and compare on a line-by-line basis, run:
```
go test -v -timeout=60m -tags "$(./btrfs_installed_tag.sh)" ./tests/conformance -compare-layers
```
