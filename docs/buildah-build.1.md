# buildah-build "1" "April 2017" "buildah"

## NAME
buildah\-build - Build an image using instructions from Containerfiles

## SYNOPSIS

**buildah build** [*options*] [*context*]

**buildah bud** [*options*] [*context*]

## DESCRIPTION
Builds an image using instructions from one or more Containerfiles or Dockerfiles and a specified
build context directory.  A Containerfile uses the same syntax as a Dockerfile internally.  For this
document, a file referred to as a Containerfile can be a file named either 'Containerfile' or 'Dockerfile'.

The build context directory can be specified as the http(s) URL of an archive, git repository or Containerfile.

If no context directory is specified, then Buildah will assume the current working directory as build context, which should contain a Containerfile.

Containerfiles ending with a ".in" suffix will be automatically preprocessed via cpp(1).  This can be useful to decompose Containerfiles into several reusable parts that can be used via CPP's **#include** directive.  Notice, a Containerfile.in file can still be used by other tools when manually preprocessing them via `cpp -E`. Any comments ( Lines beginning with `#` ) in included Containerfile(s) that are not preprocess commands, will be printed as warnings during builds.

When the URL is an archive, the contents of the URL is downloaded to a temporary location and extracted before execution.

When the URL is a Containerfile, the file is downloaded to a temporary location.

When a Git repository is set as the URL, the repository is cloned locally and then used as the build context.  A non-default branch (or commit ID) and subdirectory of the cloned git repository can be used by including their names at the end of the URL in the form `myrepo.git#mybranch:subdir`, `myrepo.git#mycommit:subdir`, or `myrepo.git#:subdir` if the subdirectory should be used from the default branch.

## OPTIONS

**--add-host**=[]

Add a custom host-to-IP mapping (host:ip)

Add a line to /etc/hosts. The format is hostname:ip. The **--add-host** option can be set multiple times. Conflicts with the --no-hosts option.

Instead of an IP address, the special flag host-gateway can be given. This resolves to an IP address the container can use to connect to the host. The IP address chosen depends on your network setup, thus there's no guarantee that Buildah can determine the host-gateway address automatically, which will then cause Buildah to fail with an error message. You can overwrite this IP address using the host_containers_internal_ip option in containers.conf.

The host-gateway address is also used by Buildah to automatically add the host.containers.internal and host.docker.internal hostnames to /etc/hosts. You can prevent that by either giving the --no-hosts option, or by setting host_containers_internal_ip="none" in containers.conf. If no host-gateway address was configured manually and Buildah fails to determine the IP address automatically, Buildah will silently skip adding these internal hostnames to /etc/hosts. If Buildah is running in a virtual machine using podman machine (this includes Mac and Windows hosts), Buildah will silently skip adding the internal hostnames to /etc/hosts, unless an IP address was configured manually; the internal hostnames are resolved by the gvproxy DNS resolver instead.

Buildah will use the /etc/hosts file of the host as a basis by default, i.e. any hostname present in this file will also be present in the /etc/hosts file of the container. A different base file can be configured using the base_hosts_file config in containers.conf

**--all-platforms**

Instead of building for a set of platforms specified using the **--platform** option, inspect the build's base images, and build for all of the platforms for which they are all available.  Stages that use *scratch* as a starting point can not be inspected, so at least one non-*scratch* stage must be present for detection to work usefully.

**--annotation** *annotation[=value]*

Add an image *annotation* (e.g. annotation=*value*) to the image metadata. Can be used multiple times.
If *annotation* is named, but neither `=` nor a `value` is provided, then the *annotation* is set to an empty value.

Note: this information is not present in Docker image formats, so it is discarded when writing images in Docker formats.

**--arch**="ARCH"

Set the ARCH of the image to be built, and that of the base image to be pulled, if the build uses one, to the provided value instead of using the architecture of the host. (Examples: arm, arm64, 386, amd64, ppc64le, s390x)

**--authfile** *path*

Path of the authentication file. Default is ${XDG_RUNTIME_DIR}/containers/auth.json. See containers-auth.json(5) for more information. This file is created using `buildah login`.

If the authorization state is not found there, $HOME/.docker/config.json is checked, which is set using `docker login`.

Note: You can also override the default path of the authentication file by setting the REGISTRY\_AUTH\_FILE
environment variable. `export REGISTRY_AUTH_FILE=path`

**--build-arg** *arg=value*

Specifies a build argument and its value, which will be interpolated in
instructions read from the Containerfiles in the same way that environment
variables are, but which will not be added to environment variable list in the
resulting image's configuration.

