package npc

import (
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

const (
	EliteModifierName     = "EliteModifier"
	EliteModifierDuration = 1_000_000 * time.Second
	EliteHealthBonus      = float32(0.75)
	EliteDamageBonus      = float32(0.50)
	EliteBodyScaleBonus   = float32(0.25)
)

// BossIdentityFromContent converts the identity authored in ClassAttributes
// into the runtime modifiers used by zone simulation. Every elite-ranked
// identity receives the shared elite profile in addition to its authored
// affixes.
func BossIdentityFromContent(
	contentIdentity game.CampaignNPCIdentity,
) (BossIdentity, bool) {
	if !contentIdentity.IsKnown || strings.TrimSpace(contentIdentity.DisplayName) == "" {
		return BossIdentity{}, false
	}
	identity := BossIdentity{
		DisplayName: contentIdentity.DisplayName,
		IsKnown:     true,
	}
	modifierIndex := 0
	isAffixGap := false
	for affixIndex, affixName := range contentIdentity.NPCAffixNames {
		modifierName := contentIdentity.NPCAffixModifierNames[affixIndex]
		modifierID := contentIdentity.NPCAffixModifierIDs[affixIndex]
		if affixName == "" {
			if modifierName != "" || modifierID != 0 {
				return BossIdentity{}, false
			}
			isAffixGap = true
			continue
		}
		if isAffixGap || !isNPCAffixMappingValid(affixName, modifierName, modifierID) {
			return BossIdentity{}, false
		}
		assetStem := strings.TrimSuffix(affixName, ".NPCAffix")
		identity.AffixNames[affixIndex] = affixName
		identity.ModifierNames[modifierIndex] = modifierName
		identity.ModifierIDs[modifierIndex] = modifierID
		modifierIndex++
		if strings.HasPrefix(assetStem, "Aura_") {
			identity.AuraRadius = 12
		}
	}
	identity.ModifierNames[modifierIndex] = EliteModifierName
	identity.ModifierIDs[modifierIndex] = util.HashID(EliteModifierName)
	return identity, true
}

func isNPCAffixMappingValid(affixName string, modifierName string, modifierID uint32) bool {
	return strings.HasSuffix(affixName, ".NPCAffix") &&
		strings.TrimSuffix(affixName, ".NPCAffix") != "" && modifierID != 0 &&
		(modifierName == "" || util.HashID(modifierName) == modifierID)
}

func IsBossIdentityValid(identity BossIdentity) bool {
	if !identity.IsKnown || strings.TrimSpace(identity.DisplayName) == "" ||
		identity.AuraRadius < 0 || math.IsNaN(float64(identity.AuraRadius)) ||
		math.IsInf(float64(identity.AuraRadius), 0) {
		return false
	}
	modifierIndex := 0
	isAffixGap := false
	for affixIndex, affixName := range identity.AffixNames {
		if affixName == "" {
			isAffixGap = true
			continue
		}
		if isAffixGap || !isNPCAffixMappingValid(affixName,
			identity.ModifierNames[affixIndex], identity.ModifierIDs[affixIndex]) {
			return false
		}
		modifierIndex++
	}
	if identity.ModifierNames[modifierIndex] != EliteModifierName ||
		identity.ModifierIDs[modifierIndex] != util.HashID(EliteModifierName) {
		return false
	}
	for index := modifierIndex + 1; index < len(identity.ModifierNames); index++ {
		if identity.ModifierNames[index] != "" || identity.ModifierIDs[index] != 0 {
			return false
		}
	}
	return true
}

func (e BossIdentity) HasModifier(modifierName string) bool {
	if modifierName == "" {
		return false
	}
	modifierID := util.HashID(modifierName)
	for _, candidateID := range e.ModifierIDs {
		if candidateID == modifierID {
			return true
		}
	}
	return false
}

func ApplyEliteProfile(profile game.CampaignNPCProfile) game.CampaignNPCProfile {
	profile.HitPoint *= 1 + EliteHealthBonus
	return profile
}
