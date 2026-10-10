package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/developer/overlay"
	"github.com/darkspinnet/darkspin/server/game"
)

// overlayNPCLimit bounds the nearest-hostile list of one overlay state poll.
const overlayNPCLimit = 16

type overlayStateProvider interface {
	OverlayState(context.Context, overlay.StateRequest) (overlay.GameState, error)
}

// OverlayState reads the debug overlay view through the provider already
// registered by NewHandler. It never mutates gameplay state.
func (e Lifecycle) OverlayState(
	ctx context.Context, req overlay.StateRequest,
) (overlay.GameState, error) {
	provider, isSupported := e.syncSnapshot.(overlayStateProvider)
	if !isSupported {
		return overlay.GameState{NearestNPCs: []overlay.NPC{}}, nil
	}
	gameState, err := provider.OverlayState(ctx, req)
	if err != nil {
		return overlay.GameState{}, fmt.Errorf("overlayGameplay: %w", err)
	}
	return gameState, nil
}

// OverlayState selects the actor's session in one read-locked pass. Without a
// session the hero stays absent and the feature reports in-game actions as
// not deployed.
func (e *gameplaySessionRegistry) OverlayState(
	ctx context.Context, req overlay.StateRequest,
) (overlay.GameState, error) {
	if ctx == nil {
		return overlay.GameState{}, errors.New("overlay state requires context")
	}
	err := ctx.Err()
	if err != nil {
		return overlay.GameState{}, fmt.Errorf("overlayContext: %w", err)
	}
	gameState := overlay.GameState{NearestNPCs: []overlay.NPC{}}
	if e == nil || req.ActorID <= 0 || req.GameID == 0 {
		return gameState, nil
	}
	for !e.mutex.TryRLock() {
		select {
		case <-ctx.Done():
			return overlay.GameState{}, fmt.Errorf("overlayLock: %w", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	defer e.mutex.RUnlock()
	peerSession, isFound := e.overlaySession(req)
	if !isFound {
		return gameState, nil
	}
	gameState.IsHeroFound = true
	gameState.Hero = overlayHero(&peerSession)
	gameState.AliveNPCCount, gameState.NearestNPCs = overlayNearestNPCs(&peerSession)
	gameState.ActionStates = overlayActionStates(&peerSession)
	return gameState, nil
}

// overlaySession prefers a deployed session when a rejoin leaves more than
// one session bound to the actor's game.
func (e *gameplaySessionRegistry) overlaySession(
	req overlay.StateRequest,
) (gameplayPeerSession, bool) {
	selected := gameplayPeerSession{}
	isSelected := false
	for _, peerSession := range e.sessions {
		if peerSession.binding.UserID != uint64(req.ActorID) ||
			peerSession.binding.GameID != req.GameID {
			continue
		}
		if peerSession.deployedObjectID != 0 && !peerSession.isRejoinPending {
			return peerSession, true
		}
		if !isSelected {
			selected = peerSession
			isSelected = true
		}
	}
	return selected, isSelected
}

// overlayHero reads the /stat values, except in Arena, where it uses the
// maxima the resource actions apply (deployedResourceMaximum).
func overlayHero(peerSession *gameplayPeerSession) overlay.Hero {
	hero := overlay.Hero{
		ObjectID: peerSession.deployedObjectID,
		X:        peerSession.playerPosition.X,
		Y:        peerSession.playerPosition.Y,
		Z:        peerSession.playerPosition.Z,
		IsDeployed: peerSession.stage.IsDungeon() &&
			peerSession.deployedObjectID != 0,
	}
	if peerSession.deployedObjectID == 0 || peerSession.squad == nil {
		return hero
	}
	creatureIndex := peerSession.deployedCreatureIndex
	hero.HitPoint = peerSession.deployedHitPoint()
	hero.HitPointMaximum = peerSession.characterHitPointMaximum(creatureIndex)
	hero.PowerPoint = peerSession.deployedManaPoint()
	hero.PowerPointMaximum = peerSession.characterManaPointMaximum(creatureIndex)
	if peerSession.binding.Mode != game.ModeArena {
		return hero
	}
	hitPointMaximum, powerPointMaximum, err := peerSession.deployedResourceMaximum()
	if err != nil {
		// Arena maxima are not initialized yet. Keep the /stat values.
		return hero
	}
	hero.HitPointMaximum = hitPointMaximum
	hero.PowerPointMaximum = powerPointMaximum
	return hero
}

type overlayNPCDistance struct {
	index           int
	distanceSquared float32
}

// overlayNearestNPCs applies the /hint filter and returns the living hostile
// count plus the nearest hostiles by planar distance.
func overlayNearestNPCs(peerSession *gameplayPeerSession) (int, []overlay.NPC) {
	npcs := make([]overlay.NPC, 0, overlayNPCLimit)
	if peerSession.zone == nil {
		return 0, npcs
	}
	npcSession := peerSession.zone.NPCs()
	if npcSession == nil {
		return 0, npcs
	}
	playerPosition := game.Vec3(peerSession.playerPosition)
	snapshots := npcSession.Snapshots()
	distances := make([]overlayNPCDistance, 0, len(snapshots))
	for index := range snapshots {
		if isHintExcluded(&snapshots[index]) {
			continue
		}
		deltaX := snapshots[index].Plan.Position.X - playerPosition.X
		deltaY := snapshots[index].Plan.Position.Y - playerPosition.Y
		distances = append(distances, overlayNPCDistance{
			index: index, distanceSquared: deltaX*deltaX + deltaY*deltaY,
		})
	}
	slices.SortStableFunc(distances, compareOverlayNPCDistance)
	for _, distance := range distances[:min(len(distances), overlayNPCLimit)] {
		npc := snapshots[distance.index]
		npcs = append(npcs, overlay.NPC{
			ObjectID:        npc.Plan.ObjectID,
			Name:            hintNPCName(*peerSession, npc),
			HitPoint:        npc.HitPoint,
			HitPointMaximum: npc.Plan.NPCProfile.HitPoint,
			Distance:        float32(math.Sqrt(float64(distance.distanceSquared))),
			Direction:       hintDirection(playerPosition, npc.Plan.Position),
		})
	}
	return len(distances), npcs
}

func compareOverlayNPCDistance(first overlayNPCDistance, second overlayNPCDistance) int {
	if first.distanceSquared < second.distanceSquared {
		return -1
	}
	if first.distanceSquared > second.distanceSquared {
		return 1
	}
	return 0
}

// overlayActionStates reports in-game action availability with the same
// predicates the queued-command consumers apply. A mode gate takes precedence
// over deployment, so a mode that never admits an action says so.
func overlayActionStates(
	peerSession *gameplayPeerSession,
) map[overlay.ActionKind]overlay.ActionState {
	isEventEligible := peerSession.isEventCommandEligible()
	isResourceEligible := peerSession.isResourceCommandEligible()
	isChainMode := peerSession.binding.Mode == game.ModeChain
	isEventReady := isEventEligible && peerSession.isDeveloperEventApplicable()
	resourceState := overlayActionState(peerSession, true, isResourceEligible)
	eventState := overlayActionState(peerSession, isChainMode, isEventReady)
	return map[overlay.ActionKind]overlay.ActionState{
		overlay.ActionSpawn: overlaySpawnState(peerSession, isEventEligible),
		overlay.ActionDrop: overlayActionState(
			peerSession, isChainMode,
			isEventEligible && peerSession.isDeveloperDropApplicable(),
		),
		overlay.ActionHeal: overlayActionState(
			peerSession, peerSession.isDeveloperHealMode(), isResourceEligible,
		),
		overlay.ActionPowerFill:  resourceState,
		overlay.ActionDamage:     resourceState,
		overlay.ActionPowerDrain: resourceState,
		overlay.ActionGoto:       eventState,
		overlay.ActionEvent:      eventState,
		overlay.ActionReset:      eventState,
		overlay.ActionDefeat:     eventState,
		overlay.ActionKill: overlayActionState(
			peerSession, isChainMode,
			isEventEligible && peerSession.isDeveloperKillApplicable(),
		),
		overlay.ActionRecap: overlayActionState(
			peerSession, isChainMode,
			isEventEligible && peerSession.isDeveloperRecapApplicable(),
		),
		overlay.ActionVictory: overlayVictoryState(peerSession, isEventEligible),
		overlay.ActionEffect: overlayActionState(
			peerSession, true, peerSession.isEffectPreviewEligible(),
		),
	}
}

func overlaySpawnState(
	peerSession *gameplayPeerSession, isEventEligible bool,
) overlay.ActionState {
	if !peerSession.binding.IsWarped {
		return overlay.ActionState{Reason: overlay.ReasonNotWarped}
	}
	return overlayActionState(
		peerSession, true, isEventEligible && peerSession.isDeveloperSpawnEligible(),
	)
}

// overlayVictoryState follows the mode-specific /victory branches of the
// developer event consumer.
func overlayVictoryState(
	peerSession *gameplayPeerSession, isEventEligible bool,
) overlay.ActionState {
	switch peerSession.binding.Mode {
	case game.ModeChain:
		return overlayActionState(
			peerSession, true,
			isEventEligible && peerSession.isDeveloperChainVictoryApplicable() &&
				peerSession.zone != nil,
		)
	case game.ModeTutorial:
		return overlayActionState(
			peerSession, true,
			isEventEligible && peerSession.isDeveloperTutorialVictoryApplicable(),
		)
	case game.ModeArena:
		return overlayActionState(
			peerSession, true,
			isEventEligible && peerSession.isDeveloperArenaVictoryApplicable(),
		)
	default:
		return overlay.ActionState{Reason: overlay.ReasonWrongMode}
	}
}

func overlayActionState(
	peerSession *gameplayPeerSession, isModeAllowed bool, isReady bool,
) overlay.ActionState {
	if !isModeAllowed {
		return overlay.ActionState{Reason: overlay.ReasonWrongMode}
	}
	if isReady {
		return overlay.ActionState{IsAvailable: true}
	}
	if peerSession.isZoneTerminal() {
		return overlay.ActionState{Reason: overlay.ReasonZoneTerminal}
	}
	return overlay.ActionState{Reason: overlay.ReasonNotDeployed}
}
