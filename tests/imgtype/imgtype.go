package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	lslog "github.com/sirupsen/logrus/hooks/slog"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/docker"
	"go.podman.io/buildah/util"
	"go.podman.io/image/v5/manifest"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/reexec"
	"go.podman.io/storage/pkg/unshare"
)

func main() {
	if reexec.Init() {
		return
	}
	unshare.MaybeReexecUsingUserNamespace(false)

	storeOptions, err := storage.DefaultStoreOptions()
	if err != nil {
		storeOptions = storage.StoreOptions{}
	}

	expectedManifestType := ""
	expectedConfigType := ""

	debug := flag.Bool("debug", false, "turn on debug logging")
	root := flag.String("root", storeOptions.GraphRoot, "storage root directory")
	runroot := flag.String("runroot", storeOptions.RunRoot, "storage runtime directory")
	driver := flag.String("storage-driver", storeOptions.GraphDriverName, "storage driver")
	imagestore := flag.String("imagestore", storeOptions.ImageStore, "storage imagestore")
	transientStore := flag.Bool("transient-store", storeOptions.TransientStore, "store some information in transient storage")
	opts := flag.String("storage-opts", "", "storage option list (comma separated)")
	policy := flag.String("signature-policy", "", "signature policy file")
	mtype := flag.String("expected-manifest-type", define.OCIv1ImageManifest, "expected manifest type")
	showm := flag.Bool("show-manifest", false, "output the manifest JSON")
	rebuildm := flag.Bool("rebuild-manifest", false, "rebuild the manifest JSON")
	showc := flag.Bool("show-config", false, "output the configuration JSON")
	rebuildc := flag.Bool("rebuild-config", false, "rebuild the configuration JSON")
	flag.Parse()
	logrus.SetOutput(io.Discard)
	logrus.AddHook(lslog.NewHook(slog.Default(), nil))
	logrus.SetLevel(logrus.ErrorLevel)
	slog.SetLogLoggerLevel(slog.LevelError)
	if debug != nil && *debug {
		logrus.SetLevel(logrus.DebugLevel)
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	switch *mtype {
	case define.OCIv1ImageManifest:
		expectedManifestType = *mtype
		expectedConfigType = v1.MediaTypeImageConfig
	case define.Dockerv2ImageManifest:
		expectedManifestType = *mtype
		expectedConfigType = manifest.DockerV2Schema2ConfigMediaType
	case "*":
		expectedManifestType = ""
		expectedConfigType = ""
	default:
		logrus.Errorf("unknown -expected-manifest-type value, expected either %q or %q or %q",
			define.OCIv1ImageManifest, define.Dockerv2ImageManifest, "*")
		return
	}
	if root != nil {
		storeOptions.GraphRoot = *root
	}
	if runroot != nil {
		storeOptions.RunRoot = *runroot
	}
	if driver != nil {
		storeOptions.GraphDriverName = *driver
		storeOptions.GraphDriverOptions = nil
	}
	if imagestore != nil {
		storeOptions.ImageStore = *imagestore
	}
	if transientStore != nil {
		storeOptions.TransientStore = *transientStore
	}
	if opts != nil && *opts != "" {
		storeOptions.GraphDriverOptions = strings.Split(*opts, ",")
	}
	systemContext := &types.SystemContext{
		SignaturePolicyPath: *policy,
	}
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		return
	}
	store, err := storage.GetStore(storeOptions)
	if err != nil {
		logrus.Errorf("error opening storage: %v", err)
		os.Exit(1)
	}
	is.Transport.SetStore(store)

	errors := false
	defer func() {
		store.Shutdown(false) //nolint:errcheck
		if errors {
			os.Exit(1)
		}
	}()
	for _, image := range args {
		var ref types.ImageReference
		oImage := v1.Image{}
		dImage := docker.V2Image{}
		oManifest := v1.Manifest{}
		dManifest := docker.V2S2Manifest{}
		manifestType := ""
		configType := ""

		ref, _, err := util.FindImage(store, "", systemContext, image)
		if err != nil {
			ref2, err2 := alltransports.ParseImageName(image)
			if err2 != nil {
				logrus.Errorf("error parsing reference %q to an image: %v", image, err)
				errors = true
				continue
			}
			ref = ref2
		}

		ctx := context.Background()
		img, err := ref.NewImage(ctx, systemContext)
		if err != nil {
			logrus.Errorf("error opening image %q: %v", image, err)
			errors = true
			continue
		}
		defer img.Close()

		config, err := img.ConfigBlob(ctx)
		if err != nil {
			logrus.Errorf("error reading configuration from %q: %v", image, err)
			errors = true
			continue
		}

		manifest, manifestType, err := img.Manifest(ctx)
		if err != nil {
			logrus.Errorf("error reading manifest from %q: %v", image, err)
			errors = true
			continue
		}

		if expectedManifestType != "" && manifestType != expectedManifestType {
			logrus.Errorf("expected manifest type %q in %q, got %q", expectedManifestType, image, manifestType)
			errors = true
			continue
		}

		switch expectedManifestType {
		case define.OCIv1ImageManifest:
			err = json.Unmarshal(manifest, &oManifest)
			if err != nil {
				logrus.Errorf("error parsing manifest from %q: %v", image, err)
				errors = true
				continue
			}
			err = json.Unmarshal(config, &oImage)
			if err != nil {
				logrus.Errorf("error parsing config from %q: %v", image, err)
				errors = true
				continue
			}
			manifestType = oManifest.MediaType
			configType = oManifest.Config.MediaType
		case define.Dockerv2ImageManifest:
			err = json.Unmarshal(manifest, &dManifest)
			if err != nil {
				logrus.Errorf("error parsing manifest from %q: %v", image, err)
				errors = true
				continue
			}
			err = json.Unmarshal(config, &dImage)
			if err != nil {
				logrus.Errorf("error parsing config from %q: %v", image, err)
				errors = true
				continue
			}
			manifestType = dManifest.MediaType
			configType = dManifest.Config.MediaType
		}

		switch manifestType {
		case define.OCIv1ImageManifest:
			if rebuildm != nil && *rebuildm {
				err = json.Unmarshal(manifest, &oManifest)
				if err != nil {
					logrus.Errorf("error parsing manifest from %q: %v", image, err)
					errors = true
					continue
				}
				manifest, err = json.Marshal(oManifest)
				if err != nil {
					logrus.Errorf("error rebuilding manifest from %q: %v", image, err)
					errors = true
					continue
				}
			}
			if rebuildc != nil && *rebuildc {
				err = json.Unmarshal(config, &oImage)
				if err != nil {
					logrus.Errorf("error parsing config from %q: %v", image, err)
					errors = true
					continue
				}
				config, err = json.Marshal(oImage)
				if err != nil {
					logrus.Errorf("error rebuilding config from %q: %v", image, err)
					errors = true
					continue
				}
			}
		case define.Dockerv2ImageManifest:
			if rebuildm != nil && *rebuildm {
				err = json.Unmarshal(manifest, &dManifest)
				if err != nil {
					logrus.Errorf("error parsing manifest from %q: %v", image, err)
					errors = true
					continue
				}
				manifest, err = json.Marshal(dManifest)
				if err != nil {
					logrus.Errorf("error rebuilding manifest from %q: %v", image, err)
					errors = true
					continue
				}
			}
			if rebuildc != nil && *rebuildc {
				err = json.Unmarshal(config, &dImage)
				if err != nil {
					logrus.Errorf("error parsing config from %q: %v", image, err)
					errors = true
					continue
				}
				config, err = json.Marshal(dImage)
				if err != nil {
					logrus.Errorf("error rebuilding config from %q: %v", image, err)
					errors = true
					continue
				}
			}
		}
		if expectedConfigType != "" && configType != expectedConfigType {
			logrus.Errorf("expected config type %q in %q, got %q", expectedConfigType, image, configType)
			errors = true
			continue
		}
		if showm != nil && *showm {
			fmt.Println(string(manifest))
		}
		if showc != nil && *showc {
			fmt.Println(string(config))
		}
	}
}
