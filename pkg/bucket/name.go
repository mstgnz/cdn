// Package bucket holds the rules that describe a valid object-storage bucket
// name. It deliberately depends on nothing else in this tree so that both the
// token config loader and the HTTP handlers can share a single definition
// without an import cycle (pkg/validator already imports pkg/config).
package bucket

import (
	"errors"
	"regexp"
)

// ErrInvalidName is returned for any name that does not satisfy namePattern.
var ErrInvalidName = errors.New("invalid bucket name: must be 3-63 characters of lowercase letters, digits or '-', starting and ending with a letter or digit")

// namePattern is intentionally stricter than the S3/MinIO grammar: dots are
// rejected too. A dot buys nothing here and invites confusion with path
// segments (a bucket literally named ".." would otherwise be spellable), and
// excluding ':' guarantees a bucket name can never break the "<bucket>:<secret>"
// form of a bucket-scoped token. Names are matched as given, so surrounding
// whitespace is a rejection rather than something silently trimmed away: the
// caller must validate exactly the string it is going to use.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// Validate reports whether name is usable as a bucket name.
func Validate(name string) error {
	if !namePattern.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}
