package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *gameplayPeerSession) graviticMovementSpeedBuff() float32 {
	if e.graviticSlowObjectID == 0 || e.graviticSlowObjectID != e.deployedObjectID {
		return 0
	}
	return zonenpc.GraviticMovementSpeedBuff
}

// Poll each member independently, including stationary heroes whose nearby
// stabilizer was destroyed by an ally. Overlapping fields apply only one slow.
func (e gameplayPendingRuntime) pollGraviticFields(packet raknet.Packet) ([][]byte, error) {
	sessionKey := packet.Address.String()
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.transportGeneration != packet.TransportGeneration ||
		member.zone == nil || !member.stage.IsDungeon() ||
		!member.dungeonSetup.IsCommitted() || member.isRejoinPending ||
		member.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := e.updateGraviticContactLocked(&member, e.now())
	if err == nil {
		e.registry.sessions[sessionKey] = member
	}
	e.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("graviticContact: %w", err)
	}
	err = publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		return nil, fmt.Errorf("graviticPublish: %w", err)
	}
	return packets, nil
}

func (e gameplayPendingRuntime) updateGraviticContactLocked(
	member *gameplayPeerSession, now time.Time,
) ([][]byte, error) {
	stabilizers := make([]zonenpc.Snapshot, 0)
	for _, plan := range member.zone.FixturePlans() {
		if !zonenpc.IsGraviticStabilizer(plan) {
			continue
		}
		stabilizer, isFound := member.zone.NPCs().NPC(plan.ObjectID)
		if isFound && !stabilizer.IsDefeated && stabilizer.HitPoint > 0 {
			stabilizers = append(stabilizers, stabilizer)
		}
	}
	if len(stabilizers) == 0 && member.graviticSlowObjectID == 0 {
		return nil, nil
	}
	position := game.Vec3(member.playerPosition)
	if member.playerMotion != nil {
		sampledPosition, err := member.playerMotion.SamplePosition(now)
		if err != nil {
			return nil, fmt.Errorf("graviticPosition: %w", err)
		}
		position = game.Vec3(sampledPosition)
	}
	nextObjectID := uint32(0)
	if member.deployedObjectID != 0 && member.deployedHitPoint() > 0 &&
		!member.isHeroSelectionPending &&
		!e.damage.npc.isTargetDebuffImmuneLocked(member, member.deployedObjectID) {
		for _, stabilizer := range stabilizers {
			if stabilizer.Plan.Position.Sub(position).Length() <= zonenpc.GraviticFieldRadius {
				nextObjectID = member.deployedObjectID
				break
			}
		}
	}
	previousObjectID := member.graviticSlowObjectID
	if previousObjectID == nextObjectID && member.isGraviticSpeedPresented {
		return nil, nil
	}
	member.graviticSlowObjectID = nextObjectID
	packets := make([][]byte, 0, 2)
	// Also clear a switched-out hero, so returning to that squad slot cannot
	// inherit a field it no longer occupies.
	if previousObjectID != 0 && previousObjectID != member.deployedObjectID {
		previousMember := *member
		previousMember.deployedObjectID = previousObjectID
		previousPacket, err := marshalGraviticSpeed(previousMember)
		if err != nil {
			return nil, fmt.Errorf("graviticPrevious: %w", err)
		}
		packets = append(packets, previousPacket)
	}
	if member.deployedObjectID == 0 {
		member.isGraviticSpeedPresented = true
		return packets, nil
	}
	currentPacket, err := marshalGraviticSpeed(*member)
	if err != nil {
		return nil, fmt.Errorf("graviticCurrent: %w", err)
	}
	if member.playerMotion != nil {
		increase := max(float32(-0.9),
			e.registry.passiveMovementIncrease(*member)+member.enemyMovementSpeedBuff())
		err = member.playerMotion.SetSpeed(now, zonePlayerMoveSpeed*(1+increase))
		if err != nil {
			return nil, fmt.Errorf("graviticMotion: %w", err)
		}
	}
	member.isGraviticSpeedPresented = true
	return append(packets, currentPacket), nil
}

func marshalGraviticSpeed(member gameplayPeerSession) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: member.deployedObjectID,
		Value:    map[uint8]float32{48: member.enemyMovementSpeedBuff()},
	})
	if err != nil {
		return nil, fmt.Errorf("graviticAttribute: %w", err)
	}
	return packet, nil
}
