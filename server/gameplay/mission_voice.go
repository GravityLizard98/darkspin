package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
)

// pollMissionVoice runs only after dungeon setup has committed. Allow the
// arrival animation and an additional second for the mission HUD to settle.
func (e *gameplayPeerSession) pollMissionVoice(now time.Time) ([][]byte, error) {
	if e.missionVoiceID == 0 {
		return nil, nil
	}
	if e.missionVoiceReadyAt.IsZero() {
		e.missionVoiceReadyAt = now.Add(heroArrivalLockDuration + time.Second)
		return nil, nil
	}
	if now.Before(e.missionVoiceReadyAt) || now.Before(e.heroInputLockedUntil) {
		return nil, nil
	}
	publication, err := zoneobjective.InitializationPublication(
		e.zone.Objective().State(), uint8(e.binding.Slot),
	)
	if err != nil {
		return nil, fmt.Errorf("voiceObjective: %w", err)
	}
	if publication == nil {
		e.missionVoiceID = 0
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectiveUpdatedMessage{
		ObjectiveID: publication.Update.ObjectiveID,
		PlayerIndex: publication.Update.PlayerIndex,
		Medal:       publication.Update.Medal, Token: publication.Update.Token,
		// Objective voice playback requires the native notification flag.
		Voiceover: e.missionVoiceID, IsShown: true,
	})
	if err != nil {
		return nil, fmt.Errorf("voiceMarshal: %w", err)
	}
	e.missionVoiceID = 0
	e.missionVoiceReadyAt = time.Time{}
	return [][]byte{packet}, nil
}
