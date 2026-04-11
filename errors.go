package rulekit

import "errors"

// ErrChecksumMismatch is returned when the SHA-256 hash of a dsl.json file
// does not match the checksum recorded in rulekit.lock.
var ErrChecksumMismatch = errors.New("rulekit: checksum mismatch")

// ErrNoRulekitDir is returned by New when no .rulekit/ directory is found
// by walking up from the current working directory.
var ErrNoRulekitDir = errors.New("rulekit: no .rulekit/ directory found")
