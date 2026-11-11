// Package model defines the server-owned API and persistence values.
package model

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
)

const Version = "1.0.11"
const APIVersion = 1

func ID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type Connection struct {
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Database     string   `json:"database"`
	DatabaseMode string   `json:"database_mode,omitempty"`
	Databases    []string `json:"databases,omitempty"`
	Schema       string   `json:"schema"`
	Username     string   `json:"username"`
	Password     string   `json:"password,omitempty"`
	TLS          string   `json:"tls"`
}

type MaskRule struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	Mode      string `json:"mode"`
	Value     string `json:"value,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	KeepStart int    `json:"keep_start,omitempty"`
	KeepEnd   int    `json:"keep_end,omitempty"`
}

type Policy struct {
	Resources       []string   `json:"resources"`
	Operations      []string   `json:"operations"`
	CIDRs           []string   `json:"cidrs"`
	Approval        string     `json:"approval"`
	MaxRows         int        `json:"max_rows"`
	MaxBytes        int        `json:"max_bytes"`
	TimeoutSeconds  int        `json:"timeout_seconds"`
	Concurrency     int        `json:"concurrency"`
	ApprovalSeconds int        `json:"approval_seconds"`
	Masks           []MaskRule `json:"masks"`
}

func DefaultPolicy() Policy {
	return Policy{Resources: []string{"*"}, Operations: []string{"select", "insert", "update", "delete"}, CIDRs: []string{"127.0.0.0/8", "::1/128"}, Approval: "writes", MaxRows: 5000, MaxBytes: 10 << 20, TimeoutSeconds: 15, Concurrency: 4, ApprovalSeconds: 300, Masks: []MaskRule{}}
}
func DefaultBindingPolicy() Policy {
	return DefaultPolicy()
}

// Accept retired content-detection fields in saved policies and configuration
// imports, but never retain or emit them. All masking comes from explicit rules.
func (p *Policy) UnmarshalJSON(data []byte) error {
	type policy Policy
	v := struct {
		*policy
		DetectPII    json.RawMessage `json:"detect_pii"`
		PIIDetectors json.RawMessage `json:"pii_detectors"`
	}{policy: (*policy)(p)}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(&v)
}

func Match(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	ok, err := path.Match(strings.ToLower(pattern), strings.ToLower(value))
	return err == nil && ok
}
func Allowed(patterns []string, value string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool { return Match(p, value) })
}
func (p Policy) AllowsColumn(database, schema, table, column string) bool {
	return p.allowsResource(database, schema, table) || p.allowsResource(database, schema, table, column)
}

// A slash separates the encoded database from the existing schema/table path.
// Literal slashes and wildcard characters in names are escaped by Resource.
func DatabaseResource(database string, parts ...string) string {
	root := "db/" + Resource(database)
	if len(parts) == 0 {
		return root
	}
	return root + "/" + Resource(parts...)
}

func (p Policy) allowsResource(database string, parts ...string) bool {
	return slices.ContainsFunc(p.Resources, func(pattern string) bool {
		value := Resource(parts...)
		if strings.HasPrefix(pattern, "db/") {
			value = DatabaseResource(database, parts...)
		}
		return ResourceAllowed([]string{pattern}, value)
	})
}

func (p Policy) AllowsDatabase(database string) bool {
	return slices.ContainsFunc(p.Resources, func(pattern string) bool {
		if !strings.HasPrefix(pattern, "db/") {
			return pattern != ""
		}
		parts := strings.SplitN(pattern, "/", 3)
		return len(parts) == 3 && parts[2] != "" && ResourceAllowed([]string{parts[1]}, Resource(database))
	})
}

// Resource escapes catalog separators and glob characters inside each name.
// Policy patterns may still use explicit '*' across encoded name components.
func Resource(parts ...string) string {
	escape := strings.NewReplacer("%", "%25", ".", "%2E", "*", "%2A", "?", "%3F", "[", "%5B", "]", "%5D", "\\", "%5C", "/", "%2F")
	encoded := make([]string, len(parts))
	for i, part := range parts {
		encoded[i] = escape.Replace(part)
	}
	return strings.Join(encoded, ".")
}

// Resource names use canonical catalog spelling; quoted PostgreSQL names must
// not gain permissions through case folding.
func ResourceAllowed(patterns []string, value string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool { ok, err := path.Match(p, value); return err == nil && ok })
}
func (p Policy) AllowsTable(database, schema, table string) bool {
	if p.allowsResource(database, schema, table) {
		return true
	}
	return slices.ContainsFunc(p.Resources, func(v string) bool {
		suffix := v
		if strings.HasPrefix(v, "db/") {
			parts := strings.SplitN(v, "/", 3)
			if len(parts) != 3 {
				return false
			}
			suffix = parts[2]
		}
		parts := strings.Split(suffix, ".")
		if len(parts) != 3 {
			return false
		}
		prefix := v[:strings.LastIndex(v, ".")]
		return (Policy{Resources: []string{prefix}}).allowsResource(database, schema, table)
	})
}
func (p Policy) Validate() error {
	for _, r := range p.Resources {
		if strings.HasPrefix(r, "db/") {
			parts := strings.SplitN(r, "/", 3)
			if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
				return fmt.Errorf("invalid database-qualified resource")
			}
			if _, err := path.Match(r, ""); err != nil {
				return fmt.Errorf("invalid resource pattern")
			}
		}
	}
	if !slices.Contains([]string{"writes", "always", "never"}, p.Approval) {
		return fmt.Errorf("invalid approval mode")
	}
	if p.MaxRows < 1 || p.MaxRows > 10000 || p.MaxBytes < 256 || p.MaxBytes > 16<<20 || p.TimeoutSeconds < 1 || p.TimeoutSeconds > 300 || p.Concurrency < 1 || p.Concurrency > 32 || p.ApprovalSeconds < 1 || p.ApprovalSeconds > 86400 {
		return fmt.Errorf("limits outside supported range")
	}
	for _, op := range p.Operations {
		if !slices.Contains([]string{"select", "insert", "update", "delete"}, op) {
			return fmt.Errorf("unsupported operation %s", op)
		}
	}
	return nil
}

type Instance struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Driver      string     `json:"driver"`
	Connection  Connection `json:"connection"`
	PasswordSet bool       `json:"password_set"`
	Enabled     bool       `json:"enabled"`
	Policy      Policy     `json:"policy"`
	Revision    int        `json:"revision"`
}
type Template struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Policy   Policy `json:"policy"`
	Revision int    `json:"revision"`
}
type Entity struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Notes      string `json:"notes"`
	Enabled    bool   `json:"enabled"`
	Revision   int    `json:"revision"`
	LastSeen   string `json:"last_seen,omitempty"`
	LastSource string `json:"last_source,omitempty"`
	Client     string `json:"client,omitempty"`
}
type Binding struct {
	EntityID      string         `json:"entity_id"`
	InstanceID    string         `json:"instance_id"`
	Policy        Policy         `json:"policy"`
	Revision      int            `json:"revision"`
	ApprovalRules []ApprovalRule `json:"approval_rules,omitempty"`
}
type ApprovalRule struct {
	ID        string   `json:"id"`
	Database  string   `json:"database,omitempty"`
	Operation string   `json:"operation"`
	Tables    []Source `json:"tables"`
}
type Effective struct {
	InstancePolicy  Policy   `json:"instance_policy"`
	Binding         Binding  `json:"binding"`
	Operations      []string `json:"operations"`
	Approval        string   `json:"approval"`
	MaxRows         int      `json:"max_rows"`
	MaxBytes        int      `json:"max_bytes"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	Concurrency     int      `json:"concurrency"`
	InstanceEnabled bool     `json:"instance_enabled"`
	EntityEnabled   bool     `json:"entity_enabled"`
}
type Credential struct {
	ID        string `json:"id"`
	EntityID  string `json:"entity_id"`
	Prefix    string `json:"prefix"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Revoked   bool   `json:"revoked"`
}

// Registration reserves a configuration-specific credential without creating an
// entity or granting access. Its first authenticated use creates a pending entity.
type Registration struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Prefix      string `json:"prefix"`
	Status      string `json:"status"`
	EntityID    string `json:"entity_id,omitempty"`
	CreatedAt   string `json:"created_at"`
	RequestedAt string `json:"requested_at,omitempty"`
	Source      string `json:"source,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}
type RegistrationApproval struct {
	Bindings []Binding `json:"bindings"`
}
type Identity struct {
	EntityID     string
	CredentialID string
	Source       string
}
type Column struct {
	Database         string `json:"database"`
	DatabaseResource string `json:"database_resource"`
	Resource         string `json:"resource"`
	TableResource    string `json:"table_resource"`
	SchemaResource   string `json:"schema_resource"`
	Schema           string `json:"schema"`
	Table            string `json:"table"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Ordinal          int    `json:"ordinal"`
	SRID             *int   `json:"srid,omitempty"`
	SpatialType      string `json:"spatial_type,omitempty"`
	Dimensions       int    `json:"dimensions,omitempty"`
	Hypertable       bool   `json:"hypertable,omitempty"`
}

type Catalog struct {
	Database   string   `json:"database"`
	Schemas    []string `json:"schemas"`
	Columns    []Column `json:"columns"`
	Extensions []string `json:"extensions"`
}
type Source struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Column string `json:"column"`
}
type Output struct {
	Name    string   `json:"name"`
	Sources []Source `json:"sources"`
	Derived bool     `json:"derived"`
	Type    string   `json:"type,omitempty"`
}
type Statement struct {
	Operation   string   `json:"operation"`
	Tables      []Source `json:"tables"`
	Reads       []Source `json:"reads"`
	Outputs     []Output `json:"outputs"`
	ReturnsRows bool     `json:"returns_rows"`
}
type Query struct {
	InstanceID      string `json:"instance_id"`
	Database        string `json:"database,omitempty"`
	SQL             string `json:"sql"`
	Params          []any  `json:"params,omitempty"`
	ClientRequestID string `json:"client_request_id"`
	Tool            string `json:"tool,omitempty"`
}

func (q *Query) UnmarshalJSON(b []byte) error {
	type wire Query
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	d.DisallowUnknownFields()
	return d.Decode((*wire)(q))
}

type Result struct {
	RequestID string   `json:"request_id"`
	Status    string   `json:"status"`
	Columns   []string `json:"columns,omitempty"`
	Rows      [][]any  `json:"rows,omitempty"`
	Affected  int64    `json:"affected,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}
type Request struct {
	ID               string   `json:"id"`
	EntityID         string   `json:"entity_id"`
	CredentialID     string   `json:"credential_id"`
	InstanceID       string   `json:"instance_id"`
	Database         string   `json:"database,omitempty"`
	ClientRequestID  string   `json:"client_request_id"`
	Digest           string   `json:"digest"`
	Status           string   `json:"status"`
	Operation        string   `json:"operation"`
	Tables           []Source `json:"tables"`
	CreatedAt        string   `json:"created_at"`
	ExpiresAt        string   `json:"expires_at"`
	Reason           string   `json:"reason,omitempty"`
	Source           string   `json:"source"`
	InstanceRevision int      `json:"instance_revision"`
	BindingRevision  int      `json:"binding_revision"`
	Query            *Query   `json:"query,omitempty"`
	Result           *Result  `json:"result,omitempty"`
	ParameterJSON    string   `json:"parameter_json,omitempty"`
}

type Configuration struct {
	Version   int        `json:"version"`
	Instances []Instance `json:"instances"`
	Entities  []Entity   `json:"entities"`
	Bindings  []Binding  `json:"bindings"`
	Templates []Template `json:"templates"`
}
type Audit struct {
	ID         int64  `json:"id"`
	Time       string `json:"time"`
	EntityID   string `json:"entity_id"`
	InstanceID string `json:"instance_id"`
	RequestID  string `json:"request_id"`
	Action     string `json:"action"`
	Decision   string `json:"decision"`
	Detail     string `json:"detail"`
	DurationMS int64  `json:"duration_ms"`
}

// RequestDetail is an administrator-only encrypted history snapshot. It never
// supplies executable payloads or cached results to the Agent retry pipeline.
type RequestDetail struct {
	ID              string            `json:"id"`
	Tool            string            `json:"tool"`
	ClientRequestID string            `json:"client_request_id"`
	EntityID        string            `json:"entity_id"`
	EntityName      string            `json:"entity_name"`
	Client          string            `json:"client"`
	InstanceID      string            `json:"instance_id"`
	InstanceName    string            `json:"instance_name"`
	Database        string            `json:"database"`
	Source          string            `json:"source"`
	TraceID         string            `json:"trace_id"`
	SQL             string            `json:"sql"`
	ParameterJSON   string            `json:"parameter_json"`
	Status          string            `json:"status"`
	Operation       string            `json:"operation"`
	Tables          []Source          `json:"tables"`
	CreatedAt       string            `json:"created_at"`
	FinishedAt      string            `json:"finished_at,omitempty"`
	DurationMS      int64             `json:"duration_ms"`
	Result          *ExecutionSummary `json:"result,omitempty"`
}

type MaskMatch struct {
	Layer  string `json:"layer"`
	ID     string `json:"id"`
	Target string `json:"target"`
	Mode   string `json:"mode"`
}

type ExecutionSummary struct {
	Columns   []string    `json:"columns"`
	RowCount  int         `json:"row_count"`
	Affected  int64       `json:"affected"`
	Truncated bool        `json:"truncated"`
	Reason    string      `json:"reason"`
	Masks     []MaskMatch `json:"masks"`
}

type AuditDetail struct {
	Event           Audit          `json:"event"`
	Request         *RequestDetail `json:"request,omitempty"`
	PayloadState    string         `json:"payload_state"`
	Events          []Audit        `json:"events"`
	EventsTruncated bool           `json:"events_truncated"`
}
