# Stage 0 pins the fixtures' starting modes with octal --chmod, which is
# absolute: the CI runner's umask (002 on checkout, group-writable files)
# cannot leak into what the stage-1 COPY --from clauses resolve against.
FROM alpine
COPY --chmod=644 copychmod.txt /fixtures/copychmod.txt
COPY --chmod=755 copysym.sh /fixtures/copysym.sh

FROM alpine
# u+x,go-rwx on a plain (0644) source file: 0644 -> 0700
COPY --from=0 --chmod=u+x,go-rwx /fixtures/copychmod.txt /tmp/sym1
# a+rX,go-w on an executable (0755) script: X keeps the exec bit, 0755
COPY --from=0 --chmod=a+rX,go-w /fixtures/copysym.sh /tmp/sym2
# a+rX on a plain (non-executable, 0644) file: the conditional X adds
# nothing, the mode stays 0644
COPY --from=0 --chmod=a+rX /fixtures/copychmod.txt /tmp/sym3

RUN stat -c "sym1:%a" /tmp/sym1
RUN stat -c "sym2:%a" /tmp/sym2
RUN stat -c "sym3:%a" /tmp/sym3
CMD /bin/sh
