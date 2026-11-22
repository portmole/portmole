// Package name turns project folders and user input into tunnel names.
package name

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
)

// Room is left in the 63-character DNS label for "-<port>", a random suffix
// and "-<handle>".
const maxLen = 30

var bad = regexp.MustCompile(`[^a-z0-9]+`)

// Clean lower-cases s and replaces every run of other characters with "-".
// The result is empty when nothing usable is left.
func Clean(s string) string {
	s = strings.Trim(bad.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > maxLen {
		s = strings.Trim(s[:maxLen], "-")
	}
	return s
}

// FromDir names a tunnel after the project folder, or randomly if the folder
// name has no usable characters.
func FromDir(dir string) string {
	if n := Clean(filepath.Base(dir)); n != "" {
		return n
	}
	return Random(8)
}

// Random returns n random characters (n even) that are valid in a name.
func Random(n int) string {
	b := make([]byte, n/2)
	rand.Read(b)
	return hex.EncodeToString(b)
}
