//go:build unix

package app

import (
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// dupFD is a descriptor of its own for f: os.NewFile owns what it is given and
// closes it when collected, so a test hands over a duplicate.
func dupFD(t *testing.T, f *os.File) int {
	t.Helper()
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	return fd
}

func TestTheLedgerDescriptorFlag(t *testing.T) {
	ok := []string{"--seed-from", "oci://x", "--seed-ledger-fd", "3", "+a"}
	from, ledger, fd, rest, err := takeSeedFlags(ok)
	if err != nil || from != "oci://x" || ledger != "" || fd != 3 || !reflect.DeepEqual(rest, []string{"+a"}) {
		t.Fatalf("got %q %q %d %q %v", from, ledger, fd, rest, err)
	}
	if _, _, fd, _, _ := takeSeedFlags([]string{"--seed-from", "oci://x", "--seed-ledger", "l"}); fd != -1 {
		t.Errorf("no descriptor named, fd = %d", fd)
	}
	for name, argv := range map[string][]string{
		"stdin":          {"--seed-from", "oci://x", "--seed-ledger-fd", "0"},
		"stderr":         {"--seed-from", "oci://x", "--seed-ledger-fd", "2"},
		"negative":       {"--seed-from", "oci://x", "--seed-ledger-fd", "-1"},
		"not a number":   {"--seed-from", "oci://x", "--seed-ledger-fd", "/dev/fd/3"},
		"no value":       {"--seed-from", "oci://x", "--seed-ledger-fd"},
		"two ledgers":    {"--seed-from", "oci://x", "--seed-ledger", "l", "--seed-ledger-fd", "3"},
		"two ledgers 2":  {"--seed-from", "oci://x", "--seed-ledger-fd", "3", "--seed-ledger", "l"},
		"without a seed": {"--seed-ledger-fd", "3", "+a"},
	} {
		if _, _, _, _, err := takeSeedFlags(argv); err == nil {
			t.Errorf("%s: accepted %q", name, argv)
		}
	}
}

// The descriptor is fstat-ed, never looked up: a pipe is taken, and a closed
// descriptor, a regular file and a directory are refused, so a descriptor the
// caller did not mean is never written a ledger.
func TestOnlyAnOpenPipeIsALedgerDescriptor(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	f, err := ledgerFromFD(dupFD(t, pw))
	if err != nil || f == nil {
		t.Fatalf("a pipe was refused: %v", err)
	}
	reg, err := os.Create(filepath.Join(t.TempDir(), "regular"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	for name, fd := range map[string]int{"regular file": dupFD(t, reg), "directory": dupFD(t, dir), "closed": 987} {
		if _, err := ledgerFromFD(fd); err == nil {
			t.Errorf("%s accepted as the ledger", name)
		}
	}
}

// What zpkx runs under a seed is the build; the ledger's write end must not be
// among its descriptors, or recipe code could write an entry. The descriptor is
// handed over inheritable (as a parent's ExtraFiles leaves it), and a child
// started after ledgerFromFD must find it closed.
func TestTheLedgerDescriptorIsNotInheritedByWhatRuns(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	fd := dupFD(t, pw)
	f, err := ledgerFromFD(fd)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	child := osexec.Command("/bin/sh", "-c", "echo forged >&"+strconv.Itoa(fd))
	if err := child.Run(); err == nil {
		t.Fatalf("a child wrote to descriptor %d: it is inherited", fd)
	}
	// Close the only writers left and read what the pipe holds: nothing.
	pw.Close()
	f.Close()
	if b, _ := io.ReadAll(pr); len(b) != 0 {
		t.Fatalf("the pipe holds %q", b)
	}
}

func TestASeedWithAnUnusableLedgerDescriptorNeverRuns(t *testing.T) {
	t.Setenv("ZPKG_VERIFY", "1")
	reg, err := os.Create(filepath.Join(t.TempDir(), "regular"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	called := false
	var rc int
	msg := captureStderr(t, func() {
		rc = runSeeded("oci://ghcr.io/go-pkgx/packages", "", dupFD(t, reg), func() int { called = true; return 0 })
	})
	if rc != 2 || called || !strings.Contains(msg, "not a pipe") {
		t.Fatalf("rc=%d called=%v stderr=%q", rc, called, msg)
	}
}

func TestASeedRunsWithALedgerPipe(t *testing.T) {
	t.Setenv("ZPKG_VERIFY", "1")
	pr, pw, _ := os.Pipe()
	defer pr.Close()
	defer pw.Close()
	called := false
	rc := runSeeded("oci://ghcr.io/go-pkgx/packages", "", dupFD(t, pw), func() int { called = true; return 4 })
	if rc != 4 || !called {
		t.Fatalf("rc=%d called=%v", rc, called)
	}
}
