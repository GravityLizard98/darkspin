package npc

// PromoteMutationAllies applies the agent's aura to nearby ordinary enemies.
// Existing elites and unrelated fixtures retain their own profiles.
func (e *Session) PromoteMutationAllies() []Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	promoted := make([]Snapshot, 0)
	for _, sourceID := range e.objectIDs {
		source := e.npcs[sourceID]
		if source.IsDefeated || source.Plan.NounName != MutationAgentNounName {
			continue
		}
		for _, objectID := range e.objectIDs {
			npc := e.npcs[objectID]
			if npc.IsDefeated || npc.Plan.IsFixture || npc.Plan.IsElite || npc.Plan.IsCaptain ||
				npc.Plan.IsBoss || npc.Plan.OwnerObjectID != 0 || npc.Faction != source.Faction ||
				npc.Plan.NounName == MutationAgentNounName ||
				npc.Plan.Position.Sub(source.Plan.Position).Length() > 15 {
				continue
			}
			npc.Plan.IsElite = true
			npc.Plan.NPCProfile = ApplyEliteProfile(npc.Plan.NPCProfile)
			npc.HitPoint *= 1 + EliteHealthBonus
			e.npcs[objectID] = npc
			promoted = append(promoted, npc)
		}
	}
	return promoted
}
