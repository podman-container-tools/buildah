package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/buildah/internal/rpc/listen"
	"go.podman.io/buildah/internal/rpc/noop"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var (
	rpcDescription = "\n  Runs a command with a rudimentary RPC server available."
	rpcCommand     = &cobra.Command{
		Use:     "rpc",
		Short:   "Run a command with a rudimentary RPC server available",
		Long:    rpcDescription,
		RunE:    rpcCmd,
		Hidden:  true,
		Example: `buildah rpc [-e|--env NAME] [-l|--listen PATH] command ...`,
		Args:    cobra.MinimumNArgs(1),
	}
)

func rpcCmd(c *cobra.Command, args []string) error {
	store, err := getStore(c)
	if err != nil {
		return err
	}

	socketPath := c.Flag("listen").Value.String()
	if socketPath == "" {
		socketDir, err := os.MkdirTemp(store.RunRoot(), "buildah-socket")
		if err != nil {
			return err
		}
		defer func() {
			if err := os.RemoveAll(socketDir); err != nil {
				logrus.Errorf("removing %s: %v", socketDir, err)
			}
		}()
		socketPath = filepath.Join(socketDir, "socket")
	}
	listener, cleanup, err := listen.Listen(socketPath)
	if err != nil {
		return err
	}
	logrus.Debugf("listening for rpc requests at %q", socketPath)
	defer func() {
		if err := cleanup(); err != nil {
			logrus.Errorf("cleaning up: %v", err)
		}
	}()

	s := grpc.NewServer()
	noop.Register(s)
	reflection.Register(s)

	var errgroup errgroup.Group
	errgroup.Go(func() error {
		if err := s.Serve(listener); err != nil {
			return err
		}
		return nil
	})
	defer func() {
		s.GracefulStop() // closes the listening socket
		// We might have called GracefulStop() before Serve() had a chance to start;
		// in that case it returns ErrServerStopped.
		if err := errgroup.Wait(); err != nil && err != grpc.ErrServerStopped {
			logrus.Errorf("while waiting for rpc service to shut down: %v", err)
		}
	}()
	cmd := exec.CommandContext(getContext(), args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	envVar := c.Flag("env").Value.String()
	if envVar != "" {
		cmd.Env = append(slices.Clone(cmd.Environ()), envVar+"="+"unix://"+socketPath)
	}
	return cmd.Run()
}

func rpcInit() {
	var rpcOptions struct {
		envVar     string
		listenPath string
	}
	rpcCommand.SetUsageTemplate(UsageTemplate())
	flags := rpcCommand.Flags()
	flags.SetInterspersed(false)
	flags.StringVarP(&rpcOptions.envVar, "env", "e", "", "set environment `variable` to point to listening socket path")
	flags.StringVarP(&rpcOptions.listenPath, "listen", "l", "", "listening socket `path`")
	rootCmd.AddCommand(rpcCommand)
}
