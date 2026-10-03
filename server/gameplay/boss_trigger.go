package gameplay

import (
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

type campaignNamedBossTriggerStep struct {
	runtime     campaignEncounterRuntime
	zone        *zone.Zone
	publication game.CampaignDirectorPublication
	sessionKey  string
	generation  uint64
	center      game.Vec3
	radius      float32
}

func (e campaignNamedBossTriggerStep) execute() {
	err := e.admit()
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"Campaign named boss trigger admission failed marker_set=%q marker=%d: %v",
			e.publication.MarkerSetName, e.publication.TriggerMarkerID, err,
		)
	}
}

func (e campaignNamedBossTriggerStep) admit() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation ||
		peerSession.zone != e.zone || peerSession.isRejoinPending ||
		peerSession.deployedObjectID == 0 || peerSession.deployedHitPoint() <= 0 {
		return nil
	}
	position := game.Vec3(peerSession.playerPosition)
	deltaX := position.X - e.center.X
	deltaY := position.Y - e.center.Y
	deltaZ := position.Z - e.center.Z
	if deltaX*deltaX+deltaY*deltaY+deltaZ*deltaZ > e.radius*e.radius {
		return nil
	}
	if e.zone.Boss() == nil || !e.zone.Boss().IsDormant() {
		return nil
	}
	encounter, err := e.zone.PlanBossFromTrigger(
		e.publication, peerSession.binding.GameID,
		peerSession.binding.ChainLevelIndex,
	)
	if err != nil {
		return fmt.Errorf("bossTriggerPlan: %w", err)
	}
	livePlans := encounter.Actors
	if encounter.Actors[0].IsCaptain && len(encounter.Actors) > 1 {
		livePlans = encounter.Actors[1:]
	}
	packets, err := npcraknet.TargetedSpawns(livePlans, peerSession.deployedObjectID)
	if err != nil {
		return fmt.Errorf("bossTriggerMarshal: %w", err)
	}
	if len(packets) == 0 {
		return fmt.Errorf("bossTriggerMarshal: no packets for %d actors", len(livePlans))
	}
	if !encounter.Actors[0].IsCaptain && !isCampaignBossIntroDelayed(encounter.Actors[0]) {
		activePacket, activeErr := bossraknet.Active(
			encounter.Actors[0].ObjectID,
			zoneboss.IsFinalBossNoun(encounter.Actors[0].NounName),
		)
		if activeErr != nil {
			return fmt.Errorf("bossTriggerActive: %w", activeErr)
		}
		if len(activePacket) == 0 {
			return fmt.Errorf("bossTriggerActive: empty packet")
		}
	}
	err = e.zone.AdmitTriggeredNamedBossEncounter(
		encounter.NamedPublication, encounter.Publication, e.publication,
		peerSession.deployedObjectID, encounter.Actors,
	)
	if err != nil {
		return fmt.Errorf("bossTriggerCommit: %w", err)
	}
	err = e.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: livePlans, TargetObjectID: peerSession.deployedObjectID,
		IsBossActive: !encounter.Actors[0].IsCaptain,
		BossObjectID: encounter.Actors[0].ObjectID,
		IsFinalBoss:  zoneboss.IsFinalBossNoun(encounter.Actors[0].NounName),
	}, 0, 0)
	if err != nil {
		return fmt.Errorf("bossTriggerPublish: %w", err)
	}
	if e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"Campaign named boss admitted marker_set=%q trigger=%d actors=%d target=%d",
			e.publication.MarkerSetName, e.publication.TriggerMarkerID,
			len(livePlans), peerSession.deployedObjectID,
		)
	}
	return nil
}

func (r campaignEncounterRuntime) scheduleNamedBossTriggers(
	sessionKey string, commandSession gameplayPeerSession,
	publications []game.CampaignDirectorPublication,
) error {
	isNamedBossTrigger := false
	for _, publication := range publications {
		if publication.CallbackName == zoneboss.GenericCallback {
			isNamedBossTrigger = true
			break
		}
	}
	if !isNamedBossTrigger {
		return nil
	}
	if commandSession.zone == nil || r.timer == nil {
		return fmt.Errorf("bossTriggerScheduler: unavailable")
	}
	if commandSession.zone.Timeline() == nil {
		return fmt.Errorf("bossTriggerTimeline: unavailable")
	}
	for _, publication := range publications {
		if publication.CallbackName != zoneboss.GenericCallback {
			continue
		}
		center, radius, delay, err := commandSession.zone.DirectorDefinition().
			BossTriggerActivation(publication)
		if err != nil {
			return fmt.Errorf("bossTriggerActivation: %w", err)
		}
		key := fmt.Sprintf("named-boss:%d:%d", publication.TriggerMarkerID, publication.EventOrdinal)
		if isNamedBossTriggerScheduled(commandSession.zone, key) {
			continue
		}
		step := campaignNamedBossTriggerStep{
			runtime: r, zone: commandSession.zone, publication: publication,
			sessionKey: sessionKey, generation: commandSession.generation,
			center: center, radius: radius,
		}
		err = commandSession.zone.Timeline().Schedule(key, delay, step.execute, r.timer.Schedule)
		if err != nil {
			if isNamedBossTriggerScheduled(commandSession.zone, key) {
				continue
			}
			return fmt.Errorf("bossTriggerSchedule: %w", err)
		}
	}
	return nil
}

func isNamedBossTriggerScheduled(currentZone *zone.Zone, key string) bool {
	if currentZone == nil || currentZone.Timeline() == nil {
		return false
	}
	for _, activeKey := range currentZone.Timeline().Keys() {
		if strings.EqualFold(activeKey, key) {
			return true
		}
	}
	return false
}
