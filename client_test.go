package gomcrest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// respond returns a Client for a server that always answers status and body.
func respond(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// recorded is what a recorder server received last. Zero if nothing was sent.
type recorded struct{ method, path, query, body string }

// recorder returns a Client for a server that records each request and
// answers 200 with resp.
func recorder(t *testing.T, resp string) (*Client, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*rec = recorded{r.Method, r.URL.Path, r.URL.RawQuery, string(b)}
		io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c, rec
}

func TestNewBaseURL(t *testing.T) {
	ok := map[string]string{
		"http://h:8080":      "http://h:8080",
		" https://h/plc/ ":   "https://h/plc",
		"http://[::1]:8080/": "http://[::1]:8080",
		"http://h:65535":     "http://h:65535",
	}
	for in, want := range ok {
		c, err := New(in)
		if err != nil || c.base != want {
			t.Errorf("New(%q): err=%v; want base %q", in, err, want)
		}
	}
	bad := []string{"", "h:8080", "ftp://h", "http://", "http://:8080", "http://u:p@h",
		"http://h/?a=1", "http://h?", "http://h#x", "http://h#",
		"http://h:70000", "http://h:0", "http://h:80:90"} // url.Parse accepts these three
	for _, in := range bad {
		if _, err := New(in); err == nil {
			t.Errorf("New(%q) succeeded, want error", in)
		}
	}
}

// A nil Option is skipped, so that callers can build []Option conditionally.
func TestNewNilOption(t *testing.T) {
	c, err := New("http://h:8080", nil, WithToken("s3cret"), nil)
	if err != nil || c.token != "s3cret" || c.hc == nil {
		t.Fatalf("New with nil options = %+v, %v", c, err)
	}
}

func TestDoSends(t *testing.T) {
	cases := []struct {
		name                         string
		opts                         []Option
		method, path                 string
		query                        url.Values
		body                         any
		wantPath, wantQuery          string
		wantAuth, wantCType, wantReq string
	}{
		{
			name: "post with token", opts: []Option{WithToken("secret")},
			method: http.MethodPost, path: "/write",
			query:    url.Values{"addr": {"D0"}, "dword": {"false"}, "sint": {"true"}},
			body:     map[string]any{"values": []int{-1}},
			wantPath: "/plc/write", wantQuery: "addr=D0&dword=false&sint=true",
			wantAuth: "Bearer secret", wantCType: "application/json", wantReq: `{"values":[-1]}`,
		},
		{
			name: "get without token", opts: []Option{WithToken("")},
			method: http.MethodGet, path: "/health",
			wantPath: "/plc/health",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r *http.Request
			var reqBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				b, _ := io.ReadAll(req.Body)
				r, reqBody = req, string(b)
				io.WriteString(w, `{"n":7}`)
			}))
			defer srv.Close()
			c, err := New(srv.URL+"/plc/", tc.opts...)
			if err != nil {
				t.Fatal(err)
			}

			var out struct{ N int }
			if err := c.do(context.Background(), tc.method, tc.path, tc.query, tc.body, &out); err != nil {
				t.Fatal(err)
			}
			if r.Method != tc.method || r.URL.Path != tc.wantPath || r.URL.RawQuery != tc.wantQuery {
				t.Errorf("sent %s %s?%s; want %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery, tc.method, tc.wantPath, tc.wantQuery)
			}
			if got := r.Header.Get("Authorization"); got != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", got, tc.wantAuth)
			}
			if got := r.Header.Get("Content-Type"); got != tc.wantCType {
				t.Errorf("Content-Type = %q, want %q", got, tc.wantCType)
			}
			if reqBody != tc.wantReq {
				t.Errorf("body = %q, want %q", reqBody, tc.wantReq)
			}
			if out.N != 7 {
				t.Errorf("decoded %+v, want N=7", out)
			}
		})
	}
}

func TestNoRedirect(t *testing.T) {
	var hit atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit.Store(true) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()

	hc := &http.Client{}
	for _, opts := range [][]Option{nil, {WithHTTPClient(hc)}} {
		c, err := New(srv.URL, append(opts, WithToken("t"))...)
		if err != nil {
			t.Fatal(err)
		}
		err = c.do(context.Background(), http.MethodPost, "/write", nil, map[string]any{}, nil)
		var ae *APIError
		if !errors.As(err, &ae) || ae.Status != http.StatusFound {
			t.Errorf("err = %v, want *APIError with status 302", err)
		}
	}
	if hit.Load() {
		t.Error("redirect was followed")
	}
	if hc.CheckRedirect != nil {
		t.Error("WithHTTPClient modified the caller's client")
	}
}

