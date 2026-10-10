// Package overlay owns the development debug overlay: /debug session keys,
// the live state view, the item and command catalog, and typed actions that
// run the same chat operations as the developer slash commands.
package overlay

import (
	"context"
	"errors"
)

var (
	// ErrActorOffline means the account is not an active authenticated user.
	ErrActorOffline = errors.New("overlay actor offline")
	// ErrSessionExpired means the key is unknown, malformed, or no longer bound
	// to the account's current login token.
	ErrSessionExpired = errors.New("overlay session expired")
	// ErrDisabled means [developer] is_overlay_enabled is false.
	ErrDisabled = errors.New("overlay disabled")
)

// Mode is the overlay-owned projection of the actor's game mode.
type Mode uint8

const (
	ModeUnknown Mode = iota
	ModeChain
	ModeTutorial
	ModeArena
)

// GameFacts describes the actor's current game instance.
type GameFacts struct {
	Mode        Mode
	PlayerCount int
	IsWarped    bool
}

// Actor is one online account resolved for the overlay. SessionDigest is the
// SHA-256 of the account's login token; the token itself never enters the
// feature.
type Actor struct {
	ID            int64
	DisplayName   string
	GameID        uint32
	SessionDigest [32]byte
	Level         uint32
	DNA           uint32
	PendingWarp   string
	Game          GameFacts
	IsGameFound   bool
}

// ActorSource resolves an online account into the facts the overlay needs.
type ActorSource interface {
	// Actor returns ErrActorOffline when the account is not active.
	Actor(ctx context.Context, accountID int64) (Actor, error)
}

// StateRequest identifies the actor whose gameplay session is inspected.
type StateRequest struct {
	ActorID int64
	GameID  uint32
}

// Hero is the actor's controlled hero in the selected gameplay session.
type Hero struct {
	ObjectID          uint32
	X                 float32
	Y                 float32
	Z                 float32
	HitPoint          float32
	HitPointMaximum   float32
	PowerPoint        float32
	PowerPointMaximum float32
	IsDeployed        bool
}

// NPC is one living hostile near the actor's hero.
type NPC struct {
	ObjectID        uint32
	Name            string
	HitPoint        float32
	HitPointMaximum float32
	Distance        float32
	Direction       string
}

// ActionState is the advisory availability of one action kind.
type ActionState struct {
	Reason      Reason
	IsAvailable bool
}

// GameState is the actor's live gameplay view. IsHeroFound is false when the
// game has no gameplay session for the actor yet.
type GameState struct {
	Hero          Hero
	AliveNPCCount int
	NearestNPCs   []NPC
	ActionStates  map[ActionKind]ActionState
	IsHeroFound   bool
}

// StateSource reads the actor's live gameplay state without mutating it.
type StateSource interface {
	OverlayState(ctx context.Context, req StateRequest) (GameState, error)
}

// Rigblock is one summonable base item.
type Rigblock struct {
	ID           uint16
	Name         string
	Slot         string
	Classes      []string
	Sciences     []string
	MinimumLevel uint32
	MaximumLevel uint32
	IsUnique     bool
}

// Affix is one summonable prefix or suffix.
type Affix struct {
	ID              uint16
	Name            string
	PartTypes       []string
	Classes         []string
	Sciences        []string
	MinimumLevel    uint32
	MaximumLevel    uint32
	IsBasicEligible bool
	IsUnique        bool
}

// ItemCatalog lists the summonable items in catalog order. An empty Name is
// replaced by a "#<id> <slot>" label.
type ItemCatalog struct {
	Rigblocks []Rigblock
	Prefixes  []Affix
	Suffixes  []Affix
}

// ItemSource lists summonable items for the catalog.
type ItemSource interface {
	Items(ctx context.Context) (ItemCatalog, error)
}
