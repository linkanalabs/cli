package output

import (
	"fmt"
	"io"
)

// renderIDs prints the "id" of each record, one per line — the cheapest shape
// for feeding a list into a follow-up command. Records without an "id", and
// shapes that carry none (scalars, empty arrays), print nothing.
func renderIDs(w io.Writer, data any) error {
	node, ok := jsonShape(data)
	if !ok {
		return renderJSON(w, data)
	}

	switch v := node.(type) {
	case *orderedObject:
		return writeID(w, v)
	case []any:
		for _, item := range v {
			o, isObject := item.(*orderedObject)
			if !isObject {
				continue
			}
			if err := writeID(w, o); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeID(w io.Writer, o *orderedObject) error {
	id, ok := o.vals["id"]
	if !ok {
		return nil
	}
	_, err := fmt.Fprintln(w, cellText(id))
	return err
}

// renderCount prints how many records the response carries: the length of an
// array, 0 for null, 1 for a single record.
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
