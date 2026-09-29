package importer

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"io"
)

// newCSVReader tolerates what spreadsheet/form tools emit: a leading UTF-8 BOM
// (which otherwise makes a quoted first header a "bare quote" error), unescaped
// quotes inside long question columns, and ragged rows.
func newCSVReader(r io.Reader) *csv.Reader {
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && bytes.Equal(b, []byte{0xEF, 0xBB, 0xBF}) {
		br.Discard(3)
	}
	cr := csv.NewReader(br)
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	return cr
}
