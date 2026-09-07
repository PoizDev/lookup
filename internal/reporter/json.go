package reporter

import (
	"encoding/json"
	"io"
)

type JSON struct{ writer io.Writer }

func NewJSON(writer io.Writer) *JSON { return &JSON{writer: writer} }

func (r *JSON) Report(result *Result) error {
	encoder := json.NewEncoder(r.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
