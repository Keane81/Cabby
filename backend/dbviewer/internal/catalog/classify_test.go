package catalog

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		name, dataType string
		kind           Kind
		sensitive      bool
		searchable     bool
		filterable     bool
	}{
		{"email", "text", KindText, false, true, true},
		{"id", "uuid", KindUUID, false, true, true},
		{"latitude", "double precision", KindNumber, false, false, true},
		{"created_at", "timestamp with time zone", KindTimestamp, false, false, true},
		{"flag", "boolean", KindBool, false, false, true},
		{"password_hash", "text", KindText, true, false, false},
		{"token_hash", "bytea", KindOther, true, false, false},
		{"raw", "bytea", KindOther, true, false, false},
		{"new_secret_value", "text", KindText, true, false, false},
		{"API_TOKEN", "text", KindText, true, false, false},
		{"payload", "jsonb", KindOther, false, false, false},
	}
	for _, tc := range tests {
		got := Classify(tc.name, tc.dataType)
		if got.Type != tc.kind || got.Sensitive != tc.sensitive || got.Searchable != tc.searchable ||
			got.Filterable != tc.filterable || got.Sortable != tc.filterable {
			t.Errorf("Classify(%q, %q) = %+v", tc.name, tc.dataType, got)
		}
	}
}
