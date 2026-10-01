package game

import "strings"

const CryosFungusNoun = "DEST_prefab_cryos_plants_shascope.Noun"

func (e CampaignDirector) CryosFungusFixtures() []CampaignDirectorMarker {
	if !strings.EqualFold(e.Level, cryosCaveLevel) {
		return nil
	}
	markers := make([]CampaignDirectorMarker, 0)
	for _, markerSet := range e.MarkerSets {
		if !strings.EqualFold(markerSet.Name, cryosCaveSceneryMarkerSet) {
			continue
		}
		for _, marker := range markerSet.Markers {
			if strings.EqualFold(marker.NounName, CryosFungusNoun) {
				markers = append(markers, marker)
			}
		}
	}
	return markers
}
