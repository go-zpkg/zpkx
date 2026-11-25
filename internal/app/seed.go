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

// The environment variables that carry the seed to the processes zpkx starts
// (the factory, and the zpkx a build script calls for its own dependencies):
// each of them is a new process and reads its own flags, so the seed travels
// the one way a process hands anything to its children. Set only inside
// runSeeded, restored on return, and read only when no flag was given.
const (
	seedFromEnv_   = "ZPKX_SEED_FROM"
	seedLedgerEnv_ = "ZPKX_SEED_LEDGER"
)

// seedActive is true while runSeeded's body runs, so the run that body makes
// does not read the environment runSeeded itself set, and seed a second time.
var seedActive bool

// seedFromEnv is the environment spelling of the leading flags.
func seedFromEnv(getenv func(string) string) (from, ledger string, err error) {
	from, ledger = getenv(seedFromEnv_), getenv(seedLedgerEnv_)
	if ledger != "" && from == "" {
		return "", "", fmt.Errorf("%s needs %s", seedLedgerEnv_, seedFromEnv_)
	}
	return from, ledger, nil
}

// runSeeded runs body with the seed source in force when from is set.
func runSeeded(from, ledger string, body func() int) int {
	if from == "" {
		return body()
	}
	defer setenvRestored(seedFromEnv_, from)()
	defer setenvRestored(seedLedgerEnv_, ledger)()
	seedActive = true
	defer func() { seedActive = false }()
	if ledger != "" {
		artifact.SetSeedLedgerFile(ledger)
		defer artifact.SetSeedLedgerFile("")
	}
	code := 0
	if err := artifact.WithSeedSource(from, func() error { code = body(); return nil }); err != nil {
		fmt.Fprintln(os.Stderr, pfx()+"seed: "+err.Error())
		return 1
	}
	return code
}
