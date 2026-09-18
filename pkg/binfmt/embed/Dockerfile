FROM --platform=${BUILDPLATFORM} registry.fedoraproject.org/fedora-minimal AS builder
RUN microdnf install -y \
	binutils-aarch64-linux-gnu \
	binutils-powerpc64le-linux-gnu \
	binutils-ppc64le-linux-gnu \
	binutils-riscv64-linux-gnu \
	binutils-s390x-linux-gnu \
	binutils-x86_64-linux-gnu
COPY *.s .
RUN <<-EOF
for arch in arm64:aarch64 ppc64le riscv64 s390x amd64:x86_64 ; do
	oci_arch=${arch%%:*}
	rpm_arch=${arch##*:}
	${rpm_arch}-linux-gnu-as -o ok_${oci_arch}.o ok_${oci_arch}.s
	${rpm_arch}-linux-gnu-ld -o ok_${oci_arch} --build-id=none -nostdlib -zrelro ok_${oci_arch}.o
	${rpm_arch}-linux-gnu-strip ok_${oci_arch}
done
EOF

FROM scratch
ARG TARGETARCH
COPY --from=builder /ok_${TARGETARCH} /ok
