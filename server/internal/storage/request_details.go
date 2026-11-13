package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"dbmcp.local/server/internal/model"
)

func (s *Store) RecordRequestDetail(v model.RequestDetail) error {
	_, err := s.DB.Exec("INSERT OR IGNORE INTO request_details(request_id,created_at,body) VALUES(?,?,?)", v.ID, v.CreatedAt, s.Seal([]byte(encode(v))))
	return err
}

func (s *Store) requestDetail(id string) (model.RequestDetail, error) {
	var body []byte
	var v model.RequestDetail
	if err := s.DB.QueryRow("SELECT body FROM request_details WHERE request_id=?", id).Scan(&body); err != nil {
		return v, err
	}
	plain, err := s.Unseal(body)
	if err == nil {
		err = json.Unmarshal(plain, &v)
	}
	return v, err
}

func (s *Store) CompleteRequestDetail(id, status string, summary model.ExecutionSummary, duration int64) error {
	v, err := s.requestDetail(id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // A request created before this version has no history snapshot.
	}
	if err != nil {
		return err
	}
	v.Status, v.Result, v.DurationMS = status, &summary, duration
	v.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.DB.Exec("UPDATE request_details SET body=? WHERE request_id=?", s.Seal([]byte(encode(v))), id)
	return err
}

// AuditDetail is read-only. In particular it never expires, claims or resumes a
// request while displaying its history, and never exports cached result rows.
func (s *Store) AuditDetail(id int64) (model.AuditDetail, error) {
	v := model.AuditDetail{PayloadState: "not_recorded", Events: []model.Audit{}}
	e := &v.Event
	err := s.DB.QueryRow("SELECT id,time,entity_id,instance_id,request_id,action,decision,detail,duration_ms FROM audit_events WHERE id=?", id).Scan(&e.ID, &e.Time, &e.EntityID, &e.InstanceID, &e.RequestID, &e.Action, &e.Decision, &e.Detail, &e.DurationMS)
	if err != nil {
		return v, err
	}
	if e.RequestID == "" {
		v.Events = append(v.Events, *e)
		return v, nil
	}
	rows, err := s.DB.Query("SELECT id,time,entity_id,instance_id,request_id,action,decision,detail,duration_ms FROM audit_events WHERE request_id=? ORDER BY id LIMIT 1001", e.RequestID)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var event model.Audit
		if err = rows.Scan(&event.ID, &event.Time, &event.EntityID, &event.InstanceID, &event.RequestID, &event.Action, &event.Decision, &event.Detail, &event.DurationMS); err != nil {
			break
		}
		v.Events = append(v.Events, event)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	if rowErr != nil {
		return v, rowErr
	}
	if len(v.Events) > 1000 {
		v.EventsTruncated = true
		v.Events = v.Events[:1000]
	}
	detail, err := s.requestDetail(e.RequestID)
	if err == nil {
		v.Request, v.PayloadState = &detail, "available"
		// Approval, revocation and expiry use the active state as the authority.
		var status, expires string
		err = s.DB.QueryRow("SELECT status,expires_at FROM requests WHERE id=?", e.RequestID).Scan(&status, &expires)
		if err == nil {
			detail.Status = status
			if (status == "pending" || status == "approved") && expires <= time.Now().UTC().Format(time.RFC3339) {
				detail.Status = "expired"
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return v, err
		}
		return v, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	// Legacy rows may still have an executable payload; expose only what exists.
	var body, status string
	var payload []byte
	err = s.DB.QueryRow("SELECT body,status,payload FROM requests WHERE id=?", e.RequestID).Scan(&body, &status, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	var old model.Request
	if err = json.Unmarshal([]byte(body), &old); err != nil {
		return v, err
	}
	detail = model.RequestDetail{ID: old.ID, EntityID: old.EntityID, InstanceID: old.InstanceID, ClientRequestID: old.ClientRequestID, Database: old.Database, Source: old.Source, Status: status, Operation: old.Operation, Tables: old.Tables, CreatedAt: old.CreatedAt}
	if (status == "pending" || status == "approved") && old.ExpiresAt <= time.Now().UTC().Format(time.RFC3339) {
		detail.Status = "expired"
	}
	v.Request, v.PayloadState = &detail, "cleared"
	if len(payload) > 0 {
		plain, err := s.Unseal(payload)
		if err != nil {
			return v, err
		}
		var query model.Query
		if err = json.Unmarshal(plain, &query); err != nil {
			return v, err
		}
		detail.SQL, detail.Tool, detail.ParameterJSON = query.SQL, query.Tool, encode(query.Params)
		detail.AgentReason = query.AgentReason
		v.PayloadState = "available"
	}
	return v, nil
}
