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

// KeepMissing carries stored entries over into an incoming map that omits them.
//
// An absent key means "keep what is stored"; a present key wins, even when
// blank, because sending an empty value is how a screen says "clear this".
func KeepMissing(incoming, stored map[string]string) map[string]string {
	merged := make(map[string]string, len(incoming)+len(stored))
	for key, value := range incoming {
		merged[key] = value
	}

	for key, value := range stored {
		if _, present := merged[key]; !present {
			merged[key] = value
		}
	}

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
