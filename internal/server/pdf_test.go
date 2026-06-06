package server

import (
	"reflect"
	"testing"
)

func TestParseSelectedPages(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "comma separated pages",
			input: "1,3,5",
			want:  []string{"1", "3", "5"},
		},
		{
			name:  "range",
			input: "2-5",
			want:  []string{"2-5"},
		},
		{
			name:  "whitespace around values",
			input: "1, 3, 5",
			want:  []string{"1", "3", "5"},
		},
		{
			name:    "empty pages",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid pages",
			input:   "1,",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSelectedPages(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSelectedPages(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSelectedPages(%q) unexpected error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseSelectedPages(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
