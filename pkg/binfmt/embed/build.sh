#!/bin/sh
for archarch in amd64:x86_64-linux-gnu arm64:aarch64-linux-gnu ppc64le:powerpc64le-linux-gnu riscv64:riscv64-linux-gnu s390x:s390x-linux-gnu; do
	goarch=${archarch%%:*}
	binutilsarch=${archarch##*:}
	${binutilsarch}-as -o ok_${goarch}.o ${ASFLAGS} ok_${goarch}.s
	${binutilsarch}-ld -o ok_${goarch} ${LDFLAGS:--build-id=none -nostdlib -zrelro} ok_${goarch}.o
	${binutilsarch}-strip ok_${goarch}
done
