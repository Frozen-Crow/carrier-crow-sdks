package carriercrow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMethods(t *testing.T) {
	ctx := context.Background()
	email := SendEmail{From: "a@example.com", To: Addresses{"b@example.com"}, CC: Addresses{"c@example.com"}, BCC: Addresses{"d@example.com"}, ReplyTo: Addresses{"r@example.com"}, Subject: "hello", HTML: "<b>hello</b>", Text: "hello", Headers: map[string]string{"X-Test": "yes"}, Attachments: []Attachment{{Filename: "a.txt", Content: "eA==", ContentType: "text/plain"}}, MessageID: "custom", Subscriber: "subscriber", Campaign: "campaign", App: "app"}
	cases := []struct {
		name, method, path, response string
		body                         any
		call                         func(*Client) error
	}{
		{"send", "POST", "/v1/emails", `{"object":"email","status":"accepted","message_id":null,"recipients":[{"address":"b@example.com","id":"mta"}]}`, email, func(c *Client) error {
			v, e := c.Emails.Send(ctx, email, SendOptions{IdempotencyKey: "given"})
			if e == nil && (v.MessageID != nil || v.Recipients[0].ID != "mta") {
				t.Fatal(v)
			}
			return e
		}},
		{"batch", "POST", "/v1/emails/batch", `{"results":[{"index":0,"status":"accepted","message_id":null},{"index":1,"status":"rejected","error":"bad","code":"invalid_request","field":"to"}]}`, map[string]any{"emails": []SendEmail{email}}, func(c *Client) error {
			v, e := c.Emails.Batch(ctx, []SendEmail{email}, SendOptions{IdempotencyKey: "given"})
			if e == nil && v.Results[1].Field != "to" {
				t.Fatal(v)
			}
			return e
		}},
		{"get email", "GET", "/v1/emails/id", `{"email":{"id":"id","messageId":"custom","bounceClass":"bad","returnPath":"return"},"events":[{"at":"now","kind":"delayed","sourceAddress":"ip"}],"updates":[{"extra":1}]}`, nil, func(c *Client) error {
			v, e := c.Emails.Get(ctx, "id")
			if e == nil && (*v.Email.MessageID != "custom" || *v.Email.ReturnPath != "return" || *v.Events[0].SourceAddress != "ip") {
				t.Fatal(v)
			}
			return e
		}},
		{"list emails", "GET", "/v1/emails?app=a&before=cursor&days=3&limit=4&q=hello+world&status=delivered&status=delayed&tenant=t", `{"emails":[],"next":null,"since":"now"}`, nil, func(c *Client) error {
			_, e := c.Emails.List(ctx, ListQuery{Q: "hello world", Status: []EmailStatus{Delivered, Delayed}, Tenant: "t", App: "a", Days: 3, Before: "cursor", Limit: 4})
			return e
		}},
		{"list domains", "GET", "/v1/domains", `{"domains":[{"id":"d","checked_at":null,"records":[{"for":"signature","status":"found"}]}]}`, nil, func(c *Client) error {
			v, e := c.Domains.List(ctx)
			if e == nil && (v.Domains[0].CheckedAt != nil || v.Domains[0].Records[0].For != "signature") {
				t.Fatal(v)
			}
			return e
		}},
		{"get domain", "GET", "/v1/domains/a%2Fb", `{"id":"a/b","checked_at":"now"}`, nil, func(c *Client) error {
			v, e := c.Domains.Get(ctx, "a/b")
			if e == nil && *v.CheckedAt != "now" {
				t.Fatal(v)
			}
			return e
		}},
		{"check domain", "POST", "/v1/domains/d/check", `{"id":"d"}`, nil, func(c *Client) error { _, e := c.Domains.Check(ctx, "d"); return e }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.RequestURI() != tc.path {
					t.Errorf("request %s %s", r.Method, r.URL.RequestURI())
				}
				if r.Header.Get("Authorization") != "Bearer ccm_test_test" || r.Header.Get("User-Agent") != "carriercrow-go/"+Version || r.Header.Get("Accept") != "application/json" {
					t.Error(r.Header)
				}
				b, _ := io.ReadAll(r.Body)
				if tc.body != nil {
					if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Idempotency-Key") != "given" {
						t.Error(r.Header)
					}
					expected, _ := json.Marshal(tc.body)
					if string(b) != string(expected) {
						t.Errorf("body %s != %s", b, expected)
					}
				} else if len(b) != 0 {
					t.Errorf("unexpected body %s", b)
				}
				io.WriteString(w, tc.response)
			}))
			defer server.Close()
			if e := tc.call(New("ccm_test_test", Options{BaseURL: server.URL})); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestRetrySafety(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, mode := range []string{"none", "key", "generate"} {
			t.Run(mode+map[bool]string{true: "batch", false: "send"}[batch], func(t *testing.T) {
				calls := 0
				key := ""
				var first []byte
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					b, _ := io.ReadAll(r.Body)
					if calls == 1 {
						key = r.Header.Get("Idempotency-Key")
						first = b
					} else if key != r.Header.Get("Idempotency-Key") || string(first) != string(b) {
						t.Error("retry changed key/body")
					}
					if calls == 1 {
						w.WriteHeader(503)
						io.WriteString(w, `{"error":"later","code":"unavailable"}`)
					} else {
						io.WriteString(w, `{}`)
					}
				}))
				defer s.Close()
				o := SendOptions{}
				if mode == "key" {
					o.IdempotencyKey = "stable"
				}
				if mode == "generate" {
					o.Retry = true
				}
				c := New("ccm_test_key", Options{BaseURL: s.URL, MaxRetries: 1})
				var e error
				if batch {
					_, e = c.Emails.Batch(context.Background(), []SendEmail{{}}, o)
				} else {
					_, e = c.Emails.Send(context.Background(), SendEmail{}, o)
				}
				if mode == "none" {
					if calls != 1 || e == nil || key != "" {
						t.Fatal(calls, e, key)
					}
				} else if calls != 2 || e != nil || key == "" {
					t.Fatal(calls, e, key)
				}
			})
		}
	}
}
func TestErrors(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 413, 422, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"fix to","code":"invalid_request","field":"to[2]"}`)
			}))
			defer s.Close()
			_, e := New("ccm_test_k", Options{BaseURL: s.URL, MaxRetries: -1}).Emails.Get(context.Background(), "id")
			var api *Error
			if !errors.As(e, &api) || api.Status != status || api.Code != "invalid_request" || api.Message != "fix to" || api.Field != "to[2]" || api.RetryAfter != 7*time.Second || calls != 1 {
				t.Fatal(api, calls)
			}
		})
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestNetworkRetries(t *testing.T) {
	for _, mode := range []string{"read", "send", "key"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := New("ccm_test_k", Options{MaxRetries: 1, HTTPClient: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })}})
			var e error
			if mode == "read" {
				_, e = c.Domains.List(context.Background())
			} else {
				o := SendOptions{}
				if mode == "key" {
					o.IdempotencyKey = "k"
				}
				_, e = c.Emails.Send(context.Background(), SendEmail{}, o)
			}
			want := 2
			if mode == "send" {
				want = 1
			}
			var api *Error
			if calls != want || !errors.As(e, &api) || api.Code != "network_error" || !errors.Is(e, io.ErrUnexpectedEOF) {
				t.Fatal(calls, e)
			}
		})
	}
}
func TestReadRetriesAndCancellation(t *testing.T) {
	for _, status := range []int{429, 503} {
		calls := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				w.WriteHeader(status)
				return
			}
			io.WriteString(w, `{"domains":[]}`)
		}))
		_, e := New("ccm_test_k", Options{BaseURL: s.URL, MaxRetries: 1}).Domains.List(context.Background())
		s.Close()
		if e != nil || calls != 2 {
			t.Fatal(calls, e)
		}
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "60"); w.WriteHeader(429) }))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, e := New("ccm_test_k", Options{BaseURL: s.URL}).Domains.List(ctx)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestPaging(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("q") != "keep" {
			t.Error("lost query")
		}
		if calls == 1 {
			io.WriteString(w, `{"emails":[{"id":"first"}],"next":"cursor","since":"now"}`)
		} else {
			if r.URL.Query().Get("before") != "cursor" {
				t.Error("lost cursor")
			}
			io.WriteString(w, `{"emails":[{"id":"second"}],"next":null,"since":"now"}`)
		}
	}))
	defer s.Close()
	var ids []string
	e := New("ccm_test_k", Options{BaseURL: s.URL}).Emails.EachPage(context.Background(), ListQuery{Q: "keep"}, func(p *EmailList) bool { ids = append(ids, p.Emails[0].ID); return true })
	if e != nil || !reflect.DeepEqual(ids, []string{"first", "second"}) {
		t.Fatal(ids, e)
	}
}
func TestEnvironmentAndDefaults(t *testing.T) {
	t.Setenv("CARRIERCROW_API_KEY", "ccm_test_environment")
	c := New("")
	if c.key != "ccm_test_environment" || c.base != DefaultBaseURL {
		t.Fatal(c)
	}
	if New("ccm_test_explicit").key != "ccm_test_explicit" {
		t.Fatal("env overrode key")
	}
}
func TestWebhookVectors(t *testing.T) {
	b, e := os.ReadFile("../test-vectors/webhooks.json")
	if e != nil {
		t.Fatal(e)
	}
	var vectors struct {
		Now    int64
		Secret string
		Cases  []struct {
			Name, Body, Signature string
			Valid                 bool
			Now                   *int64
			Tolerance             *float64
		}
	}
	if e = json.Unmarshal(b, &vectors); e != nil {
		t.Fatal(e)
	}
	for _, tc := range vectors.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			now := vectors.Now
			if tc.Now != nil {
				now = *tc.Now
			}
			o := VerifyOptions{Now: time.Unix(now, 0)}
			if tc.Tolerance != nil {
				d := time.Duration(*tc.Tolerance * float64(time.Second))
				o.Tolerance = &d
			}
			v, e := Verify([]byte(tc.Body), tc.Signature, vectors.Secret, o)
			if (e == nil) != tc.Valid {
				t.Fatalf("valid=%v error=%v", tc.Valid, e)
			}
			if tc.Valid && (v.Webhook == "" || v.Delivery == "") {
				t.Fatal(v)
			}
			if e != nil {
				var api *Error
				if !errors.As(e, &api) {
					t.Fatal("not typed", e)
				}
			}
		})
	}
}
func TestAddressesAndInvalidResponse(t *testing.T) {
	var a Addresses
	for _, b := range []string{`"one"`, `["one"]`} {
		if e := json.Unmarshal([]byte(b), &a); e != nil || !reflect.DeepEqual(a, Addresses{"one"}) {
			t.Fatal(a, e)
		}
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "bad json") }))
	defer s.Close()
	_, e := New("ccm_test_k", Options{BaseURL: s.URL}).Domains.List(context.Background())
	var api *Error
	if !errors.As(e, &api) || api.Code != "invalid_response" {
		t.Fatal(e)
	}
	if !strings.Contains(api.Message, "invalid") {
		t.Fatal(api)
	}
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenBody) Close() error             { return nil }
func TestInterruptedResponseRetry(t *testing.T) {
	for _, send := range []bool{false, true} {
		calls := 0
		c := New("ccm_test_k", Options{MaxRetries: 1, HTTPClient: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			calls++
			var body io.ReadCloser = brokenBody{}
			if calls > 1 {
				body = io.NopCloser(strings.NewReader(`{}`))
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
		})}})
		var e error
		if send {
			_, e = c.Emails.Send(context.Background(), SendEmail{})
		} else {
			_, e = c.Domains.List(context.Background())
		}
		if send {
			if calls != 1 || !errors.Is(e, io.ErrUnexpectedEOF) {
				t.Fatal(calls, e)
			}
		} else if calls != 2 || e != nil {
			t.Fatal(calls, e)
		}
	}
}

func TestReviewDefaultsAndMissingKey(t *testing.T) {
	t.Setenv("CARRIERCROW_API_KEY", "")
	calls := 0
	c := New("", Options{HTTPClient: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })}})
	if Version != "0.1.0" || c.retries != 2 || c.maxRetryAfter != 60*time.Second {
		t.Fatal(c, Version)
	}
	v, err := c.Domains.List(context.Background())
	var api *Error
	if v != nil || !errors.As(err, &api) || api.Code != "missing_api_key" || calls != 0 {
		t.Fatal(v, err, calls)
	}
}

func TestRetryAfterCapAndDisable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opts  Options
		after string
		want  time.Duration
	}{
		{"default cap", Options{}, "61", 61 * time.Second},
		{"custom cap", Options{MaxRetryAfter: time.Second}, "2", 2 * time.Second},
		{"disabled", Options{DisableRetries: true, MaxRetries: 5}, "1", time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			tc.opts.HTTPClient = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": []string{tc.after}}, Body: io.NopCloser(strings.NewReader(`{"error":"later"}`))}, nil
			})}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			v, err := New("ccm_test_key", tc.opts).Domains.List(ctx)
			var api *Error
			if v != nil || !errors.As(err, &api) || api.Code != "http_error" || api.RetryAfter != tc.want || calls != 1 {
				t.Fatal(v, err, calls)
			}
		})
	}
}

func TestAllMethodsReturnNilOnFailure(t *testing.T) {
	c := New("ccm_test_key", Options{DisableRetries: true, HTTPClient: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"bad"}`))}, nil
	})}})
	ctx := context.Background()
	check := func(v any, err error) {
		t.Helper()
		if err == nil || !reflect.ValueOf(v).IsNil() {
			t.Fatalf("result=%v err=%v", v, err)
		}
	}
	v1, e1 := c.Emails.Send(ctx, SendEmail{})
	check(v1, e1)
	v2, e2 := c.Emails.Batch(ctx, nil)
	check(v2, e2)
	v3, e3 := c.Emails.Get(ctx, "id")
	check(v3, e3)
	v4, e4 := c.Emails.List(ctx, ListQuery{})
	check(v4, e4)
	v5, e5 := c.Domains.List(ctx)
	check(v5, e5)
	v6, e6 := c.Domains.Get(ctx, "id")
	check(v6, e6)
	v7, e7 := c.Domains.Check(ctx, "id")
	check(v7, e7)
}

