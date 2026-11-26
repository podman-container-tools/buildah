# buildah-bake "1" "November 2026" "buildah"

## NAME

buildah-bake - Build images using a Bake definition file

## SYNOPSIS

**buildah bake** [*options*] [*target*...]

## DESCRIPTION

Builds one or more images using a Bake definition file. Bake provides a way
to describe multiple image builds in a single configuration file, so that
related images can share settings and be built together.

Buildah currently supports Bake definitions written in JSON. HCL definitions
are not yet supported.

By default, Buildah looks for a file named `docker-bake.json` in the current
working directory. A different file can be specified using the **--file**
option.

A Bake definition contains targets, groups and variables. Targets describe
how an image should be built, while groups allow several targets to be
selected together. Targets can also inherit settings from other targets,
which can help avoid repeating the same configuration.

If no targets are specified on the command line, Buildah selects the
`default` group or target from the definition file.

Targets selected through a group or by specifying multiple names are built
in sequence. Each target is built using the normal Buildah build
functionality.

Buildah supports a subset of the attributes available in Docker Buildx Bake.
Unsupported attributes are rejected rather than silently ignored. See the
sections below for details.

## OPTIONS

**--file**, **-f** *file*

Path to the JSON Bake definition file.

If not specified, Buildah looks for `docker-bake.json` in the current
working directory.

**--list** *type*

List information from the Bake definition without building any images.

Supported values are:

- **targets**: List the available targets and groups, along with their
  descriptions.
- **variables**: List the variables declared in the definition, including
  their types, values and descriptions.

Targets and groups whose names begin with an underscore are omitted from
the target listing.

**--var** *NAME=value*

Set a Bake variable to the specified value. This option can be used
multiple times.

The variable must be declared in the Bake definition. If the same variable
is specified more than once, the last value takes precedence.

Command-line values take precedence over environment variables and default
values.

## TARGET SELECTION

One or more target or group names can be specified after the options.

For example:

```bash
buildah bake frontend

buildah bake frontend backend

buildah bake production
```

When a group is selected, all of its targets are selected. Groups can also
contain other groups.

If the same target is included more than once, it is only built once.

If no target is specified, Buildah selects the `default` group or target.

Wildcard patterns can be used to select matching target and group names.
The patterns support the matching syntax provided by Go's `path.Match`,
including `*`, `?` and character classes.

For example:

```bash
buildah bake 'web-*'
```

The quotes are useful when running from a shell that would otherwise expand
the wildcard itself.

## BAKE DEFINITION

A Bake definition is a JSON file containing one or more of the following
top-level sections:

- **target**: Defines the image builds.
- **group**: Defines groups of targets which can be selected together.
- **variable**: Defines variables which can be used in target configuration.

Unknown fields are not accepted. This helps catch spelling mistakes and
attributes which are not currently supported by Buildah.

### Targets

Targets describe the images to build. Each target is defined by a name and
a set of attributes.

The following target attributes are currently supported:

- **context**: The build context directory or remote context.
- **dockerfile**: The Containerfile or Dockerfile to use. If omitted,
  `Dockerfile` is used.
- **tags**: A list of image names to apply to the built image.
- **platforms**: A list of target platforms.
- **args**: Build arguments passed to the build.
- **target**: The named build stage to build.
- **labels**: Image labels to apply to the built image.
- **no-cache**: Whether to disable the build cache.
- **pull**: Whether to always pull base images. A value of `true` uses the
  always-pull policy; `false` uses the default missing-image policy.
- **network**: Network mode to use when running build instructions.
- **ulimits**: A list of ulimit settings to apply during the build.
- **inherits**: A list of other targets from which to inherit configuration.
- **description**: A description of the target, shown when listing targets.

When more than one platform is specified, Buildah builds each platform and
creates a manifest list. At least one tag must be specified for a
multi-platform target. The first tag is used as the manifest list name,
and any additional tags are added as aliases for that manifest list.

For example, the following target builds for both amd64 and arm64 and
applies two tags to the resulting manifest list:

```json
{
  "target": {
    "frontend": {
      "context": "./frontend",
      "dockerfile": "Dockerfile",
      "platforms": ["linux/amd64", "linux/arm64"],
      "tags": [
        "example/frontend:latest",
        "example/frontend:stable"
      ]
    }
  }
}
```

In this example, `example/frontend:latest` is used as the manifest list
name, while `example/frontend:stable` is added as an alias.

The context defaults to the current working directory when it is not
specified. The Dockerfile defaults to `Dockerfile`, rather than Buildah's
usual Containerfile-first discovery.

