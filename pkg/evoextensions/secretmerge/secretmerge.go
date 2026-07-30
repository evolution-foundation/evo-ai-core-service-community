// Package secretmerge redacts secret values on the way out and keeps a save
// that never carried them from erasing what is stored.
// // The two halves are one change: screens round-trip the object they received,
// so the moment a value stops being returned, a wholesale overwrite drops it.
package secretmerge

import "strings"

// safeHeaderNames are the headers whose VALUE may be returned to a client. The
// map is free-form, so a denylist would miss `X-Tenant-Auth` and every other
// custom credential header; an allowlist fails closed.
var safeHeaderNames = map[string]bool{
	"accept":           true,
	"accept-encoding":  true,
	"accept-language":  true,
	"cache-control":    true,
	"content-type":     true,
	"user-agent":       true,
	"x-request-id":     true,
	"x-correlation-id": true,
}

// KeepMissing carries stored entries over into an incoming map that omits or
// echoes them.
// // A blank under a redacted name is our own redaction echoing back, so it means
// "keep", not "clear" — "blank clears" cannot coexist with a GET that returns
// blanks. Safe-listed names are never redacted, so a blank there is real intent.
// Clearing a secret happens by pointing at a vault credential or retiring the
// inline field.
func KeepMissing(incoming, stored map[string]string) map[string]string {
	merged := make(map[string]string, len(incoming)+len(stored))
	for key, value := range incoming {
		if value == "" && !safeHeaderNames[strings.ToLower(key)] {
			if storedValue, present := stored[key]; present {
				merged[key] = storedValue
				continue
			}
		}
		merged[key] = value
	}

	// An ABSENT key is deletion: RedactValues always returns the names, so a
	// round-tripping screen sends back every one it still wants.
	return merged
}

// RedactValues blanks the value of every header that is not known to be safe,
// keeping the NAMES so a screen can still render which headers are configured.
func RedactValues(headers map[string]string) map[string]string {
	redacted := make(map[string]string, len(headers))

	for name, value := range headers {
		if safeHeaderNames[strings.ToLower(name)] {
			redacted[name] = value
			continue
		}
		redacted[name] = ""
	}

	return redacted
}
