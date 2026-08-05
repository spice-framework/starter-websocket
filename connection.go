package websocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	nativews "github.com/coder/websocket"
)

// MessageType identifies a text or binary WebSocket message.
type MessageType int

const (
	// MessageText identifies a UTF-8 text message.
	MessageText MessageType = 1
	// MessageBinary identifies a binary message.
	MessageBinary MessageType = 2
)

// StatusCode identifies the limited close outcomes applications may emit.
type StatusCode int

const (
	// StatusNormalClosure indicates completed application work.
	StatusNormalClosure StatusCode = 1000
	// StatusGoingAway indicates lifecycle or caller cancellation.
	StatusGoingAway StatusCode = 1001
	// StatusPolicyViolation indicates rejected application input.
	StatusPolicyViolation StatusCode = 1008
	// StatusInternalError indicates an application handler failure.
	StatusInternalError StatusCode = 1011
)

// PeerCloseError reports a peer's close status without retaining its
// potentially sensitive close reason.
type PeerCloseError struct {
	Status StatusCode
}

// Error implements error.
func (closeError *PeerCloseError) Error() string {
	return fmt.Sprintf(
		"WebSocket peer closed with status %d",
		closeError.Status,
	)
}

// Connection is one bounded, caller-owned WebSocket connection.
type Connection struct {
	native     *nativews.Conn
	maxMessage int64
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeErr   error
}

func newConnection(
	native *nativews.Conn,
	maximum int64,
) *Connection {
	native.SetReadLimit(maximum)
	return &Connection{
		native:     native,
		maxMessage: maximum,
		closeDone:  make(chan struct{}),
	}
}

// Subprotocol returns the negotiated application protocol.
func (connection *Connection) Subprotocol() string {
	if connection == nil || connection.native == nil {
		return ""
	}
	return connection.native.Subprotocol()
}

// Read reads one complete bounded message. Canceling the context closes the
// underlying connection, matching the native library's explicit contract.
func (connection *Connection) Read(
	ctx context.Context,
) (MessageType, []byte, error) {
	if ctx == nil {
		return 0, nil, errors.New("read WebSocket message: context is nil")
	}
	if connection == nil || connection.native == nil {
		return 0, nil, errors.New("read WebSocket message: connection is invalid")
	}
	messageType, payload, err := connection.native.Read(ctx)
	if err != nil {
		return 0, nil, sanitizeConnectionError(
			"read WebSocket message",
			ctx,
			err,
		)
	}
	return MessageType(messageType), payload, nil
}

// Write sends one complete bounded text or binary message.
func (connection *Connection) Write(
	ctx context.Context,
	messageType MessageType,
	payload []byte,
) error {
	if ctx == nil {
		return errors.New("write WebSocket message: context is nil")
	}
	if connection == nil || connection.native == nil {
		return errors.New("write WebSocket message: connection is invalid")
	}
	if messageType != MessageText && messageType != MessageBinary {
		return errors.New("write WebSocket message: type must be text or binary")
	}
	if int64(len(payload)) > connection.maxMessage {
		return fmt.Errorf(
			"write WebSocket message: payload exceeds %d bytes",
			connection.maxMessage,
		)
	}
	if err := connection.native.Write(
		ctx,
		nativews.MessageType(messageType),
		payload,
	); err != nil {
		return sanitizeConnectionError("write WebSocket message", ctx, err)
	}
	return nil
}

// Ping verifies that the peer responds using the caller-owned context.
func (connection *Connection) Ping(ctx context.Context) error {
	if ctx == nil {
		return errors.New("ping WebSocket: context is nil")
	}
	if connection == nil || connection.native == nil {
		return errors.New("ping WebSocket: connection is invalid")
	}
	if err := connection.native.Ping(ctx); err != nil {
		return sanitizeConnectionError("ping WebSocket", ctx, err)
	}
	return nil
}

// Close performs one close handshake. Cancellation force-closes the socket and
// returns the caller's cause.
func (connection *Connection) Close(
	ctx context.Context,
	status StatusCode,
	reason string,
) error {
	if ctx == nil {
		return errors.New("close WebSocket: context is nil")
	}
	if connection == nil || connection.native == nil {
		return errors.New("close WebSocket: connection is invalid")
	}
	if !validStatus(status) {
		return errors.New("close WebSocket: unsupported status code")
	}
	if len(reason) > 123 || strings.ContainsAny(reason, "\x00\r\n") {
		return errors.New("close WebSocket: reason must contain at most 123 safe bytes")
	}
	if cause := context.Cause(ctx); cause != nil {
		forceErr := connection.closeNow()
		return errors.Join(
			fmt.Errorf("close WebSocket: %w", cause),
			forceErr,
		)
	}
	connection.closeOnce.Do(func() {
		go func() {
			if err := connection.native.Close(
				nativews.StatusCode(status),
				reason,
			); err != nil && !errors.Is(err, net.ErrClosed) {
				connection.closeErr = sanitizeConnectionError(
					"close WebSocket handshake",
					context.Background(),
					err,
				)
			}
			close(connection.closeDone)
		}()
	})
	select {
	case <-connection.closeDone:
		return connection.closeErr
	case <-ctx.Done():
		forceErr := connection.native.CloseNow()
		<-connection.closeDone
		if forceErr != nil {
			forceErr = fmt.Errorf(
				"close WebSocket: force close: %w",
				forceErr,
			)
		}
		return errors.Join(
			fmt.Errorf("close WebSocket: %w", context.Cause(ctx)),
			forceErr,
		)
	}
}

func sanitizeConnectionError(
	operation string,
	ctx context.Context,
	err error,
) error {
	if cause := context.Cause(ctx); cause != nil {
		return fmt.Errorf("%s: %w", operation, cause)
	}
	if status := nativews.CloseStatus(err); status != -1 {
		return &PeerCloseError{Status: StatusCode(status)}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func (connection *Connection) closeNow() error {
	if connection != nil && connection.native != nil {
		if err := connection.native.CloseNow(); err != nil {
			return fmt.Errorf("force close WebSocket: %w", err)
		}
	}
	return nil
}

func validStatus(status StatusCode) bool {
	switch status {
	case StatusNormalClosure, StatusGoingAway,
		StatusPolicyViolation, StatusInternalError:
		return true
	default:
		return false
	}
}
