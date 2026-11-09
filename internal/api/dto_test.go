package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// The wire contract promises snake_case fields and explicit nulls (never omitted).
func TestDTOJSONShape(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []string
	}{
		{"entry nulls", Entry{}, []string{`"note":null`, `"receipt_sha256":null`, `"void_reason":null`, `"version_id":0`}},
		{"ledger row nulls", LedgerRow{}, []string{`"ref_id":null`, `"amount_sen":null`, `"prev_hash":""`}},
		{"error fields null", ErrorResponse{Error: "x", Code: CodeInternal}, []string{`"fields":null`, `"code":"internal"`}},
		{"append result flattens row", AppendResult{LedgerRow: LedgerRow{ID: 7, Kind: KindCreate}}, []string{`"id":7`, `"kind":"create"`, `"entry":{`}},
		{"verify", VerifyResult{OK: true}, []string{`"broken_at":null`, `"last_backup_at":null`, `"ok":true`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.v)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(string(b), w) {
					t.Errorf("%s: missing %s in %s", tt.name, w, b)
				}
			}
		})
	}
}

func TestMethodsAndHeader(t *testing.T) {
	if len(Methods) != 6 || Methods[3].Label != "TNG eWallet" {
		t.Fatalf("methods changed: %+v", Methods)
	}
	if strings.Join(CSVHeader, ",") != "date,category,payee,method,amount_myr,note,status,entry_id,receipt_file" {
		t.Fatalf("csv header changed: %v", CSVHeader)
	}
}
