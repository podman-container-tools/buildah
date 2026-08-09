# buildah-login "1" "Apr 2019" "buildah"

## NAME
buildah\-login - Login to a container registry

## SYNOPSIS
**buildah login** [*options*] *registry*

## DESCRIPTION
**buildah login** logs into a specified registry server with the correct username
and password. **buildah login** reads in the username and password from STDIN.
The username and password can also be set using the **username** and **password** flags.
The path of the authentication file can be specified by the user by setting the **authfile**
flag. The default path used is **${XDG\_RUNTIME_DIR}/containers/auth.json**. If XDG_RUNTIME_DIR
is not set, the default is /run/user/$UID/containers/auth.json.

**buildah [GLOBAL OPTIONS]**

**buildah login [GLOBAL OPTIONS]**

**buildah login [OPTIONS] REGISTRY [GLOBAL OPTIONS]**

## OPTIONS

**--authfile**

Path of the authentication file. Default is ${XDG_RUNTIME_DIR}/containers/auth.json. See containers-auth.json(5) for more information. This file is created using `buildah login`.

Note: You can also override the default path of the authentication file by setting the REGISTRY\_AUTH\_FILE
environment variable. `export REGISTRY_AUTH_FILE=path`

**--cert-dir** *path*

Use certificates at *path* (\*.crt, \*.cert, \*.key) to connect to the registry.
The default certificates directory is _/etc/containers/certs.d_.

**--compat-auth-file**=*path*

Instead of updating the default credentials file, update the one at *path*, and use a Docker-compatible format.

**--get-login**

Return the logged-in user for the registry.  Return error if no login is found.

**--help**, **-h**

Print usage statement

**--password**, **-p**

Password for registry

**--password-stdin**

Take the password from stdin

**--tls-verify**

Require HTTPS and verification of certificates when talking to container registries (default: true). If explicitly set to true,
then TLS verification will be used. If set to false, then TLS verification will not be used. If not specified,
TLS verification will be used unless the target registry is listed as an insecure registry in registries.conf.
TLS verification cannot be used when talking to an insecure registry.

This is a boolean flag. Use the `=` form when passing a value (for example
`--tls-verify=false`). A bare `--tls-verify` is treated as true; a space-separated
form such as `--tls-verify false` is not accepted (the word after the flag is
parsed as a registry name).

**--username**, **-u**

Username for registry

**--verbose**, **-v**

print detailed information about credential store

## EXAMPLES

```
$ buildah login quay.io
Username: qiwanredhat
Password:
Login Succeeded!
```

```
$ buildah login -u testuser -p testpassword localhost:5000
Login Succeeded!
```

```
$ buildah login --authfile ./auth.json quay.io
Username: qiwanredhat
Password:
Login Succeeded!
```

```
$ buildah login --tls-verify=false -u test -p test localhost:5000
Login Succeeded!
```

```
$ buildah login --cert-dir /etc/containers/certs.d/ -u foo -p bar localhost:5000
Login Succeeded!
```

```
$ buildah login -u testuser  --password-stdin < pw.txt quay.io
Login Succeeded!
```

```
$ echo $testpassword | buildah login -u testuser --password-stdin quay.io
Login Succeeded!
```

## SEE ALSO
buildah(1), buildah-logout(1), containers-auth.json(5)
