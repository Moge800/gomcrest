package gomcrest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one gomc-rest server. It is safe for concurrent use.
//
// gomc-rest runs requests to the PLC one at a time through a queue, so
// prefer fewer, larger requests over many concurrent calls: ReadWords and
// ReadBits for contiguous ranges, RandomRead for scattered words.
type Client struct {
	base  string // validated base URL without trailing slash
	token string
	hc    *http.Client
}

// Option configures a Client. New skips nil Options.
type Option func(*Client)

// WithToken sends "Authorization: Bearer <token>" on every request.
// An empty token sends nothing.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient uses hc instead of the default client (Timeout 10s).
// hc is not modified; the Client uses a copy that does not follow redirects.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.hc = hc }
}

// New returns a Client for the gomc-rest server at baseURL, for example
// "http://192.168.0.10:8080". A path prefix such as "https://host/plc" is
// allowed for servers behind a reverse proxy.
//
// The default HTTP client times out after 10s, which also caps longer context
// deadlines. Pass WithHTTPClient to change it.
//
// Redirects are not followed: a 3xx response is returned as an *APIError.
func New(baseURL string, opts ...Option) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(base)
	// "?" or "#" would break base+path joining, even when empty ("http://h?").
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || strings.ContainsAny(base, "?#") || !validPort(u) {
		return nil, fmt.Errorf("gomcrest: invalid base URL %q: need http(s)://host[:port][/path] without credentials, query or fragment", baseURL)
	}

	c := &Client{base: base}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	hc := c.hc
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	cp := *hc
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.hc = &cp
	return c, nil
}

// validPort rejects what url.Parse lets through: a port outside 1–65535, and
// extra colons ("host:80:90" parses as host "host:80").
func validPort(u *url.URL) bool {
	if strings.Contains(u.Hostname(), ":") && !strings.HasPrefix(u.Host, "[") {
		return false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		return err == nil && n >= 1 && n <= 65535
	}
	return true
}

// maxBody caps how much of a response is read. The largest /read reply is a
// few KB; the cap guards against a misbehaving proxy. A larger 2xx body is
// ErrBadResponse; a larger error body is cut, which only shortens Message.
const maxBody = 1 << 20

// do sends a request and decodes a 2xx JSON response into out (if non-nil).
// body, if non-nil, is sent as JSON.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var rb io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rb = bytes.NewReader(b)
	}
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rb)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return transportErr(ctx, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1)) // +1 detects an oversized body
	if err != nil {
		return transportErr(ctx, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newAPIError(resp.StatusCode, data)
	}
	if len(data) > maxBody {
		return fmt.Errorf("%w: %s %s: body larger than %d bytes", ErrBadResponse, method, path, maxBody)
	}
	if out == nil {
		return nil
	}
	// json.Unmarshal accepts null and leaves out untouched.
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("%w: %s %s: null body", ErrBadResponse, method, path)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrBadResponse, method, path, err)
	}
	return nil
}

// get sends GET path?query and decodes the JSON response into a T.
func get[T any](ctx context.Context, c *Client, path string, query url.Values) (T, error) {
	var v T
	err := c.do(ctx, http.MethodGet, path, query, nil, &v)
	return v, err
}

// post sends POST path?query and requires gomc-rest's {"ok":true}, so that a
// 2xx from something else (a proxy page, an empty body) is not taken as a
// completed PLC operation.
func (c *Client) post(ctx context.Context, path string, query url.Values, body any) error {
	var ack struct {
		OK bool `json:"ok"`
	}
	if err := c.do(ctx, http.MethodPost, path, query, body, &ack); err != nil {
		return err
	}
	if !ack.OK {
		return fmt.Errorf(`%w: POST %s: missing {"ok":true}`, ErrBadResponse, path)
	}
	return nil
}

// transportErr returns the caller's context error as is, so that
// errors.Is(err, context.Canceled) works and ErrTransport does not match.
// The decision uses ctx.Err(), not err: an http.Client.Timeout error can
// also match context.DeadlineExceeded, but it is a transport error.
func transportErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return fmt.Errorf("%w: %w", ErrTransport, err)
}
