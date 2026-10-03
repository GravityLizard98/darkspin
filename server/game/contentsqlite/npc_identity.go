package contentsqlite

import (
	"fmt"
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

func campaignNPCIdentity(
	profile contentsqlite.NonPlayerNounProfile, affixesByName map[string]contentsqlite.NPCAffix,
) (game.CampaignNPCIdentity, error) {
	identity := game.CampaignNPCIdentity{
		DisplayName: profile.DisplayName, NPCAffixNames: profile.NPCAffixNames, IsKnown: true,
	}
	// Apply only the ordered class references. Parent/child links neither remove
	// an explicit reference nor add another modifier to the authored class list.
	for index, affixName := range profile.NPCAffixNames {
		if affixName == "" {
			continue
		}
		affix, isFound := affixesByName[strings.ToLower(affixName)]
		if !isFound {
			return game.CampaignNPCIdentity{}, fmt.Errorf("identityAffix[%d]: missing %q", index, affixName)
		}
		if affix.ModifierHash == 0 {
			return game.CampaignNPCIdentity{}, fmt.Errorf("identityModifier[%d]: missing ID", index)
		}
		identity.NPCAffixModifierIDs[index] = affix.ModifierHash
		if affix.ModifierName == nil {
			continue
		}
		if util.HashID(*affix.ModifierName) != affix.ModifierHash {
			return game.CampaignNPCIdentity{}, fmt.Errorf("identityModifier[%d]: name/ID mismatch", index)
		}
		identity.NPCAffixModifierNames[index] = *affix.ModifierName
	}
	return identity, nil
}
