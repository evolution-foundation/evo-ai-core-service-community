// Package secretmerge keeps a stored secret from being erased by a save that
// never carried it, and redacts secret values on the way out.
//
// The two belong together on purpose. The screens round-trip the object they
// received: they load fields from the GET and send the whole thing back on
// save. The moment a value stops being returned, a wholesale overwrite writes a
// record without it and the stored secret is gone. Sanitizing a response and
// merging on write are one change, not two.
//
// Story 2.3 hit this on agent integration configs; story 2.4 hits it again on
// tool and MCP headers, which is why the logic lives here instead of being
// copied a third time.
package secretmerge

import "strings"

// safeHeaderNames are the headers whose VALUE may be returned to a client.
// Everything else is redacted: the header map is free-form, so a name-based
// denylist would miss `X-Tenant-Auth` and every other custom credential header.
// An allowlist fails closed instead.
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
//
// An absent key means "keep what is stored". A present key wins — EXCEPT when
// it arrives empty under a redacted name and a stored value exists. That empty
// is our own redaction coming back: RedactValues keeps the NAME with a blank
// value, and the screens round-trip the object they received, so every save of
// a tool with a secret header used to arrive as `Authorization: ""` and erase
// the stored secret (the 1.6 modal data-loss path, found again in the
// adversarial review of 2026-07-29). "Blank means clear" cannot coexist with a
// redaction that sends blanks back on every GET; clearing an inline secret now
// happens by replacing it with a vault reference (2.4) or retiring it (2.7).
// Safe-listed names still honour a blank: their values are never redacted, so
// a blank there is genuine user intent.
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

	// An ABSENT key is deletion, not omission: RedactValues blanks values but
	// always returns the names, so a screen that round-trips what it received
	// sends every stored name back. Restoring absent keys here made a header
	// impossible to delete — it simply reappeared on reload.
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
