package gomcrest

import "context"

// ReadK reads a K notation address (e.g. "K4M100") as an unsigned value.
func (c *Client) ReadK(ctx context.Context, addr string) (uint32, error) {
	v, err := read[uint32](ctx, c, addr, 1, Format{})
	if err != nil {
		return 0, err
	}
	return v[0], nil
}

// ReadKInt reads a K notation address as a signed value, sign-extended from
// its width (K4 → 16 bits).
func (c *Client) ReadKInt(ctx context.Context, addr string) (int32, error) {
	v, err := read[int32](ctx, c, addr, 1, Format{SInt: true})
	if err != nil {
		return 0, err
	}
	return v[0], nil
}

// WriteK writes an unsigned value to a K notation address.
func (c *Client) WriteK(ctx context.Context, addr string, v uint32) error {
	return c.write(ctx, addr, []uint32{v}, Format{})
}

// WriteKInt writes a signed value to a K notation address.
func (c *Client) WriteKInt(ctx context.Context, addr string, v int32) error {
	return c.write(ctx, addr, []int32{v}, Format{SInt: true})
}
