package websocket

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nativews "github.com/coder/websocket"
)

func TestTLSAuthenticatedConcurrentSessionsAndCleanup(t *testing.T) {
	t.Parallel()
	const (
		sessionCount  = 16
		authorization = "Bearer local-test-token"
	)
	observer := &recordingObserver{}
	var authenticationCalls atomic.Int64
	handler, err := NewHandler(
		ServerConfig{
			Authenticate: func(_ context.Context, request *http.Request) error {
				authenticationCalls.Add(1)
				if request.Header.Get("Authorization") != authorization {
					return errors.New("invalid credential")
				}
				if request.TLS == nil || request.TLS.Version < tls.VersionTLS12 {
					return errors.New("invalid transport")
				}
				return nil
			},
			Subprotocols:    []string{"spice.test.v1"},
			MaxMessageBytes: 256,
			MaxConnections:  sessionCount,
			CloseTimeout:    2 * time.Second,
		},
		func(ctx context.Context, connection *Connection) error {
			messageType, payload, readErr := connection.Read(ctx)
			if readErr != nil {
				return readErr
			}
			return connection.Write(ctx, messageType, payload)
		},
		observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if authenticationCalls.Load() != 0 {
		t.Fatal("NewHandler performed authentication or network work")
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	clientTLS := localTLSConfig(t, server)

	results := make(chan error, sessionCount)
	var sessions sync.WaitGroup
	for index := range sessionCount {
		sessions.Go(func() {
			message := fmt.Appendf(nil, "session-%d", index)
			connection, response, cleanup, dialErr := Dial(
				context.Background(),
				ClientConfig{
					URL:             websocketURL(server.URL),
					TLSConfig:       clientTLS,
					Authorization:   authorization,
					Subprotocols:    []string{"spice.test.v1"},
					MaxMessageBytes: 256,
				},
				observer,
			)
			if dialErr != nil {
				results <- dialErr
				return
			}
			if response == nil || response.StatusCode != http.StatusSwitchingProtocols {
				results <- fmt.Errorf("handshake response = %#v", response)
				return
			}
			defer closeHTTPResponse(t, response)
			if writeErr := connection.Write(context.Background(), MessageText, message); writeErr != nil {
				results <- writeErr
				return
			}
			messageType, payload, readErr := connection.Read(context.Background())
			if readErr != nil {
				results <- readErr
				return
			}
			if messageType != MessageText || string(payload) != string(message) {
				results <- fmt.Errorf("echo type=%d payload=%q", messageType, payload)
				return
			}
			if cleanupErr := cleanup(context.Background()); cleanupErr != nil {
				results <- cleanupErr
				return
			}
			if cleanupErr := cleanup(context.Background()); cleanupErr != nil {
				results <- fmt.Errorf("idempotent cleanup: %w", cleanupErr)
				return
			}
			results <- nil
		})
	}
	sessions.Wait()
	close(results)
	for result := range results {
		if result != nil {
			t.Fatal(result)
		}
	}
	waitForResults(t, observer, sessionCount*2)
	if authenticationCalls.Load() != sessionCount {
		t.Fatalf("authentication calls = %d, want %d", authenticationCalls.Load(), sessionCount)
	}
	if handler.Active() != 0 {
		t.Fatalf("active sessions = %d", handler.Active())
	}
}

func TestTLSAuthenticationFailureDoesNotExposeCause(t *testing.T) {
	t.Parallel()
	const secret = "credential database says token-super-secret expired"
	handler, err := NewHandler(
		ServerConfig{
			Authenticate: func(context.Context, *http.Request) error {
				return errors.New(secret)
			},
		},
		func(context.Context, *Connection) error {
			return errors.New("must not run")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	_, response, _, err := Dial(context.Background(), ClientConfig{
		URL:           websocketURL(server.URL),
		TLSConfig:     localTLSConfig(t, server),
		Authorization: "Bearer rejected",
	})
	if err == nil {
		t.Fatal("Dial() error = nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Dial() error exposed authentication cause: %v", err)
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("authentication response = %#v", response)
	}
	closeHTTPResponse(t, response)
}

func TestTLSClientDoesNotFollowRedirectsOrForwardAuthorization(t *testing.T) {
	t.Parallel()
	var targetCalls atomic.Int64
	target := httptest.NewTLSServer(http.HandlerFunc(func(
		_ http.ResponseWriter,
		request *http.Request,
	) {
		targetCalls.Add(1)
		if authorization := request.Header.Get("Authorization"); authorization != "" {
			t.Errorf("redirect target received authorization %q", authorization)
		}
	}))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, response, _, err := Dial(context.Background(), ClientConfig{
		URL:           websocketURL(redirect.URL),
		TLSConfig:     localTLSConfig(t, redirect),
		Authorization: "Bearer redirect-secret",
	})
	if err == nil {
		t.Fatal("Dial(redirect) error = nil")
	}
	if response == nil || response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("redirect response = %#v", response)
	}
	closeHTTPResponse(t, response)
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target calls = %d", targetCalls.Load())
	}
}

func TestTLSHandshakeTimeoutPreservesDeadline(t *testing.T) {
	t.Parallel()
	handlerStarted := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(
		_ http.ResponseWriter,
		request *http.Request,
	) {
		close(handlerStarted)
		<-request.Context().Done()
	}))
	defer server.Close()
	_, response, _, err := Dial(context.Background(), ClientConfig{
		URL:              websocketURL(server.URL),
		TLSConfig:        localTLSConfig(t, server),
		AllowAnonymous:   true,
		HandshakeTimeout: 500 * time.Millisecond,
	})
	if response != nil {
		closeHTTPResponse(t, response)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Dial() error = %v, want deadline exceeded", err)
	}
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("TLS handshake request did not reach local server")
	}
}

func TestPeerCloseReasonIsRedacted(t *testing.T) {
	t.Parallel()
	const secret = "payload-secret-must-not-escape"
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		connection, err := nativews.Accept(writer, request, nil)
		if err != nil {
			return
		}
		if closeErr := connection.Close(nativews.StatusPolicyViolation, secret); closeErr != nil {
			t.Errorf("close local WebSocket: %v", closeErr)
		}
	}))
	defer server.Close()
	connection, response, cleanup, err := Dial(context.Background(), ClientConfig{
		URL:            websocketURL(server.URL),
		AllowInsecure:  true,
		AllowAnonymous: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	closeHTTPResponse(t, response)
	_, _, err = connection.Read(context.Background())
	var closeError *PeerCloseError
	if !errors.As(err, &closeError) || closeError.Status != StatusPolicyViolation {
		t.Fatalf("Read() error = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Read() error exposed peer close reason: %v", err)
	}
	writeErr := connection.Write(context.Background(), MessageText, []byte("after-close"))
	if writeErr != nil && strings.Contains(writeErr.Error(), secret) {
		t.Fatalf("Write() error exposed peer close reason: %v", writeErr)
	}
	if cleanupErr := cleanup(context.Background()); cleanupErr != nil {
		t.Fatalf("cleanup after peer close: %v", cleanupErr)
	}
}

func localTLSConfig(t *testing.T, server *httptest.Server) *tls.Config {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
}
