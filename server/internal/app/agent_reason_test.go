package app

import (
	"testing"

	"dbmcp.local/server/internal/model"
)

func TestAgentExplanationDoesNotChangeApprovalPayloadIdentity(t *testing.T) {
	query := model.Query{InstanceID: "instance", Database: "db", Tool: "db_query", ClientRequestID: "retry", SQL: "SELECT $1", Params: []any{1}}
	original := digest(query)
	query.AgentReason = "检查数据，读取单行结果。"
	if digest(query) != original {
		t.Fatal("optional explanation changed retry identity")
	}
	query.SQL = "SELECT $1 + 1"
	if digest(query) == original {
		t.Fatal("SQL change did not change approval identity")
	}
}
