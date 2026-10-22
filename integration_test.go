//go:build integration

package gomcrest

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"
)

// Run with: GOMCR_TEST_URL=http://host:8080 go test -tags integration ./...
// See README for the other GOMCR_TEST_* variables.
//
// Write tests change PLC devices. Run them only against GX Simulator or an
// isolated test PLC, never against equipment in operation.

func integrationClient(t *testing.T) (*Client, context.Context) {
	t.Helper()
	u := os.Getenv("GOMCR_TEST_URL")
	if u == "" {
		t.Skip("GOMCR_TEST_URL not set")
	}
	c, err := New(u, WithToken(os.Getenv("GOMCR_TEST_TOKEN")))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	if ok, err := c.IsSupportedVersion(ctx); err != nil || !ok {
		t.Fatalf("IsSupportedVersion = %v, %v; need gomc-rest %s or later", ok, err, MinimumServerVersion)
	}
	if h, err := c.Health(ctx); err != nil || !h.Connected {
		t.Fatalf("Health = %+v, %v; gomc-rest must be connected to the PLC", h, err)
	}
	return c, ctx
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// testAddrs returns a word address (2 words are used) and a bit device
// address (16 bits are used). The defaults are for read tests only; see
// writeAddrs.
func testAddrs() (word, bit string) {
	return envOr("GOMCR_TEST_WORD_ADDR", "D9000"), envOr("GOMCR_TEST_BIT_ADDR", "M9000")
}

// writeAddrs is testAddrs without defaults. A default such as D9000 may be in
// use on a real PLC, and restoring the old value afterwards does not undo what
// the PLC program did while it was changed.
func writeAddrs(t *testing.T) (word, bit string) {
	t.Helper()
	word, bit = os.Getenv("GOMCR_TEST_WORD_ADDR"), os.Getenv("GOMCR_TEST_BIT_ADDR")
	if word == "" || bit == "" {
		t.Fatal("GOMCR_TEST_ALLOW_WRITE=1 also needs GOMCR_TEST_WORD_ADDR and GOMCR_TEST_BIT_ADDR: name devices that are free to change")
	}
	return word, bit
}

// supportsK reports whether the server is v1.7.0+ (or dev).
func supportsK(t *testing.T, ctx context.Context, c *Client) bool {
	t.Helper()
	v, err := c.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parseVersion(v)
	return v == "dev" || ok && slices.Compare(got, []int{1, 7, 0}) >= 0
}

// must(f())(t) fails the test if f returned an error, else returns the value.
func must[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationRead(t *testing.T) {
	c, ctx := integrationClient(t)
	word, bit := testAddrs()

	must(c.Info(ctx))(t)
	must(c.Metrics(ctx))(t)

	if v := must(c.ReadWords(ctx, word, 2))(t); len(v) != 2 {
		t.Errorf("ReadWords: %v", v)
	}
	must(c.ReadInt16s(ctx, word, 2))(t)
	must(c.ReadDWords(ctx, word, 1))(t)
	must(c.ReadInt32s(ctx, word, 1))(t)
	must(c.ReadBits(ctx, bit, 16))(t)
	must(c.ReadBits(ctx, word+".0", 4))(t)

	if v := must(c.Read(ctx, word, 2, Format{}))(t); len(v.Ints) != 2 || v.Bools != nil {
		t.Errorf("Read(%s) = %+v", word, v)
	}
	if v := must(c.Read(ctx, bit, 4, Format{}))(t); len(v.Bools) != 4 || v.Ints != nil {
		t.Errorf("Read(%s) = %+v", bit, v)
	}

	res := must(c.RandomRead(ctx, RandomReadRequest{
		Words: []string{word}, DWords: []string{word}, Bits: []string{bit, word + ".1"},
	}))(t)
	if len(res.Words) != 1 || len(res.DWords) != 1 || len(res.Bits) != 2 {
		t.Errorf("RandomRead = %+v", res)
	}

	if _, err := c.ReadWords(ctx, bit, 1); !errors.Is(err, ErrBadResponse) {
		t.Errorf("ReadWords(%s) err = %v, want ErrBadResponse", bit, err)
	}
	if _, err := c.ReadWords(ctx, "NOPE0", 1); !errors.Is(err, ErrBadRequest) {
		t.Errorf("ReadWords(NOPE0) err = %v, want ErrBadRequest", err)
	}

	if supportsK(t, ctx, c) {
		must(c.ReadK(ctx, "K4"+bit))(t)
		must(c.ReadKInt(ctx, "K4"+bit))(t)
	}
}

func TestIntegrationWrite(t *testing.T) {
	if os.Getenv("GOMCR_TEST_ALLOW_WRITE") != "1" {
		t.Skip("GOMCR_TEST_ALLOW_WRITE=1 not set")
	}
	c, ctx := integrationClient(t)
	word, bit := writeAddrs(t)
	if must(c.Info(ctx))(t).ReadOnly {
		t.Skip("server is readonly")
	}

	// Restore the devices afterwards, even if ctx has expired.
	origWords := must(c.ReadWords(ctx, word, 2))(t)
	origBits := must(c.ReadBits(ctx, bit, 16))(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.WriteWords(ctx, word, origWords); err != nil {
			t.Errorf("restore %s: %v", word, err)
		}
		if err := c.WriteBits(ctx, bit, origBits); err != nil {
			t.Errorf("restore %s: %v", bit, err)
		}
	})

	check := func(name string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: read back %v, want %v", name, got, want)
		}
	}

	noErr(t, c.WriteWords(ctx, word, []uint16{0x1234, 0xABCD}))
	check("WriteWords", must(c.ReadWords(ctx, word, 2))(t), []uint16{0x1234, 0xABCD})

	noErr(t, c.WriteInt16s(ctx, word, []int16{-1, -32768}))
	check("WriteInt16s", must(c.ReadInt16s(ctx, word, 2))(t), []int16{-1, -32768})

	noErr(t, c.WriteDWords(ctx, word, []uint32{0xDEADBEEF}))
	check("WriteDWords", must(c.ReadDWords(ctx, word, 1))(t), []uint32{0xDEADBEEF})

	noErr(t, c.WriteInt32s(ctx, word, []int32{-123456}))
	check("WriteInt32s", must(c.ReadInt32s(ctx, word, 1))(t), []int32{-123456})

	noErr(t, c.WriteBits(ctx, bit, []bool{true, false, true, true}))
	check("WriteBits", must(c.ReadBits(ctx, bit, 4))(t), []bool{true, false, true, true})

	noErr(t, c.WriteWords(ctx, word, []uint16{0}))
	noErr(t, c.WriteBits(ctx, word+".3", []bool{true}))
	check("WriteBits word bit", must(c.ReadWords(ctx, word, 1))(t), []uint16{1 << 3})

	noErr(t, c.Write(ctx, word, Values{Ints: []int64{-2}}, Format{SInt: true}))
	check("Write ints", must(c.Read(ctx, word, 1, Format{SInt: true}))(t), Values{Ints: []int64{-2}})

	noErr(t, c.Write(ctx, bit, Values{Bools: []bool{false, true}}, Format{}))
	check("Write bools", must(c.Read(ctx, bit, 2, Format{}))(t), Values{Bools: []bool{false, true}})

	noErr(t, c.RandomWrite(ctx, RandomWriteRequest{Words: []WordItem{{word, 7}}, Bits: []BitItem{{bit, true}}}))
	res := must(c.RandomRead(ctx, RandomReadRequest{Words: []string{word}, Bits: []string{bit}}))(t)
	check("RandomWrite words", res.Words, []uint16{7})
	check("RandomWrite bits", res.Bits, []bool{true})

	// Separate request: the server's order between words and word bits is unspecified.
	noErr(t, c.RandomWrite(ctx, RandomWriteRequest{Bits: []BitItem{{word + ".15", true}}}))
	check("RandomWrite word bit", must(c.ReadWords(ctx, word, 1))(t), []uint16{7 | 1<<15})

	if supportsK(t, ctx, c) {
		noErr(t, c.WriteK(ctx, "K4"+bit, 0xA5A5))
		check("WriteK", must(c.ReadK(ctx, "K4"+bit))(t), uint32(0xA5A5))

		noErr(t, c.WriteKInt(ctx, "K4"+bit, -2))
		check("WriteKInt", must(c.ReadKInt(ctx, "K4"+bit))(t), int32(-2))
	}
}
