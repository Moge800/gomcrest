package gomcrest

import (
	"context"
	"net/url"
	"strconv"
)

// ClearMode selects which devices RemoteRun clears before running.
type ClearMode int

const (
	ClearNone        ClearMode = 0 // do not clear
	ClearExceptLatch ClearMode = 1 // clear devices outside the latch range
	ClearAll         ClearMode = 2 // clear all devices, including the latch range
)

// RunOptions configures RemoteRun.
type RunOptions struct {
	Clear ClearMode
	Force bool // run even if another device has the PLC in remote STOP/PAUSE
}

// RemoteRun puts the PLC in RUN.
func (c *Client) RemoteRun(ctx context.Context, opts RunOptions) error {
	q := url.Values{"clear": {strconv.Itoa(int(opts.Clear))}, "force": {strconv.FormatBool(opts.Force)}}
	return c.post(ctx, "/remote/run", q, nil)
}

// RemotePause puts the PLC in PAUSE. force pauses even if another device
// holds remote control.
func (c *Client) RemotePause(ctx context.Context, force bool) error {
	return c.post(ctx, "/remote/pause", url.Values{"force": {strconv.FormatBool(force)}}, nil)
}

// RemoteStop puts the PLC in STOP.
func (c *Client) RemoteStop(ctx context.Context) error {
	return c.post(ctx, "/remote/stop", nil, nil)
}

// RemoteLatchClear clears the latch range. The PLC usually has to be in STOP;
// otherwise the PLC rejects it (ErrPLC).
//
// After an error other than ErrBadRequest, ErrUnauthorized, ErrForbidden,
// ErrBusy or ErrQueueClosed, the latch range may have been cleared. Do not
// resend it automatically.
func (c *Client) RemoteLatchClear(ctx context.Context) error {
	return c.post(ctx, "/remote/latch-clear", nil, nil)
}

// RemoteReset resets the PLC. The PLC usually has to be in STOP; otherwise the
// PLC rejects it (ErrPLC).
//
// The PLC may close the connection before it answers, so an error such as
// ErrPLCConnection does not mean that the reset did not happen. Check the
// PLC's state instead of resending automatically.
func (c *Client) RemoteReset(ctx context.Context) error {
	return c.post(ctx, "/remote/reset", nil, nil)
}
