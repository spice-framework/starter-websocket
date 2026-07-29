package websocket

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	nativews "github.com/coder/websocket"
)

const (
	defaultMaxMessageBytes = int64(1 << 20)
	maxMessageBytes        = int64(16 << 20)
	defaultMaxConnections  = 256
	maxConnections         = 4096
	maxOriginPatterns      = 32
	maxSubprotocols        = 32
	maxHeaderBytes         = 8 << 10
	maxIdentityBytes       = 255
	defaultCompressionAt   = 512
	defaultCloseTimeout    = 5 * time.Second
	maxCloseTimeout        = 30 * time.Second
)

var tokenPattern = regexp.MustCompile(
	"^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$",
)

// ServerConfig defines one explicit WebSocket HTTP upgrade boundary. TLS is
// required by default and is terminated by the caller-owned HTTP server.
type ServerConfig struct {
	OriginPatterns  []string
	Subprotocols    []string
	MaxMessageBytes int64
	MaxConnections  int
	Compression     bool
	CompressionAt   int
	CloseTimeout    time.Duration
	AllowInsecure   bool
	AllowAnyOrigin  bool
}

// ClientConfig defines one explicit outbound WebSocket connection.
type ClientConfig struct {
	URL             string
	Header          http.Header
	Subprotocols    []string
	TLSConfig       *tls.Config
	MaxMessageBytes int64
	Compression     bool
	AllowInsecure   bool
}

type normalizedServerConfig struct {
	accept         nativews.AcceptOptions
	maxMessage     int64
	maxConnections int
	closeTimeout   time.Duration
	allowInsecure  bool
}

type normalizedClientConfig struct {
	dial       nativews.DialOptions
	url        string
	maxMessage int64
}

func normalizeServerConfig(
	config ServerConfig,
) (normalizedServerConfig, error) {
	maximum, err := normalizeMessageLimit(config.MaxMessageBytes)
	if err != nil {
		return normalizedServerConfig{}, fmt.Errorf(
			"construct WebSocket handler: %w",
			err,
		)
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = defaultMaxConnections
	}
	if config.MaxConnections < 1 ||
		config.MaxConnections > maxConnections {
		return normalizedServerConfig{}, fmt.Errorf(
			"construct WebSocket handler: max connections must be between 1 and %d",
			maxConnections,
		)
	}
	origins, err := normalizeOrigins(
		config.OriginPatterns,
		config.AllowAnyOrigin,
	)
	if err != nil {
		return normalizedServerConfig{}, err
	}
	subprotocols, err := normalizeSubprotocols(config.Subprotocols)
	if err != nil {
		return normalizedServerConfig{}, err
	}
	compression, threshold, err := normalizeCompression(
		config.Compression,
		config.CompressionAt,
	)
	if err != nil {
		return normalizedServerConfig{}, err
	}
	if config.CloseTimeout == 0 {
		config.CloseTimeout = defaultCloseTimeout
	}
	if config.CloseTimeout < time.Millisecond ||
		config.CloseTimeout > maxCloseTimeout {
		return normalizedServerConfig{}, fmt.Errorf(
			"construct WebSocket handler: close timeout must be between 1ms and %s",
			maxCloseTimeout,
		)
	}
	return normalizedServerConfig{
		accept: nativews.AcceptOptions{
			Subprotocols:         subprotocols,
			InsecureSkipVerify:   config.AllowAnyOrigin,
			OriginPatterns:       origins,
			CompressionMode:      compression,
			CompressionThreshold: threshold,
		},
		maxMessage:     maximum,
		maxConnections: config.MaxConnections,
		closeTimeout:   config.CloseTimeout,
		allowInsecure:  config.AllowInsecure,
	}, nil
}

