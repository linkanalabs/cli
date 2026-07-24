package output

import (
	"fmt"
	"io"
)

// renderIDs prints the "id" of each record, one per line — the cheapest shape
// for feeding a list into a follow-up command. A response with no records
// prints nothing; a response that carries records but no usable "id" is a
// usage error instead of silence, so an agent never reads an empty stdout as
// an empty list. That case is real today: the SRM e-mail templates are keyed
// by "template", not by "id".
func renderIDs(w io.Writer, data any) error {
	node, ok := jsonShape(data)
	if !ok {
		return renderJSON(w, data)
	}

	records, written := 0, 0
	switch v := node.(type) {
	case *orderedObject:
		records = 1
		n, err := writeID(w, v)
		if err != nil {
			return err
		}
		written += n
	case []any:
		records = len(v)
		for _, item := range v {
			o, isObject := item.(*orderedObject)
			if !isObject {
				continue
			}
			n, err := writeID(w, o)
			if err != nil {
				return err
			}
			written += n
		}
	}

	if records > 0 && written == 0 {
		return fmt.Errorf(`--format ids: the response carries %d record(s) but no usable "id" field; use --format json`, records)
	}
	return nil
}

// writeID prints a record's "id" and reports how many lines it wrote. A
// missing, null, empty or non-scalar id is skipped rather than emitted as a
// blank line, which would otherwise feed an empty argument into the next
// command of a pipeline.
func writeID(w io.Writer, o *orderedObject) (int, error) {
	id, ok := o.vals["id"]
	if !ok {
		return 0, nil
	}
	switch id.(type) {
	case *orderedObject, []any:
		return 0, nil
	}
	text := scalarText(id)
	if text == "" {
		return 0, nil
	}
	if _, err := fmt.Fprintln(w, text); err != nil {
		return 0, err
	}
	return 1, nil
}

// renderCount prints how many records the response carries: the length of an
// array, 0 for null, 1 for a single record. It counts what came back, not
// what exists — a paginated endpoint reports its page.
func renderCount(w io.Writer, data any) error {
	node, ok := jsonShape(data)
	if !ok {
		return renderJSON(w, data)
	}

	count := 1
	switch v := node.(type) {
	case nil:
		count = 0
	case []any:
		count = len(v)
	}
	_, err := fmt.Fprintln(w, count)
	return err
}