// stall returns the URL of a server that never answers, and a channel that
// is closed once the server has a request.
func stall(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	got := make(chan struct{})
	var once sync.Once
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(got) })
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(done) }) // runs first: Close waits for handlers
	return srv.URL, got
}

// The caller's context ends while the request waits on the server, so the
// error comes from the HTTP round trip, not from a context that had already
// ended before anything was sent.
func TestContextErrors(t *testing.T) {
	cases := []struct {
		name string
		ctx  func(got <-chan struct{}) (context.Context, context.CancelFunc)
		want error
	}{
		{"caller cancel", func(got <-chan struct{}) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			go func() { <-got; cancel() }()
			return ctx, cancel
		}, context.Canceled},
		// A deadline cannot be fired on demand; the test checks below that the
		// server had the request before it passed.
		{"caller deadline", func(<-chan struct{}) (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 300*time.Millisecond)
		}, context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, got := stall(t)
			ctx, cancel := tc.ctx(got)
			defer cancel()
			c, err := New(u)
			if err != nil {
				t.Fatal(err)
			}
			err = c.do(ctx, http.MethodGet, "/health", nil, nil, nil)
			select {
			case <-got:
			default:
				t.Fatalf("context ended before the server had the request (err = %v)", err)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if errors.Is(err, ErrTransport) {
				t.Errorf("caller's context error wrapped as ErrTransport: %v", err)
			}
		})
	}
}

func TestTransportErrors(t *testing.T) {
	slow, _ := stall(t)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	cases := []struct {
		name string
		url  string
		opts []Option
	}{
		{"http client timeout", slow, []Option{WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond})}},
		{"connection refused", closed.URL, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(tc.url, tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			err = c.do(context.Background(), http.MethodGet, "/health", nil, nil, nil)
			if !errors.Is(err, ErrTransport) {
				t.Fatalf("err = %v, want ErrTransport", err)
			}
		})
	}
}

// A Client is shared by goroutines, as in gomc-rest-gui. Each request names
// its own address, and the server answers with that address's number, so a
// mixed-up query, body or reply fails. Run with -race (CI does) to catch
// shared state in Client.
func TestConcurrentUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := strings.TrimPrefix(r.URL.Query().Get("addr"), "D")
		switch {
		case r.Header.Get("Authorization") != "Bearer s3cret":
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == "/read":
			io.WriteString(w, `{"values":[`+n+`]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/write":
			if b, _ := io.ReadAll(r.Body); string(b) != `{"values":[`+n+`]}` {
				http.Error(w, "body "+string(b)+" for D"+n, http.StatusBadRequest)
				return
			}
			io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := New(srv.URL, WithToken("s3cret"))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	const n = 50
	errs := make(chan error, 2*n)
	var wg sync.WaitGroup
	for i := range n {
		addr := "D" + strconv.Itoa(i)
		wg.Add(2)
		go func() {
			defer wg.Done()
			v, err := c.ReadWords(ctx, addr, 1)
			if err == nil && (len(v) != 1 || v[0] != uint16(i)) {
				err = fmt.Errorf("ReadWords(%s) = %v, want [%d]", addr, v, i)
			}
			errs <- err
		}()
		go func() {
			defer wg.Done()
			errs <- c.WriteWords(ctx, addr, []uint16{uint16(i)})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestBadResponse(t *testing.T) {
	var out struct{ N int }
	err := respond(t, http.StatusOK, "not json").do(context.Background(), http.MethodGet, "/info", nil, nil, &out)
	if !errors.Is(err, ErrBadResponse) {
		t.Errorf("err = %v, want ErrBadResponse", err)
	}
}

// A 2xx body over maxBody must fail even when the truncated prefix is valid
// JSON; a body of exactly maxBody bytes is fine.
func TestBodyLimit(t *testing.T) {
	pad := func(n int) string { return `{"n":7}` + strings.Repeat(" ", n-len(`{"n":7}`)) }
	for _, tc := range []struct {
		size int
		bad  bool
	}{{maxBody, false}, {maxBody + 1, true}} {
		var out struct{ N int }
		err := respond(t, http.StatusOK, pad(tc.size)).do(context.Background(), http.MethodGet, "/info", nil, nil, &out)
		if got := errors.Is(err, ErrBadResponse); got != tc.bad || (!tc.bad && out.N != 7) {
			t.Errorf("body of %d bytes: err = %v, N = %d; want ErrBadResponse: %v", tc.size, err, out.N, tc.bad)
		}
	}
}
