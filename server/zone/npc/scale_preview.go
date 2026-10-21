package npc

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type ScalePreviewRequest struct {
	Position   game.Vec3
	Radius     float32
	Multiplier float32
	NounFilter string
	IsReset    bool
}

type ScalePreview struct {
	Plan      SpawnPlan
	BodyScale float32
}

// PreviewBodyScale keeps the chosen visual size fixed while previewing.
// It does not change placement scale, navigation, damage or actor footprints.
func (e SpawnPlan) PreviewBodyScale(bodyScale float32) float32 {
	if e.ScalePreviewMultiplier > 0 {
		return e.ScalePreviewBodyScale
	}
	return bodyScale
}

func (e *Session) PreviewBodyScale(objectID uint32, bodyScale float32) float32 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	npc, isFound := e.npcs[objectID]
	if !isFound {
		return bodyScale
	}
	return npc.Plan.PreviewBodyScale(bodyScale)
}

func (e *Session) PreviewScales(req ScalePreviewRequest) ([]ScalePreview, error) {
	if e == nil || !zonegeometry.IsFinite(req.Position) || math.IsNaN(float64(req.Radius)) || math.IsInf(float64(req.Radius), 0) ||
		req.Radius <= 0 || req.Radius > 100 || math.IsNaN(float64(req.Multiplier)) ||
		math.IsInf(float64(req.Multiplier), 0) || (!req.IsReset && (req.Multiplier < 0.25 || req.Multiplier > 4)) {
		return nil, errors.New("invalid scale preview")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	previews := make([]ScalePreview, 0)
	nounFilter := strings.ToLower(req.NounFilter)
	now := time.Now()
	for _, objectID := range e.objectIDs {
		npc := e.npcs[objectID]
		if !npc.IsPublished || npc.IsDefeated || npc.HitPoint <= 0 || npc.Plan.IsFixture ||
			npc.Faction != FactionNonPlayerAligned ||
			!strings.Contains(strings.ToLower(npc.Plan.NounName), nounFilter) ||
			npc.Plan.Position.Sub(req.Position).Length() > req.Radius {
			continue
		}
		bodyScale := float32(npc.status.passiveEnrageStack)*0.08 + float32(npc.status.oozeGrowthStack)*0.1
		if now.Before(npc.status.munchExpiresAt) {
			bodyScale += float32(npc.status.munchStack) * 0.06
		}
		if npc.Plan.IsCaptain || npc.Plan.IsElite || npc.Plan.IsBoss || npc.Plan.BossIdentity.HasModifier(EliteModifierName) {
			bodyScale += EliteBonusForType(npc.Plan.NPCProfile.NPCType).BodyScale
		}
		npc.Plan.ScalePreviewMultiplier = 0
		npc.Plan.ScalePreviewBodyScale = 0
		if !req.IsReset {
			npc.Plan.ScalePreviewMultiplier = req.Multiplier
			npc.Plan.ScalePreviewBodyScale = (1+bodyScale)*req.Multiplier - 1
			bodyScale = npc.Plan.ScalePreviewBodyScale
		}
		e.npcs[objectID] = npc
		previews = append(previews, ScalePreview{Plan: npc.Plan, BodyScale: bodyScale})
	}
	return previews, nil
}
