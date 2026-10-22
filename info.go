package gomcrest

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
)

// MinimumServerVersion is the oldest gomc-rest this package supports.
// K notation (ReadK etc.) needs v1.7.0 or later.
const MinimumServerVersion = "v1.4.0"

// Health is the response of GET /health. The server answers 200 even when the
// PLC is unreachable; check Connected.
type Health struct {
	PLCStatus string `json:"plc_status"` // "ok" or "disconnected"
	Connected bool   `json:"connected"`
}

// Info is the response of GET /info.
type Info struct {
	Version             string   `json:"version"`
	GomcprotocolVersion string   `json:"gomcprotocol_version"` // "(devel)" for local builds
	Host                string   `json:"host"`                 // PLC host
	Port                int      `json:"port"`                 // PLC port
	Frame               string   `json:"frame"`                // "3e" or "4e"
	Transport           string   `json:"transport"`            // "tcp" or "udp"
	Mode                string   `json:"mode"`                 // "binary" or "ascii"
	ListenAddrs         []string `json:"listen_addrs"`
	ReadOnly            bool     `json:"readonly"`
	EnableRemote        bool     `json:"enable_remote"`
}

// Metrics is the response of GET /metrics. Counters are cumulative since the
// server started.
//
// PLC fields (RequestCount, AvgLatencyMs, RecentAvgLatencyMs) measure PLC wire
// time only. Client fields measure the full round trip including queue wait.
// BusyCount counts requests rejected because the queue was full.
type Metrics struct {
	ClientRequestCount       int64   `json:"client_request_count"`
	BusyCount                int64   `json:"busy_count"`
	ClientAvgLatencyMs       float64 `json:"client_avg_latency_ms"`
	ClientRecentAvgLatencyMs float64 `json:"client_recent_avg_latency_ms"`
	QueueLength              int     `json:"queue_length"`
	RequestCount             int64   `json:"request_count"`
	ReconnectCount           int64   `json:"reconnect_count"`
	TimeoutCount             int64   `json:"timeout_count"`
	PLCErrorCount            int64   `json:"plc_error_count"`
	AvgLatencyMs             float64 `json:"avg_latency_ms"`
	RecentAvgLatencyMs       float64 `json:"recent_avg_latency_ms"`
}

// Health reports whether gomc-rest is connected to the PLC.
// It needs no token.
func (c *Client) Health(ctx context.Context) (Health, error) {
	h, err := getStrict[Health](ctx, c, "/health", "plc_status", "connected")
	if err != nil {
		return Health{}, err
	}
	// The server derives plc_status from connected; other non-empty statuses
	// pass as is.
	if h.PLCStatus == "" || h.PLCStatus == "ok" && !h.Connected || h.PLCStatus == "disconnected" && h.Connected {
		return Health{}, fmt.Errorf("%w: GET /health: plc_status %q with connected=%v", ErrBadResponse, h.PLCStatus, h.Connected)
	}
	return h, nil
}

// Info returns the server configuration.
func (c *Client) Info(ctx context.Context) (Info, error) {
	i, err := getStrict[Info](ctx, c, "/info", "version", "gomcprotocol_version", "host", "port",
		"frame", "transport", "mode", "listen_addrs", "readonly", "enable_remote")
	if err != nil {
		return Info{}, err
	}
	if i.Version == "" {
		return Info{}, fmt.Errorf("%w: GET /info: empty version", ErrBadResponse)
	}
	return i, nil
}

// Metrics returns the server's counters and latency averages.
func (c *Client) Metrics(ctx context.Context) (Metrics, error) {
	return getStrict[Metrics](ctx, c, "/metrics", "client_request_count", "busy_count",
		"client_avg_latency_ms", "client_recent_avg_latency_ms", "queue_length", "request_count",
		"reconnect_count", "timeout_count", "plc_error_count", "avg_latency_ms", "recent_avg_latency_ms")
}

// Version returns the server version, e.g. "v1.7.0", or "dev" for a local build.
func (c *Client) Version(ctx context.Context) (string, error) {
	v, err := getStrict[struct{ Version string }](ctx, c, "/version", "version")
	if err != nil {
		return "", err
	}
	if v.Version == "" {
		return "", fmt.Errorf("%w: GET /version: empty version", ErrBadResponse)
	}
	return v.Version, nil
}

// nullableKeys lists the info keys gomc-rest may send as null: listen_addrs is
// null when the server cannot list its addresses (listenAddrs returns nil).
var nullableKeys = map[string]bool{"listen_addrs": true}

// getStrict is get for the info endpoints, whose keys gomc-rest sends on every
// response since v1.4.0. A missing or null key, such as in "{}" from a proxy or
// another service, is ErrBadResponse instead of a silent false, 0 or "".
// It does not check values: callers reject an empty version or plc_status,
// which gomc-rest never sends. Other strings, such as host, may be empty.
func getStrict[T any](ctx context.Context, c *Client, path string, keys ...string) (T, error) {
	var v T
	body, err := get[json.RawMessage](ctx, c, path, nil)
	if err != nil {
		return v, err
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(body, &present); err != nil {
		return v, fmt.Errorf("%w: GET %s: %w", ErrBadResponse, path, err)
	}
	for _, k := range keys {
		raw, ok := present[k]
		if !ok {
			return v, fmt.Errorf("%w: GET %s: missing %s", ErrBadResponse, path, k)
		}
		if string(raw) == "null" && !nullableKeys[k] {
			return v, fmt.Errorf("%w: GET %s: %s is null", ErrBadResponse, path, k)
		}
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return v, fmt.Errorf("%w: GET %s: %w", ErrBadResponse, path, err)
	}
	return v, nil
}

// IsSupportedVersion reports whether the server is MinimumServerVersion or
// later. A "dev" build is treated as supported. A version string that is not
// "dev" or vX.Y.Z returns ErrBadResponse.
func (c *Client) IsSupportedVersion(ctx context.Context) (bool, error) {
	v, err := c.Version(ctx)
	if err != nil {
		return false, err
	}
	if v == "dev" {
		return true, nil
	}
	got, ok := parseVersion(v)
	if !ok {
		return false, fmt.Errorf("%w: unexpected version %q", ErrBadResponse, v)
	}
	want, _ := parseVersion(MinimumServerVersion)
	return slices.Compare(got, want) >= 0, nil
}

var versionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// parseVersion splits "v1.7.0" into [1 7 0].
func parseVersion(s string) ([]int, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return nil, false
	}
	v := make([]int, 3)
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return nil, false // too many digits for int
		}
		v[i] = n
	}
	return v, true
}
