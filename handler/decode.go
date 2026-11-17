package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	// Reject trailing junk after the first JSON value.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("trailing data")
	}
	return nil
}
