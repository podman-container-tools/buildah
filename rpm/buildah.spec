%global with_debug 1

%if 0%{?with_debug}
%global _find_debuginfo_dwz_opts %{nil}
%global _dwz_low_mem_die_limit 0
%else
%global debug_package   %{nil}
%endif

%global gomodulesmode GO111MODULE=on

%if %{defined fedora}
%define build_with_btrfs 1
%if 0%{?fedora} >= 43
%define sequoia 1
%endif
%endif

%if %{defined rhel}
%define fips 1
%endif

%global git0 https://github.com/containers/%{name}

Name: buildah
# Set different Epoch for copr
%if %{defined copr_username}
Epoch: 102
%else
Epoch: 2
%endif
# DO NOT TOUCH the Version string!
# The TRUE source of this specfile is:
# https://github.com/containers/skopeo/blob/main/rpm/skopeo.spec
# If that's what you're reading, Version must be 0, and will be updated by Packit for
# copr and koji builds.
# If you're reading this on dist-git, the version is automatically filled in by Packit.
Version: 0
# The `AND` needs to be uppercase in the License for SPDX compatibility
License: Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause AND ISC AND MIT AND MPL-2.0
Release: %autorelease
%if %{defined golang_arches_future}
ExclusiveArch: %{golang_arches_future}
%else
ExclusiveArch: aarch64 ppc64le s390x x86_64
%endif
Summary: A command line tool used for creating OCI Images
URL: https://%{name}.io
# Tarball fetched from upstream
Source: %{git0}/archive/v%{version}.tar.gz
BuildRequires: device-mapper-devel
BuildRequires: git-core
BuildRequires: golang >= 1.16.6
BuildRequires: glib2-devel
BuildRequires: glibc-static
%if !%{defined gobuild}
BuildRequires: go-rpm-macros
%endif
BuildRequires: gpgme-devel
BuildRequires: libassuan-devel
BuildRequires: make
%if %{defined build_with_btrfs}
BuildRequires: btrfs-progs-devel
%endif
BuildRequires: shadow-utils-subid-devel
BuildRequires: sqlite-devel
Requires: containers-common-extra
%if %{defined fedora}
BuildRequires: libseccomp-static
%else
BuildRequires: libseccomp-devel
%endif
%if %{defined fedora}
BuildRequires: binutils-aarch64-linux-gnu binutils-powerpc64le-linux-gnu binutils-ppc64le-linux-gnu binutils-riscv64-linux-gnu binutils-s390x-linux-gnu binutils-x86_64-linux-gnu
%endif
Requires: libseccomp >= 2.4.1-0
Suggests: cpp
%if %{defined sequoia}
Requires: podman-sequoia
%endif

%description
The %{name} package provides a command line tool which can be used to
* create a working container from scratch
or
* create a working container from an image as a starting point
* mount/umount a working container's root file system for manipulation
* save container's root file system layer to create a new image
* delete a working container or an image

# This subpackage is only intended for CI testing.
# Not meant for end user/customer usage.
%package tests
Summary: Tests for %{name}

Requires: %{name} = %{epoch}:%{version}-%{release}
%if %{defined bats_epel}
Requires: bats
%else
Recommends: bats
%endif
Requires: bzip2
Requires: xz
Requires: zstd
Requires: which
Requires: podman
Requires: golang
Requires: jq
Requires: httpd-tools
Requires: openssl
Requires: nmap-ncat
Requires: git-daemon
Requires: libselinux-utils

%description tests
%{summary}

This package contains system tests for %{name}

%prep
%autosetup -Sgit -n %{name}-%{version}

%build
%set_build_flags
export CGO_CFLAGS=$CFLAGS

# These extra flags present in $CFLAGS have been skipped for now as they break the build
CGO_CFLAGS=$(echo $CGO_CFLAGS | sed 's/-flto=auto//g')
CGO_CFLAGS=$(echo $CGO_CFLAGS | sed 's/-Wp,D_GLIBCXX_ASSERTIONS//g')
CGO_CFLAGS=$(echo $CGO_CFLAGS | sed 's/-specs=\/usr\/lib\/rpm\/redhat\/redhat-annobin-cc1//g')

