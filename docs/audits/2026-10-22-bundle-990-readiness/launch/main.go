// Endpoint launcher for one Wayfinder ticket (#712). On Linux, ru_maxrss
// survives execve: a process spawned directly by the Python runner inherits
// the runner's resident high-water mark, which exceeds the fixture's own. This
// small parent forks the endpoint from its own few-MiB image instead, records
// the child PID, forwards termination signals and returns the child's exit
// status. A finite measurement aid, not a maintained tool.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

func main() {
	if len(os.Args) < 4 || os.Args[2] != "--" {
		fmt.Fprintln(os.Stderr, "usage: launch PIDFILE -- COMMAND [ARGS...]")
		os.Exit(2)
	}
	cmd := exec.Command(os.Args[3], os.Args[4:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		fmt.Fprintln(os.Stderr, err)
	}
	go func() {
		for s := range signals {
			_ = cmd.Process.Signal(s)
		}
	}()
	err := cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(max(exit.ExitCode(), 1))
	}
	if err != nil {
		os.Exit(1)
	}
}
