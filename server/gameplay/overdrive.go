package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

const campaignOverdriveMaximumEnergy = game.OverdriveMaximumEnergy
const campaignOverdriveBaseDrainPerSecond = game.OverdriveDrainPerSecond

func (e gameplayPeerSession) overdriveKillCharge(objectID uint32) uint32 {
	if e.zone == nil || e.zone.NPCs() == nil {
		return 0
	}
	enemy, isFound := e.zone.NPCs().NPC(objectID)
	if !isFound || !enemy.IsDefeated || enemy.Plan.IsFixture {
		return 0
	}
	// Requested balance policy, not a recovered native reward formula:
	// 84 ordinary kills fill 420 energy, and non-minions award four times more.
	if enemy.Plan.NPCProfile.NPCType != 0 {
		return 20
	}
	return 5
}

type campaignOverdriveRuntime struct {
	registry *gameplaySessionRegistry
	now      func() time.Time
	logger   *log.Logger
}

func (r campaignOverdriveRuntime) handle(
	packet raknet.Packet, command raknet.ActionCommandData,
) ([][]byte, error) {
	if command.Common.Type != raknet.ActionOverdrive {
		return nil, errors.New("overdrive command invalid")
	}
	sessionKey := packet.Address.String()
	now := r.now()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isAccepted := isFound && peerSession.stage.IsDungeon() &&
		peerSession.binding.IsOverdriveUnlocked &&
		command.Common.ObjectID == peerSession.deployedObjectID &&
		peerSession.deployedHitPoint() > 0 &&
		!peerSession.isOverdriveSpent &&
		peerSession.overdriveEnergy >= float32(campaignOverdriveMaximumEnergy) &&
		!peerSession.isOverdriveActiveAt(now)
	if !isAccepted {
		r.registry.mutex.Unlock()
		return nil, errors.New("overdrive unavailable")
	}
	duration := campaignOverdriveDuration(peerSession)
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp:    command.Common.Unknown[0],
		ResponseType: raknet.ActionResponseAccepted,
		// Accepted responses resolve an ability definition, not a hero object.
		// Build 103 sub_4D64D0 dereferences that lookup without a null guard.
		ObjectID:                 util.HashID("Overdrive"),
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime + 300,
		SourceEndMilliseconds:    packet.SourceTime + 400,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("overdriveAcknowledge: %w", err)
	}
	peerSession.isOverdriveSpent = true
	peerSession.overdriveEnergy = 0
	peerSession.overdriveStartsAt = now.Add(300 * time.Millisecond)
	peerSession.overdriveExpiresAt = peerSession.overdriveStartsAt.Add(duration)
	peerSession.overdriveActivation++
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	step := campaignOverdriveMeterStep{
		runtime: r, packet: packet.Autonomous(), sessionKey: sessionKey,
		generation: peerSession.generation, activation: peerSession.overdriveActivation,
	}
	scheduleErr := errors.New("overdrive scheduler unavailable")
	if step.packet.ScheduleFunc != nil {
		scheduleErr = step.packet.ScheduleFunc(300*time.Millisecond, step.produce)
	}
	if scheduleErr != nil {
		r.registry.mutex.Lock()
		current, isCurrent := r.registry.sessions[sessionKey]
		if isCurrent && current.generation == peerSession.generation &&
			current.overdriveActivation == step.activation {
			current.isOverdriveSpent = false
			current.overdriveEnergy = float32(campaignOverdriveMaximumEnergy)
			current.overdriveExpiresAt = time.Time{}
			current.overdriveStartsAt = time.Time{}
			r.registry.sessions[sessionKey] = current
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("overdriveSchedule: %w", scheduleErr)
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet campaign Overdrive activated game=%d user=%d duration=%s",
			peerSession.binding.GameID, peerSession.binding.UserID, duration,
		)
	}
	return [][]byte{ackPacket}, nil
}

type campaignOverdriveMeterStep struct {
	runtime    campaignOverdriveRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	activation uint64
}