Local Dockerfile paths are resolved relative to the build context. For
remote contexts, the Dockerfile path is passed through unchanged.

### Groups

Groups allow several targets to be selected using a single name. A group
contains a list of target or group names.

For example:

```json
{
  "group": {
    "default": {
      "targets": ["frontend", "backend"]
    },
    "production": {
      "description": "Production images",
      "targets": ["frontend", "backend"]
    }
  }
}
```

Running `buildah bake` with this definition selects the targets in the
`default` group.

Groups can reference other groups, but circular group references are not
allowed.

If a group and a target have the same name, the group takes precedence
when resolving that name.

### Target inheritance

A target can inherit configuration from one or more other targets using
the **inherits** attribute.

This is useful when several images share common build settings. The child
target can override settings inherited from its parents.

For example:

```json
{
  "target": {
    "base": {
      "context": ".",
      "args": {
        "VERSION": "1.0"
      },
      "labels": {
        "org.example.vendor": "example"
      }
    },
    "frontend": {
      "inherits": ["base"],
      "tags": ["example/frontend:latest"],
      "args": {
        "COMPONENT": "frontend"
      }
    },
    "backend": {
      "inherits": ["base"],
      "tags": ["example/backend:latest"],
      "args": {
        "COMPONENT": "backend"
      }
    }
  }
}
```

In this example, both images inherit the context, version argument and
vendor label from `base`, while defining their own tags and component
arguments.

When multiple parents are specified, later parents take precedence over
earlier ones. Values explicitly set by the child take precedence over
inherited values.

Build arguments and labels are merged, with child values replacing
inherited values with the same name. Tags and platforms are replaced by
values specified by the child. Ulimits from inherited targets are combined.

Circular inheritance is not supported.

### Variables

Variables can be declared in the **variable** section and used in
supported target attributes.

A variable can have a default value, a type and a description.

The supported variable types are:

- **string**
- **bool**
- **number**

If the type is omitted, Buildah infers it from the default value.

For example:

```json
{
  "variable": {
    "VERSION": {
      "type": "string",
      "default": "1.0",
      "description": "Image version"
    },
    "DEBUG": {
      "type": "bool",
      "default": false
    },
    "RELEASE": {
      "type": "string",
      "default": "latest"
    }
  },
  "target": {
    "frontend": {
      "tags": ["example/frontend:${VERSION}"],
      "args": {
        "DEBUG": "${DEBUG}"
      },
      "labels": {
        "org.example.release": "${RELEASE}"
      }
    }
  }
}
```

Variables are referenced using `${NAME}` syntax. To include a literal
variable reference without expanding it, use `$${NAME}`.

Variable references are expanded in the following target attributes:

- `context`
- `dockerfile`
- `target`
- `network`
- `ulimits`
- `tags`
- `platforms`
- Values in `args` and `labels`

Variables can also be referenced from other variable defaults. Circular
variable references are not supported.

A variable can be overridden using the **--var** option or by setting an
environment variable with the same name.

The precedence is:

1. Command-line **--var** value.
2. Environment variable.
3. Default value in the Bake definition.

For example:

```bash
VERSION=2.0 buildah bake frontend
```

or:

```bash
buildah bake --var VERSION=2.0 frontend
```

The environment variable lookup can be disabled by setting
`BUILDX_BAKE_DISABLE_VARS_ENV_LOOKUP=1`. Command-line overrides and defaults
are still used.

Variables without a default value can be supplied through the environment
or the command line. If such a variable is referenced and no value is
available, the build fails.

The **--list=variables** option can be used to inspect declared variables
and their resolved values.

## EXAMPLES

Build all targets in the default group:

```bash
buildah bake
```

Build a specific target:

```bash
buildah bake frontend
```

Build several targets:

```bash
buildah bake frontend backend
```

Use a different Bake definition:

```bash
buildah bake -f build.json
```

List available targets and groups:

```bash
buildah bake --list=targets
```

List declared variables:

```bash
buildah bake --list=variables
```

Override a variable:

```bash
buildah bake --var VERSION=2.0 frontend
```

Use an environment variable to override a default:

```bash
VERSION=2.0 buildah bake frontend
```

## LIMITATIONS

Buildah currently implements a subset of Docker Buildx Bake.

The following limitations apply:

- Only JSON Bake definitions are supported. HCL files are not supported.
- Only the target and group attributes documented above are supported.
- Targets selected together are built sequentially rather than in parallel.
- Complex variable values such as arrays and objects are not supported for
  interpolation into build attributes.

Other Buildx Bake features and attributes which are not listed in this
document are not currently supported.
