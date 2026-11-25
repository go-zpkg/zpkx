package app

import (
	"fmt"
	"os"

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
	code := 0
	if err := artifact.WithSeedSource(from, func() error { code = body(); return nil }); err != nil {
		fmt.Fprintln(os.Stderr, pfx()+"seed: "+err.Error())
		return 1
	}
	return code
}
