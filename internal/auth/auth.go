// Package auth signs this machine in to a portmole server and keeps the
// resulting token on disk.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Credentials struct {
	Server string `json:"server"`
	Token  string `json:"token"`
	Handle string `json:"handle"`
}

// Device is a sign-in waiting for the user to approve it in a browser.
type Device struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	URL        string `json:"verification_uri_complete"`
	Interval   int    `json:"interval"`
	ExpiresIn  int    `json:"expires_in"`
}

var client = &http.Client{Timeout: 15 * time.Second}

// Start asks the server for a sign-in the user can approve at Device.URL.
func Start(ctx context.Context, server string) (Device, error) {
	var d Device
	status, err := post(ctx, server+"/api/device", nil, &d)
	if err != nil {
		return d, err
	}
	if status != http.StatusOK || d.DeviceCode == "" {
		return d, fmt.Errorf("portmole: server refused to start sign-in (status %d)", status)
	}
	return d, nil
}

// Wait polls until the user approves the sign-in, then stores the credentials.
func Wait(ctx context.Context, server string, d Device) (Credentials, error) {
	interval := time.Duration(d.Interval) * time.Second
	if interval < time.Second {
		interval = time.Second // never hammer the server, whatever it says
	}
	for {
		select {
		case <-ctx.Done():
			return Credentials{}, ctx.Err()
		case <-time.After(interval):
		}
		var out struct {
			Token  string `json:"token"`
			Handle string `json:"handle"`
			Error  string `json:"error"`
		}
		status, err := post(ctx, server+"/api/device/token", map[string]string{"device_code": d.DeviceCode}, &out)
		switch {
		case ctx.Err() != nil:
			return Credentials{}, ctx.Err()
		case err != nil || status == http.StatusTooManyRequests || status >= 500:
			// A dropped request or a busy server is not a refusal; the
			// sign-in is still waiting on the other side.
		case status == http.StatusOK && out.Token != "":
			c := Credentials{Server: server, Token: out.Token, Handle: out.Handle}
			return c, save(c)
		case out.Error == "authorization_pending":
		default:
			return Credentials{}, errors.New("portmole: sign-in expired, start again")
		}
	}
}

// Load returns the stored credentials for server, if any.
func Load(server string) (Credentials, bool) {
	var c Credentials
	p, err := path()
	if err != nil {
		return c, false
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, false
	}
	return c, c.Server == server && c.Token != ""
}

// Logout revokes the stored token on the server and removes it from disk.
func Logout(ctx context.Context, server string) error {
	c, ok := Load(server)
	if !ok {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, server+"/api/token", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	// The local copy goes even when the server could not be reached.
	p, perr := path()
	if perr != nil {
		return perr
	}
	if rerr := os.Remove(p); rerr != nil && !os.IsNotExist(rerr) {
		return rerr
	}
	if err != nil {
		return fmt.Errorf("portmole: signed out locally, but the server could not be reached to revoke the token: %w", err)
	}
	return nil
}

// shortcut: a 0600 file in the user's config directory, not the OS keychain;
// move to the keychain if tokens ever grant more than opening tunnels.
func save(c Credentials) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(c)
	return os.WriteFile(p, b, 0o600)
}

func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "portmole", "credentials.json"), nil
}

func post(ctx context.Context, url string, in, out any) (int, error) {
	var body io.Reader = strings.NewReader("")
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Error bodies are JSON too; a body that is not is reported by status alone.
	json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(out)
	return resp.StatusCode, nil
}
