package gomcrest

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestReads(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		resp  string
		call  func(c *Client) (any, error)
		query string
		want  any
	}{
		{"ReadWords", `{"values":[0,65535]}`,
			func(c *Client) (any, error) { return c.ReadWords(ctx, "D100", 2) },
			"addr=D100&count=2&dword=false&sint=false", []uint16{0, 65535}},
		{"ReadInt16s", `{"values":[-32768,32767]}`,
			func(c *Client) (any, error) { return c.ReadInt16s(ctx, "D100", 2) },
			"addr=D100&count=2&dword=false&sint=true", []int16{-32768, 32767}},
		{"ReadDWords", `{"values":[4294967295]}`,
			func(c *Client) (any, error) { return c.ReadDWords(ctx, "D100", 1) },
			"addr=D100&count=1&dword=true&sint=false", []uint32{4294967295}},
		{"ReadInt32s", `{"values":[-2147483648]}`,
			func(c *Client) (any, error) { return c.ReadInt32s(ctx, "D100", 1) },
			"addr=D100&count=1&dword=true&sint=true", []int32{-2147483648}},
		{"ReadBits", `{"values":[true,false]}`,
			func(c *Client) (any, error) { return c.ReadBits(ctx, "D100.1", 2) },
			"addr=D100.1&count=2&dword=false&sint=false", []bool{true, false}},
		{"Read unsigned dwords", `{"values":[0,4294967295]}`,
			func(c *Client) (any, error) { return c.Read(ctx, "D0", 2, Format{DWord: true}) },
			"addr=D0&count=2&dword=true&sint=false", Values{Ints: []int64{0, 4294967295}}},
		{"Read signed dwords", `{"values":[-2147483648,2147483647]}`,
			func(c *Client) (any, error) { return c.Read(ctx, "D0", 2, Format{DWord: true, SInt: true}) },
			"addr=D0&count=2&dword=true&sint=true", Values{Ints: []int64{-2147483648, 2147483647}}},
		{"Read bools", `{"values":[false,true]}`,
			func(c *Client) (any, error) { return c.Read(ctx, "M0", 2, Format{}) },
			"addr=M0&count=2&dword=false&sint=false", Values{Bools: []bool{false, true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := recorder(t, tc.resp)
			got, err := tc.call(c)
			if err != nil {
				t.Fatal(err)
			}
			if rec.method != "GET" || rec.path != "/read" || rec.query != tc.query || rec.body != "" {
				t.Errorf("sent %+v; want GET /read?%s", *rec, tc.query)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestWrites(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		call  func(c *Client) error
		query string
		body  string
	}{
		{"WriteWords", func(c *Client) error { return c.WriteWords(ctx, "D100", []uint16{1, 65535}) },
			"addr=D100&dword=false&sint=false", `{"values":[1,65535]}`},
		{"WriteInt16s", func(c *Client) error { return c.WriteInt16s(ctx, "D100", []int16{-1}) },
			"addr=D100&dword=false&sint=true", `{"values":[-1]}`},
		{"WriteDWords", func(c *Client) error { return c.WriteDWords(ctx, "D100", []uint32{4294967295}) },
			"addr=D100&dword=true&sint=false", `{"values":[4294967295]}`},
		{"WriteInt32s", func(c *Client) error { return c.WriteInt32s(ctx, "D100", []int32{-2147483648}) },
			"addr=D100&dword=true&sint=true", `{"values":[-2147483648]}`},
		{"WriteBits", func(c *Client) error { return c.WriteBits(ctx, "M0", []bool{true, false}) },
			"addr=M0&dword=false&sint=false", `{"values":[true,false]}`},
		{"Write ints", func(c *Client) error { return c.Write(ctx, "D0", Values{Ints: []int64{-5}}, Format{SInt: true}) },
			"addr=D0&dword=false&sint=true", `{"values":[-5]}`},
		{"Write bools", func(c *Client) error { return c.Write(ctx, "D0.3", Values{Bools: []bool{true}}, Format{}) },
			"addr=D0.3&dword=false&sint=false", `{"values":[true]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := recorder(t, `{"ok":true}`)
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			want := recorded{"POST", "/write", tc.query, tc.body}
			if *rec != want {
				t.Errorf("sent %+v; want %+v", *rec, want)
			}
		})
	}
}

func TestReadBadResponse(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		resp string
		call func(c *Client) error
		hint string
	}{
		{"ReadWords on a bit device", `{"values":[true]}`,
			func(c *Client) error { _, err := c.ReadWords(ctx, "M0", 1); return err }, "use ReadBits"},
		{"ReadBits on a word device", `{"values":[1]}`,
			func(c *Client) error { _, err := c.ReadBits(ctx, "D0", 1); return err }, "use ReadWords"},
		{"too few values", `{"values":[1]}`,
			func(c *Client) error { _, err := c.ReadWords(ctx, "D0", 2); return err }, "got 1 values, want 2"},
		{"no values key", `{}`,
			func(c *Client) error { _, err := c.ReadWords(ctx, "D0", 1); return err }, "got 0 values"},
		{"Read mixed", `{"values":[1,true]}`,
			func(c *Client) error { _, err := c.Read(ctx, "D0", 2, Format{}); return err }, "mixed"},
		{"Read fraction", `{"values":[1.5]}`,
			func(c *Client) error { _, err := c.Read(ctx, "D0", 1, Format{}); return err }, "non-integer"},
		{"Read null", `{"values":[null]}`,
			func(c *Client) error { _, err := c.Read(ctx, "D0", 1, Format{}); return err }, "null"},
		// json.Unmarshal turns null into 0 / false for non-pointer elements.
		{"ReadWords null", `{"values":[1,null]}`,
			func(c *Client) error { _, err := c.ReadWords(ctx, "D0", 2); return err }, "null"},
		{"ReadBits null", `{"values":[null]}`,
			func(c *Client) error { _, err := c.ReadBits(ctx, "M0", 1); return err }, "null"},
		{"ReadInt32s null", `{"values":[null]}`,
			func(c *Client) error { _, err := c.ReadInt32s(ctx, "D0", 1); return err }, "null"},
		{"ReadK null", `{"values":[null]}`,
			func(c *Client) error { _, err := c.ReadK(ctx, "K4M0"); return err }, "null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := recorder(t, tc.resp)
			err := tc.call(c)
			if !errors.Is(err, ErrBadResponse) || !strings.Contains(err.Error(), tc.hint) {
				t.Errorf("err = %v; want ErrBadResponse containing %q", err, tc.hint)
			}
		})
	}
}

// The hint names another method only when the server sent booleans for a
// numeric read or numbers for a bit read. Other bad responses get no hint:
// it would send the user to check an address that is correct.
func TestReadHintOnlyOnTypeMismatch(t *testing.T) {
	ctx := context.Background()
	words := func(c *Client) error { _, err := c.ReadWords(ctx, "D0", 1); return err }
	bits := func(c *Client) error { _, err := c.ReadBits(ctx, "M0", 1); return err }
	cases := []struct {
		name, resp string
		call       func(c *Client) error
	}{
		{"words, not json", "not json", words},
		{"words, null body", "null", words},
		{"words, values not a list", `{"values":"x"}`, words},
		{"words, out of range", `{"values":[70000]}`, words},
		{"bits, not json", "not json", bits},
		{"bits, values not a list", `{"values":{}}`, bits},
		{"bits, string value", `{"values":["1"]}`, bits},
		// Scalars instead of a list: json reports "bool" / "number" against the
		// slice type, not the element type.
		{"words, values is a bool", `{"values":true}`, words},
		{"bits, values is a number", `{"values":1}`, bits},
	}
	for _, tc := range cases {
		c, _ := recorder(t, tc.resp)
		err := tc.call(c)
		if !errors.Is(err, ErrBadResponse) || strings.Contains(err.Error(), "hint") {
			t.Errorf("%s: err = %v; want ErrBadResponse without a hint", tc.name, err)
		}
	}
}

func TestWriteInvalidValues(t *testing.T) {
	for _, v := range []Values{{}, {Ints: []int64{1}, Bools: []bool{true}}} {
		c, rec := recorder(t, `{"ok":true}`)
		err := c.Write(context.Background(), "D0", v, Format{})
		if !errors.Is(err, ErrBadRequest) {
			t.Errorf("Write(%+v) err = %v, want ErrBadRequest", v, err)
		}
		if rec.method != "" {
			t.Errorf("Write(%+v) sent a request", v)
		}
	}
}

// Writes and remote control must see gomc-rest's {"ok":true}; a 2xx proxy page
// or empty body does not prove that the PLC did anything.
func TestWriteAcknowledgement(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func(c *Client) error{
		"WriteWords":  func(c *Client) error { return c.WriteWords(ctx, "D0", []uint16{1}) },
		"RandomWrite": func(c *Client) error { return c.RandomWrite(ctx, RandomWriteRequest{Words: []WordItem{{"D0", 1}}}) },
		"RemoteStop":  func(c *Client) error { return c.RemoteStop(ctx) },
	}
	for name, call := range calls {
		for _, body := range []string{"", "<html>OK</html>", `{"ok":false}`, `{}`, "null"} {
			c, _ := recorder(t, body)
			if err := call(c); !errors.Is(err, ErrBadResponse) {
				t.Errorf("%s with body %q: err = %v, want ErrBadResponse", name, body, err)
			}
		}
	}
}
