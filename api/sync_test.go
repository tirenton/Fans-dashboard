package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncUpload(t *testing.T) {
	dir := t.TempDir()
	store := NewStore()
	capDir := filepath.Join(dir, "captures")
	mux := http.NewServeMux()
	registerSync(mux, dir, filepath.Join(dir, "users.json"), store, NewRemote(), func() { store.IngestDir(capDir) })
	do := func(method, path string, body []byte, gz bool) *httptest.ResponseRecorder {
		var rd *bytes.Reader
		if gz {
			var buf bytes.Buffer
			w := gzip.NewWriter(&buf)
			w.Write(body)
			w.Close()
			rd = bytes.NewReader(buf.Bytes())
		} else {
			rd = bytes.NewReader(body)
		}
		r := httptest.NewRequest(method, path, rd)
		if gz {
			r.Header.Set("Content-Encoding", "gzip")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	capture := []byte(`{"capturedAt":"2026-11-20T10:00:00Z","feed":{"posts":[]}}`)

	if w := do("PUT", "/api/sync/captures/2026-11-20T10-00-00-000Z-light.json", capture, true); w.Code != 204 {
		t.Fatalf("capture (gzip): %d %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(capDir, "2026-11-20T10-00-00-000Z-light.json")); !bytes.Equal(b, capture) {
		t.Fatalf("capture stored as %q", b)
	}
	if store.LastFile() != "2026-11-20T10-00-00-000Z-light.json" {
		t.Fatalf("the upload should be ingested at once, last file %q", store.LastFile())
	}
	if w := do("PUT", "/api/sync/captures/2026-11-19T10-00-00-000Z-light.json", capture, false); w.Code != 409 {
		t.Fatalf("an older capture after a newer one must be refused (it would never be read): %d", w.Code)
	}
	if w := do("PUT", "/api/sync/captures/2026-11-20T10-00-00-000Z-light.json", capture, false); w.Code != 204 {
		t.Fatalf("re-sending the same capture is fine: %d", w.Code)
	}
	if w := do("PUT", "/api/sync/captures/2026-11-21T10-00-00-000Z-light.json", capture[:20], false); w.Code != 400 {
		t.Fatalf("half a capture must be refused: %d", w.Code)
	}
	for _, bad := range []string{"..%2Fx.json", "notes.txt", ".hidden.json", "2026-11-21T10-00-00-000Z.json.part"} {
		if w := do("PUT", "/api/sync/captures/"+bad, capture, false); w.Code == 204 {
			t.Fatalf("capture name %q must be refused", bad)
		}
	}
	if w := do("PUT", "/api/sync/images/abc123.jpg", []byte("JPEGDATA"), false); w.Code != 204 {
		t.Fatalf("image: %d", w.Code)
	}
	for _, bad := range []string{"abc.jpg.missing", "abc.jpg.part", ".x.jpg", "a%20b.jpg"} {
		if w := do("PUT", "/api/sync/images/"+bad, []byte("x"), false); w.Code == 204 {
			t.Fatalf("image name %q must be refused", bad)
		}
	}

	if w := do("PUT", "/api/sync/log?offset=0", []byte("line1\n"), false); w.Code != 204 {
		t.Fatalf("log replace: %d", w.Code)
	}
	if w := do("PUT", "/api/sync/log?offset=6", []byte("line2\n"), true); w.Code != 204 {
		t.Fatalf("log append: %d", w.Code)
	}
	if w := do("PUT", "/api/sync/log?offset=3", []byte("x"), false); w.Code != 409 || w.Header().Get("X-Log-Size") != "12" {
		t.Fatalf("log at a wrong offset: want 409 with the real size 12, got %d %q", w.Code, w.Header().Get("X-Log-Size"))
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "log.jsonl")); string(b) != "line1\nline2\n" {
		t.Fatalf("log is %q", b)
	}

	if w := do("PUT", "/api/sync/stop", []byte("2026-11-20 401 from refresh\n"), false); w.Code != 204 {
		t.Fatalf("stop: %d", w.Code)
	}
	var st SyncState
	json.Unmarshal(do("GET", "/api/sync/state", nil, false).Body.Bytes(), &st)
	if st.Captures != 1 || st.NewestCapture != "2026-11-20T10-00-00-000Z-light.json" || st.Images != 1 || st.LogSize != 12 || !st.Stop {
		t.Fatalf("state: %+v", st)
	}
	var miss struct{ Missing []string }
	json.Unmarshal(do("POST", "/api/sync/missing-images", []byte(`{"names":["abc123.jpg","new.jpg","../x"]}`), true).Body.Bytes(), &miss)
	if len(miss.Missing) != 1 || miss.Missing[0] != "new.jpg" {
		t.Fatalf("missing images: want only new.jpg (bad names dropped), got %v", miss.Missing)
	}
	if w := do("DELETE", "/api/sync/stop", nil, false); w.Code != 204 {
		t.Fatalf("stop delete: %d", w.Code)
	}
	if ReadHealth(dir, time.Now()).Stop != "" {
		t.Fatal("STOP should be gone")
	}
	if w := do("PUT", "/api/sync/images/big.jpg", []byte(strings.Repeat("x", maxImageBytes+1)), true); w.Code != 400 {
		t.Fatalf("an image over the limit (even gzipped small) must be refused: %d", w.Code)
	}
}
