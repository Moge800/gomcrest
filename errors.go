package gomcrest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// Sentinel errors. Use errors.Is to test for them.
//
// Errors returned by gomc-rest are *APIError values. One matches the sentinel
// for its code only when its HTTP status is also the one listed for that code;
// otherwise it matches none (any 401 still matches ErrUnauthorized), so that a
// garbled answer is never taken as, say, an ErrBusy that is safe to resend.
// ErrTransport and ErrBadResponse wrap errors raised on the client side.
var (
	ErrBadRequest      = errors.New("gomcrest: bad request")                 // code bad_request (400/405/413)
	ErrUnauthorized    = errors.New("gomcrest: unauthorized")                // code unauthorized, or any 401
	ErrForbidden       = errors.New("gomcrest: forbidden")                   // code forbidden (403): readonly or remote control disabled
	ErrPLC             = errors.New("gomcrest: PLC error")                   // code plc_error (502); see APIError.EndCode
	ErrPLCConnection   = errors.New("gomcrest: PLC connection error")        // code connection_error (503): gomc-rest cannot reach the PLC
	ErrBusy            = errors.New("gomcrest: server busy")                 // code busy (503): queue full, not executed; safe to retry
	ErrQueueClosed     = errors.New("gomcrest: queue closed")                // code queue_closed (503): server shutting down, not executed
	ErrRequestCanceled = errors.New("gomcrest: request canceled by server")  // code request_canceled (499)
	ErrRequestTimeout  = errors.New("gomcrest: request timed out on server") // code request_timeout (504)

	// ErrTransport means the client could not reach gomc-rest.
	// It is not returned when the caller's context is canceled or expired.
	ErrTransport = errors.New("gomcrest: transport error")

	// ErrBadResponse means gomc-rest answered 2xx with an unexpected body,
	// including a write or remote call without {"ok":true}. The PLC may have
	// run such a write or remote call.
	ErrBadResponse = errors.New("gomcrest: bad response")
)

// codeErrors maps each gomc-rest error code to its sentinel and the HTTP
// statuses gomc-rest sends it with (the same since v1.4.0).
var codeErrors = map[string]struct {
	err      error
	statuses []int
}{
	"bad_request":      {ErrBadRequest, []int{400, 405, 413}},
	"unauthorized":     {ErrUnauthorized, []int{401}},
	"forbidden":        {ErrForbidden, []int{403}},
	"plc_error":        {ErrPLC, []int{502}},
	"connection_error": {ErrPLCConnection, []int{503}},
	"busy":             {ErrBusy, []int{503}},
	"queue_closed":     {ErrQueueClosed, []int{503}},
	"request_canceled": {ErrRequestCanceled, []int{499}},
	"request_timeout":  {ErrRequestTimeout, []int{504}},
}

// APIError is a non-2xx response from gomc-rest (or from a proxy in front of it).
type APIError struct {
	Status  int    // HTTP status
	Code    string // gomc-rest error code; empty if the body was not a gomc-rest error
	Message string // gomc-rest error message, or the start of a non-JSON body
	EndCode string // MC protocol end code for plc_error, e.g. "0xc051"
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("gomcrest: HTTP %d", e.Status)
	if e.Code != "" {
		s += " " + e.Code
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	if e.EndCode != "" {
		s += " (end code " + e.EndCode + ")"
	}
	return s
}

// Is reports whether target is the sentinel for e's code and e.Status is a
// status gomc-rest sends with that code. Any 401 matches ErrUnauthorized, even
// without a gomc-rest body.
func (e *APIError) Is(target error) bool {
	if target == ErrUnauthorized && e.Status == http.StatusUnauthorized {
		return true
	}
	ce, ok := codeErrors[e.Code]
	return ok && ce.err == target && slices.Contains(ce.statuses, e.Status)
}

// maxMessage caps Message for non-JSON bodies such as a proxy's HTML page.
const maxMessage = 200

func newAPIError(status int, body []byte) *APIError {
	var b struct {
		Code    string `json:"code"`
		Error   string `json:"error"`
		EndCode string `json:"end_code"`
	}
	if json.Unmarshal(body, &b) == nil && b.Code != "" {
		return &APIError{Status: status, Code: b.Code, Message: b.Error, EndCode: b.EndCode}
	}
	msg := []rune(strings.TrimSpace(string(body)))
	if len(msg) > maxMessage {
		msg = append(msg[:maxMessage], '…')
	}
	return &APIError{Status: status, Message: string(msg)}
}
