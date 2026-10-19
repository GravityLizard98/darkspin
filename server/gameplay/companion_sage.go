package gameplay

import "github.com/darkspinnet/darkspin/server/util"

// reserveDefeatedSageCompanionRespawnLocked routes a shared-zone death to the
// summon owner's passive even when another player's NPC action dealt the hit.
// The registry lock must be held, and peerSession is the caller's working copy.
func (e campaignNPCActionRuntime) reserveDefeatedSageCompanionRespawnLocked(
	peerSession *gameplayPeerSession, objectID uint32,
) {
	if peerSession == nil || peerSession.zone == nil ||
		peerSession.zone.Companion() == nil || e.registry == nil {
		return
	}
	actor, isFound := peerSession.zone.Companion().Snapshot(objectID)
	if !isFound || actor.HitPoint > 0 ||
		actor.Noun != util.HashID(e.program.SupportHealerPassive.CompanionNoun) {
		return
	}
	for sessionKey, ownerSession := range e.registry.sessions {
		if ownerSession.zone != peerSession.zone ||
			ownerSession.binding.UserID != actor.UserID ||
			ownerSession.generation != actor.PeerGeneration ||
			ownerSession.deployedObjectID != actor.OwnerObjectID {
			continue
		}
		if peerSession.binding.UserID == actor.UserID &&
			peerSession.generation == actor.PeerGeneration {
			e.reserveSageCompanionRespawnLocked(
				peerSession, sessionKey, actor.PeerGeneration, objectID,
			)
			return
		}
		e.reserveSageCompanionRespawnLocked(
			&ownerSession, sessionKey, actor.PeerGeneration, objectID,
		)
		e.registry.sessions[sessionKey] = ownerSession
		return
	}
}

// sageCompanionObjectIDs selects the current deployment's live Dendrones before
// reserving combat state in the shared zone. The registry lock must be held.
func (e *gameplayPeerSession) sageCompanionObjectIDs(nounName string) []uint32 {
	if e == nil || e.sagePassive == nil || e.zone == nil ||
		e.zone.Companion() == nil || e.deployedObjectID == 0 || nounName == "" {
		return nil
	}
	noun := util.HashID(nounName)
	objectIDs := make([]uint32, 0, len(e.sagePassiveActivations))
	// Companion snapshots are sorted by object ID, so reservation order is stable.
	for _, actor := range e.zone.Companion().Snapshots() {
		if actor.UserID != e.binding.UserID || actor.PeerGeneration != e.generation ||
			actor.OwnerObjectID != e.deployedObjectID || actor.Noun != noun ||
			!actor.IsCombatant || actor.HitPoint <= 0 {
			continue
		}
		activation, isFound := e.sagePassiveActivations[actor.ObjectID]
		if !isFound || activation.ObjectID != actor.ObjectID ||
			activation.OwnerObjectID != e.deployedObjectID {
			continue
		}
		objectIDs = append(objectIDs, actor.ObjectID)
	}
	return objectIDs
}
