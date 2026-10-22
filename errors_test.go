package gomcrest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

var allSentinels = []error{
	ErrBadRequest, ErrUnauthorized, ErrForbidden, ErrPLC, ErrPLCConnection, ErrBusy,
	ErrQueueClosed, ErrRequestCanceled, ErrRequestTimeout, ErrTransport, ErrBadResponse,
}

// getErr sends GET /read to a server answering status and body.
func getErr(t *testing.T, status int, body string) (*APIError, error) {
	t.Helper()
	err := respond(t, status, body).do(context.Background(), http.MethodGet, "/read", nil, nil, nil)
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	return ae, err
}

// matchOnly checks that err matches want and no other sentinel (want may be nil).
func matchOnly(t *testing.T, err, want error) {
	t.Helper()
	for _, s := range allSentinels {
		if errors.Is(err, s) != (s == want) {
			t.Errorf("errors.Is(%v, %v) = %v", err, s, !(s == want))
		}
	}
}

func TestAPIErrorCodes(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   error
	}{
		{400, "bad_request", ErrBadRequest},
		{405, "bad_request", ErrBadRequest},
		{413, "bad_request", ErrBadRequest},
		{401, "unauthorized", ErrUnauthorized},
		{403, "forbidden", ErrForbidden},
		{502, "plc_error", ErrPLC},
		{503, "connection_error", ErrPLCConnection},
		{503, "busy", ErrBusy},
		{503, "queue_closed", ErrQueueClosed},
		{499, "request_canceled", ErrRequestCanceled},
		{504, "request_timeout", ErrRequestTimeout},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status, tc.code), func(t *testing.T) {
			ae, err := getErr(t, tc.status, fmt.Sprintf(`{"status":%d,"error":"msg","code":%q}`, tc.status, tc.code))
			if ae.Status != tc.status || ae.Code != tc.code || ae.Message != "msg" || ae.EndCode != "" {
				t.Errorf("got %+v", ae)
			}
			matchOnly(t, err, tc.want)
		})
	}
}

// A sentinel needs both the code and the HTTP status gomc-rest sends with it,
// so a garbled answer such as a 500 with "busy" is not taken as ErrBusy, which
// callers may resend as not run on the PLC. The *APIError keeps both values.
func TestAPIErrorStatusMismatch(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   error
	}{
		{500, "busy", nil},
		{401, "busy", ErrUnauthorized}, // any 401 is ErrUnauthorized
		{500, "queue_closed", nil},
		{503, "bad_request", nil},
		{400, "forbidden", nil},
		{403, "unauthorized", nil},
		{503, "plc_error", nil},
		{502, "connection_error", nil},
		{500, "request_canceled", nil},
		{503, "request_timeout", nil},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status, tc.code), func(t *testing.T) {
			ae, err := getErr(t, tc.status, fmt.Sprintf(`{"status":%d,"error":"msg","code":%q}`, tc.status, tc.code))
			if ae.Status != tc.status || ae.Code != tc.code {
				t.Errorf("got %+v", ae)
			}
			matchOnly(t, err, tc.want)
		})
	}
}

func TestAPIErrorEndCode(t *testing.T) {
	ae, err := getErr(t, 502, `{"status":502,"error":"end code 0xc051","code":"plc_error","end_code":"0xc051"}`)
	if ae.EndCode != "0xc051" {
		t.Errorf("EndCode = %q, want 0xc051", ae.EndCode)
	}
	matchOnly(t, err, ErrPLC)
}

func TestAPIErrorNotGomcRest(t *testing.T) {
	long := strings.Repeat("あ", maxMessage+50)
	cases := []struct {
		name   string
		status int
		body   string
		msg    string
		want   error
	}{
		{"proxy html", 502, " <html>Bad Gateway</html>\n", "<html>Bad Gateway</html>", nil},
		{"json without code", 503, `{"message":"down"}`, `{"message":"down"}`, nil},
		{"401 from proxy", 401, "Unauthorized", "Unauthorized", ErrUnauthorized},
		{"redirect", 302, "", "", nil},
		{"long body", 500, long, strings.Repeat("あ", maxMessage) + "…", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ae, err := getErr(t, tc.status, tc.body)
			if ae.Status != tc.status || ae.Code != "" || ae.Message != tc.msg {
				t.Errorf("got %+v, want Status=%d Message=%q", ae, tc.status, tc.msg)
			}
			matchOnly(t, err, tc.want)
		})
	}
}
