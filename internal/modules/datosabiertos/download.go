package datosabiertos

import (
	"context"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

func (m *Module) runDescargar(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
	raw := in.Str("url")
	if err := m.allowedDownload(raw); err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, core.Usagef("", "invalid url %q", raw)
	}
	dest := in.Str("dest")
	if dest == "" {
		dest = path.Base(u.Path)
	}
	body, err := env.HTTP.Stream(ctx, core.Request{Method: "GET", URL: raw})
	if err != nil {
		return nil, err
	}
	defer body.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil && filepath.Dir(dest) != "." {
		return nil, core.Wrap(core.CodeUsage, err, "", "cannot create %s", filepath.Dir(dest))
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, core.Wrap(core.CodeUsage, err, "Choose a writable destination with --dest.", "cannot write %s", tmp)
	}
	pr := &progressReader{r: body, total: body.Size(), report: env.Progress, last: time.Now()}
	n, err := io.Copy(f, pr)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return nil, core.AsError(err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "", "cannot finalise %s", dest)
	}
	abs, _ := filepath.Abs(dest)
	if err := sink.Emit(core.Row{"archivo": abs, "bytes": n, "url": raw}); err != nil {
		return nil, err
	}
	return &core.Result{Columns: []string{"archivo", "bytes", "url"}}, nil
}

type progressReader struct {
	r      io.Reader
	total  int64
	done   int64
	report func(string, ...any)
	last   time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.report != nil && time.Since(p.last) > time.Second {
		p.last = time.Now()
		if p.total > 0 {
			p.report("downloaded %.1f / %.1f MB (%.0f%%)", float64(p.done)/1e6, float64(p.total)/1e6, float64(p.done)*100/float64(p.total))
		} else {
			p.report("downloaded %.1f MB", float64(p.done)/1e6)
		}
	}
	return n, err
}
