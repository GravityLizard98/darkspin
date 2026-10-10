package developer

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/util"
)

// EventName resolves a numbered or named /event alias to its canonical
// gameplay event.
func EventName(alias string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(alias)) {
	case "1", "security-next":
		return "security-next", true
	case "2", "boss-start":
		return "boss-start", true
	case "3", "boss-complete":
		return "boss-complete", true
	default:
		return "", false
	}
}

// EventNames lists the canonical /event names in alias order.
func EventNames() []string {
	return []string{"security-next", "boss-start", "boss-complete"}
}

// EffectPreviewDefinition is one Fang world-effect preview and its server
// event asset.
type EffectPreviewDefinition struct {
	Name    string
	Context string
	Asset   uint32
}

// effectPreviewDefinitions keeps the authored order for the overlay catalog.
var effectPreviewDefinitions = func() []EffectPreviewDefinition {
	definitions := []EffectPreviewDefinition{
		{Name: "character_beam_in_plasma_electric", Context: "beam-in"},
		{Name: "character_beam_out_plasma_electric", Context: "beam-out"},
		{Name: "character_beam_in_bio", Context: "beam-in"},
		{Name: "character_beam_out_bio", Context: "beam-out"},
		{Name: "character_teleport_beam_out", Context: "beam-out"},
	}
	for index := range definitions {
		definitions[index].Asset = util.HashID(definitions[index].Name + ".ServerEventDef")
	}
	return definitions
}()

var effectPreviewDefinitionsByName = func() map[string]EffectPreviewDefinition {
	definitionsByName := make(map[string]EffectPreviewDefinition, len(effectPreviewDefinitions))
	for _, candidate := range effectPreviewDefinitions {
		definitionsByName[strings.ToLower(candidate.Name)] = candidate
	}
	return definitionsByName
}()

// EffectPreviewAsset resolves an authored effect name without case
// sensitivity.
func EffectPreviewAsset(argument string) (EffectPreviewDefinition, bool) {
	name := strings.ToLower(strings.TrimSpace(argument))
	definition, isFound := effectPreviewDefinitionsByName[name]
	return definition, isFound
}

// EffectNames lists the previewable effect names in authored order.
func EffectNames() []string {
	names := make([]string, 0, len(effectPreviewDefinitions))
	for _, definition := range effectPreviewDefinitions {
		names = append(names, definition.Name)
	}
	return names
}

// DropCategory maps a /drop create category to its campaign slot type. The
// player-facing hand category is the authored grasper slot.
func DropCategory(category string) string {
	switch strings.ToLower(category) {
	case "weapon", "foot", "offense", "defense", "utility":
		return strings.ToLower(category)
	case "hand":
		return "grasper"
	default:
		return ""
	}
}

// DropCategoryDisplay maps a campaign slot type back to its /drop name.
func DropCategoryDisplay(category string) string {
	if category == "grasper" {
		return "hand"
	}
	return category
}

// DropCategories lists the player-facing /drop create categories, led by
// "any" for an unrestricted drop.
func DropCategories() []string {
	return []string{"any", "weapon", "hand", "foot", "offense", "defense", "utility"}
}
