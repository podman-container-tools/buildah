# Buildah Self-assessment

## Table of contents

* [Metadata](#metadata)
  * [Security links](#security-links)
* [Overview](#overview)
  * [Actors](#actors)
  * [Actions](#actions)
  * [Background](#background)
  * [Goals](#goals)
  * [Non-goals](#non-goals)
* [Self-assessment use](#self-assessment-use)
* [Security functions and features](#security-functions-and-features)
* [Project compliance](#project-compliance)
* [Secure development practices](#secure-development-practices)
* [Security issue resolution](#security-issue-resolution)
* [Appendix](#appendix)

## Metadata

|||| | \-- | \-- | | Assessment Stage | Incomplete | | Software | [https://github.com/containers/buildah](https://github.com/containers/buildah) | | Security Provider | No | | Languages | Go | | SBOM | [https://github.com/containers/buildah/blob/main/go.mod](https://github.com/containers/buildah/blob/main/go.mod) |

### Security links

| Doc | url |
| :---- | :---- |
| Security file | [https://github.com/containers/buildah/blob/main/SECURITY.md](https://github.com/containers/buildah/blob/main/SECURITY.md) |

## Overview

Buildah is a tool that facilitates building Open Container Initiative (OCI) images. It provides a flexible and efficient way to create container images without requiring a running container daemon, emphasizing security through rootless builds and fine-grained control over the image creation process.

### Background

Buildah is a command-line tool designed for building OCI-compliant container images. Unlike traditional container build tools that require a daemon, Buildah operates directly as a command-line application, providing better security and flexibility.

Key characteristics:

- **Daemonless**: No background daemon process required for building images
- **Rootless builds**: Supports building images without root privileges
- **OCI-compliant**: Fully compliant with OCI image specifications
- **Dockerfile compatibility**: Can build images from Dockerfiles/Containerfiles
- **Fine-grained control**: Provides detailed control over the build process

Buildah is part of the containers ecosystem and integrates with other tools like Podman, Skopeo, and CRI-O.

### Actors

* **Buildah CLI**: The main command-line interface that users interact with for building container images.

* **OCI runtime**: Interfaces with OCI-compliant runtimes (runc, crun) to execute build commands when needed.

* **Image store**: Manages container images and their layers during the build process.

* **Registry client**: Handles interactions with container registries for pulling base images and pushing built images.

* **Storage backend**: Manages container storage layers and filesystems during the build process.

### Actions

* **Image building**:

  - Parses Dockerfile/Containerfile or receives direct build commands
  - Pulls base images from container registries
  - Executes build steps in isolated environments
  - Creates image layers and metadata
  - Applies security policies during build


* **Container creation**:

  - Creates working containers for build operations
  - Sets up namespaces and cgroups for isolation
  - Configures security policies for build containers


* **Layer management**:

  - Creates and manages image layers
  - Optimizes layer size and composition
  - Handles layer caching for efficiency


* **Image inspection**:

  - Provides detailed information about images
  - Verifies image metadata and configuration
  - Checks security attributes


* **Image publishing**:

  - Allows users to push built images to registries
  - Handles authentication with registries
  - Supports image signing

### Goals

* **Rootless image building**: Enable users to build container images without root privileges, reducing security risks.

* **Daemonless operation**: Eliminate the need for a daemon process, reducing attack surface and improving security.

* **OCI compliance**: Maintain full compatibility with OCI specifications for container images.

* **Dockerfile compatibility**: Support standard Dockerfiles/Containerfiles while providing enhanced security features.

* **Flexible build process**: Provide fine-grained control over the image building process.

### Non-goals

* **Container orchestration**: Buildah does not provide cluster orchestration capabilities.

* **Image registry**: Buildah does not operate as a container registry, though it interacts with them.

* **Continuous integration platform**: While used in CI/CD, Buildah itself is not a CI/CD platform.

## Self-assessment use

This self-assessment is created by the Buildah team to perform an internal analysis of the project's security.  It is not intended to provide a security audit of Buildah, or function as an independent assessment or attestation of Buildah's security health.

This document serves to provide Buildah users with an initial understanding of Buildah's security, where to find existing security documentation, Buildah plans for security, and general overview of Buildah security practices, both for development of Buildah as well as security of Buildah.

This document provides the CNCF TAG-Security with an initial understanding of Buildah to assist in a joint-assessment, necessary for projects under incubation.  Taken together, this document and the joint-assessment serve as a cornerstone for if and when Buildah seeks graduation and is preparing for a security audit.

## Security functions and features

### Critical Security Components

* **Rootless builds**: Buildah's core security feature that allows building images without root privileges, significantly reducing the attack surface.

* **User namespaces**: Provides process isolation during builds by mapping container user IDs to host user IDs.

* **Build isolation**: Each build operation is isolated from the host system and other builds.

* **Daemonless architecture**: Eliminates the daemon process, reducing potential attack vectors.

* **Security policy enforcement**: Applies seccomp, SELinux, and capabilities restrictions during builds.

### Security Relevant Components

* **Image verification**: Support for verifying container image signatures before using them as base images.

* **Secure defaults**: Provides secure defaults for build operations.

* **Credential management**: Secure handling of registry credentials during image operations.

* **Layer security**: Proper handling and isolation of image layers during builds.

* **Mount security**: Secure mounting of volumes and filesystems during build operations.

## Project compliance

* **OCI Compliance**: Buildah is fully compliant with the Open Container Initiative (OCI) specifications for container images.

* **OpenSSF Best Practices**: Buildah has achieved a [passing OpenSSF Best Practices badge](https://www.bestpractices.dev/projects/10579), demonstrating adherence to security best practices.

* **SELinux**: Full integration with SELinux for mandatory access control during builds.

* **AppArmor**: Support for AppArmor profiles for additional access control.

## Secure development practices

### Development Pipeline

* **Code Review Process**: All code changes require review by at least one maintainer before merging. The project uses GitHub pull requests for all contributions.

* **Automated Testing**: Comprehensive integration test suite is run in CI on every PR and also on a nightly basis. These tests exercise the buildah binary compiled using the PR's source code and the latest HEAD commit respectively.

* **Security Scanning**: Automated vulnerability scanning of dependencies using tools like Dependabot and GitHub Security Advisories. All medium and higher severity exploitable vulnerabilities are fixed in a timely way after they are confirmed.

* **Static Analysis**: Code quality and security analysis using golangci-lint which is run on every PR, ensuring testing is done prior to merge. The tool includes rules to look for common vulnerabilities in Go code.

* **Dynamic Analysis**: Comprehensive integration test suite is run in CI on every PR and also on a nightly basis. If the integration tests point out any issues in the development phase itself, those get fixed before any code is merged.

* **Container Image Security**: Built images follow security best practices and are regularly updated for security patches.

* **OpenSSF Best Practices Compliance**: Buildah has achieved a [passing OpenSSF Best Practices badge](https://www.bestpractices.dev/projects/10579), demonstrating adherence to security best practices including proper licensing, contribution guidelines, and security processes.

### Communication Channels

* Podman user room: [\#podman:fedoraproject.org](https://matrix.to/#/#podman:fedoraproject.org)

* Podman dev room: [\#podman-dev:matrix.org](https://matrix.to/#/#podman-dev:matrix.org)

* **Inbound**:

  - GitHub Issues for bug reports and feature requests
  - GitHub Discussions for community questions
  - Security issues via the security mailing list
  - Mailing lists for formal discussions
  - Clear contribution guidelines documented in [CONTRIBUTING.md](https://github.com/containers/buildah/blob/main/CONTRIBUTING.md)


* **Outbound**:

  - Release announcements via GitHub releases (and [buildah.io](http://buildah.io) for major and minor version bumps)
  - Security advisories through [https://access.redhat.com](https://access.redhat.com) and Bugzilla trackers for Fedora and RHEL on [bugzilla.redhat.com](http://bugzilla.redhat.com) .
  - Documentation updates and blog posts
  - Conference presentations and talks
  - Project website at [buildah.io](https://buildah.io) with comprehensive documentation

### Ecosystem

Buildah is a critical component of the cloud-native ecosystem:

* **OpenShift**: Buildah is integrated into Red Hat OpenShift for secure image building workflows.

* **Container Ecosystem**: Integrates with Podman for running containers, Skopeo for image operations, and CRI-O for Kubernetes runtime.

* **Development Tools**: Widely used in development environments for building container images securely.

* **CI/CD Pipelines**: Used in many CI/CD systems for building containerized applications with enhanced security.

## Security issue resolution

### Responsible Disclosures Process

* **Reporting**: Security vulnerabilities should be reported by email as documented in the [SECURITY.md](https://github.com/containers/buildah/blob/main/SECURITY.md) file.

* **Response Time**: The team commits to responding to vulnerability reports within 48 hours. All medium and higher severity exploitable vulnerabilities are prioritized as a matter of general practice.

* **Credit**: Security researchers who responsibly disclose vulnerabilities are credited in security advisories and release notes.

* **Public Disclosure**: Vulnerabilities are disclosed by the project maintainers with appropriate embargo periods for critical issues, following industry best practices for responsible disclosure.

### Vulnerability Response Process

* **Triage**: Security reports are triaged by the security team and assigned severity levels (Critical, High, Medium, Low) using CVSS scoring where applicable.

* **Investigation**: The team investigates the vulnerability, determines impact, and develops fixes. All medium and higher severity exploitable vulnerabilities discovered through static or dynamic analysis are fixed in a timely way after they are confirmed.

* **Fix Development**: Security fixes are developed in private repositories to prevent premature disclosure. The project maintains a clear process for developing and testing security patches.

* **Disclosure**: Vulnerabilities are disclosed by the team with appropriate embargo periods for critical issues. The project follows industry best practices for coordinated vulnerability disclosure.


### Incident Response

* **Detection**: Security incidents are detected through automated monitoring, user reports, security research, and the comprehensive testing suite that runs on every PR and nightly.

* **Assessment**: The team assesses the severity and impact of security incidents using CVSS scoring and industry-standard severity classification.

* **Containment**: Immediate steps are taken to contain and mitigate the impact of security incidents. If the integration tests point out any issues in the development phase, those get fixed before any code is merged.

* **Communication**: Affected users are notified through security advisories and release notes. The project maintains clear communication channels for security updates.

* **Recovery**: Patches and updates are released as quickly as possible to address security issues. All medium and high severity vulnerabilities are prioritized as a matter of general practice.

* **Post-Incident Review**: The team conducts post-incident reviews to identify improvements to the security process and prevent similar issues in the future.

## Appendix

### Known Issues Over Time

* **Security Advisories**: See [this NVD list](https://nvd.nist.gov/vuln/search#/nvd/home?vulnRevisionStatusList=published&keyword=buildah&resultType=records) for CVEs to date.

* **Code Review**: The project's code review process has caught numerous potential security issues before they reach production.

### OpenSSF Best Practices

* **Current Status**: Buildah has achieved a [passing OpenSSF Best Practices badge](https://www.bestpractices.dev/projects/10579) (100% compliance), demonstrating adherence to security best practices.

* **Key Achievements**:

  - Comprehensive project documentation and contribution guidelines
  - Robust security testing and analysis processes
  - Clear vulnerability disclosure and response procedures
  - Strong development practices with code review and automated testing
  - Proper licensing and project governance

### Related Projects / Vendors

* **Skopeo**: A command line utility to perform various operations on container images and image repositories like copying an image, inspecting a remote image, deleting an image from an image repository.

* **Podman:** A command line utility for managing OCI containers and pods.
