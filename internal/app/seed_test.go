package app

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-zpkg/artifact"
)

func TestSeedFlagsAreOnlyReadBeforeTheCommand(t *testing.T) {
	cases := []struct {
		name         string
		in           []string
		from, ledger string
		rest         []string
		wantErr      bool
	}{
		{"none", []string{"+gnu.org/make", "--", "make"}, "", "", []string{"+gnu.org/make", "--", "make"}, false},
		{"from", []string{"--seed-from", "oci://x", "+a", "--", "b"}, "oci://x", "", []string{"+a", "--", "b"}, false},
		{"both", []string{"--seed-from", "oci://x", "--seed-ledger", "l", "+a"}, "oci://x", "l", []string{"+a"}, false},
		{"ledger first", []string{"--seed-ledger", "l", "--seed-from", "oci://x"}, "oci://x", "l", []string{}, false},
		{"after the command is the program's", []string{"+a", "--", "p", "--seed-from", "oci://x"}, "", "", []string{"+a", "--", "p", "--seed-from", "oci://x"}, false},
		{"missing value", []string{"--seed-from"}, "", "", nil, true},
		{"empty value", []string{"--seed-from", ""}, "", "", nil, true},
		{"ledger alone", []string{"--seed-ledger", "l", "+a"}, "", "", nil, true},
	}
	for _, c := range cases {
		from, ledger, rest, err := takeSeedFlags(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v", c.name, err)
			continue
		}
		if c.wantErr {
			continue
		}
		if from != c.from || ledger != c.ledger || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("%s: got %q %q %q", c.name, from, ledger, rest)
		}
	}
}

func TestASeedIsAcceptedAsABuildEnvironmentAndNothingElse(t *testing.T) {
	t.Setenv("ZPKG_VERIFY", "1")
	oldBase := artifact.DistBase
	var seen string
	rc := runSeeded("oci://ghcr.io/go-pkgx/packages", filepath.Join(t.TempDir(), "l"), func() int { seen = artifact.DistBase; return 7 })
	if rc != 7 || seen != "oci://ghcr.io/go-pkgx/packages" {
		t.Fatalf("rc=%d DistBase during=%q", rc, seen)
	}
	if artifact.DistBase != oldBase {
		t.Fatalf("DistBase not restored: %q", artifact.DistBase)
	}
	// go-pkgx is still refused as a dependency source once the seed call is over.
	if err := artifact.CheckDistSource("oci://ghcr.io/go-pkgx/packages"); err == nil {
		t.Fatal("go-pkgx accepted as a dependency source after the seed call")
	}
}

func TestWithoutASeedTheBodyRunsWithTheDefaultSource(t *testing.T) {
	var seen string
	rc := runSeeded("", "", func() int { seen = artifact.DistBase; return 5 })
	if rc != 5 || seen != artifact.DefaultDistBase {
		t.Fatalf("rc=%d DistBase=%q", rc, seen)
	}
}

func TestASeedWithoutVerifiedSignaturesIsRefused(t *testing.T) {
	called := false
	ledger := filepath.Join(t.TempDir(), "l")
	if rc := runSeeded("http://dist.pkgx.dev", ledger, func() int { called = true; return 0 }); rc != 1 || called {
		t.Fatalf("an HTTP seed ran: rc=%d called=%v", rc, called)
	}
	t.Setenv("ZPKG_VERIFY", "0")
	called = false
	if rc := runSeeded("oci://ghcr.io/go-pkgx/packages", ledger, func() int { called = true; return 0 }); rc != 1 || called {
		t.Fatalf("an unverified seed ran: rc=%d called=%v", rc, called)
	}
}

func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	f()
	os.Stderr = old
	w.Close()
	return <-done
}

func TestRunRefusesAMalformedSeedFlagAndSaysWhy(t *testing.T) {
	for _, argv := range [][]string{
		{"--seed-ledger", "l", "+gnu.org/make"},
		{"--seed-from"},
		{"--seed-from", "", "-v"},
	} {
		var rc int
		msg := captureStderr(t, func() { rc = run(argv) })
		if rc != 2 || !strings.Contains(msg, "--seed-") {
			t.Errorf("%q: rc=%d stderr=%q", argv, rc, msg)
		}
	}
}

// The seed flags reach the seed machinery: a refused seed stops the command
// before it is looked up, whatever follows.
func TestRunStopsWhenTheSeedIsRefused(t *testing.T) {
	var rc int
	msg := captureStderr(t, func() {
		rc = run([]string{"--seed-from", "http://dist.pkgx.dev", "--seed-ledger", filepath.Join(t.TempDir(), "l"), "-v"})
	})
	if rc != 1 || !strings.Contains(msg, "seed:") {
		t.Fatalf("rc=%d stderr=%q", rc, msg)
	}
}

func TestTheSeedIsMarkedForTheCommandAndTheMarkerIsRestored(t *testing.T) {
	t.Setenv("ZPKG_VERIFY", "1")
	os.Unsetenv(SeededEnv)
	var marker string
	runSeeded("oci://ghcr.io/go-pkgx/packages", filepath.Join(t.TempDir(), "l"), func() int {
		marker = os.Getenv(SeededEnv)
		return 0
	})
	if marker != "oci://ghcr.io/go-pkgx/packages" {
		t.Fatalf("the command would see the marker %q", marker)
	}
	if _, ok := os.LookupEnv(SeededEnv); ok {
		t.Error("the marker stayed in the environment after the call")
	}
}

// F1: no environment spelling turns a seed on. A recipe that sets what zpkx
// used to read gets the ordinary run, not a seed.
func TestNoEnvironmentVariableStartsASeed(t *testing.T) {
	for _, k := range []string{"ZPKX_SEED_FROM", "ZPKX_SEED_LEDGER", SeededEnv} {
		t.Setenv(k, "http://dist.pkgx.dev")
	}
	var rc int
	msg := captureStderr(t, func() { rc = run([]string{"-v"}) })
	if rc != 0 || strings.Contains(msg, "seed") {
		t.Fatalf("an environment variable reached the seed machinery: rc=%d stderr=%q", rc, msg)
	}
}

// A seed pass is started once. Below one, a seed flag is refused before the
// seed machinery sees it.
func TestASeedFlagBelowASeedIsRefused(t *testing.T) {
	t.Setenv(SeededEnv, "oci://ghcr.io/go-pkgx/packages")
	var rc int
	msg := captureStderr(t, func() {
		rc = run([]string{"--seed-from", "oci://ghcr.io/go-pkgx/packages", "--seed-ledger", filepath.Join(t.TempDir(), "other"), "-v"})
	})
	if rc != 2 || !strings.Contains(msg, "already in force") {
		t.Fatalf("rc=%d stderr=%q", rc, msg)
	}
	if err := refuseNestedSeed(func(string) string { return "" }, "oci://x"); err != nil {
		t.Errorf("a first seed was refused: %v", err)
	}
	if err := refuseNestedSeed(func(string) string { return "x" }, ""); err != nil {
		t.Errorf("a run with no seed flag was refused: %v", err)
	}
}

func TestASeedWithoutALedgerIsRefusedBeforeItRuns(t *testing.T) {
	t.Setenv("ZPKG_VERIFY", "1")
	called := false
	var rc int
	msg := captureStderr(t, func() {
		rc = runSeeded("oci://ghcr.io/go-pkgx/packages", "", func() int { called = true; return 0 })
	})
	if rc != 2 || called || !strings.Contains(msg, "--seed-ledger") {
		t.Fatalf("rc=%d called=%v stderr=%q", rc, called, msg)
	}
}
