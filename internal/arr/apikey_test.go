package arr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
		wantErr string
	}{
		{
			name: "found",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/initialize.json" {
					t.Errorf("path = %s", r.URL.Path)
				}
				w.Write([]byte(`{"apiRoot":"/api/v3","apiKey":"abc123","urlBase":""}`))
			},
			want: "abc123",
		},
		{
			name:    "login required",
			handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
			wantErr: "login required",
		},
		{
			name: "redirect to login page",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login" {
					w.Write([]byte("<html>login</html>"))
					return
				}
				http.Redirect(w, r, "/login", http.StatusFound)
			},
			wantErr: "set api_key",
		},
		{
			name:    "no key",
			handler: func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"apiRoot":"/api/v3"}`)) },
			wantErr: "has no apiKey",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			got, err := FetchAPIKey(context.Background(), srv.URL+"/", srv.Client())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
