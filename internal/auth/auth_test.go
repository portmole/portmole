package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSignInStoresAndLogoutRemoves(t *testing.T) {
	dir := t.TempDir()
	for _, k := range []string{"APPDATA", "XDG_CONFIG_HOME", "HOME"} {
		t.Setenv(k, dir)
	}

	var polls, revoked atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/device", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"device_code":"dc","user_code":"ABCD-EFGH","verification_uri_complete":"http://x/activate?code=ABCD-EFGH","interval":0}`)
	})
	mux.HandleFunc("POST /api/device/token", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		if in["device_code"] != "dc" || polls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"authorization_pending"}`)
			return
		}
		io.WriteString(w, `{"token":"pm_test","handle":"dev"}`)
	})
	mux.HandleFunc("DELETE /api/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer pm_test" {
			revoked.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := context.Background()

	if _, ok := Load(srv.URL); ok {
		t.Fatal("credentials exist before sign-in")
	}
	d, err := Start(ctx, srv.URL)
	if err != nil || d.UserCode != "ABCD-EFGH" {
		t.Fatalf("Start: %+v, %v", d, err)
	}
	c, err := Wait(ctx, srv.URL, d)
	if err != nil || c.Token != "pm_test" || c.Handle != "dev" {
		t.Fatalf("Wait: %+v, %v", c, err)
	}
	if polls.Load() != 2 {
		t.Fatalf("polls = %d, want 2 (one pending, one approved)", polls.Load())
	}

	if got, ok := Load(srv.URL); !ok || got != c {
		t.Fatalf("Load: %+v, %v", got, ok)
	}
	if _, ok := Load("https://other.example"); ok {
		t.Fatal("token was offered to a different server")
	}

	if err := Logout(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if revoked.Load() != 1 {
		t.Fatal("logout did not revoke the token on the server")
	}
	if _, ok := Load(srv.URL); ok {
		t.Fatal("credentials survive logout")
	}
}
