package metrics

import (
	"net/http"
	"testing"
)

func TestRouteLabelUsesPatternOrUnmatched(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{name: "matched route", pattern: "GET /feeds/{id}", want: "GET /feeds/{id}"},
		{name: "unmatched route", want: "unmatched"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{Pattern: tt.pattern}
			if got := routeLabel(r); got != tt.want {
				t.Errorf("routeLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMethodLabelMapsUnknownMethods(t *testing.T) {
	if got := methodLabel("CUSTOM"); got != "OTHER" {
		t.Errorf("methodLabel(CUSTOM) = %q, want OTHER", got)
	}
}
