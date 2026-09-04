package parse //nolint:revive,nolintlint

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/buildah/define"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/types"
)

func TestCommonBuildOptionsFromFlagSet(t *testing.T) {
	t.Parallel()
	fs := pflag.NewFlagSet("testme", pflag.PanicOnError)
	fs.String("memory", "1GB", "")
	fs.String("shm-size", "5TB", "")
	fs.String("cpuset-cpus", "1", "")
	fs.String("cpuset-mems", "2", "")
	fs.String("cgroup-parent", "none", "")
	err := fs.Parse([]string{"--memory", "2GB"})
	assert.NoError(t, err)
	cbo, err := CommonBuildOptionsFromFlagSet(fs, fs.Lookup)
	assert.NoError(t, err)
	assert.Equal(t, cbo.Memory, int64(2147483648))
}

func TestCommonBuildOptionsSeccompFromConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "containers.conf")
	t.Setenv("CONTAINERS_CONF", configPath)
	t.Cleanup(func() {
		_, err := config.Reload()
		assert.NoError(t, err)
	})

	defaultOptions := new(define.CommonBuildOptions)
	require.NoError(t, parseSecurityOpts(nil, defaultOptions))
	if !supportsSeccomp {
		assert.Empty(t, defaultOptions.SeccompProfilePath)
	}

	tests := []struct {
		name                         string
		containersConf               string
		securityOpts                 []string
		expectedProfile              string
		expectErrorWithoutSeccompTag bool
	}{
		{
			name:            "configured unconfined",
			containersConf:  "[containers]\nseccomp_profile = \"unconfined\"\n",
			expectedProfile: "unconfined",
		},
		{
			name:                         "command line overrides config",
			containersConf:               "[containers]\nseccomp_profile = \"unconfined\"\n",
			securityOpts:                 []string{"seccomp=/tmp/custom-seccomp.json"},
			expectedProfile:              "/tmp/custom-seccomp.json",
			expectErrorWithoutSeccompTag: true,
		},
		{
			name:                         "configured custom profile",
			containersConf:               "[containers]\nseccomp_profile = \"/tmp/custom-seccomp.json\"\n",
			expectedProfile:              "/tmp/custom-seccomp.json",
			expectErrorWithoutSeccompTag: true,
		},
		{
			name:            "no configured profile",
			containersConf:  "[containers]\n",
			expectedProfile: defaultOptions.SeccompProfilePath,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(configPath, []byte(test.containersConf), 0o600))
			_, err := config.Reload()
			require.NoError(t, err)

			fs := newCommonBuildOptionsFlagSet(t, test.securityOpts)
			commonOpts, err := CommonBuildOptionsFromFlagSet(fs, fs.Lookup)
			if test.expectErrorWithoutSeccompTag && !supportsSeccomp {
				require.ErrorContains(t, err, "seccomp support is not enabled in this build")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.expectedProfile, commonOpts.SeccompProfilePath)
		})
	}
}

func newCommonBuildOptionsFlagSet(t *testing.T, securityOpts []string) *pflag.FlagSet {
	t.Helper()

	fs := pflag.NewFlagSet("common-build-options", pflag.ContinueOnError)
	fs.String("cpuset-cpus", "", "")
	fs.String("cpuset-mems", "", "")
	fs.String("cgroup-parent", "", "")
	fs.String("shm-size", "65536k", "")
	fs.StringArray("security-opt", nil, "")
	for _, securityOpt := range securityOpts {
		require.NoError(t, fs.Set("security-opt", securityOpt))
	}
	return fs
}

// TestDeviceParser verifies the given device strings is parsed correctly
func TestDeviceParser(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("Devices is only supported on Linux")
	}

	// Test defaults
	src, dest, permissions, err := Device("/dev/foo")
	assert.NoError(t, err)
	assert.Equal(t, src, "/dev/foo")
	assert.Equal(t, dest, "/dev/foo")
	assert.Equal(t, permissions, "rwm")

	// Test defaults, different dest
	src, dest, permissions, err = Device("/dev/foo:/dev/bar")
	assert.NoError(t, err)
	assert.Equal(t, src, "/dev/foo")
	assert.Equal(t, dest, "/dev/bar")
	assert.Equal(t, permissions, "rwm")

	// Test fully specified
	src, dest, permissions, err = Device("/dev/foo:/dev/bar:rm")
	assert.NoError(t, err)
	assert.Equal(t, src, "/dev/foo")
	assert.Equal(t, dest, "/dev/bar")
	assert.Equal(t, permissions, "rm")

	// Test device, permissions
	src, dest, permissions, err = Device("/dev/foo:rm")
	assert.NoError(t, err)
	assert.Equal(t, src, "/dev/foo")
	assert.Equal(t, dest, "/dev/foo")
	assert.Equal(t, permissions, "rm")

	// test bogus permissions
	_, _, _, err = Device("/dev/fuse1:BOGUS")
	assert.Error(t, err)

	_, _, _, err = Device("")
	assert.Error(t, err)

	_, _, _, err = Device("/dev/foo:/dev/bar:rm:")
	assert.Error(t, err)

	_, _, _, err = Device("/dev/foo::rm")
	assert.Error(t, err)
}

