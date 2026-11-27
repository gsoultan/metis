//go:build !unix

package app

import "errors"

// Restart cannot start this program again in place of itself here. The
// process exits with the error; a service manager set to restart it brings
// it back in the new role.
func Restart() error {
	return errors.New("this system cannot start a program again in place of itself; start Metis again to take the new role")
}
