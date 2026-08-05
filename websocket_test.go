package websocket

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	nativews "github.com/coder/websocket"
	spicestarter "github.com/spice-framework/spice/annotation/sdk/starter"
)

func TestHandlerAndClientExchangeBoundedMessages(t *testing.T) {
	t.Parallel()
	observer := &recordingObserver{}
	handler, err := NewHandler(
		ServerConfig{
			AllowInsecure:  true,
			AllowAnonymous: true,
			Subprotocols:   []string{"petclinic.v1"},
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
	server := httptest.NewServer(handler)
	defer server.Close()
	connection, response, cleanup, err := Dial(
		context.Background(),
		ClientConfig{
			URL:             websocketURL(server.URL),
			AllowInsecure:   true,
			AllowAnonymous:  true,
			Subprotocols:    []string{"petclinic.v1"},
			MaxMessageBytes: 1024,
		},
		observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer closeHTTPResponse(t, response)
	if response.StatusCode != http.StatusSwitchingProtocols ||
		connection.Subprotocol() != "petclinic.v1" {
		t.Fatalf(
			"response=%d subprotocol=%q",
			response.StatusCode,
			connection.Subprotocol(),
		)
	}
	if writeErr := connection.Write(
		context.Background(),
		MessageText,
		[]byte("owner updated"),
	); writeErr != nil {
		t.Fatal(writeErr)
	}
	messageType, payload, err := connection.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if messageType != MessageText || string(payload) != "owner updated" {
		t.Fatalf("message type=%d payload=%q", messageType, payload)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("second cleanup error = %v", err)
	}
	waitForResults(t, observer, 2)
	results := observer.Results()
	if !containsResult(results, DirectionClient, OutcomeNormal) ||
		!containsResult(results, DirectionServer, OutcomeNormal) {
		t.Fatalf("results = %#v", results)
	}
	if handler.Active() != 0 {
		t.Fatalf("active sessions = %d", handler.Active())
	}
}

func TestHandlerRequiresTLSAndRejectsCrossOrigin(t *testing.T) {
	t.Parallel()
	secureHandler, err := NewHandler(
		ServerConfig{AllowAnonymous: true},
		func(context.Context, *Connection) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.test/ws", nil)
	response := httptest.NewRecorder()
	secureHandler.ServeHTTP(response, request)
	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("insecure response = %d", response.Code)
	}

	handler, err := NewHandler(
		ServerConfig{AllowInsecure: true, AllowAnonymous: true},
		func(context.Context, *Connection) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	nonLoopbackRequest := httptest.NewRequest(http.MethodGet, "http://example.test/ws", nil)
	nonLoopbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(nonLoopbackResponse, nonLoopbackRequest)
	if nonLoopbackResponse.Code != http.StatusUpgradeRequired {
		t.Fatalf("non-loopback insecure response = %d", nonLoopbackResponse.Code)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	_, handshake, err := nativews.Dial(
		context.Background(),
		websocketURL(server.URL),
		&nativews.DialOptions{
			HTTPHeader: http.Header{
				"Origin": []string{"https://attacker.example"},
			},
		},
	)
	if err == nil {
		t.Fatal("cross-origin Dial() error = nil")
	}
	if handshake == nil || handshake.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin response = %#v", handshake)
	}
	defer closeHTTPResponse(t, handshake)
}

func TestHandlerEnforcesConnectionCapacity(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	release := make(chan struct{})
	handler, err := NewHandler(
		ServerConfig{
			AllowInsecure:  true,
			AllowAnonymous: true,
			MaxConnections: 1,
		},
		func(context.Context, *Connection) error {
			close(entered)
			<-release
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	first, firstResponse, firstCleanup, err := Dial(
		context.Background(),
		ClientConfig{
			URL:            websocketURL(server.URL),
			AllowInsecure:  true,
			AllowAnonymous: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer closeHTTPResponse(t, firstResponse)
	<-entered
	_, response, _, err := Dial(
		context.Background(),
		ClientConfig{
			URL:            websocketURL(server.URL),
			AllowInsecure:  true,
			AllowAnonymous: true,
		},
	)
	if err == nil {
		t.Fatal("capacity Dial() error = nil")
	}
	if response == nil ||
		response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("capacity response = %#v", response)
	}
	defer closeHTTPResponse(t, response)
	close(release)
	if err := firstCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if first == nil {
		t.Fatal("first connection = nil")
	}
}

func TestConnectionRejectsInvalidAndOversizedWrites(t *testing.T) {
	t.Parallel()
	handler, err := NewHandler(
		ServerConfig{
			AllowInsecure:   true,
			AllowAnonymous:  true,
			MaxMessageBytes: 4,
		},
		func(ctx context.Context, connection *Connection) error {
			_, _, readErr := connection.Read(ctx)
			return readErr
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	connection, response, cleanup, err := Dial(
		context.Background(),
		ClientConfig{
			URL:             websocketURL(server.URL),
			AllowInsecure:   true,
			AllowAnonymous:  true,
			MaxMessageBytes: 4,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer closeHTTPResponse(t, response)
	if err := connection.Write(
		context.Background(),
		MessageText,
		[]byte("12345"),
	); err == nil {
		t.Fatal("oversized Write() error = nil")
	}
	if err := connection.Write(
		context.Background(),
		MessageType(99),
		nil,
	); err == nil {
		t.Fatal("invalid type Write() error = nil")
	}
	if err := connection.Write(
		nilContext(),
		MessageText,
		nil,
	); err == nil {
		t.Fatal("nil context Write() error = nil")
	}
	if _, _, err := connection.Read(nilContext()); err == nil {
		t.Fatal("nil context Read() error = nil")
	}
	if err := connection.Ping(nilContext()); err == nil {
		t.Fatal("nil context Ping() error = nil")
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	serverTests := []ServerConfig{
		{},
		{AllowAnonymous: true, AllowAnyOrigin: true, OriginPatterns: []string{"example.test"}},
		{AllowAnonymous: true, OriginPatterns: []string{"*"}},
		{AllowAnonymous: true, OriginPatterns: []string{"["}},
		{AllowAnonymous: true, Subprotocols: []string{"bad protocol"}},
		{AllowAnonymous: true, Subprotocols: []string{"same", "same"}},
		{AllowAnonymous: true, MaxMessageBytes: maxMessageBytes + 1},
		{AllowAnonymous: true, MaxConnections: maxConnections + 1},
		{AllowAnonymous: true, CompressionAt: 512},
		{AllowAnonymous: true, Compression: true, CompressionAt: 10},
		{AllowAnonymous: true, CloseTimeout: maxCloseTimeout + time.Second},
		{AllowAnonymous: true, AllowAnyOrigin: true},
		{AllowAnonymous: true, Authenticate: func(context.Context, *http.Request) error { return nil }},
	}
	for index, config := range serverTests {
		if _, err := NewHandler(
			config,
			func(context.Context, *Connection) error { return nil },
		); err == nil {
			t.Fatalf("NewHandler(test %d) error = nil", index)
		}
	}
	if _, err := NewHandler(
		ServerConfig{AllowInsecure: true, AllowAnonymous: true},
		nil,
	); err == nil {
		t.Fatal("NewHandler(nil session handler) error = nil")
	}
	var typedNil *recordingObserver
	if _, err := NewHandler(
		ServerConfig{AllowInsecure: true, AllowAnonymous: true},
		func(context.Context, *Connection) error { return nil },
		typedNil,
	); err == nil {
		t.Fatal("NewHandler(typed nil observer) error = nil")
	}

	clientTests := []ClientConfig{
		{URL: "wss://example.test:443/ws"},
		{URL: "http://example.test:443/ws", AllowAnonymous: true},
		{URL: "ws://example.test:80/ws", AllowInsecure: true, AllowAnonymous: true},
		{
			URL:            "wss://example.test:443/ws",
			AllowInsecure:  true,
			AllowAnonymous: true,
		},
		{
			URL: "ws://example.test:80/ws",
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			AllowInsecure:  true,
			AllowAnonymous: true,
		},
		{
			URL: "wss://example.test:443/ws",
			TLSConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, //nolint:gosec // Rejection fixture.
			},
			AllowAnonymous: true,
		},
		{
			URL: "wss://example.test:443/ws",
			Header: http.Header{
				"Origin": []string{"https://example.test"},
			},
			AllowAnonymous: true,
		},
		{
			URL: "wss://example.test:443/ws",
			Header: http.Header{
				"Authorization": []string{"Bearer hidden"},
			},
			AllowAnonymous: true,
		},
		{
			URL: "wss://example.test:443/ws",
			Header: http.Header{
				"Cookie": []string{"session=hidden"},
			},
			AllowAnonymous: true,
		},
		{URL: "wss://example.test:443/ws", AllowAnonymous: true, Authorization: "Bearer secret"},
		{URL: "wss://example.test:443/ws", HandshakeTimeout: maxHandshakeTimeout + time.Second, AllowAnonymous: true},
	}
	for index, config := range clientTests {
		if _, err := normalizeClientConfig(config); err == nil {
			t.Fatalf("normalizeClientConfig(test %d) error = nil", index)
		}
	}
}

func TestClientDefaultsAreSecureAndDefensive(t *testing.T) {
	t.Parallel()
	headers := http.Header{
		"X-Request-Id": []string{"request-1"},
	}
	normalized, err := normalizeClientConfig(ClientConfig{
		URL:           "wss://service.example:443/events",
		Header:        headers,
		Authorization: "Bearer test",
	})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := normalized.dial.HTTPClient.Transport.(*http.Transport)
	if !ok ||
		transport.Proxy != nil ||
		transport.TLSClientConfig == nil ||
		transport.TLSClientConfig.MinVersion != tls.VersionTLS12 ||
		transport.TLSClientConfig.ServerName != "service.example" ||
		normalized.maxMessage != defaultMaxMessageBytes {
		t.Fatalf("normalized client = %#v", normalized)
	}
	headers.Set("X-Request-Id", "changed")
	if normalized.dial.HTTPHeader.Get("Authorization") != "Bearer test" {
		t.Fatal("client authorization was not installed")
	}
	if normalized.dial.HTTPHeader.Get("X-Request-Id") != "request-1" {
		t.Fatal("client headers were not defensively copied")
	}
	sourceTLS := &tls.Config{MinVersion: tls.VersionTLS13}
	selection, err := normalizeClientTLS(sourceTLS, &url.URL{
		Scheme: "wss",
		Host:   "service.example:443",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.config == sourceTLS ||
		selection.config.MinVersion != tls.VersionTLS13 {
		t.Fatal("TLS configuration was not defensively cloned")
	}
	origins := make([]string, maxOriginPatterns+1)
	for index := range origins {
		origins[index] = fmt.Sprintf("host-%d.example", index)
	}
	if _, err := normalizeOrigins(origins, false); err == nil {
		t.Fatal("normalizeOrigins(too many) error = nil")
	}
	if _, _, err := normalizeCompression(true, 0); err != nil {
		t.Fatal(err)
	}
	largeHeaders := http.Header{
		"X-Large": []string{strings.Repeat("x", maxHeaderBytes)},
	}
	if _, err := normalizeHeaders(largeHeaders); err == nil {
		t.Fatal("normalizeHeaders(oversized) error = nil")
	}
	//nolint:bodyclose // Nil context returns before an HTTP request or response exists.
	if _, _, _, err := Dial(nilContext(), ClientConfig{}); err == nil {
		t.Fatal("Dial(nil context) error = nil")
	}
}

func TestConnectionCloseValidationAndCancellation(t *testing.T) {
	t.Parallel()
	if err := (*Connection)(nil).Close(
		context.Background(),
		StatusNormalClosure,
		"",
	); err == nil {
		t.Fatal("nil connection Close() error = nil")
	}
	if (*Connection)(nil).Subprotocol() != "" {
		t.Fatal("nil connection Subprotocol() was not empty")
	}
	if err := (*Connection)(nil).Ping(
		context.Background(),
	); err == nil {
		t.Fatal("nil connection Ping() error = nil")
	}
	connection := &Connection{}
	if err := connection.Close(
		context.Background(),
		StatusNormalClosure,
		"",
	); err == nil {
		t.Fatal("invalid connection Close() error = nil")
	}
	handler, err := NewHandler(
		ServerConfig{AllowInsecure: true, AllowAnonymous: true},
		func(ctx context.Context, connection *Connection) error {
			_, _, readErr := connection.Read(ctx)
			return readErr
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, response, _, err := Dial(
		context.Background(),
		ClientConfig{
			URL:            websocketURL(server.URL),
			AllowInsecure:  true,
			AllowAnonymous: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer closeHTTPResponse(t, response)
	readResult := make(chan error, 1)
	go func() {
		_, _, readErr := client.Read(context.Background())
		readResult <- readErr
	}()
	pingContext, pingCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	if err := client.Ping(pingContext); err != nil {
		t.Fatal(err)
	}
	pingCancel()
	if err := client.Close(
		context.Background(),
		StatusCode(999),
		"",
	); err == nil {
		t.Fatal("invalid status Close() error = nil")
	}
	if err := client.Close(
		context.Background(),
		StatusNormalClosure,
		strings.Repeat("x", 124),
	); err == nil {
		t.Fatal("long reason Close() error = nil")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Close(
		canceled,
		StatusGoingAway,
		"",
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Close() error = %v", err)
	}
	if err := <-readResult; err == nil {
		t.Fatal("Read(after close) error = nil")
	}
}

func TestServerOutcomeRecognizesRedactedNormalPeerClose(t *testing.T) {
	t.Parallel()
	for _, status := range []StatusCode{StatusNormalClosure, StatusGoingAway} {
		outcome, closeStatus := serverOutcome(
			context.Background(),
			&PeerCloseError{Status: status},
		)
		if outcome != OutcomeNormal || closeStatus != StatusNormalClosure {
			t.Fatalf(
				"serverOutcome(status %d) = (%q, %d)",
				status,
				outcome,
				closeStatus,
			)
		}
	}
}

func TestManifest(t *testing.T) {
	t.Parallel()
	manifest := Manifest()
	spec := manifest.Spec()
	if spec.Schema != spicestarter.Schema ||
		spec.ID != "github.com/spice-framework/starter-websocket" ||
		spec.Module != "github.com/spice-framework/starter-websocket" ||
		spec.SpiceAPI != spicestarter.APIVersion ||
		spec.Review != "docs/dependency-review.md" ||
		!slices.Equal(spec.Capabilities, []string{
			"web.websocket.client",
			"web.websocket.server",
		}) ||
		len(spec.Dependencies) != 1 ||
		spec.Dependencies[0].Version != "v1.8.15" {
		t.Fatalf("Manifest() = %#v", spec)
	}
	if err := manifest.Compatible(spicestarter.APIVersion, "go1.26.5"); err != nil {
		t.Fatalf("Compatible() error = %v", err)
	}
	content, err := manifest.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	parsed, err := spicestarter.Parse(content)
	if err != nil {
		t.Fatalf("Parse(JSON()) error = %v", err)
	}
	if parsed.Spec().ID != spec.ID {
		t.Fatalf("parsed ID = %q, want %q", parsed.Spec().ID, spec.ID)
	}
}

func websocketURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ws"
}

type recordingObserver struct {
	mu      sync.Mutex
	results []Result
}

func (observer *recordingObserver) BeginSession(
	_ context.Context,
	_ Interaction,
) func(Result) {
	return func(result Result) {
		observer.mu.Lock()
		observer.results = append(observer.results, result)
		observer.mu.Unlock()
	}
}

func (observer *recordingObserver) Results() []Result {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return append([]Result(nil), observer.results...)
}

func containsResult(
	results []Result,
	direction Direction,
	outcome Outcome,
) bool {
	return slices.ContainsFunc(results, func(result Result) bool {
		return result.Interaction.Direction == direction &&
			result.Outcome == outcome &&
			result.Duration >= 0
	})
}

func waitForResults(
	t *testing.T,
	observer *recordingObserver,
	count int,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(observer.Results()) < count {
		if time.Now().After(deadline) {
			t.Fatalf("observation count = %d", len(observer.Results()))
		}
		time.Sleep(time.Millisecond)
	}
}

func nilContext() context.Context {
	return nil
}

func closeHTTPResponse(t *testing.T, response *http.Response) {
	t.Helper()
	if response == nil || response.Body == nil {
		return
	}
	if err := response.Body.Close(); err != nil {
		t.Errorf("close HTTP response: %v", err)
	}
}
