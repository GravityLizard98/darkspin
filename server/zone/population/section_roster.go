package population

import (
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

type SectionRoster struct {
	Section  sim.DirectorRouteSection
	Minions  []game.CampaignDirectorEntry
	Specials []game.CampaignDirectorEntry
}

// SectionRosters returns the session's fixed archetype choices, so horde
// composition and later waves consume the same route sections.
func (e *Session) SectionRosters() []SectionRoster {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	rosters := make([]SectionRoster, 0, len(e.sectionRosters))
	for _, roster := range e.sectionRosters {
		rosters = append(rosters, SectionRoster{
			Section:  roster.Section,
			Minions:  append([]game.CampaignDirectorEntry(nil), roster.Minions...),
			Specials: append([]game.CampaignDirectorEntry(nil), roster.Specials...),
		})
	}
	return rosters
}

func loadSectionRosters(
	director game.CampaignDirector, random *sim.SimulatorRandom,
) ([]SectionRoster, error) {
	if len(director.SectionBuckets) == 0 {
		return nil, nil
	}
	minions := PoolEntries(director, "minion")
	specials := PoolEntries(director, "special")
	roster := campaignPopulationTheme{}
	for _, entry := range minions {
		roster.minionNouns = append(roster.minionNouns, entry.NounName)
	}
	for _, entry := range specials {
		roster.lieutenantNouns = append(roster.lieutenantNouns, entry.NounName)
	}
	buckets := append([]game.CampaignSectionBucket(nil), director.SectionBuckets...)
	for index := range buckets {
		if len(minions) == 0 {
			buckets[index].MinionCount = 0
		}
		if len(specials) == 0 {
			buckets[index].SpecialCount = 0
		}
	}
	themes, err := selectSectionThemes(buckets, roster, random)
	if err != nil {
		return nil, fmt.Errorf("rosterSections: %w", err)
	}
	rosters := make([]SectionRoster, 0, 3)
	for _, section := range []sim.DirectorRouteSection{
		sim.DirectorRouteSectionA, sim.DirectorRouteSectionB, sim.DirectorRouteSectionC,
	} {
		theme := themes[section]
		rosters = append(rosters, SectionRoster{
			Section:  section,
			Minions:  sectionEntries(minions, theme.minionNouns),
			Specials: sectionEntries(specials, theme.lieutenantNouns),
		})
	}
	return rosters, nil
}

func sectionEntries(entries []game.CampaignDirectorEntry, nouns []string) []game.CampaignDirectorEntry {
	selectedEntries := make([]game.CampaignDirectorEntry, 0, len(nouns))
	for _, noun := range nouns {
		for _, entry := range entries {
			if strings.EqualFold(entry.NounName, noun) {
				selectedEntries = append(selectedEntries, entry)
				break
			}
		}
	}
	return selectedEntries
}
