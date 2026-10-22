package gomcrest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// Format selects how /read and /write interpret word values.
type Format struct {
	DWord bool // 32-bit values from two consecutive words (word devices only)
	SInt  bool // signed values (word devices and K notation only)
}

// Values holds the result of Read or the input of Write.
// Exactly one of Ints and Bools is non-empty.
type Values struct {
	Ints  []int64 // word, double word or K notation values
	Bools []bool  // bit device or word bit access (e.g. D100.1) values
}

func (f Format) query(q url.Values) url.Values {
	q.Set("dword", strconv.FormatBool(f.DWord))
	q.Set("sint", strconv.FormatBool(f.SInt))
	return q
}

// read sends GET /read and decodes the values into a []T.
func read[T any](ctx context.Context, c *Client, addr string, count int, f Format) ([]T, error) {
	q := f.query(url.Values{"addr": {addr}, "count": {strconv.Itoa(count)}})
	v, err := get[struct{ Values []*T }](ctx, c, "/read", q)
	if err != nil {
		return nil, typeHint[T](err, addr)
	}
	if len(v.Values) != count {
		return nil, fmt.Errorf("%w: /read %s: got %d values, want %d", ErrBadResponse, addr, len(v.Values), count)
	}
	vals, ok := derefAll(v.Values)
	if !ok {
		return nil, fmt.Errorf("%w: /read %s: null value", ErrBadResponse, addr)
	}
	return vals, nil
}

// typeHint names the read to use when err shows that the server sent
// booleans for a numeric read or numbers for a bit read. Other errors, such as
// invalid JSON, a value out of range or a scalar instead of the values list,
// are returned as is: a hint there would send the user to check an address
// that is correct.
func typeHint[T any](err error, addr string) error {
	var te *json.UnmarshalTypeError
	// te.Type is T only for a list element; {"values":1} reports the slice type.
	if !errors.As(err, &te) || te.Type != reflect.TypeFor[T]() {
		return err
	}
	var zero T
	switch any(zero).(type) {
	case bool:
		if strings.HasPrefix(te.Value, "number") {
			return fmt.Errorf("%w (hint: %s may be a word device; use ReadWords or another numeric read)", err, addr)
		}
	case uint16, int16, uint32, int32:
		if te.Value == "bool" {
			return fmt.Errorf("%w (hint: %s may be a bit device or bit access such as M0 or D100.1; use ReadBits)", err, addr)
		}
	}
	return err
}

// derefAll turns []*T into []T, reporting false if any element is nil.
// Values are decoded as pointers because json.Unmarshal turns null into 0 or
// false for non-pointer elements, which would look like a real reading.
func derefAll[T any](ps []*T) ([]T, bool) {
	vs := make([]T, len(ps))
	for i, p := range ps {
		if p == nil {
			return nil, false
		}
		vs[i] = *p
	}
	return vs, true
}

// write sends POST /write with {"values": values}.
func (c *Client) write(ctx context.Context, addr string, values any, f Format) error {
	q := f.query(url.Values{"addr": {addr}})
	return c.post(ctx, "/write", q, map[string]any{"values": values})
}

// ReadWords reads count 16-bit words starting at addr (e.g. "D100").
func (c *Client) ReadWords(ctx context.Context, addr string, count int) ([]uint16, error) {
	return read[uint16](ctx, c, addr, count, Format{})
}

// ReadInt16s reads count words as signed 16-bit values.
func (c *Client) ReadInt16s(ctx context.Context, addr string, count int) ([]int16, error) {
	return read[int16](ctx, c, addr, count, Format{SInt: true})
}

// ReadDWords reads count 32-bit values, each from two consecutive words.
func (c *Client) ReadDWords(ctx context.Context, addr string, count int) ([]uint32, error) {
	return read[uint32](ctx, c, addr, count, Format{DWord: true})
}

// ReadInt32s reads count signed 32-bit values, each from two consecutive words.
func (c *Client) ReadInt32s(ctx context.Context, addr string, count int) ([]int32, error) {
	return read[int32](ctx, c, addr, count, Format{DWord: true, SInt: true})
}

// ReadBits reads count bits from a bit device (e.g. "M0") or from bits of a
// word device (e.g. "D100.1").
func (c *Client) ReadBits(ctx context.Context, addr string, count int) ([]bool, error) {
	return read[bool](ctx, c, addr, count, Format{})
}

// WriteWords writes 16-bit words starting at addr.
func (c *Client) WriteWords(ctx context.Context, addr string, values []uint16) error {
	return c.write(ctx, addr, values, Format{})
}

// WriteInt16s writes signed 16-bit values starting at addr.
func (c *Client) WriteInt16s(ctx context.Context, addr string, values []int16) error {
	return c.write(ctx, addr, values, Format{SInt: true})
}

// WriteDWords writes 32-bit values, each to two consecutive words.
func (c *Client) WriteDWords(ctx context.Context, addr string, values []uint32) error {
	return c.write(ctx, addr, values, Format{DWord: true})
}

// WriteInt32s writes signed 32-bit values, each to two consecutive words.
func (c *Client) WriteInt32s(ctx context.Context, addr string, values []int32) error {
	return c.write(ctx, addr, values, Format{DWord: true, SInt: true})
}

// WriteBits writes bits to a bit device (e.g. "M0") or to bits of a word
// device (e.g. "D100.1").
func (c *Client) WriteBits(ctx context.Context, addr string, values []bool) error {
	return c.write(ctx, addr, values, Format{})
}

// Read reads count values when the device type is not known in advance, for
// example from user input. Bit results go to Values.Bools, everything else to
// Values.Ints.
func (c *Client) Read(ctx context.Context, addr string, count int, f Format) (Values, error) {
	raw, err := read[any](ctx, c, addr, count, f)
	if err != nil {
		return Values{}, err
	}
	var v Values
	for _, x := range raw {
		switch x := x.(type) {
		case bool:
			v.Bools = append(v.Bools, x)
		case float64: // exact for every 32-bit value
			if n := int64(x); float64(n) == x {
				v.Ints = append(v.Ints, n)
				continue
			}
			return Values{}, fmt.Errorf("%w: /read %s: non-integer value %v", ErrBadResponse, addr, x)
		default:
			return Values{}, fmt.Errorf("%w: /read %s: unexpected value %v", ErrBadResponse, addr, x)
		}
	}
	if len(v.Ints) > 0 && len(v.Bools) > 0 {
		return Values{}, fmt.Errorf("%w: /read %s: mixed numbers and booleans", ErrBadResponse, addr)
	}
	return v, nil
}

// Write writes v when the device type is not known in advance. Exactly one of
// v.Ints and v.Bools must be non-empty; otherwise it returns ErrBadRequest
// without sending anything.
func (c *Client) Write(ctx context.Context, addr string, v Values, f Format) error {
	switch {
	case len(v.Ints) > 0 && len(v.Bools) == 0:
		return c.write(ctx, addr, v.Ints, f)
	case len(v.Bools) > 0 && len(v.Ints) == 0:
		return c.write(ctx, addr, v.Bools, f)
	}
	return fmt.Errorf("%w: Write needs exactly one of Values.Ints and Values.Bools", ErrBadRequest)
}
