package api

import (
	"net/http/httptest"
	"testing"
)

func TestProxySourceTrustBoundary(t *testing.T) {
	resolve, err := NewSourceResolver([]string{"127.0.0.1/32", "10.0.0.0/8", "::1/128"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ peer, header, want string }{
		{"192.0.2.2:1234", "127.0.0.1", "192.0.2.2"},
		{"127.0.0.1:1234", "198.51.100.8", "198.51.100.8"},
		{"127.0.0.1:1234", "127.0.0.1, 198.51.100.8, 10.0.0.3", "198.51.100.8"},
		{"[::1]:1234", "2001:db8::8, 10.0.0.3", "2001:db8::8"},
		{"127.0.0.1:1234", "", ""},
		{"127.0.0.1:1234", "198.51.100.8, invalid", ""},
		{"127.0.0.1:1234", "127.0.0.1, 10.0.0.3", ""},
	} {
		r := httptest.NewRequest("POST", "/mcp", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("X-Forwarded-For", tc.header)
		if got := resolve(r); got != tc.want {
			t.Errorf("peer=%s header=%s got=%q want=%q", tc.peer, tc.header, got, tc.want)
		}
	}
	if _, err := NewSourceResolver([]string{"not-a-cidr"}); err == nil {
		t.Fatal("invalid trusted range accepted")
	}
	direct, _ := NewSourceResolver(nil)
	r := httptest.NewRequest("GET", "/mcp", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.8")
	if direct(r) != "127.0.0.1" {
		t.Fatal("default trusted a header")
	}
}
