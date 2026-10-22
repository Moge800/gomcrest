// Package gomcrest is a client for gomc-rest, a REST gateway for Mitsubishi
// PLCs using the MC protocol.
//
//	c, err := gomcrest.New("http://192.168.0.10:8080", gomcrest.WithToken("s3cret"))
//	if err != nil {
//		return err
//	}
//	words, err := c.ReadWords(ctx, "D100", 3) // []uint16
//
// Errors from the server match sentinel errors such as ErrBusy or ErrPLC with
// errors.Is; see APIError. The package never retries. ErrBadRequest,
// ErrUnauthorized, ErrForbidden, ErrBusy and ErrQueueClosed come back before
// the PLC is touched, so nothing was written or run. After any other error on
// a write or a Remote* call, including the caller's context being canceled or
// expiring after the request was sent, the operation may have run: read the
// value back, or check the PLC's state, before retrying. RandomWrite runs
// several PLC operations and can fail part-way, leaving the earlier ones
// written.
//
// K notation packs 4 to 32 consecutive bits into one integer: "K4M100" is the
// 16 bits M100–M115. ReadK, ReadKInt, WriteK and WriteKInt support K1–K8 and
// need gomc-rest v1.7.0 or later; older servers return ErrBadRequest. The
// server checks that a written value fits the width.
//
// The Remote* methods change the PLC's run state. They need a server started
// with -enable-remote and without -readonly; otherwise they return
// ErrForbidden. RemoteReset can return an error such as ErrPLCConnection even
// after the PLC has reset, because the PLC may close the connection before it
// answers. After such an error, check the PLC's state instead of resending
// RemoteReset or RemoteLatchClear automatically.
//
// gomc-rest serves plain HTTP, so a token can be read by anyone on the network
// path. Use it on a closed network, or put gomc-rest behind an HTTPS reverse
// proxy and pass that URL to New.
//
// It requires gomc-rest v1.4.0 or later (see MinimumServerVersion).
package gomcrest
