//go:build unix

package app

import (
	"fmt"
	"os"
	"syscall"
)

// Restart starts this program again in place of this process: the same
// binary, arguments and environment, under the same process id, so a
// service manager watching it sees it carry on. It returns only when that
// fails.
func Restart() error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find this program to start it again: %w", err)
	}
	// #nosec G204 -- this program's own binary and arguments, as it was started.
	if err := syscall.Exec(binary, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("start this program again: %w", err)
	}
	return nil
}