func TestIsValidDeviceMode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("Devices is only supported on Linux")
	}
	assert.False(t, isValidDeviceMode("BOGUS"))
	assert.False(t, isValidDeviceMode("rwx"))
	assert.True(t, isValidDeviceMode("r"))
	assert.True(t, isValidDeviceMode("rw"))
	assert.True(t, isValidDeviceMode("rm"))
	assert.True(t, isValidDeviceMode("rwm"))
}

func TestDeviceFromPath(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("Devices is only supported on Linux")
	}
	// Path is valid
	dev, err := DeviceFromPath("/dev/null")
	assert.NoError(t, err)
	assert.Equal(t, len(dev), 1)
	assert.Equal(t, dev[0].Major, int64(1))
	assert.Equal(t, dev[0].Minor, int64(3))
	assert.Equal(t, string(dev[0].Permissions), "rwm")
	assert.Equal(t, dev[0].Uid, uint32(0))
	assert.Equal(t, dev[0].Gid, uint32(0))

	// Path does not exists
	_, err = DeviceFromPath("/dev/BOGUS")
	assert.Error(t, err)

	// Path is a directory of devices
	_, err = DeviceFromPath("/dev/pts")
	assert.NoError(t, err)

	// path of directory has no device
	_, err = DeviceFromPath("/etc/passwd")
	assert.Error(t, err)
}

func TestIDMappingOptions(t *testing.T) {
	t.Parallel()
	fs := pflag.NewFlagSet("testme", pflag.PanicOnError)
	pfs := pflag.NewFlagSet("persist", pflag.PanicOnError)
	fs.String("userns-uid-map-user", "", "")
	fs.String("userns-gid-map-group", "", "")
	fs.String("userns-uid-map", "", "")
	fs.String("userns-gid-map", "", "")
	fs.String("userns", "", "")
	err := fs.Parse([]string{})
	assert.NoError(t, err)
	uos, _, err := IDMappingOptionsFromFlagSet(fs, pfs, fs.Lookup)
	assert.NoError(t, err)
	nso := uos.Find(string(specs.UserNamespace))
	assert.Equal(t, *nso, define.NamespaceOption{
		Host: true,
		Name: string(specs.UserNamespace),
	})
}

func TestIsolation(t *testing.T) {
	t.Parallel()
	def, err := defaultIsolation()
	if err != nil {
		assert.Error(t, err)
	}

	isolations := []string{"", "default", "oci", "chroot", "rootless"}
	for _, i := range isolations {
		isolation, err := IsolationOption(i)
		if err != nil {
			assert.Error(t, fmt.Errorf("isolation %q not supported", i))
		}
		var expected string
		switch i {
		case "":
			expected = def.String()
		case "default":
			expected = "oci"
		default:
			expected = i
		}

		if isolation.String() != expected {
			assert.Error(t, fmt.Errorf("isolation %q not equal to user input %q", isolation.String(), expected))
		}
	}
}

func TestNamespaceOptions(t *testing.T) {
	t.Parallel()
	fs := pflag.NewFlagSet("testme", pflag.PanicOnError)
	fs.String("cgroupns", "", "")
	err := fs.Parse([]string{"--cgroupns", "private"})
	assert.NoError(t, err)
	nsos, np, err := NamespaceOptionsFromFlagSet(fs, fs.Lookup)
	assert.NoError(t, err)
	assert.Equal(t, np, define.NetworkEnabled)
	nso := nsos.Find(string(specs.CgroupNamespace))
	assert.Equal(t, *nso, define.NamespaceOption{
		Name: string(specs.CgroupNamespace),
	})
}

func TestParsePlatform(t *testing.T) {
	t.Parallel()
	os, arch, variant, err := Platform("a/b/c")
	assert.NoError(t, err)
	assert.NoError(t, err)
	assert.Equal(t, os, "a")
	assert.Equal(t, arch, "b")
	assert.Equal(t, variant, "c")

	os, arch, variant, err = Platform("a/b")
	assert.NoError(t, err)
	assert.NoError(t, err)
	assert.Equal(t, os, "a")
	assert.Equal(t, arch, "b")
	assert.Equal(t, variant, "")

	_, _, _, err = Platform("a")
	assert.Error(t, err)
}

