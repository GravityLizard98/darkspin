package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
)

// GlobalDefinitions identifies Frozen as attribute 25. ModifierCreated only
// presents the modifier; it does not run its server-side AddAttributeModifier.
const chronoFrozenAttribute = uint8(25)

type chronoFreezeKey struct {
	zone     *zone.Zone
	objectID uint32
}

type chronoFreezeStep struct {
	runtime    campaignAbilityCommandRuntime
	key        chronoFreezeKey
	expiresAt  time.Time
	sessionKey string
	generation uint64
}

// The registry lock protects the shared zone/object deadline, including freezes
// from different party members. An older expiry must not thaw a newer freeze.
func freezeChronoObjectLocked(
	runtime campaignAbilityCommandRuntime, packet raknet.Packet,
	sessionKey string, generation uint64,
	currentZone *zone.Zone, objectID uint32, duration time.Duration,
) ([]byte, error) {
	key := chronoFreezeKey{zone: currentZone, objectID: objectID}
	expiresAt := runtime.now().Add(duration)
	if !expiresAt.After(runtime.registry.chronoFreezes[key]) {
		return nil, nil
	}
	encoded, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID, Value: map[uint8]float32{chronoFrozenAttribute: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("chronoFreezeEncode: %w", err)
	}
	step := chronoFreezeStep{
		runtime: runtime, key: key, expiresAt: expiresAt,
		sessionKey: sessionKey, generation: generation,
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: duration, Produce: step.expire,
	}})
	if err != nil {
		return nil, fmt.Errorf("chronoFreezeSchedule: %w", err)
	}
	if cancel == nil {
		return nil, fmt.Errorf("chronoFreezeSchedule: missing cancellation")
	}
	// Deliberately let the thaw finish even when the caster switches heroes.
	if runtime.registry.chronoFreezes == nil {
		runtime.registry.chronoFreezes = make(map[chronoFreezeKey]time.Time)
	}
	runtime.registry.chronoFreezes[key] = expiresAt
	return encoded, nil
}

func (e chronoFreezeStep) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.runtime.registry.chronoFreezes[e.key] != e.expiresAt {
		return nil, nil
	}
	delete(e.runtime.registry.chronoFreezes, e.key)
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone != e.key.zone {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.key.objectID, Value: map[uint8]float32{chronoFrozenAttribute: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("chronoThawEncode: %w", err)
	}
	return [][]byte{packet}, nil
}
