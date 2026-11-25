package app

import (
	"io"
	"os"
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
	rc := runSeeded("oci://ghcr.io/go-pkgx/packages", "", func() int { seen = artifact.DistBase; return 7 })
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
	if rc := runSeeded("http://dist.pkgx.dev", "", func() int { called = true; return 0 }); rc != 1 || called {
		t.Fatalf("an HTTP seed ran: rc=%d called=%v", rc, called)
	}
	t.Setenv("ZPKG_VERIFY", "0")
	called = false
	if rc := runSeeded("oci://ghcr.io/go-pkgx/packages", "", func() int { called = true; return 0 }); rc != 1 || called {
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
	msg := captureStderr(t, func() { rc = run([]string{"--seed-from", "http://dist.pkgx.dev", "-v"}) })
	if rc != 1 || !strings.Contains(msg, "seed:") {
		t.Fatalf("rc=%d stderr=%q", rc, msg)
	}
}

func TestTheSeedTravelsToChildrenThroughTheEnvironmentAndNowhereElse(t *testing.T) {
	t.Setenv("ZPKG_VERIFY", "1")
	t.Setenv(seedFromEnv_, "")
	os.Unsetenv(seedFromEnv_)
	os.Unsetenv(seedLedgerEnv_)
	var from, ledger string
	runSeeded("oci://ghcr.io/go-pkgx/packages", "/l/seed.jsonl", func() int {
		from, ledger = os.Getenv(seedFromEnv_), os.Getenv(seedLedgerEnv_)
		return 0
	})
	if from != "oci://ghcr.io/go-pkgx/packages" || ledger != "/l/seed.jsonl" {
		t.Fatalf("children would see %q %q", from, ledger)
	}
	if _, ok := os.LookupEnv(seedFromEnv_); ok {
		t.Error("the seed source stayed in the environment after the call")
	}
	if _, ok := os.LookupEnv(seedLedgerEnv_); ok {
		t.Error("the ledger stayed in the environment after the call")
	}
	// An earlier value comes back.
	t.Setenv(seedLedgerEnv_, "before")
	runSeeded("oci://ghcr.io/go-pkgx/packages", "", func() int { return 0 })
	if os.Getenv(seedLedgerEnv_) != "before" {
		t.Errorf("ledger env = %q, want it restored", os.Getenv(seedLedgerEnv_))
	}
}

func TestTheEnvironmentSpellingIsTheFlagsSpelling(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if f, l, err := seedFromEnv(get(map[string]string{seedFromEnv_: "oci://x", seedLedgerEnv_: "l"})); err != nil || f != "oci://x" || l != "l" {
		t.Errorf("both: %q %q %v", f, l, err)
	}
	if f, l, err := seedFromEnv(get(nil)); err != nil || f != "" || l != "" {
		t.Errorf("none: %q %q %v", f, l, err)
	}
	if _, _, err := seedFromEnv(get(map[string]string{seedLedgerEnv_: "l"})); err == nil {
		t.Error("a ledger with no seed was accepted")
	}
}

func TestRunReadsTheSeedFromTheEnvironmentAndRefusesIt(t *testing.T) {
	t.Setenv(seedFromEnv_, "http://dist.pkgx.dev")
	t.Setenv(seedLedgerEnv_, "")
	var rc int
	msg := captureStderr(t, func() { rc = run([]string{"-v"}) })
	if rc != 1 || !strings.Contains(msg, "seed:") {
		t.Fatalf("rc=%d stderr=%q", rc, msg)
	}
	t.Setenv(seedFromEnv_, "")
	t.Setenv(seedLedgerEnv_, "l")
	msg = captureStderr(t, func() { rc = run([]string{"-v"}) })
	if rc != 2 || !strings.Contains(msg, seedLedgerEnv_) {
		t.Fatalf("rc=%d stderr=%q", rc, msg)
	}
}
