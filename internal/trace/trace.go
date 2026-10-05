// Package trace prints what each stage is doing, so you can watch the
// request → model → tool → result flow instead of guessing at it.
package trace

import (
	"encoding/json"
	"fmt"
	"io"
)

// Tracer writes labelled steps to w. A nil *Tracer is valid and does nothing,
// so callers never need to check whether tracing is on.
type Tracer struct {
	w io.Writer
}

func New(w io.Writer) *Tracer {
	return &Tracer{w: w}
}

// Step prints name and v. Strings and []byte print as-is; anything else is
// rendered as indented JSON.
func (t *Tracer) Step(name string, v any) {
	if t == nil {
		return
	}
	var body string
	switch v := v.(type) {
	case string:
		body = v
	case []byte:
		body = string(v)
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			body = fmt.Sprintf("%+v", v)
		} else {
			body = string(b)
		}
	}
	fmt.Fprintf(t.w, "\033[2m── %s\n%s\033[0m\n", name, body)
}