%ifarch x86_64
export CGO_CFLAGS+=" -m64 -mtune=generic -fcf-protection=full"
%endif

export LDFLAGS="-X main.buildInfo=`date +%s`"

export BUILDTAGS="seccomp $(hack/systemd_tag.sh) $(hack/libsubid_tag.sh) libsqlite3"
%if !%{defined build_with_btrfs}
export BUILDTAGS+=" exclude_graphdriver_btrfs"
%endif

%if %{defined fips}
export BUILDTAGS+=" libtrust_openssl"
%endif

%if %{defined sequoia}
export BUILDTAGS+=" containers_image_sequoia"
%endif

%if %{defined fedora}
%{__rm} -f pkg/binfmt/embed/ok_amd64 pkg/binfmt/embed/ok_arm64 pkg/binfmt/embed/ok_ppc64le pkg/binfmt/embed/ok_riscv64 pkg/binfmt/embed/ok_s390x
%{__make} pkg/binfmt/embed/ok_amd64 pkg/binfmt/embed/ok_arm64 pkg/binfmt/embed/ok_ppc64le pkg/binfmt/embed/ok_riscv64 pkg/binfmt/embed/ok_s390x
%endif
%{__rm} -f internal/mkcw/embed/entrypoint*.gz
%{__make} internal/mkcw/embed/entrypoint_amd64.gz
%gobuild -o bin/%{name} ./cmd/%{name}
%gobuild -o bin/imgtype ./tests/imgtype
%gobuild -o bin/copy ./tests/copy
%gobuild -o bin/tutorial ./tests/tutorial
%gobuild -o bin/inet ./tests/inet
%gobuild -o bin/dumpspec ./tests/dumpspec
%gobuild -o bin/passwd ./tests/passwd
%gobuild -o bin/crash ./tests/crash
%gobuild -o bin/wait ./tests/wait
%gobuild -o bin/grpcnoop ./tests/rpc/noop
%gobuild -o bin/pipeloop ./tests/pipeloop/pipeloop.go
%{__make} docs

%install
make DESTDIR=%{buildroot} PREFIX=%{_prefix} install install.completions

install -d -p %{buildroot}/%{_datadir}/%{name}/test/system
cp -pav tests/. %{buildroot}/%{_datadir}/%{name}/test/system
cp bin/imgtype %{buildroot}/%{_bindir}/%{name}-imgtype
cp bin/copy    %{buildroot}/%{_bindir}/%{name}-copy
cp bin/tutorial %{buildroot}/%{_bindir}/%{name}-tutorial
cp bin/inet     %{buildroot}/%{_bindir}/%{name}-inet
cp bin/dumpspec %{buildroot}/%{_bindir}/%{name}-dumpspec
cp bin/passwd %{buildroot}/%{_bindir}/%{name}-passwd
cp bin/crash %{buildroot}/%{_bindir}/%{name}-crash
cp bin/wait %{buildroot}/%{_bindir}/%{name}-wait
cp bin/grpcnoop %{buildroot}/%{_bindir}/%{name}-grpcnoop
cp bin/pipeloop %{buildroot}/%{_bindir}/%{name}-pipeloop

rm %{buildroot}%{_datadir}/%{name}/test/system/tools/build/*

#define license tag if not already defined
%{!?_licensedir:%global license %doc}

# Include check to silence rpmlint.
%check

%files
%license LICENSE vendor/modules.txt
%doc README.md
%{_bindir}/%{name}
%{_mandir}/man1/%{name}*
%dir %{_datadir}/bash-completion
%dir %{_datadir}/bash-completion/completions
%{_datadir}/bash-completion/completions/%{name}

%files tests
%license LICENSE
%{_bindir}/%{name}-imgtype
%{_bindir}/%{name}-copy
%{_bindir}/%{name}-tutorial
%{_bindir}/%{name}-inet
%{_bindir}/%{name}-dumpspec
%{_bindir}/%{name}-passwd
%{_bindir}/%{name}-crash
%{_bindir}/%{name}-wait
%{_bindir}/%{name}-grpcnoop
%{_bindir}/%{name}-pipeloop
%{_datadir}/%{name}/test

%changelog
%autochangelog
