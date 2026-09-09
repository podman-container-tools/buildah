# buildah-copy "1" "April 2021" "buildah"

## NAME
buildah\-copy - Copies the contents of a file, URL, or directory into a container's working directory.

## SYNOPSIS
**buildah copy** *container* *src* [[*src* ...] *dest*]

## DESCRIPTION
Copies the contents of a file, URL, or a directory to a container's working
directory or a specified location in the container.  If a local directory is
specified as a source, its *contents* are copied to the destination.

## OPTIONS

**--add-history**

Add an entry to the history which will note the digest of the added content.
Defaults to false.

Note: You can also override the default value of --add-history by setting the
BUILDAH\_HISTORY environment variable. `export BUILDAH_HISTORY=true`

**--allow-empty-wildcard**

If set to true, don't return an error if globbing matches nothing. Only
meaningful when **--allow-wildcard** is true. Defaults to false.

**--allow-wildcard**

Allow glob patterns in source paths. When set to false, source paths containing
wildcard characters (*, ?, [) are rejected with an error. Defaults to true.

**--cert-dir** *path*

Use certificates at *path* (\*.crt, \*.cert, \*.key) when connecting to
registries for pulling images named with the **--from** flag.  The default
certificates directory is _/etc/containers/certs.d_.

**--checksum** *checksum*

Checksum the source content. The value of *checksum* must be a standard
container digest string. Only supported for HTTP sources.

**--chmod** *permissions*

Sets the access permissions of the destination content.  Accepts numerical or
symbolic format.  If `--from` is not used, defaults to `0755`.

**--chown** *owner*:*group*

Sets the user and group ownership of the destination content.  If `--from` is
not used, defaults to `0:0`.

**--contextdir** *directory*

Build context directory. Specifying a context directory causes Buildah to
chroot into the context directory. This means copying files pointed at
by symbolic links outside of the chroot will fail.

**--exclude** *pattern*

Exclude copying files matching the specified pattern. The option can be specified
multiple times. Patterns are matched against each file's path relative to the
context directory (or, with **--from**, relative to the source container or image root).
See containerignore(5) for supported formats.

**--from** *containerOrImage*

Use the root directory of the specified working container or image as the root
directory when resolving absolute source paths and the path of the context
directory.  If an image needs to be pulled, options recognized by `buildah pull`
can be used.  If `--chown` or `--chmod` are not used, permissions and ownership
is preserved.

**--ignorefile** *file*

Path to an alternative .containerignore (.dockerignore) file. Requires \-\-contextdir be specified.

**--include** *pattern*

Only copy files matching the specified pattern. The option can be specified multiple times.
Patterns are matched against each file's path relative to the context directory being copied.
If a path matches both an **--include** and an **--exclude** pattern, it will be excluded.
See containerignore(5) for supported formats.

**--link**

Create an independent image layer for the added files instead of modifying the working
container's filesystem. If `buildah run` creates a file and `buildah copy --link` adds a file
to the same path, the file from `buildah copy --link` will be present in the committed image.
The --link layer is applied after all container filesystem changes at commit time.

**--no-follow-symlinks**

Don't follow and dereference the symlinks when copying the files. Instead, copy
the symlinks themselves.

**--parents**

Preserve leading directories in the paths of items being copied, relative to either the
top of the build context, or to the "pivot point", a location in the source path marked
by a path component named "." (i.e., where "/./" occurs in the path).

**--quiet**, **-q**

Refrain from printing a digest of the copied content.

**--retry** *attempts*

Number of times to retry in case of failure when performing pull of images from registry.

Defaults to `3`.

**--retry-delay** *duration*

Duration of delay between retry attempts in case of failure when performing pull of images from registry.

Defaults to `2s`.

**--timestamp** *seconds*

Set the timestamp ("mtime") for added content to exactly this number of seconds
since the epoch (Unix time 0, i.e., 00:00:00 UTC on 1 January 1970) to help
allow for deterministic builds.

The destination directory into which the content is being copied will most
likely reflect the time at which the content was added to it.

**--tls-verify** *bool-value*

Require verification of certificates when pulling images referred to with the
**--from*** flag (defaults to true).  TLS verification cannot be used when
talking to an insecure registry.

## EXAMPLE

buildah copy containerID '/myapp/app.conf' '/myapp/app.conf'

buildah copy --exclude=**/*.md docs containerID 'docs' '/docs'

buildah copy --parents containerID './x/a.txt' './y/a.txt' '/parents'

buildah copy --chown myuser:mygroup containerID '/myapp/app.conf' '/myapp/app.conf'

buildah copy --chmod 660 containerID '/myapp/app.conf' '/myapp/app.conf'

buildah copy containerID '/home/myuser/myproject.go'

buildah copy containerID '/home/myuser/myfiles.tar' '/tmp'

buildah copy containerID '/tmp/workingdir' '/tmp/workingdir'

buildah copy containerID 'https://github.com/containers/buildah' '/tmp'

buildah copy containerID 'passwd' 'certs.d' /etc

buildah copy containerID '/tmp/myfiles-*' '/dest/'

buildah copy --allow-empty-wildcard=true containerID '/tmp/logs-*' '/dest/'

## FILES

### .containerignore/.dockerignore

If the .containerignore/.dockerignore file exists in the context directory,
`buildah copy` reads its contents. If both exist, then .containerignore is used.

When the `--ignorefile` option is specified Buildah reads it and
uses it to decide which content to exclude when copying content into the
working container.

Users can specify a series of Unix shell glob patterns in an ignore file to
identify files/directories to exclude.

Buildah supports a special wildcard string `**` which matches any number of
directories (including zero). For example, `**/*.go` will exclude all files that
end with .go that are found in all directories.

Example .containerignore/.dockerignore file:

```
# here are files we want to exclude
*/*.c
**/output*
src
```

`*/*.c`
Excludes files and directories whose names end with .c in any top level subdirectory. For example, the source file include/rootless.c.

`**/output*`
Excludes files and directories starting with `output` from any directory.

`src`
Excludes files named src and the directory src as well as any content in it.

Lines starting with ! (exclamation mark) can be used to make exceptions to
exclusions. The following is an example .containerignore/.dockerignore file that uses this
mechanism:
```
*.doc
!Help.doc
```

Exclude all doc files except Help.doc when copying content into the container.

This functionality is compatible with the handling of .containerignore files described here:

https://github.com/containers/common/blob/main/docs/containerignore.5.md

## SEE ALSO
buildah(1), containerignore(5)
