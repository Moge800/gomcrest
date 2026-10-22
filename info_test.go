package gomcrest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Every field has a non-zero value so that a wrong json tag shows up.
func TestInfoEndpoints(t *testing.T) {
	routes := map[string]string{
		"/health":  `{"plc_status":"ok","connected":true}`,
		"/version": `{"version":"v1.7.0"}`,
		"/info": `{"version":"v1.7.0","gomcprotocol_version":"v0.3.0","host":"192.168.0.1","port":5007,` +
			`"frame":"3e","transport":"tcp","mode":"binary","listen_addrs":["192.168.1.10:8080","127.0.0.1:8080"],` +
			`"readonly":true,"enable_remote":true}`,
		"/metrics": `{"client_request_count":1300,"busy_count":12,"client_avg_latency_ms":32.5,` +
			`"client_recent_avg_latency_ms":28.1,"queue_length":4,"request_count":1234,"reconnect_count":2,` +
			`"timeout_count":3,"plc_error_count":5,"avg_latency_ms":8.33,"recent_avg_latency_ms":7.12}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok || r.Method != http.MethodGet || r.URL.RawQuery != "" {
			http.Error(w, `{"status":404,"error":"unexpected request","code":"bad_request"}`, http.StatusNotFound)
			return
		}
		io.WriteString(w, body)
	}))
	defer srv.Close()
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	h, err := c.Health(ctx)
	if want := (Health{PLCStatus: "ok", Connected: true}); err != nil || h != want {
		t.Errorf("Health = %+v, %v; want %+v", h, err, want)
	}

	v, err := c.Version(ctx)
	if err != nil || v != "v1.7.0" {
		t.Errorf("Version = %q, %v; want v1.7.0", v, err)
	}

	info, err := c.Info(ctx)
	wantInfo := Info{
		Version: "v1.7.0", GomcprotocolVersion: "v0.3.0", Host: "192.168.0.1", Port: 5007,
		Frame: "3e", Transport: "tcp", Mode: "binary",
		ListenAddrs: []string{"192.168.1.10:8080", "127.0.0.1:8080"}, ReadOnly: true, EnableRemote: true,
	}
	if err != nil || !reflect.DeepEqual(info, wantInfo) {
		t.Errorf("Info = %+v, %v; want %+v", info, err, wantInfo)
	}

	m, err := c.Metrics(ctx)
	wantM := Metrics{
		ClientRequestCount: 1300, BusyCount: 12, ClientAvgLatencyMs: 32.5, ClientRecentAvgLatencyMs: 28.1,
		QueueLength: 4, RequestCount: 1234, ReconnectCount: 2, TimeoutCount: 3, PLCErrorCount: 5,
		AvgLatencyMs: 8.33, RecentAvgLatencyMs: 7.12,
	}
	if err != nil || m != wantM {
		t.Errorf("Metrics = %+v, %v; want %+v", m, err, wantM)
	}
}

// A 200 with null, or without a field gomc-rest always sends (e.g. "{}" from a
// proxy or another service), must not look like a valid answer.
func TestInfoSchemaLessResponses(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func(c *Client) error{
		"Health":  func(c *Client) error { _, err := c.Health(ctx); return err },
		"Info":    func(c *Client) error { _, err := c.Info(ctx); return err },
		"Version": func(c *Client) error { _, err := c.Version(ctx); return err },
		"Metrics": func(c *Client) error { _, err := c.Metrics(ctx); return err },
	}
	for name, call := range calls {
		for _, body := range []string{"null", " null\n", "{}"} {
			if err := call(respond(t, http.StatusOK, body)); !errors.Is(err, ErrBadResponse) {
				t.Errorf("%s with body %q: err = %v, want ErrBadResponse", name, body, err)
			}
		}
	}
}

// Keys gomc-rest always sends must be present, so that a missing key is not
// read as false / 0. Legitimate false, 0 and null values still pass.
func TestInfoRequiredKeys(t *testing.T) {
	ctx := context.Background()
	health := func(c *Client) error { _, err := c.Health(ctx); return err }
	info := func(c *Client) error { _, err := c.Info(ctx); return err }
	metrics := func(c *Client) error { _, err := c.Metrics(ctx); return err }
	version := func(c *Client) error { _, err := c.Version(ctx); return err }
	fullInfo := `{"version":"v1.7.0","gomcprotocol_version":"v0.3.0","host":"h","port":5007,"frame":"3e",` +
		`"transport":"tcp","mode":"binary","listen_addrs":null,"readonly":false,"enable_remote":false}`
	zeroMetrics := `{"client_request_count":0,"busy_count":0,"client_avg_latency_ms":0,"client_recent_avg_latency_ms":0,` +
		`"queue_length":0,"request_count":0,"reconnect_count":0,"timeout_count":0,"plc_error_count":0,` +
		`"avg_latency_ms":0,"recent_avg_latency_ms":0}`
	cases := []struct {
		name string
		call func(c *Client) error
		body string
		bad  bool
	}{
		{"health without connected", health, `{"plc_status":"ok"}`, true},
		{"health ok but not connected", health, `{"plc_status":"ok","connected":false}`, true},
		{"health disconnected but connected", health, `{"plc_status":"disconnected","connected":true}`, true},
		{"health disconnected", health, `{"plc_status":"disconnected","connected":false}`, false},
		{"info with version only", info, `{"version":"v1.7.0"}`, true},
		{"info with null listen_addrs", info, fullInfo, false}, // listenAddrs can return nil
		{"metrics all zero", metrics, zeroMetrics, false},
		{"metrics missing a key", metrics, strings.Replace(zeroMetrics, `"busy_count":0,`, "", 1), true},
		// null would decode to a zero value; only listen_addrs may be null.
		{"version null", version, `{"version":null}`, true},
		{"health status null", health, `{"plc_status":null,"connected":true}`, true},
		{"info readonly null", info, strings.Replace(fullInfo, `"readonly":false`, `"readonly":null`, 1), true},
		{"metrics counter null", metrics, strings.Replace(zeroMetrics, `"busy_count":0`, `"busy_count":null`, 1), true},
		// gomc-rest never sends an empty version or plc_status. host has no
		// check on the server and may be empty.
		{"version empty", version, `{"version":""}`, true},
		{"health status empty", health, `{"plc_status":"","connected":false}`, true},
		{"info version empty", info, strings.Replace(fullInfo, `"version":"v1.7.0"`, `"version":""`, 1), true},
		{"info host empty", info, strings.Replace(fullInfo, `"host":"h"`, `"host":""`, 1), false},
	}
	for _, tc := range cases {
		err := tc.call(respond(t, http.StatusOK, tc.body))
		if got := errors.Is(err, ErrBadResponse); got != tc.bad {
			t.Errorf("%s: err = %v, want ErrBadResponse: %v", tc.name, err, tc.bad)
		}
	}
}

func TestIsSupportedVersion(t *testing.T) {
	ctx := context.Background()
	supported := map[string]bool{
		"v1.4.0":  true,
		"v1.3.9":  false,
		"v0.14.1": false,
		"v1.10.0": true, // numeric, not string, comparison
		"v2.0.0":  true,
		"1.7.0":   true,
		"dev":     true,
	}
	for v, want := range supported {
		got, err := respond(t, http.StatusOK, fmt.Sprintf(`{"version":%q}`, v)).IsSupportedVersion(ctx)
		if err != nil || got != want {
			t.Errorf("IsSupportedVersion(%q) = %v, %v; want %v", v, got, err, want)
		}
	}
	for _, v := range []string{"", "v1.7", "v1.7.0-rc1", "(devel)", "v99999999999999999999.0.0"} {
		_, err := respond(t, http.StatusOK, fmt.Sprintf(`{"version":%q}`, v)).IsSupportedVersion(ctx)
		if !errors.Is(err, ErrBadResponse) {
			t.Errorf("IsSupportedVersion(%q) err = %v, want ErrBadResponse", v, err)
		}
	}
	// Server errors pass through unchanged.
	_, err := respond(t, http.StatusUnauthorized, `{"status":401,"error":"x","code":"unauthorized"}`).IsSupportedVersion(ctx)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}
