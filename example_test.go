package gomcrest_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/moge800/gomcrest"
)

// These examples need a running gomc-rest, so they have no Output and are
// only compiled.

func Example() {
	c, err := gomcrest.New("http://192.168.0.10:8080", gomcrest.WithToken("s3cret"))
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	words, err := c.ReadWords(ctx, "D100", 3)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(words)

	if err := c.WriteBits(ctx, "M0", []bool{true, false}); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_RandomRead() {
	c, err := gomcrest.New("http://192.168.0.10:8080")
	if err != nil {
		log.Fatal(err)
	}
	res, err := c.RandomRead(context.Background(), gomcrest.RandomReadRequest{
		Words:  []string{"D100", "D200"},
		DWords: []string{"D300"},
		Bits:   []string{"D100.1", "M0"},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Words, res.DWords, res.Bits)
}

// Read and Write suit tools where the device comes from user input.
func ExampleClient_Read() {
	c, err := gomcrest.New("http://192.168.0.10:8080")
	if err != nil {
		log.Fatal(err)
	}
	v, err := c.Read(context.Background(), "M0", 4, gomcrest.Format{})
	if err != nil {
		log.Fatal(err)
	}
	if v.Bools != nil {
		fmt.Println("bits:", v.Bools)
	} else {
		fmt.Println("values:", v.Ints)
	}
}

func ExampleAPIError() {
	c, err := gomcrest.New("http://192.168.0.10:8080")
	if err != nil {
		log.Fatal(err)
	}
	err = c.WriteWords(context.Background(), "D100", []uint16{1})

	var apiErr *gomcrest.APIError
	switch {
	case err == nil:
	case errors.Is(err, gomcrest.ErrBusy):
		// The PLC did not run the request: safe to retry after about 1 second.
	case errors.Is(err, gomcrest.ErrTransport), errors.Is(err, gomcrest.ErrRequestTimeout),
		errors.Is(err, gomcrest.ErrBadResponse),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The write may have happened: read the value back before retrying.
	case errors.As(err, &apiErr) && apiErr.EndCode != "":
		fmt.Println("PLC end code:", apiErr.EndCode)
	default:
		log.Fatal(err)
	}
}
