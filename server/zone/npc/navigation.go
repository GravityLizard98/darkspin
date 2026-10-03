package npc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/util"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// ConfigureNavigation supplies noun geometry once at zone creation. Restored
// actor objects retain their stored planLayer; new actors select at creation.
func (e *Session) ConfigureNavigation(mesh *navigation.Mesh, footprints map[uint32]game.NavigationFootprint) error {
	if e == nil {
		return errors.New("npc navigation unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.navigationMesh = mesh
	e.navigationFootprints = footprints
	for objectID, npc := range e.npcs {
		if !zonegeometry.IsFiniteScalar(npc.Navigation.Radius) || npc.Navigation.Radius < 0 {
			return fmt.Errorf("actorRadius[%d]: invalid", objectID)
		}
		if npc.Navigation.IsPresent {
			layerInfo, isFound := mesh.LayerInfo(npc.Navigation.PlanLayer)
			if !isFound || layerInfo.PlanLayer != npc.Navigation.PlanLayer {
				return fmt.Errorf("actorLayer[%d]: unavailable", objectID)
			}
			continue
		}
		actor, err := e.navigationForPlan(npc.Plan)
		if err != nil {
			return fmt.Errorf("actorNavigation[%d]: %w", objectID, err)
		}
		npc.Navigation = actor
		e.npcs[objectID] = npc
	}
	return nil
}

func (e *Session) navigationForPlan(plan SpawnPlan) (navigation.ActorNavigation, error) {
	if e.navigationFootprints == nil {
		return navigation.ActorNavigation{}, nil
	}
	stem := strings.TrimSuffix(strings.ToLower(plan.NounName), ".noun")
	footprint, isFound := e.navigationFootprints[util.HashID(stem)]
	// Compatibility plans without imported nouns retain their previous radius.
	radius := max(plan.NPCProfile.FootprintRadius, float32(0.1))
	if isFound {
		scale := plan.PlacementScale
		if scale == 0 {
			scale = 1
		}
		var err error
		radius, err = footprint.ActorRadius(scale)
		if err != nil {
			return navigation.ActorNavigation{}, fmt.Errorf("planRadius: %w", err)
		}
	}
	mode := uint8(0)
	if plan.IsArena {
		mode = uint8(game.ModeArena)
	}
	actor := navigation.ActorNavigation{Radius: radius, Mode: mode}
	if e.navigationMesh == nil {
		return actor, nil
	}
	planLayer, isLayerFound := e.navigationMesh.SelectFootprintLayer(radius, mode)
	if !isLayerFound {
		return navigation.ActorNavigation{}, errors.New("actor navigation layer unavailable")
	}
	actor.PlanLayer = planLayer
	actor.IsPresent = true
	return actor, nil
}

func (e Snapshot) NavigationRadius() float32 {
	if e.Navigation.IsPresent || e.Navigation.Radius > 0 {
		return e.Navigation.Radius
	}
	return e.Plan.NPCProfile.FootprintRadius
}
