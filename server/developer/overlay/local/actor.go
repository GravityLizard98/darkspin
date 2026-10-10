// Package local adapts the in-process account, game, and item registries to
// the debug overlay.
package local

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/developer/overlay"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type actorSource struct {
	userManager *sporenet.UserManager
	gameManager *game.Manager
}

// NewActorSource resolves overlay actors from active users and their games.
func NewActorSource(
	userManager *sporenet.UserManager, gameManager *game.Manager,
) overlay.ActorSource {
	return actorSource{userManager: userManager, gameManager: gameManager}
}

// Actor reads one active account. Only an authenticated user with a login
// token counts as online; the token is reduced to its SHA-256 digest here.
func (e actorSource) Actor(ctx context.Context, accountID int64) (overlay.Actor, error) {
	if ctx == nil {
		return overlay.Actor{}, errors.New("overlay actor requires context")
	}
	err := ctx.Err()
	if err != nil {
		return overlay.Actor{}, fmt.Errorf("actorContext: %w", err)
	}
	if e.userManager == nil || accountID <= 0 {
		return overlay.Actor{}, fmt.Errorf("actorRequest: %w", overlay.ErrActorOffline)
	}
	user := e.userManager.UserByID(accountID)
	if user == nil {
		return overlay.Actor{}, fmt.Errorf("actorLookup: %w", overlay.ErrActorOffline)
	}
	view := user.View()
	// An empty display name fails every chat operation with
	// ErrSenderNotMember, so such an account cannot act through the overlay.
	if view.AuthToken == "" || view.DisplayName == "" || view.Account.ID != accountID {
		return overlay.Actor{}, fmt.Errorf("actorSession: %w", overlay.ErrActorOffline)
	}
	actor := overlay.Actor{
		ID: accountID, DisplayName: view.DisplayName, GameID: user.CurrentGameID(),
		SessionDigest: sha256.Sum256([]byte(view.AuthToken)),
		Level:         view.Account.Level, DNA: view.Account.DNA,
	}
	if e.gameManager == nil {
		return actor, nil
	}
	pendingWarp, isWarpFound := e.gameManager.CampaignWarp(accountID)
	if isWarpFound {
		actor.PendingWarp = pendingWarp
	}
	if actor.GameID == 0 {
		return actor, nil
	}
	instance := e.gameManager.Game(actor.GameID)
	if instance == nil {
		return actor, nil
	}
	actor.IsGameFound = true
	actor.Game = overlay.GameFacts{
		Mode:        overlayMode(instance.Info.Mode),
		IsWarped:    instance.IsWarped(),
		PlayerCount: len(instance.Players()),
	}
	return actor, nil
}

// overlayMode maps the mode set at game creation, which gameplay bindings copy
// at join.
func overlayMode(mode game.Mode) overlay.Mode {
	switch mode {
	case game.ModeChain:
		return overlay.ModeChain
	case game.ModeTutorial:
		return overlay.ModeTutorial
	case game.ModeArena:
		return overlay.ModeArena
	default:
		return overlay.ModeUnknown
	}
}

var _ overlay.ActorSource = actorSource{}
