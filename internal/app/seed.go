package app

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-zpkg/artifact"
)

// setenvRestored sets key (or unsets it when v is empty) and returns the func
// that puts back what was there.
func setenvRestored(key, v string) func() {
	old, had := os.LookupEnv(key)
	if v == "" {
		os.Unsetenv(key)
	} else {
		os.Setenv(key, v)
	}
	return func() {
		if had {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	}
}

// takeSeedFlags reads a LEADING `--seed-from <oci-url> [--seed-ledger <file>]`.
//
// A seed is somebody else's artifact used as the BUILD ENVIRONMENT of one
// bootstrap pass (artifact.WithSeedSource): verified, recorded in the ledger,
// never a dependency source. The flags are only honoured before the command,
// so a program's own arguments can never switch it on; --seed-ledger without
// --seed-from is an error, not a silent no-op.
func takeSeedFlags(argv []string) (from, ledger string, rest []string, err error) {
	rest = argv
	for len(rest) > 0 {
		var dst *string
		switch rest[0] {
		case "--seed-from":
			dst = &from
		case "--seed-ledger":
			dst = &ledger
		}
		if dst == nil {
			break
		}
		val := ""
		if len(rest) > 1 {
			val = rest[1]
		}
		if val == "" {
			return "", "", argv, fmt.Errorf("%s needs a value", rest[0])
		}
		*dst = val
		rest = rest[min(2, len(rest)):]
	}
	if ledger != "" && from == "" {
		return "", "", argv, fmt.Errorf("--seed-ledger needs --seed-from")
	}
	return from, ledger, rest, nil
}

// seedDigest is `zpkx --seed-digest <ledger>`: it prints sha256:<hex>, the
// digest of the ledger's canonical form, the one `zpkb factory
// --seed-ledger-sha256` recomputes. The build's OUTSIDE runs it on the ledger
// the seed install left and records the line, so a recipe that rewrites the
// ledger afterwards is caught by the factory. An empty or unreadable ledger is
// an error: the pass installed nothing from its seed.
func seedDigest(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, pfx()+"seed-digest: usage: zpkx --seed-digest <ledger>")
		return 2
	}
	es, err := artifact.ReadSeedLedger(args[0])
	if err != nil {
		fmt.Fprintln(stderr, pfx()+"seed-digest: "+err.Error())
		return 1
	}
	if len(es) == 0 {
		fmt.Fprintf(stderr, "%sseed-digest: ledger %s is empty: the pass installed nothing from its seed\n", pfx(), args[0])
		return 1
	}
	fmt.Fprintf(stdout, "sha256:%s\n", artifact.SeedLedgerDigest(es))
	return 0
}

// SeededEnv is the marker zpkx puts in the environment of the command it runs
// under a seed. It is INFORMATION for that command (the factory learns that a
// seed is in force and must name it), never an instruction: zpkx does not read
// it to turn a seed on, so nothing a recipe sets can start one. While it is
// present a seed flag is refused, because a seed pass is started once, by the
// factory's own invocation, and a second seeded zpkx below it would install
// artifacts that no ledger the factory holds records.
const SeededEnv = "ZPKX_SEEDED"

// refuseNestedSeed is the refusal of a seed flag below a seed that is already
// in force.
func refuseNestedSeed(getenv func(string) string, from string) error {
	if from != "" && getenv(SeededEnv) != "" {
		return fmt.Errorf("a seed is already in force (%s is set) and a seed pass is started once, by the factory: --seed-from is refused here", SeededEnv)
	}
	return nil
}

// seedRoot is true while the invocation that STARTED the seed runs: it is the
// one zpkx allowed to install what the build environment is made of.
var seedRoot bool

// refuseUnderSeed is the refusal of a zpkx that is asked for packages below a
// seed it did not start. The seed pass gave the build its environment, recorded
// in a ledger the factory holds and signs into the provenance; a recipe that
// runs `zpkx +tool` itself would fetch from the ordinary registry, outside that
// ledger, and the artifact would say it was built from a seed it was not wholly
// built from. So a build under a seed gets its tools from its PLAN only: the
// answer is a declared build dependency, not a nested call. It fails closed and
// names what was asked for.
func refuseUnderSeed(getenv func(string) string, root bool, plus, rest []string) error {
	if root || getenv(SeededEnv) == "" {
		return nil
	}
	want := "a package set"
	switch {
	case len(plus) > 0:
		want = "+" + strings.Join(plus, " +")
	case len(rest) > 0:
		want = rest[0]
	}
	return fmt.Errorf("a seed is in force (%s is set): this build gets its tools from its plan, and %s would be fetched from outside it, unrecorded in the seed ledger. Declare it as a build dependency of the recipe", SeededEnv, want)
}

// runSeeded runs body with the seed source in force when from is set. The
// ledger is required: a seed that is not recorded is not installed.
func runSeeded(from, ledger string, body func() int) int {
	if from == "" {
		return body()
	}
	if ledger == "" {
		fmt.Fprintln(os.Stderr, pfx()+"seed: --seed-from needs --seed-ledger: a seed that is not recorded is not installed")
		return 2
	}
	defer setenvRestored(SeededEnv, from)()
	artifact.SetSeedLedgerFile(ledger)
	defer artifact.SetSeedLedgerFile("")
	seedRoot = true
	defer func() { seedRoot = false }()
	code := 0
	if err := artifact.WithSeedSource(from, func() error { code = body(); return nil }); err != nil {
		fmt.Fprintln(os.Stderr, pfx()+"seed: "+err.Error())
		return 1
	}
	return code
}
