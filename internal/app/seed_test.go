package app

import (
	"reflect"
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

func TestRunRefusesALedgerWithoutASeed(t *testing.T) {
	if rc := run([]string{"--seed-ledger", "l", "+gnu.org/make"}); rc != 2 {
		t.Fatalf("rc = %d, want 2", rc)
	}
}
