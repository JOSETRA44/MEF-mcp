// Package render writes command output in the five ARSENAL-SPEC formats.
//
// Streaming formats (jsonl, csv) write each row as it arrives, so `--all`
// over an unbounded source runs in constant memory. Buffered formats (table,
// md, json) need every row first — table to size its columns, json to fill
// `count` before `rows`.
package render

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// Formats lists every supported format, in manifest order.
var Formats = []string{"table", "json", "jsonl", "csv", "md"}

// Valid reports whether f is a supported format.
func Valid(f string) bool {
	for _, x := range Formats {
		if x == f {
			return true
		}
	}
	return false
}

// Envelope is the success document from ARSENAL-SPEC §1.
type Envelope struct {
	OK        bool           `json:"ok"`
	Command   string         `json:"command"`
	Source    string         `json:"source"`
	FetchedAt string         `json:"fetched_at"`
	Cached    bool           `json:"cached"`
	Count     int            `json:"count"`
	Meta      map[string]any `json:"meta"`
	Data      any            `json:"data"`
	Rows      []core.Row     `json:"rows"`
}

// ErrorEnvelope is the failure document; it goes to stdout in json/jsonl mode.
type ErrorEnvelope struct {
	OK        bool        `json:"ok"`
	Command   string      `json:"command"`
	Source    string      `json:"source"`
	FetchedAt string      `json:"fetched_at"`
	Error     *core.Error `json:"error"`
}

// Now formats a timestamp the way every envelope does.
func Now(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Writer is a core.Sink bound to an output format.
type Writer struct {
	Format  string
	Out     io.Writer
	Columns []string // known up front for streaming csv; else derived
	Max     int      // row ceiling; -1 = unlimited
	Color   bool

	rows      []core.Row
	emitted   int
	dropped   int
	csvw      *csv.Writer
	csvHeader bool
}

// SetColumns implements core.Sink; ignored once a csv header is written.
func (w *Writer) SetColumns(cols []string) {
	if !w.csvHeader && len(cols) > 0 {
		w.Columns = cols
	}
}

// Truncated reports how many rows were discarded by the Max ceiling.
func (w *Writer) Truncated() int { return w.dropped }

// NewWriter returns a sink for format f.
func NewWriter(f string, out io.Writer, max int) *Writer {
	return &Writer{Format: f, Out: out, Max: max}
}

// Streaming reports whether rows are written as they arrive.
func (w *Writer) Streaming() bool { return w.Format == "jsonl" || w.Format == "csv" }

// Wanted implements core.Sink.
func (w *Writer) Wanted() int {
	if w.Max < 0 {
		return -1
	}
	if left := w.Max - w.emitted; left > 0 {
		return left
	}
	return 0
}

// Emitted is the number of rows accepted so far.
func (w *Writer) Emitted() int { return w.emitted }

// Rows returns the buffered rows (buffered formats only).
func (w *Writer) Rows() []core.Row { return w.rows }

// Emit implements core.Sink, silently dropping rows past Max.
func (w *Writer) Emit(rows ...core.Row) error {
	for _, r := range rows {
		if w.Max >= 0 && w.emitted >= w.Max {
			w.dropped++
			continue
		}
		w.emitted++
		switch w.Format {
		case "jsonl":
			if err := writeJSONLine(w.Out, r); err != nil {
				return err
			}
		case "csv":
			if err := w.writeCSVRow(r); err != nil {
				return err
			}
		default:
			w.rows = append(w.rows, r)
		}
	}
	return nil
}

func writeJSONLine(out io.Writer, r core.Row) error {
	b, err := marshal(r)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = out.Write(b)
	return err
}

// marshal encodes without HTML escaping, so "&" and "<" survive for jq users.
func marshal(v any) ([]byte, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(sb.String(), "\n")), nil
}

func (w *Writer) writeCSVRow(r core.Row) error {
	if w.csvw == nil {
		w.csvw = csv.NewWriter(w.Out)
	}
	if !w.csvHeader {
		if len(w.Columns) == 0 {
			w.Columns = Columns(nil, []core.Row{r})
		}
		if err := w.csvw.Write(w.Columns); err != nil {
			return err
		}
		w.csvHeader = true
	}
	rec := make([]string, len(w.Columns))
	for i, c := range w.Columns {
		rec[i] = Cell(r[c], false)
	}
	if err := w.csvw.Write(rec); err != nil {
		return err
	}
	w.csvw.Flush()
	return w.csvw.Error()
}

