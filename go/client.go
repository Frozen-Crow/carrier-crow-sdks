// Package carriercrow sends email and reads Carrier Crow telemetry.
package carriercrow

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const Version = "1.0.0"
const DefaultBaseURL = "https://api.mta.carriercrow.com"

// Error carries API failures and local/network failures (Status zero).
type Error struct {
	Status     int
	Code       string
	Message    string
	Field      string
	RetryAfter time.Duration
	Cause      error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

type Options struct {
	BaseURL    string
	HTTPClient *http.Client
	MaxRetries int
}
type SendOptions struct {
	IdempotencyKey string
	Retry          bool
}
type Client struct {
	Emails    *EmailsService
	Domains   *DomainsService
	key, base string
	http      *http.Client
	retries   int
}
type EmailsService struct{ client *Client }
type DomainsService struct{ client *Client }

// New reads CARRIERCROW_API_KEY when apiKey is empty. Reads retry twice by default.
func New(apiKey string, options ...Options) *Client {
	o := Options{}
	if len(options) > 0 {
		o = options[0]
	}
	if apiKey == "" {
		apiKey = os.Getenv("CARRIERCROW_API_KEY")
	}
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	retries := o.MaxRetries
	if retries == 0 {
		retries = 2
	}
	if retries < 0 {
		retries = 0
	}
	c := &Client{key: apiKey, base: strings.TrimRight(o.BaseURL, "/"), http: o.HTTPClient, retries: retries}
	c.Emails = &EmailsService{c}
	c.Domains = &DomainsService{c}
	return c
}
func (c *Client) request(ctx context.Context, method, path string, body any, out any, send *SendOptions) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return &Error{Code: "invalid_request", Message: err.Error(), Cause: err}
		}
	}
	key := ""
	retries := c.retries
	if send != nil {
		key = send.IdempotencyKey
		if send.Retry && key == "" {
			b := make([]byte, 16)
			if _, err = rand.Read(b); err != nil {
				return &Error{Code: "key_generation", Message: err.Error(), Cause: err}
			}
			key = hex.EncodeToString(b)
		}
		if key == "" {
			retries = 0
		}
	}
	for attempt := 0; ; attempt++ {
		req, e := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
		if e != nil {
			return &Error{Code: "invalid_request", Message: e.Error(), Cause: e}
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("User-Agent", "carriercrow-go/"+Version)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		resp, e := c.http.Do(req)
		var failure *Error
		retry := false
		if e != nil {
			failure = &Error{Code: "network_error", Message: e.Error(), Cause: e}
			retry = true
		} else {
			payload, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				failure = &Error{Code: "network_error", Message: readErr.Error(), Cause: readErr}
				retry = true
			} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if out == nil {
					return nil
				}
				if e = json.Unmarshal(payload, out); e != nil {
					return &Error{Status: resp.StatusCode, Code: "invalid_response", Message: e.Error(), Cause: e}
				}
				return nil
			}
			if failure == nil {
				var wire struct {
					Error string `json:"error"`
					Code  string `json:"code"`
					Field string `json:"field"`
				}
				_ = json.Unmarshal(payload, &wire)
				if wire.Error == "" {
					wire.Error = http.StatusText(resp.StatusCode)
				}
				failure = &Error{Status: resp.StatusCode, Code: wire.Code, Message: wire.Error, Field: wire.Field}
				if secs, e := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 32); e == nil && secs > 0 {
					failure.RetryAfter = time.Duration(secs) * time.Second
				}
				retry = resp.StatusCode == 429 || resp.StatusCode == 503
			}
		}
		if !retry || attempt >= retries || ctx.Err() != nil {
			return failure
		}
		delay := 100 * time.Millisecond * time.Duration(1<<min(attempt, 8))
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(delay)))
		if n != nil {
			delay += time.Duration(n.Int64())
		}
		if failure.RetryAfter > delay {
			delay = failure.RetryAfter
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &Error{Code: "network_error", Message: ctx.Err().Error(), Cause: ctx.Err()}
		case <-timer.C:
		}
	}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func (s *EmailsService) Send(ctx context.Context, p SendEmail, options ...SendOptions) (*SendResult, error) {
	o := SendOptions{}
	if len(options) > 0 {
		o = options[0]
	}
	v := new(SendResult)
	e := s.client.request(ctx, "POST", "/v1/emails", p, v, &o)
	return v, e
}
func (s *EmailsService) Batch(ctx context.Context, p []SendEmail, options ...SendOptions) (*BatchResponse, error) {
	o := SendOptions{}
	if len(options) > 0 {
		o = options[0]
	}
	v := new(BatchResponse)
	e := s.client.request(ctx, "POST", "/v1/emails/batch", struct {
		Emails []SendEmail `json:"emails"`
	}{p}, v, &o)
	return v, e
}
func (s *EmailsService) Get(ctx context.Context, id string) (*EmailDetail, error) {
	v := new(EmailDetail)
	e := s.client.request(ctx, "GET", "/v1/emails/"+url.PathEscape(id), nil, v, nil)
	return v, e
}
func (s *EmailsService) List(ctx context.Context, q ListQuery) (*EmailList, error) {
	v := url.Values{}
	for k, x := range map[string]string{"q": q.Q, "tenant": q.Tenant, "app": q.App, "before": q.Before} {
		if x != "" {
			v.Set(k, x)
		}
	}
	for _, x := range q.Status {
		v.Add("status", string(x))
	}
	if q.Days != 0 {
		v.Set("days", strconv.Itoa(q.Days))
	}
	if q.Limit != 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	p := new(EmailList)
	e := s.client.request(ctx, "GET", "/v1/emails?"+v.Encode(), nil, p, nil)
	return p, e
}

// EachPage follows next cursors until exhausted or the callback returns false.
func (s *EmailsService) EachPage(ctx context.Context, q ListQuery, visit func(*EmailList) bool) error {
	for {
		p, e := s.List(ctx, q)
		if e != nil {
			return e
		}
		if !visit(p) || p.Next == nil || *p.Next == "" {
			return nil
		}
		if *p.Next == q.Before {
			return &Error{Code: "invalid_response", Message: "pagination cursor did not advance"}
		}
		q.Before = *p.Next
	}
}
func (s *DomainsService) List(ctx context.Context) (*DomainList, error) {
	v := new(DomainList)
	e := s.client.request(ctx, "GET", "/v1/domains", nil, v, nil)
	return v, e
}
func (s *DomainsService) Get(ctx context.Context, id string) (*Domain, error) {
	return s.domain(ctx, "GET", id, "")
}
func (s *DomainsService) Check(ctx context.Context, id string) (*Domain, error) {
	return s.domain(ctx, "POST", id, "/check")
}
func (s *DomainsService) domain(ctx context.Context, method, id, suffix string) (*Domain, error) {
	v := new(Domain)
	e := s.client.request(ctx, method, "/v1/domains/"+url.PathEscape(id)+suffix, nil, v, nil)
	return v, e
}
