package gomcrest

import (
	"context"
	"testing"
)

func TestK(t *testing.T) {
	ctx := context.Background()

	c, rec := recorder(t, `{"values":[65535]}`)
	if v, err := c.ReadK(ctx, "K4M100"); err != nil || v != 65535 {
		t.Errorf("ReadK = %d, %v; want 65535", v, err)
	}
	if want := (recorded{"GET", "/read", "addr=K4M100&count=1&dword=false&sint=false", ""}); *rec != want {
		t.Errorf("ReadK sent %+v; want %+v", *rec, want)
	}

	c, rec = recorder(t, `{"values":[-1]}`)
	if v, err := c.ReadKInt(ctx, "K4M100"); err != nil || v != -1 {
		t.Errorf("ReadKInt = %d, %v; want -1", v, err)
	}
	if want := (recorded{"GET", "/read", "addr=K4M100&count=1&dword=false&sint=true", ""}); *rec != want {
		t.Errorf("ReadKInt sent %+v; want %+v", *rec, want)
	}

	c, rec = recorder(t, `{"ok":true}`)
	if err := c.WriteK(ctx, "K8X0", 4294967295); err != nil {
		t.Fatal(err)
	}
	if want := (recorded{"POST", "/write", "addr=K8X0&dword=false&sint=false", `{"values":[4294967295]}`}); *rec != want {
		t.Errorf("WriteK sent %+v; want %+v", *rec, want)
	}

	if err := c.WriteKInt(ctx, "K1M0", -8); err != nil {
		t.Fatal(err)
	}
	if want := (recorded{"POST", "/write", "addr=K1M0&dword=false&sint=true", `{"values":[-8]}`}); *rec != want {
		t.Errorf("WriteKInt sent %+v; want %+v", *rec, want)
	}
}
