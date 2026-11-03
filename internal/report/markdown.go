package report

import (
	"fmt"
	"os"
	"strings"

	"github.com/JOSETRA44/mef-mcp/internal/render"
)

// writeMarkdown produces a memo an agent can paste into a reply or a repo.
func writeMarkdown(path string, d *Doc) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", d.Title)
	if d.Subtitle != "" {
		fmt.Fprintf(&sb, "_%s_\n\n", d.Subtitle)
	}
	if d.Analysis != nil && d.Analysis.Summary != "" {
		fmt.Fprintf(&sb, "%s\n\n", d.Analysis.Summary)
	}
	if d.Analysis != nil && len(d.Analysis.Findings) > 0 {
		sb.WriteString("## Hallazgos\n\n")
		for _, f := range d.Analysis.Findings {
			fmt.Fprintf(&sb, "- **%s.** %s\n", f.Title, f.Detail)
		}
		sb.WriteString("\n")
	}
	if len(d.Rows) > 0 {
		sb.WriteString("## Datos\n\n")
		cols := d.Columns
		headers := make([]string, len(cols))
		for i, c := range cols {
			headers[i] = d.Header(c)
		}
		// render.Markdown escapes pipes and collapses whitespace already.
		rows := d.Rows
		if d.Totals != nil {
			rows = append(append([]map[string]any(nil), rows...), d.Totals)
		}
		table := &strings.Builder{}
		if err := render.Markdown(table, cols, rows); err != nil {
			return err
		}
		body := table.String()
		if i := strings.IndexByte(body, '\n'); i > 0 {
			// Replace the machine header row with human labels.
			body = "| " + strings.Join(headers, " | ") + " |" + body[i:]
		}
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	if len(d.Notes) > 0 {
		sb.WriteString("## Notas\n\n")
		for _, n := range d.Notes {
			fmt.Fprintf(&sb, "- %s\n", n)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("---\n\n")
	for _, p := range d.provenance() {
		fmt.Fprintf(&sb, "%s  \n", p)
	}
	if d.SourceURL != "" {
		fmt.Fprintf(&sb, "\n<%s>\n", d.SourceURL)
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
