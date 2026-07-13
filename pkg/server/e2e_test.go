package server

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(New(log).Routes())
	t.Cleanup(ts.Close)
	return ts
}

func newJarClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func TestE2ELobby(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `<meta name="viewport"`) {
		t.Fatalf("body missing viewport meta tag")
	}
	if !strings.Contains(string(body), "manifest.webmanifest") {
		t.Fatalf("body missing manifest.webmanifest link")
	}
}

// createTable posts a new table with the given client/name and returns the
// table id, following the redirect to confirm the board renders.
func createTable(t *testing.T, client *http.Client, base, name string) string {
	t.Helper()
	resp, err := client.PostForm(base+"/games", url.Values{"name": {name}})
	if err != nil {
		t.Fatalf("POST /games: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /games (after redirect follow) status = %d, want 200", resp.StatusCode)
	}
	loc := resp.Request.URL.Path
	const prefix = "/games/"
	if !strings.HasPrefix(loc, prefix) {
		t.Fatalf("final URL %q does not look like a table board", loc)
	}
	return strings.TrimPrefix(loc, prefix)
}

func TestE2ECreateAndBoard(t *testing.T) {
	ts := newTestServer(t)
	client := newJarClient()

	id := createTable(t, client, ts.URL, "Alice")

	resp, err := client.Get(ts.URL + "/games/" + id)
	if err != nil {
		t.Fatalf("GET /games/%s: %v", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /games/%s status = %d, want 200", id, resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "sse-connect=") {
		t.Fatalf("board page missing sse-connect attribute")
	}
}

func TestE2EJoinSecondPlayer(t *testing.T) {
	ts := newTestServer(t)
	creator := newJarClient()
	id := createTable(t, creator, ts.URL, "Alice")

	joiner := newJarClient()
	resp, err := joiner.Get(ts.URL + "/games/" + id)
	if err != nil {
		t.Fatalf("GET /games/%s (joiner): %v", id, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /games/%s (joiner) status = %d, want 200", id, resp.StatusCode)
	}
	if !strings.Contains(string(body), "Join table") {
		t.Fatalf("expected join page for unseated player, got: %s", body)
	}

	resp, err = joiner.PostForm(ts.URL+"/games/"+id+"/join", url.Values{"name": {"Bob"}})
	if err != nil {
		t.Fatalf("POST join: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST join (after redirect follow) status = %d, want 200", resp.StatusCode)
	}
	if strings.Contains(string(body), "Join table") {
		t.Fatalf("expected board page after joining, still shows join form")
	}
	if !strings.Contains(string(body), "sse-connect=") {
		t.Fatalf("board page after join missing sse-connect attribute")
	}
}

func TestE2ESSEDeliversBoard(t *testing.T) {
	ts := newTestServer(t)
	client := newJarClient()
	id := createTable(t, client, ts.URL, "Alice")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/games/"+id+"/events", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET events status = %d, want 200", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	found := false
	for scanner.Scan() {
		if scanner.Text() == "event: board" {
			found = true
			break
		}
	}
	if !found {
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			t.Fatalf("reading SSE stream: %v", err)
		}
		t.Fatalf("SSE stream ended without an 'event: board' line")
	}
}

func TestE2EPWAAssets(t *testing.T) {
	ts := newTestServer(t)

	cases := []struct {
		path        string
		contentType string
	}{
		{"/sw.js", "application/javascript"},
		{"/static/manifest.webmanifest", "application/manifest+json"},
		{"/static/icons/icon-192.png", "image/png"},
	}
	for _, c := range cases {
		resp, err := http.Get(ts.URL + c.path)
		if err != nil {
			t.Fatalf("GET %s: %v", c.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", c.path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, c.contentType) {
			t.Fatalf("GET %s Content-Type = %q, want prefix %q", c.path, ct, c.contentType)
		}
	}
}
