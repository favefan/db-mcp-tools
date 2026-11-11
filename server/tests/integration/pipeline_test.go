// Package integration runs only against a disposable container explicitly
// provisioned for this test invocation. Never point it at a business database.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dbmcp.local/server/internal/api"
	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestContainerPipeline(t *testing.T) {
	if os.Getenv("DBMCP_TEST_CONTAINER") != "1" {
		t.Skip("requires a newly created disposable database container")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	driver := os.Getenv("DBMCP_TEST_DRIVER")
	if driver == "" {
		driver = "postgres"
	}
	port, _ := strconv.Atoi(os.Getenv("DBMCP_TEST_PORT"))
	if port == 0 {
		port = 25432
	}
	i, db, table, parameter := fixture(t, ctx, driver, port)
	defer db.Close()
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	i, err = s.SaveInstance(i)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.SaveEntity(model.Entity{Name: "container agent", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c, token, err := s.Issue(e.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := s.SaveBinding(model.Binding{EntityID: e.ID, InstanceID: i.ID, Policy: model.DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := s.Authenticate(token, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	i = multipleDatabases(t, ctx, a, s, i, db, identity, token, table)
	objectScopes(t, ctx, a, s, i, db, identity, table)
	i, err = s.Instance(i.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	binding, err = s.Binding(e.ID, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	query := func(sql, id, tool string, params ...any) model.Query {
		return model.Query{InstanceID: i.ID, SQL: sql, ClientRequestID: id, Tool: tool, Params: params}
	}
	cols, err := a.Catalog(ctx, identity, i.ID)
	visible := 0
	for _, col := range cols {
		if strings.EqualFold(col.Schema+"."+col.Table, table) {
			visible++
		}
	}
	if err != nil || visible != 4 {
		t.Fatalf("catalog %d: %v", len(cols), err)
	}
	res, err := a.Query(ctx, identity, query("SELECT email AS e,password FROM "+table+" ORDER BY id", "read", "db_query"))
	if err != nil || res.Status != "succeeded" {
		t.Fatalf("query %#v: %v", res, err)
	}
	if res.Rows[0][0] != "[EMAIL]" || res.Rows[0][1] != "[REDACTED]" {
		t.Fatalf("mask bypass: %#v", res.Rows)
	}
	audits, err := s.Audits(e.ID, i.ID, "succeeded", 0)
	if err != nil || len(audits) == 0 {
		t.Fatal("missing successful audit", err)
	}
	detail, err := s.AuditDetail(audits[0].ID)
	if err != nil || detail.Request == nil || detail.Request.SQL == "" || detail.Request.Result == nil || detail.Request.Result.RowCount != len(res.Rows) || len(detail.Request.Result.Masks) != 2 {
		t.Fatalf("request detail: %#v %v", detail, err)
	}
	active, err := s.Request(res.RequestID, true)
	if err != nil || active.Query != nil {
		t.Fatal("history preserved an executable payload", err)
	}
	var exact model.Query
	encodedExact := fmt.Sprintf(`{"instance_id":%q,"sql":%q,"params":[9007199254740993],"client_request_id":"exact-integer","tool":"db_query"}`, i.ID, "SELECT id FROM "+table+" WHERE id="+parameter)
	if err = json.Unmarshal([]byte(encodedExact), &exact); err != nil {
		t.Fatal(err)
	}
	integer, err := a.Query(ctx, identity, exact)
	if err != nil || integer.Status != "succeeded" || len(integer.Rows) != 1 || fmt.Sprint(integer.Rows[0][0]) != "9007199254740993" {
		t.Fatalf("integer precision %#v %v", integer, err)
	}
	binding.Policy.Masks = append(binding.Policy.Masks, model.MaskRule{ID: "deny-password", Target: "password", Mode: "deny"})
	binding, err = s.SaveBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Query(ctx, identity, query("SELECT id FROM "+table+" WHERE password="+parameter, "denied", "db_query", "secret-value")); err == nil {
		t.Fatal("forbidden WHERE column accepted")
	}
	if cached, e := a.Status(identity, res.RequestID); e != nil || len(cached.Rows) > 0 {
		t.Fatalf("changed policy exposed cached values: %#v %v", cached, e)
	}
	write := query("UPDATE "+table+" SET counter=counter+1 WHERE id="+parameter, "write", "db_execute", 1)
	if driver == "postgres" || driver == "sqlserver" {
		forbidden := write
		if driver == "postgres" {
			forbidden.SQL += " RETURNING email,password"
			write.SQL += " RETURNING email"
		} else {
			forbidden.SQL = "UPDATE " + table + " SET counter=counter+1 OUTPUT inserted.email,inserted.password WHERE id=" + parameter
			write.SQL = "UPDATE " + table + " SET counter=counter+1 OUTPUT inserted.email WHERE id=" + parameter
		}
		if _, err = a.Query(ctx, identity, forbidden); err == nil {
			t.Fatal("forbidden returned column accepted")
		}
	}
	pending, err := a.Query(ctx, identity, write)
	if err != nil || pending.Status != "pending" {
		t.Fatalf("approval %#v %v", pending, err)
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT counter FROM "+table+" WHERE id=1").Scan(&count); err != nil || count != 0 {
		t.Fatal("write happened before approval")
	}
	if err = a.Decide(pending.RequestID, "approve"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			r, e := a.Resume(ctx, identity, pending.RequestID)
			if e == nil && r.Status != "succeeded" && r.Status != "executing" {
				e = fmt.Errorf("unexpected state %s", r.Status)
			}
			errs <- e
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if err = db.QueryRowContext(ctx, "SELECT counter FROM "+table+" WHERE id=1").Scan(&count); err != nil || count != 1 {
		t.Fatalf("write executed %d times: %v", count, err)
	}
	res, err = a.Query(ctx, identity, write)
	if err != nil || res.Status != "succeeded" {
		t.Fatalf("retry %#v %v", res, err)
	}
	audits, err = s.Audits(e.ID, i.ID, "succeeded", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range audits {
		if event.RequestID != pending.RequestID {
			continue
		}
		detail, err := s.AuditDetail(event.ID)
		if err != nil || detail.Request == nil || detail.Request.Result == nil || detail.Request.Status != "succeeded" {
			t.Fatal("concurrent resume lost request summary", err)
		}
		break
	}
	write.Params = []any{2}
	if _, err = a.Query(ctx, identity, write); err != storage.ErrConflict {
		t.Fatalf("payload conflict %v", err)
	}
	// Exercise the official HTTP MCP client and separate Admin authentication.
	admin := httptest.NewServer(api.Admin(a, func() {}, "foreground"))
	defer admin.Close()
	req, _ := http.NewRequestWithContext(ctx, "GET", admin.URL+"/admin/v1/instances", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("entity accessed Admin")
	}
	mcpHTTP := httptest.NewServer(api.MCP(a))
	defer mcpHTTP.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "container-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: mcpHTTP.URL + "/mcp", HTTPClient: &http.Client{Transport: authTransport{token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "db_query", Arguments: query("SELECT email AS e FROM "+table+" WHERE id="+parameter, "mcp", "db_query", 1)})
	if err != nil || result.IsError {
		t.Fatalf("MCP %v %#v", err, result)
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	if strings.Contains(string(encoded), "person@example.com") || !strings.Contains(string(encoded), "[EMAIL]") {
		t.Fatalf("MCP masking %s", encoded)
	}
	if driver == "postgres" {
		plan, err := a.Query(ctx, identity, query("SELECT id FROM "+table+" WHERE id=$1", "explain", "db_explain", 1))
		if err != nil || plan.Status != "succeeded" || len(plan.Rows) != 1 || !strings.Contains(fmt.Sprint(plan.Rows[0][0]), "node_type") {
			t.Fatalf("non-executing plan %#v %v", plan, err)
		}
	}
	if ext := os.Getenv("DBMCP_TEST_EXTENSIONS"); ext != "" {
		catalog, err := a.Catalog(ctx, identity, i.ID)
		if err != nil {
			t.Fatal(err)
		}
		extensions, err := a.DB.Extensions(ctx, i)
		if err != nil || len(extensions) == 0 {
			t.Fatalf("extension discovery %v %v", extensions, err)
		}
		if ext == "postgis" {
			found := false
			for _, c := range catalog {
				if c.Table == "locations" && c.Name == "geom" && c.SRID != nil && *c.SRID == 4326 {
					found = true
				}
			}
			if !found {
				t.Fatal("spatial column metadata missing")
			}
			res, err := a.Query(ctx, identity, query("SELECT geom FROM public.locations WHERE id=1", "geometry", "db_query"))
			if err != nil || res.Status != "succeeded" || len(res.Rows) != 1 || !strings.Contains(fmt.Sprint(res.Rows[0][0]), "SRID=4326;POINT") {
				t.Fatalf("spatial normalization %#v %v", res, err)
			}
		} else if ext == "timescaledb" {
			found := false
			for _, c := range catalog {
				if c.Table == "metrics" && c.Hypertable {
					found = true
				}
			}
			if !found {
				t.Fatal("hypertable metadata missing")
			}
			res, err := a.Query(ctx, identity, query("SELECT ts,value FROM public.metrics", "hypertable", "db_query"))
			if err != nil || res.Status != "succeeded" || len(res.Rows) != 1 {
				t.Fatalf("hypertable query %#v %v", res, err)
			}
		}
	}
	remember := query("UPDATE "+table+" SET counter=counter+1 WHERE id="+parameter, "remember-one", "db_execute", 2)
	next, err := a.Query(ctx, identity, remember)
	if err != nil || next.Status != "pending" {
		t.Fatalf("remember request %#v %v", next, err)
	}
	if err = a.Decide(next.RequestID, "remember"); err != nil {
		t.Fatal(err)
	}
	next, err = a.Resume(ctx, identity, next.RequestID)
	if err != nil || next.Status != "succeeded" {
		t.Fatalf("remember resume %#v %v", next, err)
	}
	remember.ClientRequestID = "remember-two"
	next, err = a.Query(ctx, identity, remember)
	if err != nil || next.Status != "succeeded" {
		t.Fatalf("remembered scope %#v %v", next, err)
	}
	if err = db.QueryRowContext(ctx, "SELECT counter FROM "+table+" WHERE id=2").Scan(&count); err != nil || count != 2 {
		t.Fatalf("remembered write count=%d %v", count, err)
	}
	for index, sql := range []string{"INSERT INTO " + table + "(id,email,counter) VALUES(3,'inserted@example.com',0)", "DELETE FROM " + table + " WHERE id=3"} {
		q := query(sql, fmt.Sprintf("dml-%d", index), "db_execute")
		r, err := a.Query(ctx, identity, q)
		if err != nil || r.Status != "pending" {
			t.Fatalf("DML approval %#v %v", r, err)
		}
		if err = a.Decide(r.RequestID, "approve"); err != nil {
			t.Fatal(err)
		}
		r, err = a.Resume(ctx, identity, r.RequestID)
		if err != nil || r.Status != "succeeded" || r.Affected != 1 {
			t.Fatalf("DML execution %#v %v", r, err)
		}
	}
	if driver == "dm" {
		lob, err := a.Query(ctx, identity, query("SELECT text_value,binary_value FROM DBMCP_TEST.large_values WHERE id=1", "native-lob", "db_query"))
		if err != nil || lob.Status != "succeeded" || len(lob.Rows) != 1 || lob.Rows[0][0] != "bounded text" || lob.Rows[0][1] != "base64:AP9B" {
			t.Fatalf("DM LOB normalization %#v %v", lob, err)
		}
	}
	i.Policy.MaxRows = 1
	i.Policy.MaxBytes = 256
	i, err = s.SaveInstance(i)
	if err != nil {
		t.Fatal(err)
	}
	limited, err := a.Query(ctx, identity, query("SELECT id FROM "+table+" ORDER BY id", "row-limit", "db_query"))
	if err != nil || limited.Status != "succeeded" || len(limited.Rows) != 1 || !limited.Truncated || limited.Reason != "row_limit" {
		t.Fatalf("row limit %#v %v", limited, err)
	}
	encodedLimited, err := json.Marshal(limited)
	if err != nil || len(encodedLimited) > i.Policy.MaxBytes {
		t.Fatalf("public result exceeded the byte limit: %d %v", len(encodedLimited), err)
	}
	tooLarge, err := a.Query(ctx, identity, query("SELECT '"+strings.Repeat("x", 600)+"' AS payload FROM "+table+" WHERE id=1", "byte-limit", "db_query"))
	if err != nil || tooLarge.Status != "failed" || len(tooLarge.Rows) != 0 {
		t.Fatalf("cell byte limit %#v %v", tooLarge, err)
	}
	if _, err = s.Backup(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Resume(ctx, identity, pending.RequestID); err == nil {
		t.Fatal("revoked credential read cached result")
	}
	logs, err := s.Audits("", "", "", 0)
	if err != nil || len(logs) == 0 {
		t.Fatal("missing audit")
	}
	bytes, _ := json.Marshal(logs)
	if strings.Contains(string(bytes), "secret-value") || strings.Contains(string(bytes), token) {
		t.Fatal("secret in audit")
	}
}
