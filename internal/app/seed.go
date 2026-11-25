package app

import (
	"fmt"
	"os"

	"github.com/go-zpkg/artifact"
)

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

// runSeeded runs body with the seed source in force when from is set.
func runSeeded(from, ledger string, body func() int) int {
	if from == "" {
		return body()
	}
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
