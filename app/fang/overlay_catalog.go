//go:build windows && cgo && fangoverlay && fangdebug

package main

/*
#include "overlay_internal.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unsafe"
)

const (
	overlayCatalogPath      = "/debug/v1/catalog"
	overlayCatalogByteLimit = 8 << 20
	overlayItemLimit        = 8192
	overlayNameListLimit    = 512
	overlaySpawnCountLimit  = 10
	overlayLevelLimit       = 100
)

var errOverlayCatalogAllocation = errors.New("catalog allocation failed")

type overlayRigblockDTO struct {
	ID           uint16   `json:"id"`
	Name         string   `json:"name"`
	Slot         string   `json:"slot"`
	Classes      []string `json:"classes"`
	Sciences     []string `json:"sciences"`
	MinimumLevel int      `json:"minimum_level"`
	MaximumLevel int      `json:"maximum_level"`
	IsUnique     bool     `json:"is_unique"`
}

type overlayAffixDTO struct {
	ID              uint16   `json:"id"`
	Name            string   `json:"name"`
	PartTypes       []string `json:"part_types"`
	Classes         []string `json:"classes"`
	Sciences        []string `json:"sciences"`
	MinimumLevel    int      `json:"minimum_level"`
	MaximumLevel    int      `json:"maximum_level"`
	IsBasicEligible *bool    `json:"is_basic_eligible"`
	IsUnique        bool     `json:"is_unique"`
}

type overlayCatalogDTO struct {
	SchemaVersion   int                  `json:"schema_version"`
	CatalogHash     string               `json:"catalog_hash"`
	Rigblocks       []overlayRigblockDTO `json:"rigblocks"`
	Prefixes        []overlayAffixDTO    `json:"prefixes"`
	Suffixes        []overlayAffixDTO    `json:"suffixes"`
	EventNames      []string             `json:"event_names"`
	EffectNames     []string             `json:"effect_names"`
	DropCategories  []string             `json:"drop_categories"`
	SpawnCountLimit uint32               `json:"spawn_count_limit"`
	LevelLimit      uint32               `json:"level_limit"`
}

// overlayVocabulary assigns one mask bit per distinct token (case-folded) so
// the render thread filters items without comparing strings. "all" maps to
// OVERLAY_VOCABULARY_ALL; tokens past the bit limit get no bit.
type overlayVocabulary struct {
	names         []string
	indexesByName map[string]int
}

func newOverlayVocabulary() *overlayVocabulary {
	return &overlayVocabulary{indexesByName: make(map[string]int)}
}

func (e *overlayVocabulary) bit(token string) uint32 {
	name := strings.TrimSpace(token)
	if name == "" {
		return 0
	}
	key := strings.ToLower(name)
	if key == "all" {
		return C.OVERLAY_VOCABULARY_ALL
	}
	index, isFound := e.indexesByName[key]
	if !isFound {
		if len(e.names) >= C.OVERLAY_VOCABULARY_LIMIT {
			return 0
		}
		index = len(e.names)
		e.indexesByName[key] = index
		e.names = append(e.names, name)
	}
	return 1 << uint(index)
}

func (e *overlayVocabulary) mask(tokens []string) C.uint {
	var mask uint32
	for _, token := range tokens {
		mask |= e.bit(token)
	}
	return C.uint(mask)
}

func (e *overlayVocabulary) publish(destination *C.overlay_vocabulary) {
	destination.count = C.uint(len(e.names))
	for index, name := range e.names {
		copyOverlayText(destination.names[index][:], name)
	}
}

// overlayCatalogVocabulary groups the three vocabularies built together.
type overlayCatalogVocabulary struct {
	slot    *overlayVocabulary
	class   *overlayVocabulary
	science *overlayVocabulary
}

// syncCatalog fetches the catalog once the session is bound, and again with
// If-None-Match when the server build changes. A failure retries later and
// keeps any catalog already published.
func (e *overlayClient) syncCatalog(ctx context.Context) {
	if e.state.connection != C.OVERLAY_CONNECTION_CONNECTED {
		return
	}
	if e.isCatalogLoaded && e.catalogBuildID == e.buildID {
		return
	}
	if !e.catalogAttempt.IsZero() && time.Since(e.catalogAttempt) < overlayCatalogRetry {
		return
	}
	e.catalogAttempt = time.Now()
	etag := ""
	if e.isCatalogLoaded {
		etag = e.catalogETag
	}
	reply, err := e.send(ctx, overlayRequest{
		method: http.MethodGet,
		path:   overlayCatalogPath,
		etag:   etag,
		limit:  overlayCatalogByteLimit,
	})
	C.overlay_trace_net(C.OVERLAY_TRACE_CATALOG_STATUS, C.uint(reply.status))
	if err != nil {
		// The status trace above records the failure; retry after the delay.
		return
	}
	if reply.status == http.StatusNotModified && e.isCatalogLoaded {
		e.catalogBuildID = e.buildID
		return
	}
	if reply.status != http.StatusOK {
		return
	}
	catalog, err := newOverlayCatalog(reply.payload)
	if err != nil {
		C.overlay_trace_net(C.OVERLAY_TRACE_CATALOG_RIGBLOCKS, 0)
		return
	}
	C.overlay_publish_catalog(catalog)
	C.overlay_trace_net(C.OVERLAY_TRACE_CATALOG_RIGBLOCKS, catalog.rigblock_count)
	e.catalogETag = reply.etag
	e.catalogBuildID = e.buildID
	e.isCatalogLoaded = true
}

// newOverlayCatalog decodes the catalog into one C block: the header, then
// rigblocks, prefixes, suffixes, events, effects and drop categories. The
// block is immutable once published and is never freed.
func newOverlayCatalog(payload []byte) (*C.overlay_catalog, error) {
	var dto overlayCatalogDTO
	err := json.Unmarshal(payload, &dto)
	if err != nil {
		return nil, fmt.Errorf("catalogDecode: %w", err)
	}
	if dto.SchemaVersion != overlaySchemaVersion {
		return nil, fmt.Errorf("catalogSchema: %w", errOverlaySchema)
	}
	rigblocks := dto.Rigblocks[:min(len(dto.Rigblocks), overlayItemLimit)]
	prefixes := dto.Prefixes[:min(len(dto.Prefixes), overlayItemLimit)]
	suffixes := dto.Suffixes[:min(len(dto.Suffixes), overlayItemLimit)]
	eventNames := dto.EventNames[:min(len(dto.EventNames), overlayNameListLimit)]
	effectNames := dto.EffectNames[:min(len(dto.EffectNames), overlayNameListLimit)]
	dropCategories := dto.DropCategories[:min(len(dto.DropCategories), overlayNameListLimit)]
	itemCount := len(rigblocks) + len(prefixes) + len(suffixes)
	nameCount := len(eventNames) + len(effectNames) + len(dropCategories)
	size := C.sizeof_overlay_catalog + itemCount*C.sizeof_overlay_item +
		nameCount*C.sizeof_overlay_name
	catalog := C.overlay_allocate_catalog(C.size_t(size))
	if catalog == nil {
		return nil, fmt.Errorf("catalogBlock: %w", errOverlayCatalogAllocation)
	}
	catalog.spawn_count_limit = C.uint(overlayLimit(dto.SpawnCountLimit, overlaySpawnCountLimit))
	catalog.level_limit = C.uint(overlayLimit(dto.LevelLimit, overlayLevelLimit))
	vocabulary := overlayCatalogVocabulary{
		slot:    newOverlayVocabulary(),
		class:   newOverlayVocabulary(),
		science: newOverlayVocabulary(),
	}
	cursor := unsafe.Add(unsafe.Pointer(catalog), C.sizeof_overlay_catalog)
	catalog.rigblocks = (*C.overlay_item)(cursor)
	catalog.rigblock_count = C.uint(len(rigblocks))
	items := unsafe.Slice((*C.overlay_item)(cursor), len(rigblocks))
	for index, rigblock := range rigblocks {
		fillOverlayRigblock(&items[index], rigblock, vocabulary)
	}
	cursor = unsafe.Add(cursor, len(rigblocks)*C.sizeof_overlay_item)
	catalog.prefixes = (*C.overlay_item)(cursor)
	catalog.prefix_count = C.uint(len(prefixes))
	items = unsafe.Slice((*C.overlay_item)(cursor), len(prefixes))
	for index, prefix := range prefixes {
		fillOverlayAffix(&items[index], prefix, vocabulary)
	}
	cursor = unsafe.Add(cursor, len(prefixes)*C.sizeof_overlay_item)
	catalog.suffixes = (*C.overlay_item)(cursor)
	catalog.suffix_count = C.uint(len(suffixes))
	items = unsafe.Slice((*C.overlay_item)(cursor), len(suffixes))
	for index, suffix := range suffixes {
		fillOverlayAffix(&items[index], suffix, vocabulary)
	}
	cursor = unsafe.Add(cursor, len(suffixes)*C.sizeof_overlay_item)
	catalog.events = (*C.overlay_name)(cursor)
	catalog.event_count = C.uint(len(eventNames))
	cursor = fillOverlayNames(cursor, eventNames)
	catalog.effects = (*C.overlay_name)(cursor)
	catalog.effect_count = C.uint(len(effectNames))
	cursor = fillOverlayNames(cursor, effectNames)
	catalog.drop_categories = (*C.overlay_name)(cursor)
	catalog.drop_category_count = C.uint(len(dropCategories))
	fillOverlayNames(cursor, dropCategories)
	vocabulary.slot.publish(&catalog.slots)
	vocabulary.class.publish(&catalog.classes)
	vocabulary.science.publish(&catalog.sciences)
	return catalog, nil
}

func overlayLimit(limit uint32, fallback uint32) uint32 {
	if limit == 0 || limit > fallback {
		return fallback
	}
	return limit
}

func overlayLevel(level int) C.uint {
	if level < 0 {
		return 0
	}
	if level > 0xFFFF {
		return 0xFFFF
	}
	return C.uint(level)
}

func fillOverlayRigblock(item *C.overlay_item, rigblock overlayRigblockDTO,
	vocabulary overlayCatalogVocabulary) {
	item.id = C.uint(rigblock.ID)
	item.minimum_level = overlayLevel(rigblock.MinimumLevel)
	item.maximum_level = overlayLevel(rigblock.MaximumLevel)
	item.slot_mask = C.uint(vocabulary.slot.bit(rigblock.Slot))
	item.class_mask = vocabulary.class.mask(rigblock.Classes)
	item.science_mask = vocabulary.science.mask(rigblock.Sciences)
	item.is_unique = overlayFlag(rigblock.IsUnique)
	item.is_basic_eligible = 1
	copyOverlayText(item.name[:], overlayItemName(rigblock.Name, rigblock.ID, rigblock.Slot))
}

// Prefixes carry no is_basic_eligible field; they count as eligible.
func fillOverlayAffix(item *C.overlay_item, affix overlayAffixDTO,
	vocabulary overlayCatalogVocabulary) {
	item.id = C.uint(affix.ID)
	item.minimum_level = overlayLevel(affix.MinimumLevel)
	item.maximum_level = overlayLevel(affix.MaximumLevel)
	item.slot_mask = vocabulary.slot.mask(affix.PartTypes)
	item.class_mask = vocabulary.class.mask(affix.Classes)
	item.science_mask = vocabulary.science.mask(affix.Sciences)
	item.is_unique = overlayFlag(affix.IsUnique)
	item.is_basic_eligible = 1
	if affix.IsBasicEligible != nil {
		item.is_basic_eligible = overlayFlag(*affix.IsBasicEligible)
	}
	copyOverlayText(item.name[:], overlayItemName(affix.Name, affix.ID,
		strings.Join(affix.PartTypes, "/")))
}

// overlayItemName mirrors the server's never-empty fallback label.
func overlayItemName(name string, id uint16, slot string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return fmt.Sprintf("#%d %s", id, slot)
}

func fillOverlayNames(cursor unsafe.Pointer, names []string) unsafe.Pointer {
	destinations := unsafe.Slice((*C.overlay_name)(cursor), len(names))
	for index, name := range names {
		copyOverlayText(destinations[index].text[:], name)
	}
	return unsafe.Add(cursor, len(names)*C.sizeof_overlay_name)
}
