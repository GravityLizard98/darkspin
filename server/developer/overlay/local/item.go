package local

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/developer/overlay"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

// itemNameLocale is the client text locale the item catalog names come from.
const itemNameLocale = "en-us"

// TextSource reads one imported localization table keyed by its lowercase
// "0x%08x" locale key.
type TextSource interface {
	LocalizedTexts(ctx context.Context, locale string, tableID uint32) (map[string]string, error)
}

type itemSource struct {
	partCatalog *game.PartCatalog
	textSource  TextSource
}

// NewItemSource lists summonable items from the immutable part catalog. A nil
// textSource leaves every item with its fallback label.
func NewItemSource(partCatalog *game.PartCatalog, textSource TextSource) overlay.ItemSource {
	return itemSource{partCatalog: partCatalog, textSource: textSource}
}

// Items reads rigblock and suffix names from the loot name tables the content
// importer references. The prefix name table is not established, so prefixes
// keep their fallback labels.
func (e itemSource) Items(ctx context.Context) (overlay.ItemCatalog, error) {
	if e.partCatalog == nil {
		return overlay.ItemCatalog{}, errors.New("item part catalog unavailable")
	}
	rigblockNamesByKey, err := e.names(ctx, "LootRigblockNames")
	if err != nil {
		return overlay.ItemCatalog{}, fmt.Errorf("rigblockNames: %w", err)
	}
	uniqueRigblockNamesByKey, err := e.names(ctx, "LootUniqueRigblockNames")
	if err != nil {
		return overlay.ItemCatalog{}, fmt.Errorf("uniqueRigblockNames: %w", err)
	}
	suffixNamesByKey, err := e.names(ctx, "LootSuffixNames")
	if err != nil {
		return overlay.ItemCatalog{}, fmt.Errorf("suffixNames: %w", err)
	}
	uniqueSuffixNamesByKey, err := e.names(ctx, "LootUniqueSuffixNames")
	if err != nil {
		return overlay.ItemCatalog{}, fmt.Errorf("uniqueSuffixNames: %w", err)
	}
	definitions := e.partCatalog.Rigblocks()
	items := overlay.ItemCatalog{
		Rigblocks: make([]overlay.Rigblock, 0, len(definitions)),
	}
	for _, definition := range definitions {
		items.Rigblocks = append(items.Rigblocks, overlay.Rigblock{
			ID: definition.RigblockID,
			Name: itemName(
				definition.RigblockID, definition.IsUniqueFamily,
				rigblockNamesByKey, uniqueRigblockNamesByKey,
			),
			Slot:         definition.SlotType,
			Classes:      categoryNames(definition.ClassType),
			Sciences:     categoryNames(definition.ScienceType),
			MinimumLevel: definition.MinimumLevel,
			MaximumLevel: definition.MaximumLevel,
			IsUnique:     definition.IsUniqueFamily,
		})
	}
	for _, definition := range e.partCatalog.Affixes("prefix") {
		items.Prefixes = append(items.Prefixes, overlayAffix(definition, ""))
	}
	for _, definition := range e.partCatalog.Affixes("suffix") {
		name := itemName(
			definition.ID, definition.IsUniqueFamily,
			suffixNamesByKey, uniqueSuffixNamesByKey,
		)
		items.Suffixes = append(items.Suffixes, overlayAffix(definition, name))
	}
	return items, nil
}

func (e itemSource) names(ctx context.Context, tableName string) (map[string]string, error) {
	if e.textSource == nil {
		return map[string]string{}, nil
	}
	namesByKey, err := e.textSource.LocalizedTexts(ctx, itemNameLocale, util.HashID(tableName))
	if err != nil {
		return nil, fmt.Errorf("namesRead: %w", err)
	}
	return namesByKey, nil
}

func overlayAffix(definition game.PartAffixDefinition, name string) overlay.Affix {
	return overlay.Affix{
		ID: definition.ID, Name: name,
		PartTypes:       append([]string{}, definition.PartTypes...),
		Classes:         categoryNames(definition.ClassType),
		Sciences:        categoryNames(definition.ScienceType),
		MinimumLevel:    definition.MinimumLevel,
		MaximumLevel:    definition.MaximumLevel,
		IsBasicEligible: definition.IsBasicEligible,
		IsUnique:        definition.IsUniqueFamily,
	}
}

// itemName prefers the table of the item's family and falls back to the
// other one. An empty result leaves the feature's fallback label in place.
func itemName(
	id uint16, isUnique bool, namesByKey map[string]string,
	uniqueNamesByKey map[string]string,
) string {
	key := fmt.Sprintf("0x%08x", id)
	primaryNamesByKey, secondaryNamesByKey := namesByKey, uniqueNamesByKey
	if isUnique {
		primaryNamesByKey, secondaryNamesByKey = uniqueNamesByKey, namesByKey
	}
	name := firstLine(primaryNamesByKey[key])
	if name != "" {
		return name
	}
	return firstLine(secondaryNamesByKey[key])
}

func firstLine(text string) string {
	lineEnd := strings.IndexByte(text, '\n')
	if lineEnd >= 0 {
		text = text[:lineEnd]
	}
	return strings.TrimSpace(text)
}

// categoryNames splits an authored class or science list the way
// game.partCategoryContains reads it.
func categoryNames(categories string) []string {
	names := make([]string, 0, 2)
	for field := range strings.SplitSeq(strings.ToLower(categories), ",") {
		field = strings.TrimSpace(field)
		if field != "" {
			names = append(names, field)
		}
	}
	return names
}

var _ overlay.ItemSource = itemSource{}
