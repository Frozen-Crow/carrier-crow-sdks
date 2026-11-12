package carriercrow

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// VerifyOptions uses wall time and a 300 second tolerance by default.
// Tolerance is a pointer so callers can explicitly require zero tolerance.
type VerifyOptions struct {
	Now       time.Time
	Tolerance *time.Duration
}

// Verify authenticates the exact raw request bytes before parsing JSON.
func Verify(body []byte, signatureHeader, secret string, options ...VerifyOptions) (*WebhookBody, error) {
	fail := func(message string) (*WebhookBody, error) {
		return nil, &Error{Code: "invalid_signature", Message: message}
	}
	o := VerifyOptions{}
	if len(options) > 0 {
		o = options[0]
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	tolerance := 300 * time.Second
	if o.Tolerance != nil {
		tolerance = *o.Tolerance
	}
	if tolerance < 0 {
		return fail("webhook tolerance must be nonnegative")
	}
	var timestamp string
	timestampSeen := false
	var signatures []string
	for _, part := range strings.Split(signatureHeader, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == "t" && !timestampSeen {
			timestampSeen = true
			timestamp = kv[1]
		}
		if kv[0] == "v1" {
			signatures = append(signatures, kv[1])
		}
	}
	if timestamp == "" || len(signatures) == 0 {
		return fail("missing webhook timestamp or signature")
	}
	for _, c := range timestamp {
		if c < '0' || c > '9' {
			return fail("invalid webhook timestamp")
		}
	}
	seconds, e := strconv.ParseInt(timestamp, 10, 64)
	if e != nil {
		return fail("invalid webhook timestamp")
	}
	// Compare integer Unix seconds, matching the server's timestamp granularity.
	delta := float64(now.Unix()) - float64(seconds)
	if delta < 0 {
		delta = -delta
	}
	if delta > tolerance.Seconds() {
		return fail("webhook timestamp outside tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	expected := mac.Sum(nil)
	matched := false
	for _, signature := range signatures {
		if len(signature) != 64 || strings.IndexFunc(signature, func(c rune) bool {
			return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f')
		}) >= 0 {
			continue
		}
		decoded, e := hex.DecodeString(signature)
		if e == nil && hmac.Equal(expected, decoded) {
			matched = true
		}
	}
	if !matched {
		return fail("webhook signature does not match")
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil || raw == nil {
		return fail("invalid webhook JSON")
	}
	for _, key := range []string{"webhook", "delivery", "events"} {
		if len(raw[key]) == 0 || string(raw[key]) == "null" {
			return fail("invalid webhook body")
		}
	}
	result := new(WebhookBody)
	if e = json.Unmarshal(body, result); e != nil {
		return fail("invalid webhook body")
	}
	return result, nil
}
