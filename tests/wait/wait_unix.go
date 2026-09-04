//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

func main() {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, uintptr(1), 0, 0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
		os.Exit(1)
	}
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s [CMD ...]\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}
	cmd := exec.Command(os.Args[1], os.Args[2:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	caught := false
	for range 100 {
		wpid, err := unix.Wait4(-1, nil, unix.WNOHANG, nil)
		if err != nil {
			break
		}
		if wpid == 0 {
			time.Sleep(100 * time.Millisecond)
		} else {
			// log an error: the child process was expected to reap
			// its own reparented child processes; we shouldn't
			// have had to clean them up on its behalf
			logrus.Errorf("caught reparented child process %d", wpid)
			caught = true
		}
	}
	if !caught {
		if err == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "%v", err)
	}
	os.Exit(1)
}
