package source

import (
	"context"
	"fmt"
	"os"

	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/types"
)

// PushOptions includes data to alter certain knobs when pushing a source
// image.
type PushOptions struct {
	// Require HTTPS and verify certificates when accessing the registry.
	TLSVerify bool
	// [username[:password] to use when connecting to the registry.
	Credentials string
	// Quiet the progress bars when pushing.
	Quiet bool
	// If set after copying the artifact, write the digest of the resulting image to the file
	DigestFile string
}

// Push the source image at `sourcePath` to `imageInput` at a container
// registry.
func Push(ctx context.Context, sourcePath string, imageInput string, options PushOptions) error {
	srcRef, err := layout.ParseReference(sourcePath)
	if err != nil {
		return err
	}
	destRef, err := stringToImageReference(imageInput)
	if err != nil {
		return err
	}

	sysCtx := &types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(!options.TLSVerify),
	}
	if options.Credentials != "" {
		authConf, err := parse.AuthConfig(options.Credentials)
		if err != nil {
			return err
		}
		sysCtx.DockerAuthConfig = authConf
	}

	policy, err := signature.DefaultPolicy(sysCtx)
	if err != nil {
		return fmt.Errorf("obtaining default signature policy: %w", err)
	}
	policyContext, err := signature.NewPolicyContext(policy)
	if err != nil {
		return fmt.Errorf("creating new signature policy context: %w", err)
	}

	copyOpts := &copy.Options{
		DestinationCtx: sysCtx,
	}
	if !options.Quiet {
		copyOpts.ReportWriter = os.Stderr
	}
	manifestBytes, err := copy.Image(ctx, policyContext, destRef, srcRef, copyOpts)
	if err != nil {
		return fmt.Errorf("pushing source image: %w", err)
	}

	if options.DigestFile != "" {
		manifestDigest, err := manifest.Digest(manifestBytes)
		if err != nil {
			return fmt.Errorf("computing digest of manifest of source: %w", err)
		}
		if err = os.WriteFile(options.DigestFile, []byte(manifestDigest.String()+"\n"), 0o644); err != nil {
			return fmt.Errorf("failed to write digest to file %q: %w", options.DigestFile, err)
		}
	}

	return nil
}