func normalizeClientConfig(
	config ClientConfig,
) (normalizedClientConfig, error) {
	target, parsed, err := normalizeURL(config.URL, config.AllowInsecure)
	if err != nil {
		return normalizedClientConfig{}, err
	}
	maximum, err := normalizeMessageLimit(config.MaxMessageBytes)
	if err != nil {
		return normalizedClientConfig{}, fmt.Errorf(
			"construct WebSocket client: %w",
			err,
		)
	}
	subprotocols, err := normalizeSubprotocols(config.Subprotocols)
	if err != nil {
		return normalizedClientConfig{}, fmt.Errorf(
			"construct WebSocket client: %w",
			err,
		)
	}
	headers, err := normalizeHeaders(config.Header)
	if err != nil {
		return normalizedClientConfig{}, err
	}
	tlsSelection, err := normalizeClientTLS(config.TLSConfig, parsed)
	if err != nil {
		return normalizedClientConfig{}, err
	}
	compression := nativews.CompressionDisabled
	if config.Compression {
		compression = nativews.CompressionNoContextTakeover
	}
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return normalizedClientConfig{}, errors.New(
			"construct WebSocket client: default HTTP transport is unavailable",
		)
	}
	transport := defaultTransport.Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = tlsSelection.config
	return normalizedClientConfig{
		dial: nativews.DialOptions{
			HTTPClient: &http.Client{
				Transport: transport,
			},
			HTTPHeader:      headers,
			Subprotocols:    subprotocols,
			CompressionMode: compression,
		},
		url:        target,
		maxMessage: maximum,
	}, nil
}

func normalizeMessageLimit(limit int64) (int64, error) {
	if limit == 0 {
		return defaultMaxMessageBytes, nil
	}
	if limit < 1 || limit > maxMessageBytes {
		return 0, fmt.Errorf(
			"max message bytes must be between 1 and %d",
			maxMessageBytes,
		)
	}
	return limit, nil
}

func normalizeOrigins(
	patterns []string,
	allowAny bool,
) ([]string, error) {
	if allowAny && len(patterns) != 0 {
		return nil, errors.New(
			"construct WebSocket handler: explicit origins and allow-any-origin are mutually exclusive",
		)
	}
	if len(patterns) > maxOriginPatterns {
		return nil, fmt.Errorf(
			"construct WebSocket handler: origin patterns must not exceed %d",
			maxOriginPatterns,
		)
	}
	normalized := append([]string(nil), patterns...)
	slices.Sort(normalized)
	for index, pattern := range normalized {
		if pattern == "*" ||
			pattern == "" ||
			len(pattern) > maxIdentityBytes ||
			strings.TrimSpace(pattern) != pattern ||
			strings.ContainsAny(pattern, "\x00\r\n\t ") {
			return nil, errors.New(
				"construct WebSocket handler: origin patterns must be exact safe host patterns",
			)
		}
		if _, err := path.Match(pattern, pattern); err != nil {
			return nil, errors.New(
				"construct WebSocket handler: origin pattern is malformed",
			)
		}
		if index > 0 && pattern == normalized[index-1] {
			return nil, fmt.Errorf(
				"construct WebSocket handler: duplicate origin pattern %q",
				pattern,
			)
		}
	}
	return normalized, nil
}

func normalizeSubprotocols(protocols []string) ([]string, error) {
	if len(protocols) > maxSubprotocols {
		return nil, fmt.Errorf(
			"WebSocket subprotocols must not exceed %d",
			maxSubprotocols,
		)
	}
	normalized := append([]string(nil), protocols...)
	for index, protocol := range normalized {
		if len(protocol) > maxIdentityBytes ||
			!tokenPattern.MatchString(protocol) {
			return nil, errors.New(
				"WebSocket subprotocols must be portable HTTP tokens",
			)
		}
		for prior := range index {
			if protocol == normalized[prior] {
				return nil, fmt.Errorf(
					"duplicate WebSocket subprotocol %q",
					protocol,
				)
			}
		}
	}
	return normalized, nil
}

func normalizeCompression(
	enabled bool,
	threshold int,
) (nativews.CompressionMode, int, error) {
	if !enabled {
		if threshold != 0 {
			return nativews.CompressionDisabled, 0, errors.New(
				"construct WebSocket handler: compression threshold requires compression",
			)
		}
		return nativews.CompressionDisabled, 0, nil
	}
	if threshold == 0 {
		threshold = defaultCompressionAt
	}
	if threshold < 256 || int64(threshold) > maxMessageBytes {
		return nativews.CompressionDisabled, 0, fmt.Errorf(
			"construct WebSocket handler: compression threshold must be between 256 and %d",
			maxMessageBytes,
		)
	}
	return nativews.CompressionNoContextTakeover, threshold, nil
}