func TestParsePullPolicy(t *testing.T) {
	t.Parallel()
	for name, want := range define.PolicyMap {
		t.Run(name, func(t *testing.T) {
			got, err := pullPolicyWithFlags(name, false, false)
			require.NoErrorf(t, err, "expected value %q to be recognized", name)
			require.Equal(t, want, got)
		})
	}
	for _, name := range []string{"try", "truth"} {
		t.Run(name, func(t *testing.T) {
			_, err := pullPolicyWithFlags(name, false, false)
			require.Error(t, err)
		})
	}
}

func TestSplitStringWithColonEscape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		volume         string
		expectedResult []string
	}{
		{"/root/a:/root/test:O", []string{"/root/a", "/root/test", "O"}},
		{"/root/a\\:b/c:/root/test:O", []string{"/root/a:b/c", "/root/test", "O"}},
		{"/root/a:/root/test\\:test1/a:O", []string{"/root/a", "/root/test:test1/a", "O"}},
		{"/root/a\\:b/c:/root/test\\:test1/a:O", []string{"/root/a:b/c", "/root/test:test1/a", "O"}},
	}
	for _, args := range tests {
		val := SplitStringWithColonEscape(args.volume)
		assert.Equal(t, val, args.expectedResult)
	}
}

func TestSystemContextFromFlagSet(t *testing.T) {
	t.Parallel()
	fs := pflag.NewFlagSet("testme", pflag.PanicOnError)
	fs.Bool("tls-verify", false, "")
	err := fs.Parse([]string{"--tls-verify", "false"})
	assert.NoError(t, err)
	sc, err := SystemContextFromFlagSet(fs, fs.Lookup)
	assert.NoError(t, err)
	assert.Equal(t, sc, &types.SystemContext{
		BigFilesTemporaryDir:        GetTempDir(),
		DockerInsecureSkipTLSVerify: types.OptionalBoolFalse,
		DockerRegistryUserAgent:     fmt.Sprintf("Buildah/%s", define.Version),
	})
}

func TestGetBuildOutput(t *testing.T) {
	testCases := []struct {
		description string
		input       string
		output      define.BuildOutputOption
	}{
		{
			description: "hyphen",
			input:       "-",
			output: define.BuildOutputOption{
				IsStdout: true,
			},
		},
		{
			description: "just-a-path",
			input:       "/tmp",
			output: define.BuildOutputOption{
				IsDir: true,
				Path:  "/tmp",
			},
		},
		{
			description: "normal-path",
			input:       "type=local,dest=/tmp",
			output: define.BuildOutputOption{
				IsDir: true,
				Path:  "/tmp",
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			result, err := GetBuildOutput(testCase.input)
			require.NoErrorf(t, err, "expected to be able to parse %q", testCase.input)
			assert.Equal(t, testCase.output, result)
		})
	}
}

func TestSecrets(t *testing.T) {
	errorTests := []struct {
		name  string
		input string
	}{
		{"bare-src-no-equals", "id=,src"},
		{"bare-id-no-equals", "id"},
		{"known-key-without-value", "id=mysecret,src"},
		{"empty-id", "id="},
		{"unknown-key", "id=mysecret,bogus=x"},
	}
	for _, tc := range errorTests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Secrets([]string{tc.input})
			assert.Error(t, err, "expected error for input %q", tc.input)
		})
	}

	tmpfile, err := os.CreateTemp(t.TempDir(), "secret")
	require.NoError(t, err)
	_, err = tmpfile.WriteString("supersecret")
	require.NoError(t, err)
	tmpfile.Close()

	t.Setenv("TEST_SECRET_ENV", "envsecret")

	validFile := tmpfile.Name()

	successTests := []struct {
		name     string
		input    string
		expected define.Secret
	}{
		{
			"id-and-file-src",
			"id=mysecret,src=" + validFile,
			define.Secret{ID: "mysecret", Source: validFile, SourceType: "file"},
		},
		{
			"id-and-env",
			"id=TEST_SECRET_ENV,env=TEST_SECRET_ENV",
			define.Secret{ID: "TEST_SECRET_ENV", Source: "TEST_SECRET_ENV", SourceType: "env"},
		},
		{
			"id-only-env-fallback",
			"id=TEST_SECRET_ENV",
			define.Secret{ID: "TEST_SECRET_ENV", Source: "TEST_SECRET_ENV", SourceType: "env"},
		},
	}
	for _, tc := range successTests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Secrets([]string{tc.input})
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result[tc.expected.ID])
		})
	}
}
