//go:build !unix

package app

// closeOnExec: a descriptor is not named by a number off unix, so there is
// nothing to mark; ledgerFromFD already refuses a number that is not a pipe.
func closeOnExec(int) {}