func normalizeURL(
	target string,
	allowInsecure bool,
) (string, *url.URL, error) {
	if target == "" || strings.TrimSpace(target) != target {
		return "", nil, errors.New(
			"construct WebSocket client: URL is required",
		)
	}
	parsed, err := url.Parse(target)
	if err != nil ||
		parsed.User != nil ||
		parsed.Fragment != "" ||
		parsed.Host == "" {
		return "", nil, errors.New(
			"construct WebSocket client: URL must be an exact ws/wss endpoint without credentials or fragment",
		)
	}
	if err := validateWebSocketScheme(
		parsed.Scheme,
		allowInsecure,
	); err != nil {
		return "", nil, err
	}
	if err := validateURLHost(parsed.Host); err != nil {
		return "", nil, err
	}
	return parsed.String(), parsed, nil
}

func validateWebSocketScheme(
	scheme string,
	allowInsecure bool,
) error {
	if scheme != "wss" && scheme != "ws" {
		return errors.New(
			"construct WebSocket client: wss is required unless insecure local development is explicit",
		)
	}
	if scheme == "ws" && !allowInsecure {
		return errors.New(
			"construct WebSocket client: wss is required unless insecure local development is explicit",
		)
	}
	if scheme == "wss" && allowInsecure {
		return errors.New(
			"construct WebSocket client: insecure mode is valid only for ws URLs",
		)
	}
	return nil
}

func validateURLHost(hostPort string) error {
	host, portText, err := net.SplitHostPort(hostPort)
	if err != nil || host == "" {
		return errors.New(
			"construct WebSocket client: URL host must include an exact port",
		)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return errors.New(
			"construct WebSocket client: URL port must be between 1 and 65535",
		)
	}
	return nil
}

type normalizedTLS struct {
	config *tls.Config
}

func normalizeClientTLS(
	source *tls.Config,
	target *url.URL,
) (normalizedTLS, error) {
	if target.Scheme == "ws" {
		if source != nil {
			return normalizedTLS{}, errors.New(
				"construct WebSocket client: TLS configuration cannot be used with ws",
			)
		}
		return normalizedTLS{}, nil
	}
	var cloned *tls.Config
	if source == nil {
		cloned = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: target.Hostname(),
		}
	} else {
		cloned = source.Clone()
	}
	if cloned.InsecureSkipVerify {
		return normalizedTLS{}, errors.New(
			"construct WebSocket client: TLS certificate verification is required",
		)
	}
	if cloned.MinVersion == 0 {
		cloned.MinVersion = tls.VersionTLS12
	}
	if cloned.MinVersion < tls.VersionTLS12 {
		return normalizedTLS{}, errors.New(
			"construct WebSocket client: TLS 1.2 or newer is required",
		)
	}
	return normalizedTLS{config: cloned}, nil
}

func normalizeHeaders(source http.Header) (http.Header, error) {
	normalized := make(http.Header, len(source))
	total := 0
	for name, values := range source {
		canonical := http.CanonicalHeaderKey(name)
		if canonical == "" ||
			canonical != name ||
			isReservedHeader(canonical) {
			return nil, errors.New(
				"construct WebSocket client: headers must use canonical non-WebSocket names",
			)
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\x00\r\n") {
				return nil, errors.New(
					"construct WebSocket client: header value is invalid",
				)
			}
			total += len(canonical) + len(value)
			if total > maxHeaderBytes {
				return nil, fmt.Errorf(
					"construct WebSocket client: headers exceed %d bytes",
					maxHeaderBytes,
				)
			}
			normalized[canonical] = append(
				normalized[canonical],
				value,
			)
		}
	}
	return normalized, nil
}

func isReservedHeader(name string) bool {
	switch name {
	case "Connection", "Host", "Origin", "Sec-Websocket-Accept",
		"Sec-Websocket-Extensions", "Sec-Websocket-Key",
		"Sec-Websocket-Protocol", "Sec-Websocket-Version", "Upgrade":
		return true
	default:
		return false
	}
}

func validateObservers(observers []Observer) error {
	for index, observer := range observers {
		if nilInterface(observer) {
			return fmt.Errorf(
				"construct WebSocket integration: observer %d is nil",
				index,
			)
		}
	}
	return nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() { //nolint:exhaustive // Only nil-capable kinds require handling.
	case reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
