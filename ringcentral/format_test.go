package ringcentral

import (
	"encoding/json"
	"testing"
)

func TestFormatResourceID(t *testing.T) {
	tests := []struct {
		name string
		id   any
		want string
	}{
		{name: "string", id: "sms-1", want: "sms-1"},
		{name: "int64", id: int64(3559892082021), want: "3559892082021"},
		{name: "float64 integer", id: float64(3559892082021), want: "3559892082021"},
		{name: "json number", id: json.Number("3559892082021"), want: "3559892082021"},
		{name: "float64 decimal", id: 12.5, want: "12.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatResourceID(tt.id); got != tt.want {
				t.Fatalf("FormatResourceID(%v) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}
