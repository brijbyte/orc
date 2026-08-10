package main

import "testing"

func TestResolveWebAddrPortPrecedence(t *testing.T) {
	t.Setenv("PORT", "8888")
	tests := []struct {
		name    string
		addr    string
		port    int
		portSet bool
		want    string
	}{
		{"flag", "127.0.0.1", 9999, true, "127.0.0.1:9999"},
		{"flag replaces address port", "127.0.0.1:6666", 9999, true, "127.0.0.1:9999"},
		{"address port", "127.0.0.1:6666", 7777, false, "127.0.0.1:6666"},
		{"environment", "127.0.0.1", 7777, false, "127.0.0.1:8888"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveWebAddr(test.addr, test.port, test.portSet)
			if err != nil || got != test.want {
				t.Fatalf("address = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestResolveWebAddrDefaultPort(t *testing.T) {
	t.Setenv("PORT", "")
	got, err := resolveWebAddr("127.0.0.1", 7777, false)
	if err != nil || got != "127.0.0.1:7777" {
		t.Fatalf("address = %q, %v", got, err)
	}
}

func TestResolveWebAddrRejectsBadEnvironment(t *testing.T) {
	t.Setenv("PORT", "http")
	if _, err := resolveWebAddr("127.0.0.1", 7777, false); err == nil {
		t.Fatal("invalid PORT was accepted")
	}
}
