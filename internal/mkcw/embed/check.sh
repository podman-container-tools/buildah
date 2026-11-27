#!/usr/bin/env bash
expected="This image is designed to be run as a confidential workload using libkrun."
cd $(dirname ${BASH_SOURCE[0]})
for GOARCH in amd64 arm64 ppc64le riscv64 s390x ; do
	make -C ../../.. internal/mkcw/embed/entrypoint_$GOARCH
	case $GOARCH in
		amd64) QEMUARCH=x86_64;;
		arm64) QEMUARCH=aarch64;;
		powerpc64le|ppc64le) QEMUARCH=ppc64le;;
		riscv64|s390x) QEMUARCH=$GOARCH;;
	esac
	actual="$(qemu-$QEMUARCH ./entrypoint_$GOARCH 2>&1)"
	status="$?"
	if test "$status" -ne 1 ; then
		echo expected exit status 1 from entrypoint_$GOARCH: "$status"
		exit 1
	fi
	if test "$actual" != "$expected" ; then
		echo unexpected error from entrypoint_$GOARCH: "$actual"
		exit 1
	fi
done
