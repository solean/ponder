package main

import "testing"

func TestParseServeOptionsAddress(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "loopback default", want: "127.0.0.1:8080"},
		{name: "explicit address", args: []string{"-addr", "0.0.0.0:9000"}, want: "0.0.0.0:9000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseServeOptions(tt.args)
			if err != nil {
				t.Fatalf("parseServeOptions() error = %v", err)
			}
			if got.addr != tt.want {
				t.Errorf("serve address = %q, want %q", got.addr, tt.want)
			}
		})
	}
}
