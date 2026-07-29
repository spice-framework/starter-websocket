// Package websocket provides reviewed, bounded RFC 6455 server and client
// integration for Spice applications.
package websocket

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	nativews "github.com/coder/websocket"
)

// SessionHandler owns one accepted session until it returns.
type SessionHandler func(context.Context, *Connection) error

// Handler is an instance-owned HTTP WebSocket upgrade boundary.
type Handler struct {
	config    normalizedServerConfig
	handle    SessionHandler
	observers []Observer
	slots     chan struct{}
	active    atomic.Int64
}

// NewHandler constructs a WebSocket HTTP handler without starting a listener
// or goroutine.
func NewHandler(
	config ServerConfig,
	handle SessionHandler,
	observers ...Observer,
) (*Handler, error) {
	if handle == nil {
		return nil, errors.New(
			"construct WebSocket handler: session handler is nil",
		)
	}
	normalized, err := normalizeServerConfig(config)
	if err != nil {
		return nil, err
	}
	if err := validateObservers(observers); err != nil {
		return nil, err
	}
	return &Handler{
		config:    normalized,
		handle:    handle,
		observers: append([]Observer(nil), observers...),
		slots:     make(chan struct{}, normalized.maxConnections),
	}, nil
}

// Active returns the number of currently accepted sessions.
func (handler *Handler) Active() int64 {
	if handler == nil {
		return 0
	}
	return handler.active.Load()
}

// ServeHTTP upgrades and owns one bounded session. TLS termination remains the
// caller-owned HTTP server's responsibility.
func (handler *Handler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if handler == nil || request == nil {
		http.Error(writer, "WebSocket handler unavailable", http.StatusServiceUnavailable)
		return
	}
	if request.TLS == nil && !handler.config.allowInsecure {
		http.Error(writer, "secure WebSocket transport required", http.StatusUpgradeRequired)
		return
	}
	select {
	case handler.slots <- struct{}{}:
		defer func() { <-handler.slots }()
	default:
		http.Error(writer, "WebSocket capacity exhausted", http.StatusServiceUnavailable)
		return
	}
	native, err := nativews.Accept(writer, request, &handler.config.accept)
	if err != nil {
		return
	}
	connection := newConnection(native, handler.config.maxMessage)
	handler.active.Add(1)
	defer handler.active.Add(-1)
	interaction := Interaction{
		Direction:   DirectionServer,
		Subprotocol: connection.Subprotocol(),
	}
	ctx := request.Context()
	finish := beginObservers(
		request.Context(),
		interaction,
		handler.observers,
	)
	started := time.Now()
	handlerErr := handler.handle(ctx, connection)
	outcome, status := serverOutcome(ctx, handlerErr)
	closeContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		handler.config.closeTimeout,
	)
	if err := connection.Close(closeContext, status, ""); err != nil {
		if forceErr := connection.closeNow(); forceErr != nil {
			outcome = OutcomeFailed
		}
	}
	cancel()
	finish(Result{
		Interaction: interaction,
		Outcome:     outcome,
		Duration:    time.Since(started),
	})
}

func serverOutcome(
	ctx context.Context,
	handlerErr error,
) (Outcome, StatusCode) {
	if context.Cause(ctx) != nil {
		return OutcomeCanceled, StatusGoingAway
	}
	if handlerErr == nil {
		return OutcomeNormal, StatusNormalClosure
	}
	status := nativews.CloseStatus(handlerErr)
	if status == nativews.StatusNormalClosure ||
		status == nativews.StatusGoingAway {
		return OutcomeNormal, StatusNormalClosure
	}
	return OutcomeFailed, StatusInternalError
}

var _ http.Handler = (*Handler)(nil)
