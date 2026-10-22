package gomcrest

import (
	"context"
	"testing"
)

func TestRemote(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		call        func(c *Client) error
		path, query string
	}{
		{func(c *Client) error { return c.RemoteRun(ctx, RunOptions{}) }, "/remote/run", "clear=0&force=false"},
		{func(c *Client) error { return c.RemoteRun(ctx, RunOptions{Clear: ClearAll, Force: true}) }, "/remote/run", "clear=2&force=true"},
		{func(c *Client) error { return c.RemoteRun(ctx, RunOptions{Clear: ClearExceptLatch}) }, "/remote/run", "clear=1&force=false"},
		{func(c *Client) error { return c.RemotePause(ctx, true) }, "/remote/pause", "force=true"},
		{func(c *Client) error { return c.RemoteStop(ctx) }, "/remote/stop", ""},
		{func(c *Client) error { return c.RemoteLatchClear(ctx) }, "/remote/latch-clear", ""},
		{func(c *Client) error { return c.RemoteReset(ctx) }, "/remote/reset", ""},
	}
	for _, tc := range cases {
		c, rec := recorder(t, `{"ok":true}`)
		if err := tc.call(c); err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if want := (recorded{"POST", tc.path, tc.query, ""}); *rec != want {
			t.Errorf("sent %+v; want %+v", *rec, want)
		}
	}
}
