// Copyright (c) 2015-2026 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestForwarderCharacterization(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://backend.internal/a%2Fb?prefix=one%2Ftwo&limit=10", nil)
	req.Host = "storage.example:9443"
	req.RemoteAddr = "192.0.2.44:43210"
	req.Header.Set("X-Test-Header", "preserved")

	got, recorder := forwardAndCapture(t, req, false)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if got.URL.Scheme != "http" || got.URL.Host != "backend.internal" {
		t.Fatalf("outbound URL authority = %s://%s, want http://backend.internal", got.URL.Scheme, got.URL.Host)
	}
	if got.URL.EscapedPath() != "/a%2Fb" {
		t.Fatalf("outbound escaped path = %q, want %q", got.URL.EscapedPath(), "/a%2Fb")
	}
	if got.URL.RawQuery != "prefix=one%2Ftwo&limit=10" {
		t.Fatalf("outbound raw query = %q", got.URL.RawQuery)
	}
	if got.RequestURI != "" {
		t.Fatalf("outbound RequestURI = %q, want empty", got.RequestURI)
	}
	if got.Host != "backend.internal" {
		t.Fatalf("outbound Host = %q, want backend.internal", got.Host)
	}
	if got.ProtoMajor != 1 || got.ProtoMinor != 1 {
		t.Fatalf("outbound protocol = %s, want HTTP/1.1", got.Proto)
	}
	if got.Header.Get("X-Test-Header") != "preserved" {
		t.Fatalf("ordinary header = %q, want preserved", got.Header.Get("X-Test-Header"))
	}
}

func TestForwarderPassHost(t *testing.T) {
	for _, test := range []struct {
		name     string
		passHost bool
		wantHost string
	}{
		{name: "target host", wantHost: "backend.internal"},
		{name: "original host", passHost: true, wantHost: "storage.example:9443"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://backend.internal/object", nil)
			req.Host = "storage.example:9443"
			got, _ := forwardAndCapture(t, req, test.passHost)
			if got.Host != test.wantHost {
				t.Fatalf("outbound Host = %q, want %q", got.Host, test.wantHost)
			}
		})
	}
}

func TestForwarderContext(t *testing.T) {
	type contextKey struct{}
	const marker = "request-context"

	for _, test := range []struct {
		method    string
		wantValue bool
	}{
		{method: http.MethodGet},
		{method: http.MethodPost, wantValue: true},
	} {
		t.Run(test.method, func(t *testing.T) {
			req := httptest.NewRequest(test.method, "http://backend.internal/object", nil)
			req = req.WithContext(context.WithValue(req.Context(), contextKey{}, marker))
			got, _ := forwardAndCapture(t, req, false)
			if value := got.Context().Value(contextKey{}); (value == marker) != test.wantValue {
				t.Fatalf("outbound context value = %v, want value present = %t", value, test.wantValue)
			}
		})
	}
}

func TestForwarderUpgrade(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://backend.internal/socket", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")

	got, _ := forwardAndCapture(t, req, false)
	if !strings.EqualFold(got.Header.Get("Connection"), "Upgrade") {
		t.Fatalf("outbound Connection = %q, want Upgrade", got.Header.Get("Connection"))
	}
	if got.Header.Get("Upgrade") != "websocket" {
		t.Fatalf("outbound Upgrade = %q, want websocket", got.Header.Get("Upgrade"))
	}
}

func TestForwarderErrorHandler(t *testing.T) {
	wantErr := errors.New("backend unavailable")
	var handled error
	forwarder := NewForwarder(&Forwarder{
		RoundTripper: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, wantErr
		}),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			handled = err
			w.WriteHeader(http.StatusServiceUnavailable)
		},
	})
	recorder := httptest.NewRecorder()
	forwarder.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://backend.internal/object", nil))

	if !errors.Is(handled, wantErr) {
		t.Fatalf("handled error = %v, want %v", handled, wantErr)
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestForwarderRejectsSpoofedForwardingHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://backend.internal:9000/object", nil)
	req.Host = "storage.example:9443"
	req.RemoteAddr = "192.0.2.44:43210"
	req.Header.Set("Connection", "X-Forwarded-Host, X-Real-IP")
	req.Header.Set("Forwarded", "for=203.0.113.66;proto=https")
	req.Header.Set(xForwardedFor, "203.0.113.66")
	req.Header.Set(xForwardedHost, "attacker.invalid")
	req.Header.Set(xForwardedPort, "443")
	req.Header.Set(xForwardedProto, "https")
	req.Header.Set(xRealIP, "203.0.113.66")

	got, _ := forwardAndCapture(t, req, false)
	wantHeaders := map[string]string{
		xForwardedFor:   "192.0.2.44",
		xForwardedHost:  "storage.example:9443",
		xForwardedPort:  "9443",
		xForwardedProto: "http",
		xRealIP:         "192.0.2.44",
	}
	for name, want := range wantHeaders {
		if value := got.Header.Get(name); value != want {
			t.Errorf("outbound %s = %q, want %q", name, value, want)
		}
	}
	if value := got.Header.Get("Forwarded"); value != "" {
		t.Errorf("outbound Forwarded = %q, want empty", value)
	}
}

func forwardAndCapture(t *testing.T, req *http.Request, passHost bool) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	var captured *http.Request
	forwarder := NewForwarder(&Forwarder{
		PassHost: passHost,
		RoundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			captured = req
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Status:     http.StatusText(http.StatusNoContent),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	})
	recorder := httptest.NewRecorder()
	forwarder.ServeHTTP(recorder, req)
	if captured == nil {
		t.Fatal("forwarder did not call RoundTripper")
	}
	return captured, recorder
}
