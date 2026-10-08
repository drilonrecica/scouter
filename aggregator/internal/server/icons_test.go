package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

func TestIconsNeedTokenAndAreImmutable(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "0123456789abcdef.png"), []byte("png bytes"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o644)
	s := New(state.NewStore(""), "secret")
	s.ServeIcons(dir)
	srv := httptest.NewServer(s)
	defer srv.Close()

	if resp := get(t, srv, "/v1/icons/0123456789abcdef", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: %d", resp.StatusCode)
	}
	resp := get(t, srv, "/v1/icons/0123456789abcdef", "secret", nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "png bytes" ||
		resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("icon: %d %q %q %q", resp.StatusCode, body, resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"))
	}
	for _, bad := range []string{"secret.txt", "..%2fsecret", "0123456789ABCDEF", "fedcba9876543210"} {
		if resp := get(t, srv, "/v1/icons/"+bad, "secret", nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", bad, resp.StatusCode)
		}
	}
}
