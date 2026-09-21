FROM mirror.gcr.io/busybox
COPY . /upload/
RUN test -f /upload/go.mod
RUN test -f /upload/cmd/main.go
