package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// Anchor uses the presentation-only noun already used for retained beam ends.
// Its replicated identity makes the native attached-effect slots removable.
func Anchor(objectID uint32, position game.Vec3) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("security anchor object invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID("SweepingBeamMarker.Noun"),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, IsCollisionEnabled: false,
	})
	if err != nil {
		return nil, fmt.Errorf("anchorMarshal: %w", err)
	}
	return packet, nil
}
