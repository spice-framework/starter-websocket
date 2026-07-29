package websocket

import (
	"context"
	"slices"
	"time"
)

// Direction identifies an inbound or outbound WebSocket session.
type Direction string

const (
	// DirectionClient identifies an outbound session.
	DirectionClient Direction = "client"
	// DirectionServer identifies an inbound session.
	DirectionServer Direction = "server"
)

// Interaction contains payload-free session facts.
type Interaction struct {
	Direction   Direction
	Subprotocol string
}

// Outcome identifies a payload-free terminal session state.
type Outcome string

const (
	// OutcomeNormal identifies an ordinary completed or peer-closed session.
	OutcomeNormal Outcome = "normal"
	// OutcomeCanceled identifies caller or lifecycle cancellation.
	OutcomeCanceled Outcome = "canceled"
	// OutcomeFailed identifies an application or protocol failure.
	OutcomeFailed Outcome = "failed"
)

// Result describes one completed session.
type Result struct {
	Interaction Interaction
	Outcome     Outcome
	Duration    time.Duration
}

// Observer receives session begin/end facts without message payloads,
// headers, URLs, peer addresses, or close reasons.
type Observer interface {
	BeginSession(context.Context, Interaction) func(Result)
}

func beginObservers(
	ctx context.Context,
	interaction Interaction,
	observers []Observer,
) func(Result) {
	finishers := make([]func(Result), 0, len(observers))
	for _, observer := range observers {
		if finish := observer.BeginSession(ctx, interaction); finish != nil {
			finishers = append(finishers, finish)
		}
	}
	return func(result Result) {
		for _, finish := range slices.Backward(finishers) {
			finish(result)
		}
	}
}
