package gameplay

import "github.com/darkspinnet/darkspin/server/game"

// Developer command eligibility. The queued-command consumers and the debug
// overlay's availability check share these predicates, so the overlay does
// not report an action available where its consumer would discard it or
// reject it at apply time.

// isEffectPreviewEligible is the coarse admission of a queued effect preview.
func (e *gameplayPeerSession) isEffectPreviewEligible() bool {
	return e.stage.IsDungeon() && e.dungeonSetup.IsCommitted() && e.deployedObjectID != 0
}

// isResourceCommandEligible is the coarse admission of a queued developer
// resource command.
func (e *gameplayPeerSession) isResourceCommandEligible() bool {
	return e.stage.IsDungeon() && e.dungeonSetup.IsCommitted() && e.deployedObjectID != 0 &&
		e.squad != nil && !e.isZoneTerminal()
}

// isEventCommandEligible is the coarse admission of a queued developer event.
func (e *gameplayPeerSession) isEventCommandEligible() bool {
	return e.stage.IsDungeon() && e.dungeonSetup.IsCommitted() &&
		e.deployedObjectID != 0 && !e.isZoneTerminal()
}

// isDeveloperSpawnEligible is the warped-zone requirement of a queued /spawn.
func (e *gameplayPeerSession) isDeveloperSpawnEligible() bool {
	return e.binding.IsWarped && e.zone != nil && e.zone.NPCs() != nil &&
		e.deployedObjectID != 0 && !e.isZoneTerminal()
}

// isDeveloperEventApplicable is the campaign-chain gate of the reset, goto,
// boss, security, and defeat apply steps.
func (e *gameplayPeerSession) isDeveloperEventApplicable() bool {
	return e.binding.Mode == game.ModeChain && e.squad != nil &&
		e.deployedObjectID != 0 && !e.isZoneTerminal()
}

// isDeveloperKillApplicable is the apply gate of /kill.
func (e *gameplayPeerSession) isDeveloperKillApplicable() bool {
	return e.isDeveloperEventApplicable() && e.zone.NPCs() != nil
}

// isDeveloperChainVictoryApplicable is the apply gate of a campaign /victory.
func (e *gameplayPeerSession) isDeveloperChainVictoryApplicable() bool {
	return e.isDeveloperEventApplicable() && e.chainResult == nil
}

// isDeveloperTutorialVictoryApplicable is the apply gate of a tutorial /victory.
func (e *gameplayPeerSession) isDeveloperTutorialVictoryApplicable() bool {
	return e.binding.Mode == game.ModeTutorial && !e.isZoneTerminal()
}

// isDeveloperArenaVictoryApplicable is the apply gate of an Arena /victory.
func (e *gameplayPeerSession) isDeveloperArenaVictoryApplicable() bool {
	return e.binding.Mode == game.ModeArena && e.binding.Team != 0
}

// isDeveloperRecapApplicable is the caller gate of /recap.
func (e *gameplayPeerSession) isDeveloperRecapApplicable() bool {
	return e.binding.Mode == game.ModeChain && e.zone != nil && !e.isZoneTerminal()
}

// isDeveloperDropApplicable is the apply gate of /drop create.
func (e *gameplayPeerSession) isDeveloperDropApplicable() bool {
	return e.binding.Mode == game.ModeChain && e.zone != nil &&
		e.zone.NPCs() != nil && e.deployedObjectID != 0 && !e.isZoneTerminal()
}

// isDeveloperHealMode duplicates the campaign-squad mode gate that /heal
// reaches through setCampaignCharacterHitPoints (session.go, "campaign squad
// unavailable"). session.go exceeds the file-size limit, so the original
// expression stays in place.
func (e *gameplayPeerSession) isDeveloperHealMode() bool {
	return e.binding.Mode == game.ModeChain || e.binding.Mode == game.ModeTutorial
}
