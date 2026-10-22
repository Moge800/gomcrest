package gomcrest

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRandomRead(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		req  RandomReadRequest
		resp string
		body string
		want RandomReadResult
	}{
		{"all lists",
			RandomReadRequest{Words: []string{"D100", "D200"}, DWords: []string{"D300"}, Bits: []string{"D100.1", "M0"}},
			`{"words":[0,65535],"dwords":[4294967295],"bits":[true,false]}`,
			`{"words":["D100","D200"],"dwords":["D300"],"bits":["D100.1","M0"]}`,
			RandomReadResult{Words: []uint16{0, 65535}, DWords: []uint32{4294967295}, Bits: []bool{true, false}}},
		{"bits only, nil lists omitted",
			RandomReadRequest{Bits: []string{"M0"}},
			`{"words":[],"dwords":[],"bits":[true]}`,
			`{"bits":["M0"]}`,
			RandomReadResult{Words: []uint16{}, DWords: []uint32{}, Bits: []bool{true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := recorder(t, tc.resp)
			got, err := c.RandomRead(ctx, tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if want := (recorded{"POST", "/random-read", "", tc.body}); *rec != want {
				t.Errorf("sent %+v; want %+v", *rec, want)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRandomReadCountMismatch(t *testing.T) {
	c, _ := recorder(t, `{"words":[1],"dwords":[],"bits":[]}`)
	_, err := c.RandomRead(context.Background(), RandomReadRequest{Words: []string{"D0", "D1"}})
	if !errors.Is(err, ErrBadResponse) {
		t.Errorf("err = %v, want ErrBadResponse", err)
	}
}

// null in any list must not become 0 / false.
func TestRandomReadNull(t *testing.T) {
	req := RandomReadRequest{Words: []string{"D0"}, DWords: []string{"D2"}, Bits: []string{"M0"}}
	for _, resp := range []string{
		`{"words":[null],"dwords":[1],"bits":[true]}`,
		`{"words":[1],"dwords":[null],"bits":[true]}`,
		`{"words":[1],"dwords":[1],"bits":[null]}`,
	} {
		c, _ := recorder(t, resp)
		if _, err := c.RandomRead(context.Background(), req); !errors.Is(err, ErrBadResponse) {
			t.Errorf("response %s: err = %v, want ErrBadResponse", resp, err)
		}
	}
}

// gomc-rest sends all three lists on every response ([] when empty), so a
// missing or null list is a bad response even if the request left it empty.
func TestRandomReadMissingList(t *testing.T) {
	req := RandomReadRequest{Bits: []string{"M0"}}
	for _, resp := range []string{
		`{"bits":[true]}`,
		`{"words":[],"bits":[true]}`,
		`{"words":null,"dwords":[],"bits":[true]}`,
		`{"words":[],"dwords":[],"bits":null}`,
	} {
		c, _ := recorder(t, resp)
		if _, err := c.RandomRead(context.Background(), req); !errors.Is(err, ErrBadResponse) {
			t.Errorf("response %s: err = %v, want ErrBadResponse", resp, err)
		}
	}
}

func TestRandomWrite(t *testing.T) {
	c, rec := recorder(t, `{"ok":true}`)
	err := c.RandomWrite(context.Background(), RandomWriteRequest{
		Words:  []WordItem{{"D0", 65535}},
		DWords: []DWordItem{{"D2", 4294967295}},
		Bits:   []BitItem{{"M0", true}, {"D10.15", false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := recorded{"POST", "/random-write", "",
		`{"words":[{"addr":"D0","value":65535}],"dwords":[{"addr":"D2","value":4294967295}],` +
			`"bits":[{"addr":"M0","value":true},{"addr":"D10.15","value":false}]}`}
	if *rec != want {
		t.Errorf("sent %+v\nwant %+v", *rec, want)
	}
}

func TestRandomEmpty(t *testing.T) {
	ctx := context.Background()
	c, rec := recorder(t, `{}`)
	if _, err := c.RandomRead(ctx, RandomReadRequest{Words: []string{}}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("RandomRead err = %v, want ErrBadRequest", err)
	}
	if err := c.RandomWrite(ctx, RandomWriteRequest{}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("RandomWrite err = %v, want ErrBadRequest", err)
	}
	if rec.method != "" {
		t.Error("empty request was sent")
	}
}