func (e campaignOverdriveMeterStep) produce() ([][]byte, error) {
	now := e.runtime.now()
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation ||
		member.overdriveActivation != e.activation {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	isActive := member.isOverdriveActiveAt(now)
	remaining := member.overdriveExpiresAt.Sub(now)
	duration := campaignOverdriveDuration(member)
	energy := member.presentedOverdriveEnergy(now)
	e.runtime.registry.mutex.RUnlock()
	statePacket, err := raknet.MarshalApplication(raknet.LabsPlayerOverdriveStateMessage{
		Slot: uint8(member.binding.Slot), Energy: energy, IsActive: isActive,
		DrainPerSecond: float32(campaignOverdriveMaximumEnergy / duration.Seconds()),
	})
	if err != nil {
		return nil, fmt.Errorf("overdriveMeter: %w", err)
	}
	if isActive {
		delay := min(500*time.Millisecond, remaining)
		err = e.packet.ScheduleFunc(delay, e.produce)
		if err != nil {
			return nil, fmt.Errorf("overdriveMeterSchedule: %w", err)
		}
	}
	return [][]byte{statePacket}, nil
}

func (e *gameplayPeerSession) rechargeOverdrive(
	baseEnergy uint32, now time.Time,
) (float32, bool) {
	if e == nil || baseEnergy == 0 || !e.binding.IsOverdriveUnlocked {
		return 0, false
	}
	creatureIndex := e.deployedCreatureIndex
	if creatureIndex >= uint32(len(e.binding.Creatures)) {
		return 0, false
	}
	bonus := max(float32(-1), e.binding.Creatures[creatureIndex].OverdriveBuildupIncrease)
	// Base charge is independent of difficulty-scaled experience rewards.
	recharge := float32(baseEnergy) * (1 + bonus)
	if recharge <= 0 {
		return 0, false
	}
	if e.isOverdriveActiveAt(now) {
		duration := campaignOverdriveDuration(*e)
		remaining := float32(e.overdriveExpiresAt.Sub(now)) /
			float32(duration) * float32(campaignOverdriveMaximumEnergy)
		energy := min(float32(campaignOverdriveMaximumEnergy), remaining+recharge)
		e.overdriveExpiresAt = now.Add(time.Duration(
			float64(duration) * float64(energy) / campaignOverdriveMaximumEnergy,
		))
		return energy, true
	}
	if e.overdriveEnergy >= float32(campaignOverdriveMaximumEnergy) {
		return 0, false
	}
	e.overdriveEnergy = min(
		float32(campaignOverdriveMaximumEnergy), e.overdriveEnergy+recharge,
	)
	if e.overdriveEnergy >= float32(campaignOverdriveMaximumEnergy) {
		e.isOverdriveSpent = false
	}
	return e.overdriveEnergy, true
}

func (e gameplayPeerSession) presentedOverdriveEnergy(now time.Time) float32 {
	if !e.binding.IsOverdriveUnlocked {
		return 0
	}
	if !e.isOverdriveActiveAt(now) {
		return e.overdriveEnergy
	}
	duration := campaignOverdriveDuration(e)
	remaining := float32(e.overdriveExpiresAt.Sub(now)) /
		float32(duration) * float32(campaignOverdriveMaximumEnergy)
	return min(float32(campaignOverdriveMaximumEnergy), max(float32(0), remaining))
}

func campaignOverdriveDuration(peerSession gameplayPeerSession) time.Duration {
	creatureIndex := peerSession.deployedCreatureIndex
	if creatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return campaignOverdriveBaseDuration()
	}
	duration, err := game.ResolveOverdriveDuration(
		campaignOverdriveMaximumEnergy, campaignOverdriveBaseDrainPerSecond,
		peerSession.binding.Creatures[creatureIndex].OverdriveDurationIncrease,
	)
	if err != nil {
		return campaignOverdriveBaseDuration()
	}
	return duration
}

func campaignOverdriveBaseDuration() time.Duration {
	duration, err := game.ResolveOverdriveDuration(
		campaignOverdriveMaximumEnergy, campaignOverdriveBaseDrainPerSecond, 0,
	)
	if err != nil {
		return 6 * time.Second
	}
	return duration
}
