package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-zpkg/artifact"
)

func writeLedger(t *testing.T, es ...artifact.SeedEntry) string {
	t.Helper()
	var b bytes.Buffer
	for _, e := range es {
		l, _ := json.Marshal(e)
		b.Write(append(l, '\n'))
	}
	p := filepath.Join(t.TempDir(), "ledger")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSeedDigestPrintsTheDigestTheFactoryRecomputes(t *testing.T) {
	a := artifact.SeedEntry{Project: "llvm.org", Version: "23.1.2", Digest: "sha256:" + strings.Repeat("b", 64), Signers: []string{"go-pkgx"}, Source: "oci://ghcr.io/go-pkgx/packages"}
	b := artifact.SeedEntry{Project: "gnu.org/make", Version: "4.4.1", Digest: "sha256:" + strings.Repeat("a", 64), Signers: []string{"go-pkgx"}, Source: "oci://ghcr.io/go-pkgx/packages"}
	var out, errw bytes.Buffer
	// Written in the opposite order of the canonical one: the digest is of the
	// set, not of how it was written.
	if code := seedDigest([]string{writeLedger(t, a, b)}, &out, &errw); code != 0 {
		t.Fatalf("exit %d: %s", code, errw.String())
	}
	want := "sha256:" + artifact.SeedLedgerDigest([]artifact.SeedEntry{b, a}) + "\n"
	if out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

func TestSeedDigestRefusesWhatIsNotOneNonEmptyLedger(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"no argument":   nil,
		"two arguments": {"a", "b"},
		"missing file":  {filepath.Join(t.TempDir(), "nope")},
		"empty ledger":  {empty},
	} {
		var out, errw bytes.Buffer
		if code := seedDigest(args, &out, &errw); code == 0 || out.Len() != 0 || errw.Len() == 0 {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", name, code, out.String(), errw.String())
		}
	}
}