// Finish writes buffered output. For json it builds the envelope.
func (w *Writer) Finish(env Envelope, res *core.Result) error {
	cols := w.Columns
	if res != nil && len(res.Columns) > 0 {
		cols = res.Columns
	}
	switch w.Format {
	case "jsonl":
		return nil
	case "csv":
		if !w.csvHeader && len(cols) > 0 {
			// Empty result: still emit the header so downstream tools see a schema.
			cw := csv.NewWriter(w.Out)
			if err := cw.Write(cols); err != nil {
				return err
			}
			cw.Flush()
			return cw.Error()
		}
		return nil
	case "json":
		env.Rows = w.rows
		if env.Rows == nil {
			env.Rows = []core.Row{}
		}
		env.Count = len(env.Rows)
		if env.Meta == nil {
			env.Meta = map[string]any{}
		}
		enc := json.NewEncoder(w.Out)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	case "md":
		return Markdown(w.Out, Columns(cols, w.rows), w.rows)
	default:
		return Table(w.Out, Columns(cols, w.rows), w.rows, w.Color)
	}
}

// Columns returns preferred columns, extended with any extra keys the rows
// carry (sorted, so output is deterministic across runs).
func Columns(preferred []string, rows []core.Row) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range preferred {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	var extra []string
	for _, r := range rows {
		for k := range r {
			if !seen[k] {
				seen[k] = true
				extra = append(extra, k)
			}
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// Cell formats one value. human=true adds thousands separators for display.
func Cell(v any, human bool) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		if human {
			return group(strconv.Itoa(t))
		}
		return strconv.Itoa(t)
	case int64:
		if human {
			return group(strconv.FormatInt(t, 10))
		}
		return strconv.FormatInt(t, 10)
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return ""
		}
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			s := strconv.FormatInt(int64(t), 10)
			if human {
				return group(s)
			}
			return s
		}
		s := strconv.FormatFloat(t, 'f', 2, 64)
		if human {
			whole, frac, _ := strings.Cut(s, ".")
			return group(whole) + "." + frac
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []string:
		return strings.Join(t, ", ")
	default:
		b, err := marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

func group(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var sb strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		sb.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(s[i : i+3])
	}
	if neg {
		return "-" + sb.String()
	}
	return sb.String()
}

const maxCell = 60

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxCell {
		return s
	}
	r := []rune(s)
	return string(r[:maxCell-1]) + "…"
}

func isNumeric(v any) bool {
	switch v.(type) {
	case int, int64, float64:
		return true
	}
	return false
}

// Table renders an aligned plain-text table for humans.
func Table(out io.Writer, cols []string, rows []core.Row, color bool) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(out, "(no rows)")
		return err
	}
	width := make([]int, len(cols))
	cells := make([][]string, len(rows))
	for i, c := range cols {
		width[i] = utf8.RuneCountInString(c)
	}
	for ri, r := range rows {
		cells[ri] = make([]string, len(cols))
		for ci, c := range cols {
			s := clip(Cell(r[c], true))
			cells[ri][ci] = s
			if n := utf8.RuneCountInString(s); n > width[ci] {
				width[ci] = n
			}
		}
	}
	var sb strings.Builder
	pad := func(s string, w int, right bool) string {
		gap := strings.Repeat(" ", w-utf8.RuneCountInString(s))
		if right {
			return gap + s
		}
		return s + gap
	}
	for i, c := range cols {
		if i > 0 {
			sb.WriteString("  ")
		}
		h := pad(c, width[i], false)
		if color {
			h = "\x1b[1m" + h + "\x1b[0m"
		}
		sb.WriteString(h)
	}
	sb.WriteByte('\n')
	for i := range cols {
		if i > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(strings.Repeat("-", width[i]))
	}
	sb.WriteByte('\n')
	for ri, r := range rows {
		for ci, c := range cols {
			if ci > 0 {
				sb.WriteString("  ")
			}
			sb.WriteString(pad(cells[ri][ci], width[ci], isNumeric(r[c])))
		}
		sb.WriteByte('\n')
	}
	_, err := io.WriteString(out, strings.TrimRight(sb.String(), " ")+"")
	return err
}

// Markdown renders a GitHub-flavoured table; pipes and newlines are escaped.
func Markdown(out io.Writer, cols []string, rows []core.Row) error {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, "|", `\|`)
		return strings.Join(strings.Fields(s), " ")
	}
	var sb strings.Builder
	if len(cols) == 0 {
		_, err := io.WriteString(out, "_no rows_\n")
		return err
	}
	sb.WriteString("|")
	for _, c := range cols {
		sb.WriteString(" " + esc(c) + " |")
	}
	sb.WriteString("\n|")
	for _, c := range cols {
		_ = c
		sb.WriteString(" --- |")
	}
	sb.WriteByte('\n')
	for _, r := range rows {
		sb.WriteString("|")
		for _, c := range cols {
			sb.WriteString(" " + esc(Cell(r[c], false)) + " |")
		}
		sb.WriteByte('\n')
	}
	_, err := io.WriteString(out, sb.String())
	return err
}

// WriteError prints the error envelope (json/jsonl) to out.
func WriteError(out io.Writer, env ErrorEnvelope) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(env)
}
