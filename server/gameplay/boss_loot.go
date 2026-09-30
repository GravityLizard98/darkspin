package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	lootraknet "github.com/darkspinnet/darkspin/server/zone/loot/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const destructorEquipmentCount = 6

func isCampaignDestructorLoot(plan zonenpc.SpawnPlan) bool {
	return plan.OwnerObjectID == 0 && zoneboss.IsFinalBossNoun(plan.NounName)
}

type destructorEquipmentDrop struct {
	pickup      zoneinteract.EquipmentPickup
	destination sim.Position
}

// Prepare the whole burst before publishing it; the caller owns the shared
// NPC reservation, so every participant sees the same six world pickups.
func (e *gameplayPeerSession) spawnDestructorEquipment(
	enemy zonenpc.Snapshot, gameplayJoin *game.GameplayJoin, sourceTime uint64,
) ([][]byte, uint32, error) {
	if e == nil || gameplayJoin == nil || e.zone == nil || e.zone.DropRandom() == nil {
		return nil, 0, errors.New("destructor loot unavailable")
	}
	subjects := e.campaignPartSubjects()
	if len(subjects) == 0 {
		return nil, 0, errors.New("destructor loot roster unavailable")
	}
	source := sim.Position(enemy.Plan.Position)
	radius := max(float32(3), enemy.Plan.NPCProfile.FootprintRadius+1)
	slotBag := game.CampaignPartSlotBag{}
	rarityBag := game.CampaignPartRarityBag{IsDestructorReward: true}
	drops := make([]destructorEquipmentDrop, 0, destructorEquipmentCount)
	packets := make([][]byte, 0)
	for index := 0; index < destructorEquipmentCount; index++ {
		choice := e.zone.DropRandom().Uint32()
		subject := subjects[choice%uint32(len(subjects))]
		part, err := gameplayJoin.GenerateCampaignPartFromBag(
			subject, e.binding.Difficulty, e.binding.AvatarLevel, choice, &slotBag, &rarityBag,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("bossPart[%d]: %w", index, err)
		}
		objectID, err := e.reserveCampaignObjectID()
		if err != nil {
			return nil, 0, fmt.Errorf("bossObject[%d]: %w", index, err)
		}
		angle := 2 * math.Pi * float64(index) / destructorEquipmentCount
		destination := e.destructorDropDestination(source, sim.Position{
			X: source.X + radius*float32(math.Cos(angle)),
			Y: source.Y + radius*float32(math.Sin(angle)), Z: source.Z,
		})
		plan, err := zoneloot.PlanEquipment(zoneloot.EquipmentPlanInput{
			ObjectID: objectID, Rarity: zoneloot.Rarity(part.Rarity),
			Source: source, Destination: destination,
			SimulationTime: time.Duration(sourceTime) * time.Millisecond,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("bossPlan[%d]: %w", index, err)
		}
		dropPackets, err := lootraknet.MarshalEquipmentDrop(plan, part)
		if err != nil {
			return nil, 0, fmt.Errorf("bossMarshal[%d]: %w", index, err)
		}
		packets = append(packets, dropPackets...)
		drops = append(drops, destructorEquipmentDrop{
			destination: destination,
			pickup: zoneinteract.EquipmentPickup{
				ObjectID: objectID, Part: part, WinnerRewardChoice: choice,
				WinnerRewardDifficulty: e.binding.Difficulty,
				IsWinnerReward:         true, IsWinnerRewardBoss: true, IsDestructorReward: true,
			},
		})
	}
	for index, drop := range drops {
		err := e.registerCampaignPickup(zoneinteract.PickupEquipment, drop.pickup.ObjectID, source, drop.destination)
		if err != nil {
			e.removeDestructorDrops(drops[:index])
			return nil, 0, fmt.Errorf("bossRegister[%d]: %w", index, err)
		}
		err = e.zone.PickupPayload().AddEquipment(drop.pickup)
		if err != nil {
			e.removeDestructorDrops(drops[:index+1])
			return nil, 0, fmt.Errorf("bossTrack[%d]: %w", index, err)
		}
	}
	return packets, drops[0].pickup.ObjectID, nil
}

func (e *gameplayPeerSession) removeDestructorDrops(drops []destructorEquipmentDrop) {
	for _, drop := range drops {
		e.zone.Pickups().Remove(drop.pickup.ObjectID)
		e.zone.PickupPayload().RemoveEquipment(drop.pickup.ObjectID)
	}
}

func (e *gameplayPeerSession) destructorDropDestination(source sim.Position, destination sim.Position) sim.Position {
	nav := e.zone.Navigation()
	if nav == nil {
		return destination
	}
	layer, isLayerFound := nav.SelectLayer(campaignSecurityBlitzFootprintFallback, zonenavigation.HeroHeight)
	if !isLayerFound {
		return source
	}
	options := navigation.ProjectionOptions{PlanLayer: layer, MaxDistance: zonenavigation.ProjectionDistance}
	playerProjection, err := nav.Project(navigation.Vec3(e.playerPosition), options)
	if err != nil {
		return source
	}
	options.ComponentID = playerProjection.ComponentID
	options.IsComponentConstrained = true
	options.MaxDistance = 6
	projection, err := nav.Project(navigation.Vec3(destination), options)
	if err != nil {
		return source
	}
	return sim.Position(projection.Position)
}
