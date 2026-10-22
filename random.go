package gomcrest

import (
	"context"
	"fmt"
	"net/http"
)

// RandomReadRequest lists non-contiguous addresses to read in one request.
// The server checks the limits: each list holds up to 255 addresses, Words and
// the bits of words in Bits (e.g. "D100.1") count together toward 255, and Bits
// may hold at most 16 bit devices (e.g. "M0").
type RandomReadRequest struct {
	Words  []string `json:"words,omitempty"`  // word devices, e.g. "D100"
	DWords []string `json:"dwords,omitempty"` // 32-bit values from word devices
	Bits   []string `json:"bits,omitempty"`   // bit devices (e.g. "M0") or bits of words (e.g. "D100.1")
}

// RandomReadResult holds the values in the order of the request.
type RandomReadResult struct {
	Words  []uint16 `json:"words"`
	DWords []uint32 `json:"dwords"`
	Bits   []bool   `json:"bits"`
}

// RandomRead reads all addresses in req in one serialized job on the server.
// Words, DWords and bits of words are read together by MC random read, but
// each bit device in Bits costs a separate PLC read: read contiguous bits such
// as M0-M15 with ReadBits instead. Since the job can issue several PLC reads,
// the values are not guaranteed to come from one PLC scan; if they must be
// consistent, have the PLC program copy them into one block and read that.
// An empty req returns ErrBadRequest without sending anything. A response
// without all three lists is ErrBadResponse, even for lists req left empty.
func (c *Client) RandomRead(ctx context.Context, req RandomReadRequest) (RandomReadResult, error) {
	if len(req.Words)+len(req.DWords)+len(req.Bits) == 0 {
		return RandomReadResult{}, fmt.Errorf("%w: RandomRead needs at least one address", ErrBadRequest)
	}
	// A pointer to each list tells a missing or null list apart from [], which
	// gomc-rest sends for an empty one. Pointer elements tell null apart from
	// 0 / false; see derefAll.
	var raw struct {
		Words  *[]*uint16 `json:"words"`
		DWords *[]*uint32 `json:"dwords"`
		Bits   *[]*bool   `json:"bits"`
	}
	if err := c.do(ctx, http.MethodPost, "/random-read", nil, req, &raw); err != nil {
		return RandomReadResult{}, err
	}
	if raw.Words == nil || raw.DWords == nil || raw.Bits == nil {
		return RandomReadResult{}, fmt.Errorf("%w: /random-read: words, dwords or bits missing or null", ErrBadResponse)
	}
	rw, rd, rb := *raw.Words, *raw.DWords, *raw.Bits
	if len(rw) != len(req.Words) || len(rd) != len(req.DWords) || len(rb) != len(req.Bits) {
		return RandomReadResult{}, fmt.Errorf("%w: /random-read: got %d/%d/%d words/dwords/bits, want %d/%d/%d",
			ErrBadResponse, len(rw), len(rd), len(rb), len(req.Words), len(req.DWords), len(req.Bits))
	}
	words, ok1 := derefAll(rw)
	dwords, ok2 := derefAll(rd)
	bits, ok3 := derefAll(rb)
	if !ok1 || !ok2 || !ok3 {
		return RandomReadResult{}, fmt.Errorf("%w: /random-read: null value", ErrBadResponse)
	}
	return RandomReadResult{Words: words, DWords: dwords, Bits: bits}, nil
}

// WordItem is one 16-bit value for RandomWrite.
type WordItem struct {
	Addr  string `json:"addr"`
	Value uint16 `json:"value"`
}

// DWordItem is one 32-bit value for RandomWrite.
type DWordItem struct {
	Addr  string `json:"addr"`
	Value uint32 `json:"value"`
}

// BitItem is one bit for RandomWrite.
type BitItem struct {
	Addr  string `json:"addr"`
	Value bool   `json:"value"`
}

// RandomWriteRequest lists non-contiguous values to write in one request.
// Each list holds up to 255 items. Words and DWords need word devices.
type RandomWriteRequest struct {
	Words  []WordItem  `json:"words,omitempty"`
	DWords []DWordItem `json:"dwords,omitempty"`
	Bits   []BitItem   `json:"bits,omitempty"` // bit devices or word bit access (e.g. "D100.1")
}

// RandomWrite writes all items in req in one serialized job on the server.
// It is not atomic: the server writes Words and DWords, then bit devices, then
// bits of words (read-modify-write per word) as separate PLC operations, so an
// error can leave the earlier ones written. Read the addresses back before
// resending. An empty req returns ErrBadRequest without sending anything.
func (c *Client) RandomWrite(ctx context.Context, req RandomWriteRequest) error {
	if len(req.Words)+len(req.DWords)+len(req.Bits) == 0 {
		return fmt.Errorf("%w: RandomWrite needs at least one item", ErrBadRequest)
	}
	return c.post(ctx, "/random-write", nil, req)
}
