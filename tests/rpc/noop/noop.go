package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/buildah/define"
	noop "go.podman.io/buildah/internal/rpc/noop/pb"
	"go.podman.io/buildah/pkg/cli"
	"go.podman.io/storage/pkg/reexec"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	if reexec.Init() {
		return
	}

	rootCmd := cobra.Command{
		Use:          "rpc",
		Short:        "poke a no-op GRPC endpoint",
		Long:         "poke a no-op GRPC endpoint",
		Args:         cobra.MinimumNArgs(0),
		RunE:         poke,
		Version:      define.Version,
		SilenceUsage: true,
	}
	rootCmd.Flags().StringP("env", "e", "", "connect to location set in environment `variable`")
	rootCmd.Flags().StringP("connect", "c", "", "connect to `location`")
	rootCmd.Flags().SetInterspersed(true)
	rootCmd.Flags().Usage = func() {
		fmt.Println("[-e|--env var] [-c|--connect socket] endpoint json")
	}

	var exitCode int

	if err := rootCmd.Execute(); err != nil {
		if logrus.IsLevelEnabled(logrus.TraceLevel) {
			fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		exitCode = cli.ExecErrorCodeGeneric
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if w, ok := ee.Sys().(syscall.WaitStatus); ok {
				exitCode = w.ExitStatus()
			}
		}
	}
	os.Exit(exitCode)
}

func poke(c *cobra.Command, args []string) error {
	ctx := context.Background()

	socketPath := c.Flag("connect").Value.String()
	if socketPath == "" {
		envVar := c.Flag("env").Value.String()
		if envVar == "" {
			return errors.New("neither --connect nor --env were specified")
		}
		var ok bool
		socketPath, ok = os.LookupEnv(envVar)
		if !ok {
			return fmt.Errorf("environment variable %q not set", envVar)
		}
	}
	if socketPath == "" {
		return errors.New("configured server location is empty")
	}

	cc, err := grpc.NewClient(socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connecting to service: %w", err)
	}
	noopClient := noop.NewNoopClient(cc)
	response, err := noopClient.Noop(ctx, &noop.NoopRequest{Ignored: strings.Join(args[:], ",")})
	if err != nil {
		return fmt.Errorf("server responded with error: %w", err)
	}
	fmt.Println(response.String())

	return err
}