func TestWebhookFirstTimestampAndLowercase(t *testing.T) {
	body := []byte(`{"webhook":"w","delivery":"d","events":[]}`)
	mac := hmac.New(sha256.New, []byte("whsec_test_secret"))
	mac.Write(append([]byte("1000."), body...))
	sig := hex.EncodeToString(mac.Sum(nil))
	for _, tc := range []struct {
		header string
		valid  bool
	}{
		{"t=1000,t=1,v1=" + sig, true},
		{"t=1,t=1000,v1=" + sig, false},
		{"t=,t=1000,v1=" + sig, false},
		{"t=1000,v1=" + strings.ToUpper(sig), false},
		{"t=1000,v1=" + strings.ToUpper(sig) + ",v1=" + sig, true},
	} {
		v, err := Verify(body, tc.header, "whsec_test_secret", VerifyOptions{Now: time.Unix(1000, 0)})
		if (err == nil) != tc.valid || (!tc.valid && v != nil) {
			t.Fatalf("%s: %v %v", tc.header, v, err)
		}
	}
}

func TestPagingCycle(t *testing.T) {
	calls := 0
	c := New("ccm_test_key", Options{HTTPClient: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		cursor := []string{"a", "b", "a"}[calls]
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"emails":[],"next":"` + cursor + `"}`))}, nil
	})}})
	err := c.Emails.EachPage(context.Background(), ListQuery{}, func(*EmailList) bool { return true })
	var api *Error
	if !errors.As(err, &api) || api.Code != "invalid_response" || calls != 3 {
		t.Fatal(err, calls)
	}
}
