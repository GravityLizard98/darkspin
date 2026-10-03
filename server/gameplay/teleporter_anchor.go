package gameplay

import (
	"errors"
	"fmt"

	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
)

func (e *gameplayPeerSession) teleporterAnchorObjectID(sourceID uint32) (uint32, error) {
	if e == nil || e.zone == nil || e.zone.Security() == nil {
		return 0, errors.New("teleporter anchor zone unavailable")
	}
	objectID, isFound := e.zone.Security().AnchorObjectID(sourceID)
	if isFound {
		return objectID, nil
	}
	objectID, err := e.zone.ReserveObjectIDs(1)
	if err != nil {
		return 0, fmt.Errorf("anchorReserve: %w", err)
	}
	objectID, err = e.zone.Security().BindAnchor(sourceID, objectID)
	if err != nil {
		return 0, fmt.Errorf("anchorBind: %w", err)
	}
	return objectID, nil
}

// A reconnect baseline can be encoded while threats are moving. Track each
// peer's emitted state independently of the party's logical route progress.
func (e *gameplayPeerSession) syncSecurityTeleporterStates() ([][]byte, error) {
	if e == nil || e.zone == nil || e.zone.Security() == nil {
		return nil, nil
	}
	snapshot := e.zone.Security().Snapshot()
	threats := e.zone.SecurityThreats()
	if e.securityTeleporterStates == nil {
		e.securityTeleporterStates = make(map[uint32]bool)
	}
	packets := make([][]byte, 0)
	for index, objectID := range snapshot.ObjectID {
		if objectID == 0 {
			continue
		}
		teleport, isFound := zonesecurity.Route(index)
		if !isFound {
			return nil, fmt.Errorf("securityStateRoute[%d]: unavailable", index)
		}
		isActive := snapshot.Presented[index] && !zonesecurity.HasThreat(teleport, threats)
		previousState, isPresented := e.securityTeleporterStates[objectID]
		if isPresented && previousState == isActive {
			continue
		}
		statePackets, err := securityraknet.State(objectID, teleport, isActive, false)
		if err != nil {
			return nil, fmt.Errorf("securityStateMarshal[%d]: %w", index, err)
		}
		e.securityTeleporterStates[objectID] = isActive
		packets = append(packets, statePackets...)
	}
	return packets, nil
}
