package overlay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/developer"
)

// Catalog lists the summonable items and the command vocabulary the overlay
// offers. Enemy nouns and warp levels stay owned by Fang. Callers must not
// modify the returned slices.
type Catalog struct {
	Items           ItemCatalog
	EventNames      []string
	EffectNames     []string
	DropCategories  []string
	SpawnCountLimit int
	LevelLimit      int
}

// catalogIndex is the immutable catalog plus the ID sets summon validation
// reads.
type catalogIndex struct {
	catalog     Catalog
	rigblockIDs map[uint16]bool
	prefixIDs   map[uint16]bool
	suffixIDs   map[uint16]bool
}

// Catalog returns the catalog, built once per process on first use. A failed
// build is retried on the next call.
func (e *Service) Catalog(ctx context.Context) (Catalog, error) {
	if !e.isEnabled {
		return Catalog{}, fmt.Errorf("catalogEnabled: %w", ErrDisabled)
	}
	index, err := e.catalogIndex(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("catalogBuild: %w", err)
	}
	return index.catalog, nil
}

func (e *Service) catalogIndex(ctx context.Context) (*catalogIndex, error) {
	e.catalogMutex.Lock()
	defer e.catalogMutex.Unlock()
	if e.catalog != nil {
		return e.catalog, nil
	}
	if e.itemSource == nil {
		return nil, errors.New("catalog item source unavailable")
	}
	items, err := e.itemSource.Items(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalogItems: %w", err)
	}
	e.catalog = newCatalogIndex(items)
	return e.catalog, nil
}

func newCatalogIndex(items ItemCatalog) *catalogIndex {
	index := &catalogIndex{
		catalog: Catalog{
			Items: ItemCatalog{
				Rigblocks: make([]Rigblock, 0, len(items.Rigblocks)),
				Prefixes:  make([]Affix, 0, len(items.Prefixes)),
				Suffixes:  make([]Affix, 0, len(items.Suffixes)),
			},
			EventNames:      developer.EventNames(),
			EffectNames:     developer.EffectNames(),
			DropCategories:  developer.DropCategories(),
			SpawnCountLimit: spawnCountLimit,
			LevelLimit:      levelLimit,
		},
		rigblockIDs: make(map[uint16]bool, len(items.Rigblocks)),
		prefixIDs:   make(map[uint16]bool, len(items.Prefixes)),
		suffixIDs:   make(map[uint16]bool, len(items.Suffixes)),
	}
	for _, rigblock := range items.Rigblocks {
		if rigblock.ID == 0 {
			continue
		}
		if rigblock.Name == "" {
			rigblock.Name = fallbackItemName(rigblock.ID, []string{rigblock.Slot}, "item")
		}
		rigblock.Classes = nonNilNames(rigblock.Classes)
		rigblock.Sciences = nonNilNames(rigblock.Sciences)
		index.catalog.Items.Rigblocks = append(index.catalog.Items.Rigblocks, rigblock)
		index.rigblockIDs[rigblock.ID] = true
	}
	index.catalog.Items.Prefixes = appendAffixes(
		index.catalog.Items.Prefixes, index.prefixIDs, items.Prefixes, "prefix",
	)
	index.catalog.Items.Suffixes = appendAffixes(
		index.catalog.Items.Suffixes, index.suffixIDs, items.Suffixes, "suffix",
	)
	return index
}

func appendAffixes(
	affixes []Affix, affixIDs map[uint16]bool, sources []Affix, kind string,
) []Affix {
	for _, affix := range sources {
		if affix.ID == 0 {
			continue
		}
		if affix.Name == "" {
			affix.Name = fallbackItemName(affix.ID, affix.PartTypes, kind)
		}
		affix.PartTypes = nonNilNames(affix.PartTypes)
		affix.Classes = nonNilNames(affix.Classes)
		affix.Sciences = nonNilNames(affix.Sciences)
		affixes = append(affixes, affix)
		affixIDs[affix.ID] = true
	}
	return affixes
}

// isSummonable checks summon IDs against the catalog. Affix 0 means none.
func (e *catalogIndex) isSummonable(req ActionRequest) bool {
	return e.rigblockIDs[req.Rigblock] &&
		(req.PrimaryPrefix == 0 || e.prefixIDs[req.PrimaryPrefix]) &&
		(req.SecondaryPrefix == 0 || e.prefixIDs[req.SecondaryPrefix]) &&
		(req.Suffix == 0 || e.suffixIDs[req.Suffix])
}

// fallbackItemName labels an item whose localized name is unknown as
// "#<id> <slot-or-part_types>".
func fallbackItemName(id uint16, slots []string, kind string) string {
	slotNames := make([]string, 0, len(slots))
	for _, slot := range slots {
		if slot != "" {
			slotNames = append(slotNames, slot)
		}
	}
	if len(slotNames) == 0 {
		return fmt.Sprintf("#%d %s", id, kind)
	}
	return fmt.Sprintf("#%d %s", id, strings.Join(slotNames, ","))
}

func nonNilNames(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}
