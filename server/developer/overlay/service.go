package overlay

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"

	"github.com/darkspinnet/darkspin/server/chat"
)

// Options wires the overlay feature. ChatService runs actions through the
// same operations the slash commands call.
type Options struct {
	ChatService *chat.Service
	ActorSource ActorSource
	StateSource StateSource
	ItemSource  ItemSource
	BuildID     string
	Version     string
	IsEnabled   bool
}

// Service binds /debug keys and serves the overlay state, catalog, and
// actions for the bound account.
type Service struct {
	chatService  *chat.Service
	actorSource  ActorSource
	stateSource  StateSource
	itemSource   ItemSource
	buildID      string
	version      string
	isEnabled    bool
	sessionStore *sessionStore
	catalogMutex sync.Mutex
	catalog      *catalogIndex
}

// NewService creates the overlay feature. A disabled service answers every
// bind with chat.ErrOverlayUnavailable and serves nothing.
func NewService(options Options) *Service {
	return &Service{
		chatService: options.ChatService, actorSource: options.ActorSource,
		stateSource: options.StateSource, itemSource: options.ItemSource,
		buildID: options.BuildID, version: options.Version,
		isEnabled: options.IsEnabled, sessionStore: newSessionStore(),
	}
}

// State is the overlay view of one bound actor. ActionStates contains every
// action kind.
type State struct {
	BuildID      string
	Version      string
	Actor        Actor
	Game         GameState
	ActionStates map[ActionKind]ActionState
}

// BindOverlay binds a /debug key to the sender's current login token. It is
// accepted only from a loopback Blaze peer, and a newer key of the same
// account replaces the older one.
func (e *Service) BindOverlay(ctx context.Context, req chat.OverlayBindCommand) error {
	if !e.isEnabled {
		return fmt.Errorf("bindEnabled: %w", chat.ErrOverlayUnavailable)
	}
	if !req.IsLocalPeer {
		return fmt.Errorf("bindPeer: %w", chat.ErrOverlayPeerRemote)
	}
	if req.Key == "" {
		return fmt.Errorf("bindKey: %w", chat.ErrOverlayKeyMissing)
	}
	if !isValidKey(req.Key) {
		return fmt.Errorf("bindKeyFormat: %w", chat.ErrOverlayKeyInvalid)
	}
	if e.actorSource == nil {
		return fmt.Errorf("bindActorSource: %w", chat.ErrOverlayUnavailable)
	}
	actor, err := e.actorSource.Actor(ctx, req.Sender.ID)
	if errors.Is(err, ErrActorOffline) {
		return fmt.Errorf("bindActor: %w", chat.ErrSenderNotMember)
	}
	if err != nil {
		return fmt.Errorf("bindActorResolve: %w", err)
	}
	e.sessionStore.bind(digestKey(req.Key), sessionBinding{
		accountID: actor.ID, sessionDigest: actor.SessionDigest,
	})
	return nil
}

// Authenticate resolves the account bound to key. The binding ends when the
// account is no longer active or its login token changed.
func (e *Service) Authenticate(ctx context.Context, key string) (Actor, error) {
	if !e.isEnabled {
		return Actor{}, fmt.Errorf("authenticateEnabled: %w", ErrDisabled)
	}
	if !isValidKey(key) {
		return Actor{}, fmt.Errorf("authenticateKey: %w", ErrSessionExpired)
	}
	digest := digestKey(key)
	binding, isFound := e.sessionStore.lookup(digest)
	if !isFound {
		return Actor{}, fmt.Errorf("authenticateBinding: %w", ErrSessionExpired)
	}
	if e.actorSource == nil {
		return Actor{}, errors.New("authenticate actor source unavailable")
	}
	actor, err := e.actorSource.Actor(ctx, binding.accountID)
	if errors.Is(err, ErrActorOffline) {
		e.sessionStore.remove(digest, binding)
		return Actor{}, fmt.Errorf("authenticateActor: %w", ErrSessionExpired)
	}
	if err != nil {
		return Actor{}, fmt.Errorf("authenticateActorResolve: %w", err)
	}
	if subtle.ConstantTimeCompare(actor.SessionDigest[:], binding.sessionDigest[:]) != 1 {
		e.sessionStore.remove(digest, binding)
		return Actor{}, fmt.Errorf("authenticateToken: %w", ErrSessionExpired)
	}
	return actor, nil
}

// State returns the actor's account, game, and live gameplay view.
func (e *Service) State(ctx context.Context, actor Actor) (State, error) {
	if !e.isEnabled {
		return State{}, fmt.Errorf("stateEnabled: %w", ErrDisabled)
	}
	gameState, err := e.gameState(ctx, actor)
	if err != nil {
		return State{}, fmt.Errorf("stateGame: %w", err)
	}
	return State{
		BuildID: e.buildID, Version: e.version, Actor: actor, Game: gameState,
		ActionStates: actionStates(actor, gameState),
	}, nil
}

func (e *Service) gameState(ctx context.Context, actor Actor) (GameState, error) {
	if !actor.IsGameFound || e.stateSource == nil {
		return GameState{NearestNPCs: []NPC{}}, nil
	}
	gameState, err := e.stateSource.OverlayState(ctx, StateRequest{
		ActorID: actor.ID, GameID: actor.GameID,
	})
	if err != nil {
		return GameState{}, fmt.Errorf("gameStateRead: %w", err)
	}
	if gameState.NearestNPCs == nil {
		gameState.NearestNPCs = []NPC{}
	}
	return gameState, nil
}

// actionStates completes the gameplay availability: account actions need no
// game, and an in-game action the source did not report fails closed.
func actionStates(actor Actor, gameState GameState) map[ActionKind]ActionState {
	kinds := ActionKinds()
	states := make(map[ActionKind]ActionState, len(kinds))
	for _, kind := range kinds {
		if isAccountAction(kind) {
			states[kind] = ActionState{IsAvailable: true}
			continue
		}
		if !actor.IsGameFound {
			states[kind] = ActionState{Reason: ReasonNoGame}
			continue
		}
		state, isFound := gameState.ActionStates[kind]
		if !isFound || (!gameState.IsHeroFound && state.IsAvailable) {
			state = ActionState{Reason: ReasonNotDeployed}
		}
		states[kind] = state
	}
	return states
}
