package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
)

func (e heroChargeSchedule) beginPhantomSlide() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		return nil, nil
	}
	jumpPacket, err := actionraknet.ChargeSlide(
		e.sourceObjectID, game.Vec3(e.startPosition), game.Vec3(e.destination),
		zonePlayerMoveSpeed*e.definition.MovementSpeedMultiplier,
	)
	if err != nil {
		return nil, fmt.Errorf("phantomSlide: %w", err)
	}
	collisionPacket, err := raknet.MarshalApplication(raknet.ObjectCollisionUpdateMessage{
		ObjectID: e.sourceObjectID, IsCollisionEnabled: false,
	})
	if err != nil {
		return nil, fmt.Errorf("phantomCollisionDisable: %w", err)
	}
	restorePacket, err := raknet.MarshalApplication(raknet.ObjectCollisionUpdateMessage{
		ObjectID: e.sourceObjectID, IsCollisionEnabled: true,
	})
	if err != nil {
		return nil, fmt.Errorf("phantomCollisionRestore: %w", err)
	}
	e.run.mutex.Lock()
	if e.run.isCleaned {
		e.run.mutex.Unlock()
		return nil, nil
	}
	e.run.isCollisionSuppressed = true
	e.run.collisionRestorePacket = restorePacket
	e.run.mutex.Unlock()
	e.runtime.registry.queueChargePacketsLocked(e.run.zone, [][]byte{collisionPacket, jumpPacket})
	return nil, nil
}

// The registry lock is held by release, interruption and retirement callers.
func (e *heroChargeRun) restorePhantomCollisionLocked() [][]byte {
	e.mutex.Lock()
	if !e.isCollisionSuppressed {
		e.mutex.Unlock()
		return nil
	}
	e.isCollisionSuppressed = false
	packets := [][]byte{e.collisionRestorePacket}
	e.collisionRestorePacket = nil
	e.mutex.Unlock()
	if e.registry != nil {
		e.registry.queueChargePacketsLocked(e.zone, packets)
		return nil
	}
	return packets
}
