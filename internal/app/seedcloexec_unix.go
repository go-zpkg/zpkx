//go:build unix

package app

import "syscall"

// closeOnExec marks fd close-on-exec: execve(2) closes it, so the command zpkx
// becomes (Exec is syscall.Exec) does not hold the ledger.
func closeOnExec(fd int) { syscall.CloseOnExec(fd) }
