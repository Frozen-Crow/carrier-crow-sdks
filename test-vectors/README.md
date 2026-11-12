# Webhook signature vectors

`webhooks.json` fixes `now` in Unix seconds and uses an obviously fake shared
secret. Every SDK reads these same cases. `body` is the original UTF-8 text,
including spaces and the final newline; never parse and reserialize it before
verification. Each case has `name`, `body`, `signature`, and `valid`; optional
`now` and `tolerance` override the common clock and 300-second window.

Signatures are computed as lowercase hex HMAC-SHA256 with the entire `secret`
as key and the bytes of `str(timestamp) + '.' + body` as the message. The valid
case, multi-v1 rotation, and inclusive past/future boundaries must parse to the
original JSON object. Tampered bodies, expired timestamps, malformed headers,
wrong signatures, and authenticated invalid JSON must raise the SDK error.

Signature verification follows `api/src/webhooks/signing.ts` exactly: the first
`t=` wins when repeated, and `v1` accepts only 64 lowercase hexadecimal characters.
SDKs additionally parse the authenticated body, so authenticated invalid JSON is
an error even though the reference signature check succeeds.
