package websocket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	nativews "github.com/coder/websocket"

	"github.com/spice-framework/spice/lifecycle"
)

// Dial opens one explicit outbound WebSocket connection.
func Dial(
	ctx context.Context,
	config ClientConfig,
	observers ...Observer,
) (*Connection, *http.Response, lifecycle.Cleanup, error) {
	if ctx == nil {
		return nil, nil, nil, errors.New(
			"dial WebSocket: context is nil",
		)
	}
	normalized, err := normalizeClientConfig(config)
	if err != nil {
		return nil, nil, nil, err
	}
	if observerErr := validateObservers(observers); observerErr != nil {
		return nil, nil, nil, observerErr
	}
	native, response, err := nativews.Dial(
		ctx,
		normalized.url,
		&normalized.dial,
	)
	if err != nil {
		return nil, response, nil, errors.New(
			"dial WebSocket: handshake failed",
		)
	}
	connection := newConnection(native, normalized.maxMessage)
	interaction := Interaction{
		Direction:   DirectionClient,
		Subprotocol: connection.Subprotocol(),
	}
	finish := beginObservers(ctx, interaction, observers)
	started := time.Now()
	var finishOnce sync.Once
	cleanup := func(cleanupContext context.Context) error {
		if cleanupContext == nil {
			return errors.New("close WebSocket client: context is nil")
		}
		closeErr := connection.Close(
			cleanupContext,
			StatusNormalClosure,
			"",
		)
		outcome := OutcomeNormal
		if closeErr != nil {
			outcome = OutcomeFailed
			if context.Cause(cleanupContext) != nil {
				outcome = OutcomeCanceled
			}
		}
		finishOnce.Do(func() {
			finish(Result{
				Interaction: interaction,
				Outcome:     outcome,
				Duration:    time.Since(started),
			})
		})
		if closeErr != nil {
			return fmt.Errorf("close WebSocket client: %w", closeErr)
		}
		return nil
	}
	return connection, response, cleanup, nil
}
