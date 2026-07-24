package workbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBootstrapAuthorityConsumesNonceBeforeReturningBearer(t *testing.T) {
	now := time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC)
	nonce := strings.Repeat("n", 43)
	bearer := strings.Repeat("b", 43)
	values := []string{nonce, bearer}
	authority, issued, err := newBootstrapAuthority(func() time.Time { return now }, time.Minute, func() (string, error) {
		value := values[0]
		values = values[1:]
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if issued != nonce {
		t.Fatalf("issued nonce = %q, want %q", issued, nonce)
	}

	minted, err := authority.consume(nonce)
	if err != nil {
		t.Fatal(err)
	}
	if minted != bearer || minted == nonce {
		t.Fatalf("minted bearer = %q, want distinct %q", minted, bearer)
	}
	if !authority.authorizes("Bearer " + bearer) {
		t.Fatal("minted bearer was not authorized")
	}
	if authority.authorizes("Bearer " + nonce) {
		t.Fatal("bootstrap nonce was accepted as a bearer")
	}
	if _, err := authority.consume(nonce); !errors.Is(err, ErrBootstrapDenied) {
		t.Fatalf("replayed nonce error = %v, want denial", err)
	}
}

func TestBootstrapAuthorityRejectsExpiredAndWrongNonces(t *testing.T) {
	now := time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC)
	valid := strings.Repeat("n", 43)
	bearer := strings.Repeat("b", 43)
	values := []string{valid, bearer}
	authority, _, err := newBootstrapAuthority(func() time.Time { return now }, time.Minute, func() (string, error) {
		value := values[0]
		values = values[1:]
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.consume(strings.Repeat("x", 43)); !errors.Is(err, ErrBootstrapDenied) {
		t.Fatalf("wrong nonce error = %v, want denial", err)
	}
	if _, err := authority.consume(valid); err != nil {
		t.Fatalf("valid nonce after wrong attempt = %v", err)
	}
	values = []string{valid, bearer}
	expiring, _, err := newBootstrapAuthority(func() time.Time { return now }, time.Minute, func() (string, error) {
		value := values[0]
		values = values[1:]
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute + time.Nanosecond)
	if _, err := expiring.consume(valid); !errors.Is(err, ErrBootstrapDenied) {
		t.Fatalf("expired nonce error = %v, want denial", err)
	}
}

func TestBootstrapRouteRequiresExactOriginAndIssuesDistinctBearer(t *testing.T) {
	authority, nonce, err := newBootstrapAuthority(time.Now, time.Minute, NewBearerToken)
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://127.0.0.1:43117"
	handler, err := NewAPI(APIOptions{Bootstrap: authority, AllowedOrigin: origin})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"nonce":"`+nonce+`"}`))
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), nonce) {
		t.Fatalf("bootstrap response reflected nonce: %q", response.Body.String())
	}
	var bootstrap struct {
		Bearer string `json:"bearer"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &bootstrap); err != nil {
		t.Fatal(err)
	}
	if len(bootstrap.Bearer) != 43 || bootstrap.Bearer == nonce {
		t.Fatalf("bootstrap bearer = %q, want a distinct 256-bit token", bootstrap.Bearer)
	}
	for _, check := range []struct {
		name   string
		token  string
		status int
	}{
		{name: "nonce is not a bearer", token: nonce, status: http.StatusUnauthorized},
		{name: "minted bearer is accepted", token: bootstrap.Bearer, status: http.StatusServiceUnavailable},
	} {
		t.Run(check.name, func(t *testing.T) {
			health := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			health.Host = "127.0.0.1:43117"
			health.Header.Set("Authorization", "Bearer "+check.token)
			healthResponse := httptest.NewRecorder()
			handler.ServeHTTP(healthResponse, health)
			if healthResponse.Code != check.status {
				t.Fatalf("health status=%d want=%d body=%q", healthResponse.Code, check.status, healthResponse.Body.String())
			}
		})
	}

	replay := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"nonce":"`+nonce+`"}`))
	replay.Host = "127.0.0.1:43117"
	replay.Header.Set("Origin", origin)
	replay.Header.Set("Content-Type", "application/json")
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusUnauthorized {
		t.Fatalf("replay status=%d body=%q", replayResponse.Code, replayResponse.Body.String())
	}

	foreignAuthority, foreignNonce, err := newBootstrapAuthority(time.Now, time.Minute, NewBearerToken)
	if err != nil {
		t.Fatal(err)
	}
	foreignHandler, err := NewAPI(APIOptions{Bootstrap: foreignAuthority, AllowedOrigin: origin})
	if err != nil {
		t.Fatal(err)
	}
	foreign := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"nonce":"`+foreignNonce+`"}`))
	foreign.Host = "127.0.0.1:43117"
	foreign.Header.Set("Origin", "http://localhost:43117")
	foreign.Header.Set("Content-Type", "application/json")
	foreignResponse := httptest.NewRecorder()
	foreignHandler.ServeHTTP(foreignResponse, foreign)
	if foreignResponse.Code != http.StatusForbidden {
		t.Fatalf("foreign bootstrap status=%d body=%q", foreignResponse.Code, foreignResponse.Body.String())
	}
}
