package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dbmcp.local/server/internal/app"
	"dbmcp.local/server/internal/model"
	"dbmcp.local/server/internal/storage"
)

func TestDatabaseFailureReachesAuthenticatedAdminStream(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := app.New(s)
	defer a.Close()
	server := httptest.NewServer(Admin(a, func() {}, "foreground"))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/admin/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+s.AdminToken)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.Status)
	}
	reader := bufio.NewReader(response.Body)
	for range 3 { // Initial event confirms that the subscriber is registered.
		if _, err = reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	i, _ := s.SaveInstance(model.Instance{Name: "unavailable", Enabled: true, Driver: "sqlserver", Policy: model.DefaultPolicy(), Connection: model.Connection{Host: "127.0.0.1", Port: 1, Username: "fixture", TLS: "disable", DatabaseMode: "all"}})
	entity, _ := s.SaveEntity(model.Entity{Name: "agent", Enabled: true})
	_, token, _ := s.Issue(entity.ID, "")
	_, _ = s.SaveBinding(model.Binding{EntityID: entity.ID, InstanceID: i.ID, Policy: model.DefaultPolicy()})
	identity, _ := s.Authenticate(token, "127.0.0.1")
	queryCtx, cancelQuery := context.WithCancel(ctx)
	cancelQuery() // Exercise a database failure without contacting a target server.
	result, err := a.Query(queryCtx, identity, model.Query{InstanceID: i.ID, Database: "fixture", Tool: "db_execute", SQL: "INSERT INTO secret_table VALUES ('secret-value')", ClientRequestID: "stream"})
	if err == nil || result.Status != "failed" {
		t.Fatalf("query: %#v %v", result, err)
	}
	line, err := reader.ReadString('\n')
	if err != nil || line != "event: database-failed\n" {
		t.Fatalf("failure event: %q %v", line, err)
	}
	line, err = reader.ReadString('\n')
	var failure app.DatabaseFailure
	if err != nil || json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &failure) != nil || failure.RequestID != result.RequestID || failure.Phase != "preparation" {
		t.Fatalf("failure data: %q %v", line, err)
	}
	if strings.Contains(line, "secret") {
		t.Fatal("SQL leaked through SSE")
	}
}