Please refer to the [BUILD TIME VARIABLES](#build-time-variables) section for the
list of variables that can be overridden within the Containerfile at run time.

**--build-arg-file** *path*

Specifies a file containing lines of build arguments of the form arg=value. The suggested file name is argfile.conf.

Comment lines beginning with `#` are ignored, along with blank lines. All others should be of the `arg=value` format passed to `--build-arg`.

If several arguments are provided via the `--build-arg-file` and `--build-arg` options, the build arguments will be merged across all of the provided files and command line arguments.

Any file provided in a `--build-arg-file` option will be read before the arguments supplied via the `--build-arg` option.

When a given argument name is specified several times, the last instance is the one that is passed to the resulting builds. This means `--build-arg` values always override those in a `--build-arg-file`.

**--build-context** *name=value*

Specify an additional build context using its short name and its location. Additional
build contexts can be referenced in the same manner as we access different stages in `COPY`
instruction.

Valid values could be:
* Local directory – e.g. --build-context project2=../path/to/project2/src
* HTTP URL to a tarball – e.g. --build-context src=https://example.org/releases/src.tar
* Container image – specified with a container-image:// prefix, e.g. --build-context alpine=container-image://alpine:3.15, (also accepts docker://, docker-image://)

On the Containerfile side, you can reference the build context on all commands that accept the “from” parameter.
Here’s how that might look:

```Dockerfile
FROM [name]
COPY --from=[name] ...
RUN --mount=from=[name] …
```

The value of `[name]` is matched with the following priority order:

* Named build context defined with --build-context [name]=..
* Stage defined with AS [name] inside Containerfile
* Image [name], either local or in a remote registry

**--cache-from**

Repository to utilize as a potential list of cache sources. When specified, Buildah will try to look for
cache images in the specified repositories and will attempt to pull cache images instead of actually
executing the build steps locally. Buildah will only attempt to pull previously cached images if they
are considered as valid cache hits.

Use the `--cache-to` option to populate a remote repository or repositories with cache content.

Example

```bash
# populate a cache and also consult it
buildah build -t test --layers --cache-to registry/myrepo/cache --cache-from registry/myrepo/cache .
```

Note: `--cache-from` option is ignored unless `--layers` is specified.

Note: Buildah's `--cache-from` option is designed differently than Docker and BuildKit's `--cache-from` option. Buildah's
distributed cache mechanism pulls intermediate images from the remote registry itself, unlike Docker and BuildKit where
the intermediate image is stored in the image itself. Buildah's approach is similar to kaniko, which
does not inflate the size of the original image with intermediate images.  Also, intermediate images can truly be
kept distributed across one or more remote registries using Buildah's caching mechanism.

**--cache-to**

Set this flag to specify list of remote repositories that will be used to store cache images. Buildah will attempt to
push newly built cache image to the remote repositories.

Note: Use the `--cache-from` option in order to use cache content in a remote repository.

Example

```bash
# populate a cache and also consult it
buildah build -t test --layers --cache-to registry/myrepo/cache --cache-from registry/myrepo/cache .
```

Note: `--cache-to` option is ignored unless `--layers` is specified.

Note: Buildah's `--cache-to` option is designed differently than Docker and BuildKit's `--cache-to` option. Buildah's
distributed cache mechanism push intermediate images to the remote registry itself, unlike Docker and BuildKit where
the intermediate image is stored in the image itself. Buildah's approach is similar to kaniko, which
does not inflate the size of the original image with intermediate images.  Also, intermediate images can truly be
kept distributed across one or more remote registries using Buildah's caching mechanism.

**--cache-ttl** *duration*

Limit the use of cached images to only consider images with created timestamps less than *duration* ago.
For example if `--cache-ttl=1h` is specified, Buildah will only consider intermediate cache images which are created
under the duration of one hour, and intermediate cache images outside this duration will be ignored.

Note: Setting `--cache-ttl=0` manually is equivalent to using `--no-cache` in the implementation since this would
effectively mean that user is not willing to use cache at all.

**--cap-add**=*CAP\_xxx*

When executing RUN instructions, run the command specified in the instruction
with the specified capability added to its capability set.
Certain capabilities are granted by default; this option can be used to add
more.

**--cap-drop**=*CAP\_xxx*

When executing RUN instructions, run the command specified in the instruction
with the specified capability removed from its capability set.
The CAP\_CHOWN, CAP\_DAC\_OVERRIDE, CAP\_FOWNER, CAP\_FSETID, CAP\_KILL,
CAP\_NET\_BIND\_SERVICE, CAP\_SETFCAP, CAP\_SETGID, CAP\_SETPCAP, and
CAP\_SETUID capabilities are granted by default; this option can be used to
remove them. The list of default capabilities is managed in containers.conf(5).

If a capability is specified to both the **--cap-add** and **--cap-drop**
options, it will be dropped, regardless of the order in which the options were
given.

**--cert-dir** *path*

Use certificates at *path* (\*.crt, \*.cert, \*.key) to connect to the registry
and retrieve contents from HTTPS locations for ADD instructions.
The default certificates directory is _/etc/containers/certs.d_.

**--cgroup-parent**=""

Path to cgroups under which the cgroup for RUN instructions will be created. If the path is not absolute, the path is considered to be relative to the cgroups path of the init process. Cgroups will be created if they do not already exist.

**--cgroupns** *how*

Sets the configuration for cgroup namespaces when handling `RUN` instructions.
The configured value can be "" (the empty string) or "private" to indicate
that a new cgroup namespace should be created, or it can be "host" to indicate
that the cgroup namespace in which `buildah` itself is being run should be reused.

**--compat-volumes**

Handle directories marked using the VOLUME instruction (both in this build, and
those inherited from base images) such that their contents can only be modified
by ADD and COPY instructions. Any changes made in those locations by RUN
instructions will be reverted. Before the introduction of this option, this
behavior was the default, but it is now disabled by default.

**--compress**

This option is added to be aligned with other containers CLIs.
Buildah doesn't send a copy of the context directory to a daemon or a remote server.
Thus, compressing the data before sending it is irrelevant to Buildah.

**--compression-format** *format*

Specifies the compression format to use.  Supported values are: `gzip`, `zstd` and `zstd:chunked`.
`zstd:chunked` is incompatible with encrypting images, and will be treated as `zstd` with a warning in that case.
If not specified, the format is read from the `compression_format` setting in containers.conf.

This option affects cache pushes with `--cache-to` and the final image when it is written to a non-local destination (e.g., `dir:`, `oci:`, `oci-archive:`, or a registry).
When the output is local container storage (the default), layers are always decompressed on ingest, so compression is applied at `buildah push` time instead.

**--compression-level** *level*

Specifies the compression level to use.  The value is specific to the compression algorithm used, e.g. for zstd the accepted values are in the range 1-20 (inclusive), while for gzip it is 1-9 (inclusive).
If not specified, the level is read from the `compression_level` setting in containers.conf.

**--cpp-flag**=""

Set additional flags to pass to the C Preprocessor cpp(1).
Containerfiles ending with a ".in" suffix will be preprocessed via cpp(1). This option can be used to pass additional flags to cpp.
Note: You can also set default CPPFLAGS by setting the BUILDAH\_CPPFLAGS
environment variable (e.g., `export BUILDAH_CPPFLAGS="-DDEBUG"`).

**--cpu-period**=*0*

Set the CPU period for the Completely Fair Scheduler (CFS), which is a
duration in microseconds. Once the container's CPU quota is used up, it will
not be scheduled to run until the current period ends. Defaults to 100000
microseconds.

On some systems, changing the CPU limits may not be allowed for non-root
users. For more details, see
https://github.com/containers/podman/blob/main/troubleshooting.md#26-running-containers-with-cpu-limits-fails-with-a-permissions-error

**--cpu-quota**=*0*

Limit the CPU CFS (Completely Fair Scheduler) quota

Limit the container's CPU usage. By default, containers run with the full
CPU resource. This flag tells the kernel to restrict the container's CPU usage
to the quota you specify.

On some systems, changing the CPU limits may not be allowed for non-root
users. For more details, see
https://github.com/containers/podman/blob/main/troubleshooting.md#26-running-containers-with-cpu-limits-fails-with-a-permissions-error

**--cpu-shares**, **-c**=*0*

CPU shares (relative weight)

By default, all containers get the same proportion of CPU cycles. This proportion
can be modified by changing the container's CPU share weighting relative
to the weighting of all other running containers.

To modify the proportion from the default of 1024, use the **--cpu-shares**
flag to set the weighting to 2 or higher.

The proportion will only apply when CPU-intensive processes are running.
When tasks in one container are idle, other containers can use the
left-over CPU time. The actual amount of CPU time will vary depending on
the number of containers running on the system.

For example, consider three containers, one has a cpu-share of 1024 and
two others have a cpu-share setting of 512. When processes in all three
containers attempt to use 100% of CPU, the first container would receive
50% of the total CPU time. If you add a fourth container with a cpu-share
of 1024, the first container only gets 33% of the CPU. The remaining containers
receive 16.5%, 16.5% and 33% of the CPU.

On a multi-core system, the shares of CPU time are distributed over all CPU
cores. Even if a container is limited to less than 100% of CPU time, it can
use 100% of each individual CPU core.

For example, consider a system with more than three cores. If you start one
container **{C0}** with **-c=512** running one process, and another container
**{C1}** with **-c=1024** running two processes, this can result in the following
division of CPU shares:

    PID    container	CPU	CPU share
    100    {C0}		0	100% of CPU0
    101    {C1}		1	100% of CPU1
    102    {C1}		2	100% of CPU2

**--cpuset-cpus**=""

  CPUs in which to allow execution (0-3, 0,1)

**--cpuset-mems**=""

Memory nodes (MEMs) in which to allow execution (0-3, 0,1). Only effective on NUMA systems.

If you have four memory nodes on your system (0-3), use `--cpuset-mems=0,1`
then processes in your container will only use memory from the first
two memory nodes.

**--created-annotation**

Add an image *annotation* (see also **--annotation**) to the image metadata
setting "org.opencontainers.image.created" to the current time, or to the
datestamp specified to the **--source-date-epoch** or **--timestamp** flag,
if either was used.  If *false*, no such annotation will be present in the
written image.

Note: this information is not present in Docker image formats, so it is discarded when writing images in Docker formats.

**--creds** *creds*

The [username[:password]] to use to authenticate with the registry if required.
If one or both values are not supplied, a command line prompt will appear and the
value can be entered.  The password is entered without echo.

**--cw** *options*

Produce an image suitable for use as a confidential workload running in a
trusted execution environment (TEE) using krun (i.e., *crun* built with the
libkrun feature enabled and invoked as *krun*).  Instead of the conventional
contents, the root filesystem of the image will contain an encrypted disk image
and configuration information for krun.

The value for *options* is a comma-separated list of key=value pairs, supplying
configuration information which is needed for producing the additional data
which will be included in the container image.

Recognized _keys_ are:

*attestation_url*: The location of a key broker / attestation server.
If a value is specified, the new image's workload ID, along with the passphrase
used to encrypt the disk image, will be registered with the server, and the
server's location will be stored in the container image.
At run-time, krun is expected to contact the server to retrieve the passphrase
using the workload ID, which is also stored in the container image.
If no value is specified, a *passphrase* value *must* be specified.

*cpus*: The number of virtual CPUs which the image expects to be run with at
run-time.  If not specified, a default value will be supplied.

*firmware_library*: The location of the libkrunfw-sev shared library.  If not
specified, `buildah` checks for its presence in a number of hard-coded
locations.

*memory*: The amount of memory which the image expects to be run with at
run-time, as a number of megabytes.  If not specified, a default value will be
supplied.

*passphrase*: The passphrase to use to encrypt the disk image which will be
included in the container image.
If no value is specified, but an *attestation_url* value is specified, a
randomly-generated passphrase will be used.
The authors recommend setting an *attestation_url* but not a *passphrase*.

*slop*: Extra space to allocate for the disk image compared to the size of the
container image's contents, expressed either as a percentage (..%) or a size
value (bytes, or larger units if suffixes like KB or MB are present), or a sum
of two or more such specifications.  If not specified, `buildah` guesses that
25% more space than the contents will be enough, but this option is provided in
case its guess is wrong.

*type*: The type of trusted execution environment (TEE) which the image should
be marked for use with.  Accepted values are "SEV" (AMD Secure Encrypted
Virtualization - Encrypted State) and "SNP" (AMD Secure Encrypted
Virtualization - Secure Nested Paging).  If not specified, defaults to "SNP".

*workload_id*: A workload identifier which will be recorded in the container
image, to be used at run-time for retrieving the passphrase which was used to
encrypt the disk image.  If not specified, a semi-random value will be derived
from the base image's image ID.

**--decryption-key** *key[:passphrase]*

The [key[:passphrase]] to be used for decryption of images. Key can point to keys and/or certificates. Decryption will be tried with all keys. If the key is protected by a passphrase, it is required to be passed in the argument and omitted otherwise.

**--device**=*device*

Add a host device, or devices under a directory, to the environment of any
**RUN** instructions run during the build.  The optional *permissions*
parameter can be used to specify device permissions, using any one or more of
**r** for read, **w** for write, and **m** for **mknod**(2).

Example: **--device=/dev/sdc:/dev/xvdc:rwm**.

Note: if _host-device_ is a symbolic link then it will be resolved first.
The container will only store the major and minor numbers of the host device.

The device to share can also be specified using a Container Device Interface
(CDI) specification (https://github.com/cncf-tags/container-device-interface).

Note: if the user only has access rights via a group, accessing the device
from inside a rootless container will fail. The **crun**(1) runtime offers a
workaround for this by adding the option **--annotation run.oci.keep_original_groups=1**.

**--disable-compression**, **-D**

Don't compress filesystem layers when building the image unless it is required
by the location where the image is being written.  This is the default setting,
because image layers are compressed automatically when they are pushed to
registries, and images being written to local storage would only need to be
decompressed again to be stored.  Compression can be forced in all cases by
specifying **--disable-compression=false**.

**--disable-content-trust**

This is a Docker specific option to disable image verification to a Container
registry and is not supported by Buildah.  This flag is a NOOP and provided
solely for scripting compatibility.

**--dns**=[]

Set custom DNS servers.  Invalid if using **--dns** with **--network=none**.

This option can be used to override the DNS configuration passed to the container. Typically this is necessary when the host DNS configuration is invalid for the container (e.g., 127.0.0.1). When this is the case the `--dns` flag is necessary for every run.

The special value **none** can be specified to disable creation of /etc/resolv.conf in the container by Buildah. The /etc/resolv.conf file in the image will be used without changes.

**--dns-option**=[]

Set custom DNS options. Invalid if using **--dns-option** with **--network=none**.

**--dns-search**=[]

Set custom DNS search domains. Invalid if using **--dns-search** with **--network=none**.

**--env** *env[=value]*

Add a value (e.g. env=*value*) to the built image.  Can be used multiple times.
If neither `=` nor a `*value*` are specified, but *env* is set in the current
environment, the value from the current environment will be added to the image.
The value of *env* can be overridden by ENV instructions in the Containerfile.
To remove an environment variable from the built image, use the `--unsetenv`
option.

**--file**, **-f** *Containerfile*

Specifies a Containerfile which contains instructions for building the image,
either a local file or an **http** or **https** URL.  If more than one
Containerfile is specified, *FROM* instructions will only be accepted from the
last specified file.

If a local file is specified as the Containerfile and it does not exist, the
context directory will be prepended to the local file value.

If you specify `-f -`, the Containerfile contents will be read from stdin.

**--force-compression**

If set, uses the specified compression algorithm even if the destination contains a differently-compressed variant already.
Defaults to `true` if `--compression-format` is explicitly specified on the command-line or `compression_format` is set in containers.conf, `false` otherwise.

**--force-rm** *bool-value*

Always remove intermediate containers after a build, even if the build fails (default false).

**--format**

Control the format for the built image's manifest and configuration data.
Recognized formats include *oci* (OCI image-spec v1.0, the default) and
*docker* (version 2, using schema format 2 for the manifest).

Note: You can also override the default format by setting the BUILDAH\_FORMAT
environment variable.  `export BUILDAH_FORMAT=docker`

**--from**

Overrides the first `FROM` instruction within the Containerfile.  If there are multiple
FROM instructions in a Containerfile, only the first is changed.

**--group-add**=*group* | *keep-groups*

Assign additional groups to the primary user running within the container
process.

- `keep-groups` is a special flag that tells Buildah to keep the supplementary
group access.

Allows container to use the user's supplementary group access. If file systems
or devices are only accessible by the rootless user's group, this flag tells the
OCI runtime to pass the group access into the container. Currently only
available with the `crun` OCI runtime. Note: `keep-groups` is exclusive, other
groups cannot be specified with this flag.

**--help**, **-h**

Print usage statement

**--hooks-dir** *path*

Each `*.json` file in the path configures a hook for buildah build containers. For more details on the syntax of the JSON files and the semantics of hook injection, see oci-hooks(5). Buildah currently support both the 1.0.0 and 0.1.0 hook schemas, although the 0.1.0 schema is deprecated.

This option may be set multiple times; paths from later options have higher precedence (oci-hooks(5) discusses directory precedence).

For the annotation conditions, buildah uses any annotations set in the generated OCI configuration.

For the bind-mount conditions, only mounts explicitly requested by the caller via --volume are considered. Bind mounts that buildah inserts by default (e.g. /dev/shm) are not considered.

If --hooks-dir is unset for root callers, Buildah will currently default to /usr/share/containers/oci/hooks.d and /etc/containers/oci/hooks.d in order of increasing precedence. Using these defaults is deprecated, and callers should migrate to explicitly setting --hooks-dir.

**--http-proxy**=true

By default proxy environment variables are passed into the container if set
for the buildah process.  This can be disabled by setting the `--http-proxy`
option to `false`.  The environment variables passed in include `http_proxy`,
`https_proxy`, `ftp_proxy`, `no_proxy`, and also the upper case versions of
those.

**--identity-label** *bool-value*

Adds a label `io.buildah.version` with its value set to the version of buildah
which built the image (default true unless `--timestamp` or
`--source-date-epoch` is used).

**--ignorefile** *file*

Path to an alternative .containerignore (.dockerignore) file.

**--iidfile** *ImageIDfile*

Write the built image's ID to the file.  When `--platform` is specified more
than once, attempting to use this option will trigger an error.

**--iidfile-raw** *ImageIDfile*

Write the built image's ID to the file without the algorithm prefix
(e.g., `sha256:`). When `--platform` is specified more than once, attempting to
use this option will trigger an error.

An alias `--raw-iidfile` is also available.

**--inherit-annotations** *bool-value*

Inherit the annotations from the base image or base stages. (default true).
Use cases which set this flag to *false* may need to do the same for the
**--created-annotation** flag.

**--inherit-labels** *bool-value*

Inherit the labels from the base image or base stages. (default true).

**--ipc** *how*

Sets the configuration for IPC namespaces when handling `RUN` instructions.
The configured value can be "" (the empty string) or "container" to indicate
that a new IPC namespace should be created, or it can be "host" to indicate
that the IPC namespace in which `buildah` itself is being run should be reused,
or it can be the path to an IPC namespace which is already in use by
another process.

**--isolation** *type*

Controls what type of isolation is used for running processes as part of `RUN`
instructions.  Recognized types include *oci* (OCI-compatible runtime, the
default), *rootless* (OCI-compatible runtime invoked using a modified
configuration, with *--no-new-keyring* added to its *create* invocation,
reusing the host's network and UTS namespaces, and creating private IPC, PID,
mount, and user namespaces; the default for unprivileged users), and *chroot*
(an internal wrapper that leans more toward chroot(1) than container
technology, reusing the host's control group, network, IPC, and PID namespaces,
and creating private mount and UTS namespaces, and creating user namespaces
only when they're required for ID mapping).

Note: You can also override the default isolation type by setting the
BUILDAH\_ISOLATION environment variable.  `export BUILDAH_ISOLATION=oci`

**--jobs** *N*

Run up to N concurrent stages in parallel.  If the number of jobs is greater than 1,
stdin will be read from /dev/null.  If 0 is specified, then there is
no limit on the number of jobs that run in parallel.

**--label** *label[=value]*

Add an image *label* (e.g. label=*value*) to the image metadata. Can be used multiple times.
If *label* is named, but neither `=` nor a `value` is provided, then the *label* is set to an empty value.

Users can set a special LABEL **io.containers.capabilities=CAP1,CAP2,CAP3** in
a Containerfile that specifies the list of Linux capabilities required for the
container to run properly. This label specified in a container image tells
container engines, like Podman, which recognize this label to run the container with just these
capabilities. The container engine launches the container with just the specified
capabilities, as long as this list of capabilities is a subset of the default
list.

If the specified capabilities are not in the default set, container engines
should print an error message and will run the container with the default
capabilities.

**--layer-label** *label[=value]*

Add an intermediate image *label* (e.g. label=*value*) to the metadata in intermediate images, i.e., any images built for
non-final stages and for non-final instructions in stages when **--layers** is **true**. It can be used multiple times.
If *label* is named, but neither `=` nor a `value` is provided, then the *label* is set to an empty value.

**--layers** *bool-value*

Cache intermediate images during the build process (Default is `false`).

Note: You can also override the default value of layers by setting the BUILDAH\_LAYERS
environment variable. `export BUILDAH_LAYERS=true`

**--logfile** *filename*

Log output which would be sent to standard output and standard error to the
specified file instead of to standard output and standard error.

**--logsplit** *bool-value*

If --logfile and --platform is specified following flag allows end-users to split log file for each
platform into different files with naming convention as `${logfile}_${platform-os}_${platform-arch}`.

**--manifest** *listName*

Name of the manifest list to which the built image will be added.  Creates the
manifest list if it does not exist.  This option is useful for building multi
architecture images.
If _listName_ does not include a registry name component, the registry name
*localhost* will be prepended to the list name.

**--memory**, **-m**=""

Memory limit (format: <number>[<unit>], where unit = b, k, m or g)

Allows you to constrain the memory available to a container. If the host
supports swap memory, then the **-m** memory setting can be larger than physical
RAM. If a limit of 0 is specified (not using **-m**), the container's memory is
not limited. The actual limit may be rounded up to a multiple of the operating
system's page size (the value would be very large, that's millions of trillions).

**--memory-swap**="LIMIT"

A limit value equal to memory plus swap. Must be used with the  **-m**
(**--memory**) flag. The swap `LIMIT` should always be larger than **-m**
(**--memory**) value.  By default, the swap `LIMIT` will be set to double
the value of --memory.

The format of `LIMIT` is `<number>[<unit>]`. Unit can be `b` (bytes),
`k` (kilobytes), `m` (megabytes), or `g` (gigabytes). If you don't specify a
unit, `b` is used. Set LIMIT to `-1` to enable unlimited swap.

**--metadata-file** *MetadataFile*

Write information about the built image to the named file.  When `--platform`
is specified more than once, attempting to use this option will trigger an
error.

**--mount** *mount-instruction*

Adds this mount to each `RUN` command in a Containerfile before executing. For example:

`buildah build --mount type=secret,id=mysecret ...`

and a Containerfile entry of:

`RUN cat /run/secrets/mysecret`

Has the same effect as:

`RUN --mount=type=secret,id=mysecret cat /run/secrets/mysecret`

**--network**, **--net**=*mode*

Sets the configuration for network namespaces when handling `RUN` instructions.

Valid _mode_ values are:

- **none**: no networking. Invalid if using **--dns**, **--dns-opt**, or **--dns-search**;
- **host**: use the host network stack. Note: the host mode gives the container full access to local system services such as D-bus and is therefore considered insecure;
- **ns:**_path_: path to a network namespace to join;
- **private**: create a new namespace for the container (default)
- **\<network name|ID\>**: Join the network with the given name or ID, e.g. use `--network mynet` to join the network with the name mynet. Only supported for rootful users.
- **pasta[:OPTIONS,...]**: use **pasta**(1) to create a user-mode networking
    stack. \
    This is only supported in rootless mode. \
    By default, IPv4 and IPv6 addresses and routes, as well as the pod interface
    name, are copied from the host. If port forwarding isn't configured, ports
    are forwarded dynamically as services are bound on either side (init
    namespace or container namespace). Port forwarding preserves the original
    source IP address. Options described in pasta(1) can be specified as
    comma-separated arguments. \
    In terms of pasta(1) options, **--config-net** is given by default, in
    order to configure networking when the container is started, and
    **--no-map-gw** is also assumed by default, to avoid direct access from
    container to host using the gateway address. The latter can be overridden
    by passing **--map-gw** in the pasta-specific options (despite not being an
    actual pasta(1) option). \
    Also, **-t none** and **-u none** are passed to disable
    automatic port forwarding based on bound ports. Similarly, **-T none** and
    **-U none** are given to disable the same functionality from container to
    host. \
    Some examples:
    - **pasta:--map-gw**: Allow the container to directly reach the host using the
        gateway address.
    - **pasta:--mtu,1500**: Specify a 1500 bytes MTU for the _tap_ interface in
        the container.
    - **pasta:--ipv4-only,-a,10.0.2.0,-n,24,-g,10.0.2.2,--dns-forward,10.0.2.3,-m,1500,--no-ndp,--no-dhcpv6,--no-dhcp**,
        disable IPv6, assign
        `10.0.2.0/24` to the `tap0` interface in the container, with gateway
        `10.0.2.3`, enable DNS forwarder reachable at `10.0.2.3`, set MTU to 1500
        bytes, disable NDP, DHCPv6 and DHCP support.
    - **pasta:-I,tap0,--ipv4-only,-a,10.0.2.0,-n,24,-g,10.0.2.2,--dns-forward,10.0.2.3,--no-ndp,--no-dhcpv6,--no-dhcp**,
        same as above, but leave the MTU to 65520 bytes
    - **pasta:-t,auto,-u,auto,-T,auto,-U,auto**: enable automatic port forwarding
        based on observed bound ports from both host and container sides
    - **pasta:-T,5201**: enable forwarding of TCP port 5201 from container to
        host, using the loopback interface instead of the tap interface for improved
        performance

**--no-cache**

Do not use existing cached images for the container build. Build from the start with a new set of cached layers.

**--no-hostname**

Do not create the _/etc/hostname_ file in the container for RUN instructions.

By default, Buildah manages the _/etc/hostname_ file, adding the container's own hostname.  When the **--no-hostname** option is set, the image's _/etc/hostname_ will be preserved unmodified if it exists.

**--no-hosts**

Do not create the _/etc/hosts_ file in the container for RUN instructions.

By default, Buildah manages _/etc/hosts_, adding the container's own IP address.
**--no-hosts** disables this, and the image's _/etc/hosts_ will be preserved unmodified. Conflicts with the --add-host option.

**--omit-history** *bool-value*

Omit build history information in the built image. (default false).

This option is useful for the cases where end users explicitly
want to set `--omit-history` to omit the optional `History` from
built images or when working with images built using build tools that
do not include `History` information in their images.

**--os**="OS"

Set the OS of the image to be built, and that of the base image to be pulled, if the build uses one, instead of using the current operating system of the host.

**--os-feature** *feature*

Set the name of a required operating system *feature* for the image which will
be built.  By default, if the image is not based on *scratch*, the base image's
required OS feature list is kept, if the base image specified any.  This option
is typically only meaningful when the image's OS is Windows.

If *feature* has a trailing `-`, then the *feature* is removed from the set of
required features which will be listed in the image.

**--os-version** *version*

Set the exact required operating system *version* for the image which will be
built.  By default, if the image is not based on *scratch*, the base image's
required OS version is kept, if the base image specified one.  This option is
typically only meaningful when the image's OS is Windows, and is typically set in
Windows base images, so using this option is usually unnecessary.

**--output**, **-o**=""

Additional output (format: type=local,dest=path)

The --output (or -o) option supplements the default behavior of building a
container image by allowing users to export the image's contents as files on
the local filesystem, which can be useful for generating local binaries, code
generation, etc.

The value for --output is a comma-separated sequence of key=value pairs,
defining the output type and options.

Supported _keys_ are:
 **dest**: Destination for exported output. Can be set to `-` to indicate standard output, or to an absolute or relative path.
 **type**: Defines the type of output to be written. Must be one of the values listed below.

Valid _type_ values are:
 **local**: write the resulting build files to a directory on the client-side.
 **tar**: write the resulting files as a single tarball (.tar).

Alternatively, instead of a comma-separated sequence, the value of **--output**
can be just the destination (in the `**dest**` format) (e.g. `--output
some-path`, `--output -`), and the **type** will be inferred to be **tar** if
the output destination is `-`, and **local** otherwise.

Timestamps on the output contents will be set to exactly match the value
specified using the **--timestamp** flag, or to exactly match the value
specified for the **--source-date-epoch** flag, if either are specified.

Note that the **--tag** option can also be used to write the image to any location described by `containers-transports(5)`.

**--pid** *how*

Sets the configuration for PID namespaces when handling `RUN` instructions.
The configured value can be "" (the empty string) or "private" to indicate
that a new PID namespace should be created, or it can be "host" to indicate
that the PID namespace in which `buildah` itself is being run should be reused,
or it can be the path to a PID namespace which is already in use by another
process.

**--platform**="OS/ARCH[/VARIANT]"

Set the OS/ARCH of the built image (and its base image, if your build uses one)
to the provided value instead of using the current operating system and
architecture of the host (for example `linux/arm`, `linux/arm64`, `linux/amd64`).

The `--platform` flag can be specified more than once, or given a
comma-separated list of values as its argument.  When more than one platform is
specified, the `--manifest` option should be used instead of the `--tag`
option.

OS/ARCH pairs are those used by the Go Programming Language.  In several cases
the ARCH value for a platform differs from one produced by other tools such as
the `arch` command.  Valid OS and architecture name combinations are listed as
values for $GOOS and $GOARCH at https://golang.org/doc/install/source#environment,
and can also be found by running `go tool dist list`.

The `buildah build` command allows building images for all Linux architectures, even non-native architectures. When building images for a different architecture,  the `RUN` instructions require emulation software installed on the host provided by packages like `qemu-user-static`. Note: it is always preferred to build images on the native architecture if possible.

**NOTE:** The `--platform` option may not be used in combination with the `--arch`, `--os`, or `--variant` options.

**--preprocess**

If specified, will always attempt to use the C Preprocessor cpp(1),
even if the Containerfile doesn't end with the ".in" suffix.
Note: You can also configure this behavior by setting the BUILDAH\_PREPROCESS
environment variable to `1`, `true`, or `yes`.

**--pull**

Pull image policy. If not specified, the default is **missing**. If an explicit
**--pull** argument is provided without any value, use the **always** behavior.

- **always**: Pull base and SBOM scanner images from the registries listed in
registries.conf.  Raise an error if a base or SBOM scanner image is not found
in the registries, even if an image with the same name is present locally.

- **missing**: SBOM scanner images only if they could not be found in the local
containers storage.  Raise an error if no image could be found and the pull
fails.

- **never**: Do not pull base and SBOM scanner images from registries, use only
the local versions.  Raise an error if the image is not present locally.

- **newer**: Pull base and SBOM scanner images from the registries listed in
registries.conf if newer.  Raise an error if a base or SBOM scanner image is
not found in the registries when image with the same name is not present
locally.

**--quiet**, **-q**

Suppress output messages which indicate which instruction is being processed,
and of progress when pulling images from a registry, and when writing the
output image.

**--retry** *attempts*

Number of times to retry in case of failure when performing push/pull of images to/from registry.

Defaults to `3`.

**--retry-delay** *duration*

Duration of delay between retry attempts in case of failure when performing push/pull of images to/from registry.

Defaults to `2s`.

**--rewrite-timestamp**

When generating new layers for the image, ensure that no newly added content
bears a timestamp later than the value used by the **--source-date-epoch**
flag, if one was provided, by replacing any timestamps which are later than
that value, with that value.

**--rm** *bool-value*

Remove intermediate containers after a successful build (default true).

**--runtime** *path*

The *path* to an alternate OCI-compatible runtime, which will be used to run
commands specified by the **RUN** instruction. Default is `runc`, or `crun` when machine is configured to use cgroups V2.

Note: You can also override the default runtime by setting the BUILDAH\_RUNTIME
environment variable.  `export BUILDAH_RUNTIME=/usr/bin/crun`

**--runtime-flag** *flag*

Adds global flags for the container runtime. To list the supported flags, please
consult the manpages of the selected container runtime.

Note: Do not pass the leading `--` to the flag. To pass the runc flag `--log-format json`
to buildah build, the option given would be `--runtime-flag log-format=json`.

**--save-stages** *bool-value*

Preserve intermediate stage images instead of removing them after the build completes
(Default is `false`). By default, Buildah removes intermediate stage images to save space.
This option keeps those images, which can be useful for debugging multi-stage builds or
for reusing intermediate stages in subsequent builds.

`--save-stages` can be used with `--layers` and subsequent builds with `--layers` can use
the preserved intermediate layers as cache.

When combined with `--stage-labels`, all stage images (including the final image) will
include metadata labels for easier identification and management.

**--sbom** *preset*

Generate SBOMs (Software Bills Of Materials) for the output image by scanning
the working container and build contexts using the named combination of scanner
image, scanner commands, and merge strategy.  Must be specified with one or
more of **--sbom-image-output**, **--sbom-image-purl-output**, **--sbom-output**,
and **--sbom-purl-output**.  Recognized presets, and the set of options which
they equate to:

 - "syft", "syft-cyclonedx":
     --sbom-scanner-image=ghcr.io/anchore/syft
     --sbom-scanner-command="/syft scan -q dir:{ROOTFS} --output cyclonedx-json={OUTPUT}"
     --sbom-scanner-command="/syft scan -q dir:{CONTEXT} --output cyclonedx-json={OUTPUT}"
     --sbom-merge-strategy=merge-cyclonedx-by-component-name-and-version
 - "syft-spdx":
     --sbom-scanner-image=ghcr.io/anchore/syft
     --sbom-scanner-command="/syft scan -q dir:{ROOTFS} --output spdx-json={OUTPUT}"
     --sbom-scanner-command="/syft scan -q dir:{CONTEXT} --output spdx-json={OUTPUT}"
     --sbom-merge-strategy=merge-spdx-by-package-name-and-versioninfo
 - "trivy", "trivy-cyclonedx":
     --sbom-scanner-image=ghcr.io/aquasecurity/trivy
     --sbom-scanner-command="trivy filesystem -q {ROOTFS} --format cyclonedx --output {OUTPUT}"
     --sbom-scanner-command="trivy filesystem -q {CONTEXT} --format cyclonedx --output {OUTPUT}"
     --sbom-merge-strategy=merge-cyclonedx-by-component-name-and-version
 - "trivy-spdx":
     --sbom-scanner-image=ghcr.io/aquasecurity/trivy
     --sbom-scanner-command="trivy filesystem -q {ROOTFS} --format spdx-json --output {OUTPUT}"
     --sbom-scanner-command="trivy filesystem -q {CONTEXT} --format spdx-json --output {OUTPUT}"
     --sbom-merge-strategy=merge-spdx-by-package-name-and-versioninfo

**--sbom-image-output** *path*

When generating SBOMs, store the generated SBOM in the specified path in the
output image.  There is no default.

**--sbom-image-purl-output** *path*

When generating SBOMs, scan them for PURL ([package
URL](https://github.com/package-url/purl-spec/blob/master/PURL-SPECIFICATION.rst))
information, and save a list of found PURLs to the specified path in the output
image.  There is no default.

**--sbom-merge-strategy** *method*

If more than one **--sbom-scanner-command** value is being used, use the
specified method to merge the output from later commands with output from
earlier commands.  Recognized values include:

 - cat
     Concatenate the files.
 - merge-cyclonedx-by-component-name-and-version
     Merge the "component" fields of JSON documents, ignoring values from
     documents when the combination of their "name" and "version" values is
     already present.  Documents are processed in the order in which they are
     generated, which is the order in which the commands that generate them
     were specified.
 - merge-spdx-by-package-name-and-versioninfo
     Merge the "package" fields of JSON documents, ignoring values from
     documents when the combination of their "name" and "versionInfo" values is
     already present.  Documents are processed in the order in which they are
     generated, which is the order in which the commands that generate them
     were specified.

**--sbom-output** *file*

When generating SBOMs, store the generated SBOM in the named file on the local
filesystem.  There is no default.

**--sbom-purl-output** *file*

When generating SBOMs, scan them for PURL ([package
URL](https://github.com/package-url/purl-spec/blob/master/PURL-SPECIFICATION.rst))
information, and save a list of found PURLs to the named file in the local
filesystem.  There is no default.

**--sbom-scanner-command** *image*

Generate SBOMs by running the specified command from the scanner image.  If
multiple commands are specified, they are run in the order in which they are
specified.  These text substitutions are performed:
  - {ROOTFS}
      The root of the built image's filesystem, bind mounted.
  - {CONTEXT}
      The build context and additional build contexts, bind mounted.
  - {OUTPUT}
      The name of a temporary output file, to be read and merged with others or copied elsewhere.

**--sbom-scanner-image** *image*

Generate SBOMs using the specified scanner image.

**--secret**=**id=id[,src=*envOrFile*][,env=ENV][,type=file|env]**

Pass secret information to be used in the Containerfile for building images
in a safe way that will not end up stored in the final image, or be seen in other stages.
The value of the secret will be read from an environment variable or file named
by the "id" option, or named by the "src" option if it is specified, or from an
environment variable specified by the "env" option.
The secret will be mounted in the container at `/run/secrets/<id>` by default.

To later use the secret, use the --mount flag in a `RUN` instruction within a `Containerfile`:

`RUN --mount=type=secret,id=mysecret cat /run/secrets/mysecret`

The location of the secret in the container can be overridden using the
"target", "dst", or "destination" option of the `RUN --mount` flag.

`RUN --mount=type=secret,id=mysecret,target=/run/secrets/myothersecret cat /run/secrets/myothersecret`

The secret may alternatively be exposed as an environment variable to the process:

`RUN --mount=type=secret,id=mysecret,env=FOO sh -c 'echo "Hello $FOO"'`

Note: changing the contents of secret files will not trigger a rebuild of layers that use said secrets.

**--security-opt**=[]

Security Options

  "apparmor=unconfined"   : Turn off apparmor confinement for the container
  "apparmor=your-profile" : Set the apparmor confinement profile for the container

  "label=user:USER"       : Set the label user for the container
  "label=role:ROLE"       : Set the label role for the container
  "label=type:TYPE"       : Set the label type for the container
  "label=level:LEVEL"     : Set the label level for the container
  "label=disable"         : Turn off label confinement for the container

  "mask=_/path/1:/path/2_": The paths to mask separated by a colon. A masked path cannot be accessed inside the container.

  "no-new-privileges"     : Disable container processes from gaining additional privileges

  "seccomp=unconfined"    : Turn off seccomp confinement for the container
  "seccomp=profile.json   : JSON configuration for a seccomp filter

  "unmask=_ALL_ or _/path/1:/path/2_, or shell expanded paths (/proc/*): Paths to unmask separated by a colon. If set to **ALL**, it unmasks all the paths that are masked or made read-only by default.
  The default masked paths are **/proc/acpi, /proc/interrupts, /proc/kcore, /proc/keys, /proc/latency_stats, /proc/sched_debug, /proc/scsi, /proc/timer_list, /proc/timer_stats, /sys/devices/virtual/powercap, /sys/firmware**, and **/sys/fs/selinux**.  The default paths that are read-only are **/proc/asound**, **/proc/bus**, **/proc/fs**, **/proc/irq**, **/proc/sys**, and **/proc/sysrq-trigger**.

**--shm-size**=""

Size of `/dev/shm`. The format is `<number><unit>`. `number` must be greater than `0`.
Unit is optional and can be `b` (bytes), `k` (kilobytes), `m`(megabytes), or `g` (gigabytes).
If you omit the unit, the system uses bytes. If you omit the size entirely, the system uses `64m`.

**--sign-by** *fingerprint*

Sign the built image using the GPG key that matches the specified fingerprint.

**--skip-unused-stages** *bool-value*

Skip stages in multi-stage builds which don't affect the target stage. (Default is `true`).

**--source-date-epoch** *seconds*

Set the "created" timestamp for the built image to this number of seconds since
the epoch (Unix time 0, i.e., 00:00:00 UTC on 1 January 1970) (default is to
use the value set in the `SOURCE_DATE_EPOCH` environment variable, or the
current time if it is not set).

The "created" timestamp is written into the image's configuration and manifest
when the image is committed, so running the same build two different times
will ordinarily produce images with different sha256 hashes, even if no other
changes were made to the Containerfile and build context.

When this flag is set, a `SOURCE_DATE_EPOCH` build arg will provide its value
for a stage in which it is declared.

When this flag is set, the image configuration's "created" timestamp is always
set to the time specified, which should allow for identical images to be built
at different times using the same set of inputs.

When this flag is set, output written as specified to the **--output** flag
will bear exactly the specified timestamp.

Conflicts with the similar **--timestamp** flag, which also sets its specified
time on the contents of new layers.

**--source-policy-file** *pathname*

Specifies the path to a BuildKit-compatible source policy JSON file.  When
specified, source references (e.g., base images in FROM instructions) are
evaluated against the policy rules before being used.

Source policies allow controlling which images can be used as base images and
optionally converting image references (e.g., pinning tags to specific digests)
without modifying Containerfiles.  This is useful for enforcing organizational
policies and ensuring build reproducibility.

The policy file is a JSON document containing an array of rules.  Each rule has:
- **action**: The action to take when the rule matches.  Valid actions are:
  - **ALLOW**: Explicitly allow the source (no transformation).
  - **DENY**: Block the source and fail the build.
  - **CONVERT**: Transform the source to a different reference specified in `updates`.
- **selector**: Specifies which sources the rule applies to.
  - **identifier**: The source identifier to match (e.g., `docker-image://docker.io/library/alpine:latest`).
  - **matchType**: How to match the identifier.  Valid types are `EXACT` and `WILDCARD` (supports `*` and `?` glob patterns).  Defaults to `WILDCARD` if not specified.
- **updates**: For `CONVERT` actions, specifies the replacement identifier.

Rules are evaluated in order; the first matching rule wins.  If no rule matches,
the source is allowed by default.

Note: Source policy CONVERT rules are processed after **--build-context** substitutions
but before any substitutions specified in **containers-registries.conf(5)**.  This provides
multiple ways to override which base image is used for a particular stage, in order of
precedence: `--build-context`, then source policy, then registries.conf.

Example policy file that pins alpine:latest to a specific digest:
```json
{
  "rules": [
    {
      "action": "CONVERT",
      "selector": {
        "identifier": "docker-image://docker.io/library/alpine:latest"
      },
      "updates": {
        "identifier": "docker-image://docker.io/library/alpine@sha256:..."
      }
    }
  ]
}
```

Example policy file that denies all ubuntu images:
```json
{
  "rules": [
    {
      "action": "DENY",
      "selector": {
        "identifier": "docker-image://docker.io/library/ubuntu:*",
        "matchType": "WILDCARD"
      }
    }
  ]
}
```

**--squash**

Squash all layers, including those from base image(s), into one single layer. (Default is false).

By default, Buildah preserves existing base-image layers and adds only one new layer on a build.
The --layers option can be used to preserve intermediate build layers.

**--ssh**=**default**|*id[=socket>|<key>[,<key>]*

SSH agent socket or keys to expose to the build.
The socket path can be left empty to use the value of `default=$SSH_AUTH_SOCK`

To later use the ssh agent, use the --mount flag in a `RUN` instruction within a `Containerfile`:

`RUN --mount=type=secret,id=id mycmd`

**NOTE:** When using the non-default `root` user, then either `mode`, `uid` or `gid` options
must be specified with the `mount` options. For example making ssh socket available to `app`
user with `uid=50000`:

```
# Switch to application user (Note that uid depends on container image)
USER app
# ...and check whether ssh identities are available
RUN --mount=type=ssh,uid=50000 \
    ssh-add -L
```

**--stage-labels** *bool-value*

Add metadata labels to all intermediate stage images of the multistage build,
including the final image (Default is `false`).
This option requires `--save-stages` to be enabled.

When enabled, all intermediate stage images and final image will be labeled with:
  - `io.buildah.stage.name`: The stage alias (from `FROM ... AS alias`), or stage position
    if no alias is specified
  - `io.buildah.stage.base`: The base image used by this stage (pullspec or image ID
    when stage uses another stage as base)

These labels make it easier to identify, query, and manage images from multi-stage builds.

**--stdin**

Pass stdin into the RUN containers. Sometimes commands being RUN within a Containerfile
want to request information from the user. For example apt asking for a confirmation for install.
Use --stdin to be able to interact from the terminal during the build.

**--tag**, **-t** *imageName*

Specifies the name which will be assigned to the resulting image if the build
process completes successfully.
If _imageName_ does not include a registry name component, the registry name *localhost* will be prepended to the image name.

The **--tag** option supports all transports from `containers-transports(5)`.
If no transport is specified, the `containers-storage` (i.e., local storage) transport is used.

  __buildah build --tag=oci-archive:./foo.ociarchive .__

  __buildah build -t quay.io/username/foo  .__

**--target** *stageName*

Set the target build stage to build.  When building a Containerfile with multiple build stages, --target
can be used to specify an intermediate build stage by name as the final stage for the resulting image.
Commands after the target stage will be skipped.

**--timestamp** *seconds*

Set the "created" timestamp for the built image to this number of seconds since
the epoch (Unix time 0, i.e., 00:00:00 UTC on 1 January 1970) (defaults to
current time).

The "created" timestamp is written into the image's configuration and manifest
when the image is committed, so running the same build two different times
will ordinarily produce images with different sha256 hashes, even if no other
changes were made to the Containerfile and build context.

When --timestamp is set, the "created" timestamp is always set to the time
specified, which should allow for identical images to be built at different
times using the same set of inputs.

When --timestamp is set, all content in layers created as part of the build,
and output written as specified to the **--output** flag, will also bear this
same timestamp.

Conflicts with the similar **--source-date-epoch** flag, which by default does
not affect the timestamps of layer contents.

**--tls-verify** *bool-value*

Require HTTPS and verification of certificates when talking to container registries (defaults to true) and retrieving content from HTTPS locations for ADD instructions.  TLS verification cannot be used when talking to an insecure registry.

**--ulimit** *type*=*soft-limit*[:*hard-limit*]

Specifies resource limits to apply to processes launched when processing `RUN` instructions.
This option can be specified multiple times.  Recognized resource types
include:
  "core": maximum core dump size (ulimit -c)
  "cpu": maximum CPU time (ulimit -t)
  "data": maximum size of a process's data segment (ulimit -d)
  "fsize": maximum size of new files (ulimit -f)
  "locks": maximum number of file locks (ulimit -x)
  "memlock": maximum amount of locked memory (ulimit -l)
  "msgqueue": maximum amount of data in message queues (ulimit -q)
  "nice": niceness adjustment (nice -n, ulimit -e)
  "nofile": maximum number of open files (ulimit -n)
  "nofile": maximum number of open files (1048576); when run by root
  "nproc": maximum number of processes (ulimit -u)
  "nproc": maximum number of processes (1048576); when run by root
  "rss": maximum size of a process's (ulimit -m)
  "rtprio": maximum real-time scheduling priority (ulimit -r)
  "rttime": maximum amount of real-time execution between blocking syscalls
  "sigpending": maximum number of pending signals (ulimit -i)
  "stack": maximum stack size (ulimit -s)

**--unsetannotation** *annotation*

Unset the image annotation, causing the annotation not to be inherited from the base image.

**--unsetenv** *env*

Unset environment variables from the final image.

**--unsetlabel** *label*

Unset the image label, causing the label not to be inherited from the base image.

**--userns** *how*

Sets the configuration for user namespaces when handling `RUN` instructions.
The configured value can be "" (the empty string) , "private" or "auto" to indicate
that a new user namespace should be created, it can be "host" to indicate that
the user namespace in which `buildah` itself is being run should be reused, or
it can be the path to an user namespace which is already in use by another
process.

auto: automatically create a unique user namespace.

The --userns=auto flag, requires that the user name containers and a range of subordinate user ids that the build container is allowed to use be specified in the /etc/subuid and /etc/subgid files.

Example: `containers:2147483647:2147483648`.

Buildah allocates unique ranges of UIDs and GIDs from the containers subordinate user ids. The size of the ranges is based on the number of UIDs required in the image. The number of UIDs and GIDs can be overridden with the size option.

Valid `auto` options:

* gidmapping=CONTAINER_GID:HOST_GID:SIZE: to force a GID mapping to be present in the user namespace.
* size=SIZE: to specify an explicit size for the automatic user namespace. e.g. --userns=auto:size=8192. If size is not specified, auto will estimate a size for the user namespace.
* uidmapping=CONTAINER_UID:HOST_UID:SIZE: to force a UID mapping to be present in the user namespace.

**--userns-gid-map** *mapping*

Directly specifies a GID mapping which should be used to set ownership, at the
filesystem level, on the working container's contents.
Commands run when handling `RUN` instructions will default to being run in
their own user namespaces, configured using the UID and GID maps.

Entries in this map take the form of one or more colon-separated triples of a starting
in-container GID, a corresponding starting host-level GID, and the number of
consecutive IDs which the map entry represents.

This option overrides the *remap-gids* setting in the *options* section of
/etc/containers/storage.conf.

If this option is not specified, but a global --userns-gid-map setting is
supplied, settings from the global option will be used.

**--userns-gid-map-group** *group*

Specifies that a GID mapping which should be used to set ownership, at the
filesystem level, on the working container's contents, can be found in entries
in the `/etc/subgid` file which correspond to the specified group.
Commands run when handling `RUN` instructions will default to being run in
their own user namespaces, configured using the UID and GID maps.
If --userns-uid-map-user is specified, but --userns-gid-map-group is not
specified, `buildah` will assume that the specified user name is also a
suitable group name to use as the default setting for this option.

Users can specify the maps directly using `--userns-gid-map` described in the buildah(1) man page.

**NOTE:** When this option is specified by a rootless user, the specified mappings are relative to the rootless usernamespace in the container, rather than being relative to the host as it would be when run rootful.

**--userns-uid-map** *mapping*

Directly specifies a UID mapping which should be used to set ownership, at the
filesystem level, on the working container's contents.
Commands run when handling `RUN` instructions will default to being run in
their own user namespaces, configured using the UID and GID maps.

Entries in this map take the form of one or more colon-separated triples of a starting
in-container UID, a corresponding starting host-level UID, and the number of
consecutive IDs which the map entry represents.

This option overrides the *remap-uids* setting in the *options* section of
/etc/containers/storage.conf.

If this option is not specified, but a global --userns-uid-map setting is
supplied, settings from the global option will be used.

**--userns-uid-map-user** *user*

Specifies that a UID mapping which should be used to set ownership, at the
filesystem level, on the working container's contents, can be found in entries
in the `/etc/subuid` file which correspond to the specified user.
Commands run when handling `RUN` instructions will default to being run in
their own user namespaces, configured using the UID and GID maps.
If --userns-gid-map-group is specified, but --userns-uid-map-user is not
specified, `buildah` will assume that the specified group name is also a
suitable user name to use as the default setting for this option.

**NOTE:** When this option is specified by a rootless user, the specified mappings are relative to the rootless usernamespace in the container, rather than being relative to the host as it would be when run rootful.

**--uts** *how*

Sets the configuration for UTS namespaces when handling `RUN` instructions.
The configured value can be "" (the empty string) or "container" to indicate
that a new UTS namespace should be created, or it can be "host" to indicate
that the UTS namespace in which `buildah` itself is being run should be reused,
or it can be the path to a UTS namespace which is already in use by another
process.

**--variant**=""

Set the architecture variant of the image to be pulled.

**--volume**, **-v**[=*[HOST-DIR:CONTAINER-DIR[:OPTIONS]]*]

Mount a host directory into containers when executing *RUN* instructions during
the build.  The `OPTIONS` are a comma delimited list and can be:

   * [rw|ro]
   * [U]
   * [z|Z|O]
   * [`[r]shared`|`[r]slave`|`[r]private`] <sup>[[1]](#Footnote1)</sup>

The `CONTAINER-DIR` must be an absolute path such as `/src/docs`. The `HOST-DIR`
must be an absolute path as well. Buildah bind-mounts the `HOST-DIR` to the
path you specify. For example, if you supply `/foo` as the host path,
Buildah copies the contents of `/foo` to the container filesystem on the host
and bind mounts that into the container.

You can specify multiple  **-v** options to mount one or more mounts to a
container.

  `Write Protected Volume Mounts`

You can add the `:ro` or `:rw` suffix to a volume to mount it read-only or
read-write mode, respectively. By default, the volumes are mounted read-write.
See examples.

  `Chowning Volume Mounts`

By default, Buildah does not change the owner and group of source volume directories mounted into containers. If a container is created in a new user namespace, the UID and GID in the container may correspond to another UID and GID on the host.

The `:U` suffix tells Buildah to use the correct host UID and GID based on the UID and GID within the container, to change the owner and group of the source volume.

  `Labeling Volume Mounts`

Labeling systems like SELinux require that proper labels are placed on volume
content mounted into a container. Without a label, the security system might
prevent the processes running inside the container from using the content. By
default, Buildah does not change the labels set by the OS.

To change a label in the container context, you can add either of two suffixes
`:z` or `:Z` to the volume mount. These suffixes tell Buildah to relabel file
objects on the shared volumes. The `z` option tells Buildah that two containers
share the volume content. As a result, Buildah labels the content with a shared
content label. Shared volume labels allow all containers to read/write content.
The `Z` option tells Buildah to label the content with a private unshared label.
Only the current container can use a private volume.

  `Overlay Volume Mounts`

   The `:O` flag tells Buildah to mount the directory from the host as a temporary storage using the Overlay file system. The `RUN` command containers are allowed to modify contents within the mountpoint and are stored in the container storage in a separate directory.  In Overlay FS terms the source directory will be the lower, and the container storage directory will be the upper. Modifications to the mount point are destroyed when the `RUN` command finishes executing, similar to a tmpfs mount point.

  Any subsequent execution of `RUN` commands sees the original source directory content, any changes from previous RUN commands no longer exist.

  One use case of the `overlay` mount is sharing the package cache from the host into the container to allow speeding up builds.

  Note:

 - The `O` flag is not allowed to be specified with the `Z` or `z` flags. Content mounted into the container is labeled with the private label.
       On SELinux systems, labels in the source directory must be readable by the container label. If not, SELinux container separation must be disabled for the container to work.
 - Modification of the directory volume mounted into the container with an overlay mount can cause unexpected failures.  It is recommended that you do not modify the directory until the container finishes running.

By default bind mounted volumes are `private`. That means any mounts done
inside container will not be visible on the host and vice versa. This behavior can
be changed by specifying a volume mount propagation property.

When the mount propagation policy is set to `shared`, any mounts completed inside
the container on that volume will be visible to both the host and container. When
the mount propagation policy is set to `slave`, one way mount propagation is enabled
and any mounts completed on the host for that volume will be visible only inside of the container.
To control the mount propagation property of the volume use the `:[r]shared`,
`:[r]slave` or `:[r]private` propagation flag. The propagation property can
be specified only for bind mounted volumes and not for internal volumes or
named volumes. For mount propagation to work on the source mount point (the mount point
where source dir is mounted on) it has to have the right propagation properties. For
shared volumes, the source mount point has to be shared. And for slave volumes,
the source mount has to be either shared or slave. <sup>[[1]](#Footnote1)</sup>

Use `df <source-dir>` to determine the source mount and then use
`findmnt -o TARGET,PROPAGATION <source-mount-dir>` to determine propagation
properties of source mount, if `findmnt` utility is not available, the source mount point
can be determined by looking at the mount entry in `/proc/self/mountinfo`. Look
at `optional fields` and see if any propagation properties are specified.
`shared:X` means the mount is `shared`, `master:X` means the mount is `slave` and if
nothing is there that means the mount is `private`. <sup>[[1]](#Footnote1)</sup>

To change propagation properties of a mount point use the `mount` command. For
example, to bind mount the source directory `/foo` do
`mount --bind /foo /foo` and `mount --make-private --make-shared /foo`. This
will convert /foo into a `shared` mount point.  The propagation properties of the source
mount can be changed directly. For instance if `/` is the source mount for
`/foo`, then use `mount --make-shared /` to convert `/` into a `shared` mount.

## BUILD TIME VARIABLES

The ENV instruction in a Containerfile can be used to define variable values.  When the image
is built, the values will persist in the container image.  At times it is more convenient to
change the values in the Containerfile via a command-line option rather than changing the
values within the Containerfile itself.

The following variables can be used in conjunction with the `--build-arg` option to override the
corresponding values set in the Containerfile using the `ENV` instruction.

  * HTTP_PROXY
  * HTTPS_PROXY
  * FTP_PROXY
  * NO_PROXY

Please refer to the [Using Build Time Variables](#using-build-time-variables) section of the Examples.

## EXAMPLE

### Build an image using local Containerfiles

buildah build .

buildah build -f Containerfile .

cat ~/Containerfile | buildah build -f - .

buildah build -f Containerfile.simple -f Containerfile.notsosimple .

buildah build --timestamp=$(date '+%s') -t imageName .

buildah build -t imageName .

buildah build --tls-verify=true -t imageName -f Containerfile.simple .

buildah build --tls-verify=false -t imageName .

buildah build --runtime-flag log-format=json .

buildah build -f Containerfile --runtime-flag debug .

buildah build --authfile /tmp/auths/myauths.json --cert-dir ~/auth --tls-verify=true --creds=username:password -t imageName -f Containerfile.simple .

buildah build --memory 40m --cpu-period 10000 --cpu-quota 50000 --ulimit nofile=1024:1028 -t imageName .

buildah build --security-opt label=level:s0:c100,c200 --cgroup-parent /path/to/cgroup/parent -t imageName .

buildah build --arch=arm --variant v7 -t imageName .

buildah build --volume /home/test:/myvol:ro,Z -t imageName .

buildah build -v /home/test:/myvol:z,U -t imageName .

buildah build -v /var/lib/dnf:/var/lib/dnf:O -t imageName .

buildah build --layers -t imageName .

buildah build --save-stages -t imageName .

buildah build --save-stages --layers -t imageName .

buildah build --save-stages --stage-labels -t imageName .

buildah build --save-stages --stage-labels --layers -t imageName .

buildah build --no-cache -t imageName .

buildah build -f Containerfile --layers --force-rm -t imageName .

buildah build --no-cache --rm=false -t imageName .

buildah build --dns-search=example.com --dns=223.5.5.5 --dns-option=use-vc .

buildah build -f Containerfile.in --cpp-flag="-DDEBUG" -t imageName .

buildah build --network mynet .

buildah build --env LANG=en_US.UTF-8 -t imageName .

buildah build --env EDITOR -t imageName .

buildah build --unsetenv LANG -t imageName .

buildah build --os-version 10.0.19042.1645 -t imageName .

buildah build --os-feature win32k -t imageName .

buildah build --os-feature win32k- -t imageName .

buildah build --secret=id=mysecret .

buildah build --secret=id=mysecret,env=MYSECRET .

buildah build --secret=id=mysecret,src=MYSECRET,type=env .

buildah build --secret=id=mysecret,src=.mysecret,type=file .

buildah build --secret=id=mysecret,src=.mysecret .

### Building an image with a source policy

buildah build --source-policy-file /etc/buildah/source-policy.json -t imageName .

### Using FROM --after for explicit stage dependencies

When using local transports like `FROM oci-archive:file.ociarchive` where the file is produced by an earlier stage, Buildah cannot automatically detect the dependency. Use the `--after` flag on the FROM instruction to declare explicit stage dependencies:

```Dockerfile
FROM quay.io/skopeo/stable AS builder
RUN --mount=type=bind,target=/src,rw skopeo copy docker://quay.io/fedora/fedora-minimal oci-archive:/src/fedora.ociarchive

FROM --after=builder oci-archive:fedora.ociarchive
# This stage will wait for builder to complete before evaluating FROM
```

### Building an multi-architecture image using the --manifest option (requires emulation software)

buildah build --arch arm --manifest myimage /tmp/mysrc

buildah build --arch amd64 --manifest myimage /tmp/mysrc

buildah build --arch s390x --manifest myimage /tmp/mysrc

buildah bud --platform linux/s390x,linux/ppc64le,linux/amd64 --manifest myimage /tmp/mysrc

buildah build --platform linux/arm64 --platform linux/amd64 --manifest myimage /tmp/mysrc

buildah bud --all-platforms --manifest myimage /tmp/mysrc

### Building an image using (--output) custom build output

buildah build -o out .

buildah build --output type=local,dest=out .

buildah build --output type=tar,dest=out.tar .

buildah build -o - . > out.tar

### Preserving and querying intermediate stage images

Build a multi-stage image while preserving intermediate stages with metadata labels:

buildah build --save-stages --stage-labels -t myapp .

Find an intermediate image for a specific stage name:

buildah images --filter "label=io.buildah.stage.name=builder"

Find an intermediate image for a specific stage base image:

buildah images --filter "label=io.buildah.stage.base=golang:1.21"

### Building an image using a URL

  This will clone the specified GitHub repository from the URL and use it as context. The Containerfile or Dockerfile at the root of the repository is used as the context of the build. This only works if the GitHub repository is a dedicated repository.

  buildah build https://github.com/containers/PodmanHello.git

  Note: Github does not support using `git://` for performing `clone` operation due to recent changes in their security guidance (https://github.blog/2021-09-01-improving-git-protocol-security-github/). Use an `https://` URL if the source repository is hosted on Github.

### Building an image using a URL to a tarball'ed context

  Buildah will fetch the tarball archive, decompress it and use its contents as the build context.  The Containerfile or Dockerfile at the root of the archive and the rest of the archive will get used as the context of the build. If you pass an -f PATH/Containerfile option as well, the system will look for that file inside the contents of the tarball.

  buildah build -f dev/Containerfile https://10.10.10.1/buildah/context.tar.gz

  Note: supported compression formats are 'xz', 'bzip2', 'gzip' and 'identity' (no compression).

### Using Build Time Variables

#### Replace the value set for the HTTP_PROXY environment variable within the Containerfile.

buildah build --build-arg=HTTP_PROXY="http://127.0.0.1:8321"

## ENVIRONMENT

**BUILD\_REGISTRY\_SOURCES**

BUILD\_REGISTRY\_SOURCES, if set, is treated as a JSON object which contains
lists of registry names under the keys `insecureRegistries`,
`blockedRegistries`, and `allowedRegistries`.

When pulling an image from a registry, if the name of the registry matches any
of the items in the `blockedRegistries` list, the image pull attempt is denied.
If there are registries in the `allowedRegistries` list, and the registry's
name is not in the list, the pull attempt is denied.

**TMPDIR**
The TMPDIR environment variable allows the user to specify where temporary files
are stored while pulling and pushing images.  Defaults to '/var/tmp'.

## Files

### `.containerignore`/`.dockerignore`

If the .containerignore/.dockerignore file exists in the context directory,
`buildah build` reads its contents. If both exist, then .containerignore is used.
Use the `--ignorefile` flag to override the ignore file path location. Buildah uses the content to exclude files and directories from the context directory, when executing COPY and ADD directives in the Containerfile/Dockerfile.
COPY and ADD also support **--exclude**; patterns are relative to the copy source.

Users can specify a series of Unix shell globals in a
.containerignore/.dockerignore file to identify files/directories to exclude.

Buildah supports a special wildcard string `**` which matches any number of
directories (including zero). For example, `**/*.go` will exclude all files that
end with .go that are found in all directories.

Example .containerignore file:

```
# exclude this content for image
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

Exclude all doc files except Help.doc from the image.

This functionality is compatible with the handling of .containerignore files described here:

https://github.com/containers/common/blob/main/docs/containerignore.5.md

Note: when the argument to ADD is a git repository, the local .containerignore is not applied.

**registries.conf** (`/etc/containers/registries.conf`)

registries.conf is the configuration file which specifies which container registries should be consulted when completing image names which do not include a registry or domain portion.

**policy.json** (`/etc/containers/policy.json`)

Signature policy file.  This defines the trust policy for container images.  Controls which container registries can be used for image, and whether or not the tool should trust the images.

## SEE ALSO
buildah(1), cpp(1), buildah-login(1), docker-login(1), namespaces(7), pid\_namespaces(7), containers-policy.json(5), containers-registries.conf(5), user\_namespaces(7), crun(1), runc(8), containers.conf(5), oci-hooks(5), containers-transports(5), containers-auth.json(5)

## FOOTNOTES
<a name="Footnote1">1</a>: The Buildah project is committed to inclusivity, a core value of open source. The `master` and `slave` mount propagation terminology used here is problematic and divisive, and should be changed. However, these terms are currently used within the Linux kernel and must be used as-is at this time. When the kernel maintainers rectify this usage, Buildah will follow suit immediately.
