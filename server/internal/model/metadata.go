package model

// TableColumn augments a catalog column with its declared nullability.
type TableColumn struct {
	Column
	Nullable bool `json:"nullable"`
}

type IndexColumn struct {
	Name       string `json:"name"`
	Descending bool   `json:"descending,omitempty"`
	Included   bool   `json:"included,omitempty"`
	Prefix     int    `json:"prefix_length,omitempty"`
}

type TableIndex struct {
	Name    string        `json:"name"`
	Type    string        `json:"type"`
	Unique  bool          `json:"unique"`
	Columns []IndexColumn `json:"columns"`
}

type TableDescription struct {
	Kind       string        `json:"kind,omitempty"`
	Columns    []TableColumn `json:"columns"`
	PrimaryKey []string      `json:"primary_key"`
	Indexes    []TableIndex  `json:"indexes"`
}
