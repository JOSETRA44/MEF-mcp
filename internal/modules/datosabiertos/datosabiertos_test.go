package datosabiertos

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/testutil"
)

const rid = "5f3b3cbe-3955-41cc-8662-1757ebb5cf53"

func fakeModule() *Module {
	return &Module{EP: Endpoints{Search: "https://fake/search", Detail: "https://fake/detail", Datastore: "https://fake/datastore", Files: "https://files/"}}
}

func TestDatastoreRecordsSortedColumns(t *testing.T) {
	pg, err := decodeDatastore(testutil.Fixture(t, "datastore_3.json"), rid, false, "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Rows) != 3 || pg.Null {
		t.Fatalf("rows=%d null=%v", len(pg.Rows), pg.Null)
	}
	if !sort.StringsAreSorted(pg.Columns) || len(pg.Columns) < 50 {
		t.Fatalf("columns not sorted/complete: %v", pg.Columns[:5])
	}
	if _, ok := pg.Rows[0]["SECTOR"].(string); !ok {
		t.Fatal("values must stay text so codes keep leading zeros")
	}
}

func TestDatastore409ListsValidColumns(t *testing.T) {
	_, err := decodeDatastore(testutil.Fixture(t, "datastore_409.json"), rid, false, "u")
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeUsage {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(e.Hint, "SECTOR_NOMBRE") || !strings.HasPrefix(e.Hint, "Valid columns:") {
		t.Fatalf("hint %q", e.Hint)
	}
}

func TestDatastoreNullRecords(t *testing.T) {
	pg, err := decodeDatastore(testutil.Fixture(t, "datastore_null.json"), rid, false, "u")
	if err != nil || !pg.Null {
		t.Fatalf("pg=%+v err=%v", pg, err)
	}
}

func TestConsultarUnloadedResourceIsNotFound(t *testing.T) {
	m := fakeModule()
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "datastore", Reply: testutil.Static(testutil.Fixture(t, "datastore_null.json"))}}}
	cmd := m.consultar()
	_, err := cmd.Run(context.Background(), testutil.Env(t, fake), testutil.Input(t, cmd, map[string]any{"resource_id": rid}), &testutil.Sink{})
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeNotFound || !strings.Contains(e.Hint, "download") {
		t.Fatalf("got %v", err)
	}
}

func TestConsultar409BodyWinsOverStatus(t *testing.T) {
	m := fakeModule()
	fake := &testutil.Fake{Routes: []testutil.Route{{Match: "datastore", Status: 409, Reply: testutil.Static(testutil.Fixture(t, "datastore_409.json"))}}}
	cmd := m.consultar()
	_, err := cmd.Run(context.Background(), testutil.Env(t, fake), testutil.Input(t, cmd, map[string]any{"resource_id": rid, "filter": []string{"NOEXISTE=1"}}), &testutil.Sink{})
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeUsage {
		t.Fatalf("got %v", err)
	}
}

func TestConsultarRejectsNonUUID(t *testing.T) {
	cmd := fakeModule().consultar()
	_, err := cmd.Run(context.Background(), testutil.Env(t, &testutil.Fake{}), testutil.Input(t, cmd, map[string]any{"resource_id": "canon"}), &testutil.Sink{})
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeUsage {
		t.Fatalf("got %v", err)
	}
}

func TestBuscarAndDataset(t *testing.T) {
	m := fakeModule()
	fake := &testutil.Fake{Routes: []testutil.Route{
		{Match: "search", Reply: testutil.Static(testutil.Fixture(t, "search_deuda.json"))},
		{Match: "detail", Reply: testutil.Static(testutil.Fixture(t, "detail_comparacion.json"))},
	}}
	cmd := m.buscar()
	sink := &testutil.Sink{}
	if _, err := cmd.Run(context.Background(), testutil.Env(t, fake), testutil.Input(t, cmd, map[string]any{"text": "deuda"}), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.Rows) == 0 || !strings.Contains(sink.Rows[0]["dataset"].(string), "deuda") {
		t.Fatalf("rows %v", sink.Rows)
	}
	ds := m.dataset()
	sink = &testutil.Sink{}
	res, err := ds.Run(context.Background(), testutil.Env(t, fake), testutil.Input(t, ds, map[string]any{"slug": "comparacion-de-presupuesto-ejecucion-gasto"}), sink)
	if err != nil || len(sink.Rows) != 4 {
		t.Fatalf("rows=%d err=%v", len(sink.Rows), err)
	}
	if res.Data.(map[string]any)["categoria"] != "presupuesto-publico" || sink.Rows[0]["resource_id"] != rid {
		t.Fatalf("data %v row %v", res.Data, sink.Rows[0])
	}
}

func TestDownloadRefusesForeignHosts(t *testing.T) {
	cmd := fakeModule().descargar()
	_, err := cmd.Run(context.Background(), testutil.Env(t, &testutil.Fake{}), testutil.Input(t, cmd, map[string]any{"url": "https://evil.example/x.csv"}), &testutil.Sink{})
	var e *core.Error
	if !errors.As(err, &e) || e.Code != core.CodeUsage {
		t.Fatalf("got %v", err)
	}
}
