package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{a: "1.0.1", b: "1.0.0", want: 1},
		{a: "v1.1.0", b: "1.0.9", want: 1},
		{a: "1.0.0", b: "1.0.0", want: 0},
		{a: "1.0", b: "1.0.0", want: 0},
		{a: "1.0.0", b: "1.0.1", want: -1},
		{a: "2.0.0", b: "10.0.0", want: -1},
		{a: "1.1.0", b: "1.1.0-beta", want: 1},
		{a: "1.0.0", b: "dev-20240101120000", want: 1},
	}

	for _, tc := range cases {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckAt(t *testing.T) {
	cases := []struct {
		name        string
		current     string
		body        string
		wantUpdate  bool
		wantLatest  string
		wantPageURL string
	}{
		{
			name:        "newer release available",
			current:     "1.0.0",
			body:        `{"tag_name":"v1.2.0","html_url":"https://github.com/OUBIGFA/WinTray/releases/tag/v1.2.0"}`,
			wantUpdate:  true,
			wantLatest:  "1.2.0",
			wantPageURL: "https://github.com/OUBIGFA/WinTray/releases/tag/v1.2.0",
		},
		{
			name:        "already up to date",
			current:     "1.2.0",
			body:        `{"tag_name":"v1.2.0","html_url":"https://github.com/OUBIGFA/WinTray/releases/tag/v1.2.0"}`,
			wantUpdate:  false,
			wantLatest:  "1.2.0",
			wantPageURL: "https://github.com/OUBIGFA/WinTray/releases/tag/v1.2.0",
		},
		{
			name:        "missing page falls back to the repository",
			current:     "1.0.0",
			body:        `{"tag_name":"v1.0.0"}`,
			wantUpdate:  false,
			wantLatest:  "1.0.0",
			wantPageURL: "https://github.com/OUBIGFA/WinTray",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("User-Agent") == "" {
					t.Errorf("request is missing a User-Agent header")
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			got, err := checkAt(context.Background(), server.URL, tc.current)
			if err != nil {
				t.Fatalf("checkAt returned error: %v", err)
			}
			if got.HasUpdate != tc.wantUpdate {
				t.Errorf("HasUpdate = %t, want %t", got.HasUpdate, tc.wantUpdate)
			}
			if got.Latest != tc.wantLatest {
				t.Errorf("Latest = %q, want %q", got.Latest, tc.wantLatest)
			}
			if got.PageURL != tc.wantPageURL {
				t.Errorf("PageURL = %q, want %q", got.PageURL, tc.wantPageURL)
			}
		})
	}
}

func TestCheckAtRejectsErrorResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	if _, err := checkAt(context.Background(), server.URL, "1.0.0"); err == nil {
		t.Fatal("checkAt accepted an error response")
	}
}

func TestCheckAtRejectsEmptyTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":""}`))
	}))
	defer server.Close()

	if _, err := checkAt(context.Background(), server.URL, "1.0.0"); err == nil {
		t.Fatal("checkAt accepted a release without a tag")
	}
}
