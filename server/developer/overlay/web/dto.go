package web

import (
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/developer/overlay"
)

// schemaVersion is carried by every JSON body of the overlay API.
const schemaVersion = 1

type errorResponse struct {
	SchemaVersion int    `json:"schema_version"`
	Code          string `json:"code"`
	Message       string `json:"message"`
}

type stateResponse struct {
	SchemaVersion int                       `json:"schema_version"`
	BuildID       string                    `json:"build_id"`
	Version       string                    `json:"version"`
	Account       accountDTO                `json:"account"`
	Game          *gameDTO                  `json:"game"`
	Hero          *heroDTO                  `json:"hero"`
	AliveNPCCount int                       `json:"alive_npc_count"`
	NearestNPCs   []npcDTO                  `json:"nearest_npcs"`
	ActionStates  map[string]actionStateDTO `json:"action_states"`
}

type accountDTO struct {
	DisplayName string `json:"display_name"`
	Level       uint32 `json:"level"`
	DNA         uint32 `json:"dna"`
	PendingWarp string `json:"pending_warp"`
}

type gameDTO struct {
	GameID      uint32 `json:"game_id"`
	Mode        string `json:"mode"`
	IsWarped    bool   `json:"is_warped"`
	PlayerCount int    `json:"player_count"`
}

type heroDTO struct {
	IsDeployed        bool    `json:"is_deployed"`
	ObjectID          uint32  `json:"object_id"`
	X                 float32 `json:"x"`
	Y                 float32 `json:"y"`
	Z                 float32 `json:"z"`
	HitPoint          float32 `json:"hit_point"`
	HitPointMaximum   float32 `json:"hit_point_max"`
	PowerPoint        float32 `json:"power_point"`
	PowerPointMaximum float32 `json:"power_point_max"`
}

type npcDTO struct {
	ObjectID        uint32  `json:"object_id"`
	Name            string  `json:"name"`
	HitPoint        float32 `json:"hit_point"`
	HitPointMaximum float32 `json:"hit_point_max"`
	Distance        float32 `json:"distance"`
	Direction       string  `json:"direction"`
}

type actionStateDTO struct {
	IsAvailable bool   `json:"is_available"`
	Reason      string `json:"reason"`
}

func newStateResponse(state overlay.State) stateResponse {
	response := stateResponse{
		SchemaVersion: schemaVersion, BuildID: state.BuildID, Version: state.Version,
		Account: accountDTO{
			DisplayName: state.Actor.DisplayName, Level: state.Actor.Level,
			DNA: state.Actor.DNA, PendingWarp: state.Actor.PendingWarp,
		},
		AliveNPCCount: state.Game.AliveNPCCount,
		NearestNPCs:   make([]npcDTO, 0, len(state.Game.NearestNPCs)),
		ActionStates:  make(map[string]actionStateDTO, len(state.ActionStates)),
	}
	if state.Actor.IsGameFound {
		response.Game = &gameDTO{
			GameID: state.Actor.GameID, Mode: modeNames[state.Actor.Game.Mode],
			IsWarped: state.Actor.Game.IsWarped, PlayerCount: state.Actor.Game.PlayerCount,
		}
	}
	if state.Actor.IsGameFound && state.Game.IsHeroFound {
		hero := state.Game.Hero
		response.Hero = &heroDTO{
			IsDeployed: hero.IsDeployed, ObjectID: hero.ObjectID,
			X: finiteNumber(hero.X), Y: finiteNumber(hero.Y), Z: finiteNumber(hero.Z),
			HitPoint:          finiteNumber(hero.HitPoint),
			HitPointMaximum:   finiteNumber(hero.HitPointMaximum),
			PowerPoint:        finiteNumber(hero.PowerPoint),
			PowerPointMaximum: finiteNumber(hero.PowerPointMaximum),
		}
	}
	for _, npc := range state.Game.NearestNPCs {
		response.NearestNPCs = append(response.NearestNPCs, npcDTO{
			ObjectID: npc.ObjectID, Name: npc.Name,
			HitPoint:        finiteNumber(npc.HitPoint),
			HitPointMaximum: finiteNumber(npc.HitPointMaximum),
			Distance:        finiteNumber(npc.Distance), Direction: npc.Direction,
		})
	}
	for _, kind := range overlay.ActionKinds() {
		actionState := state.ActionStates[kind]
		response.ActionStates[actionNamesByKind[kind]] = actionStateDTO{
			IsAvailable: actionState.IsAvailable, Reason: reasonNames[actionState.Reason],
		}
	}
	return response
}

type catalogResponse struct {
	SchemaVersion   int           `json:"schema_version"`
	CatalogHash     string        `json:"catalog_hash"`
	Rigblocks       []rigblockDTO `json:"rigblocks"`
	Prefixes        []prefixDTO   `json:"prefixes"`
	Suffixes        []suffixDTO   `json:"suffixes"`
	EventNames      []string      `json:"event_names"`
	EffectNames     []string      `json:"effect_names"`
	DropCategories  []string      `json:"drop_categories"`
	SpawnCountLimit int           `json:"spawn_count_limit"`
	LevelLimit      int           `json:"level_limit"`
}

type rigblockDTO struct {
	ID           uint16   `json:"id"`
	Name         string   `json:"name"`
	Slot         string   `json:"slot"`
	Classes      []string `json:"classes"`
	Sciences     []string `json:"sciences"`
	MinimumLevel uint32   `json:"minimum_level"`
	MaximumLevel uint32   `json:"maximum_level"`
	IsUnique     bool     `json:"is_unique"`
}

type prefixDTO struct {
	ID           uint16   `json:"id"`
	Name         string   `json:"name"`
	PartTypes    []string `json:"part_types"`
	Classes      []string `json:"classes"`
	Sciences     []string `json:"sciences"`
	MinimumLevel uint32   `json:"minimum_level"`
	MaximumLevel uint32   `json:"maximum_level"`
	IsUnique     bool     `json:"is_unique"`
}

type suffixDTO struct {
	ID              uint16   `json:"id"`
	Name            string   `json:"name"`
	PartTypes       []string `json:"part_types"`
	Classes         []string `json:"classes"`
	Sciences        []string `json:"sciences"`
	MinimumLevel    uint32   `json:"minimum_level"`
	MaximumLevel    uint32   `json:"maximum_level"`
	IsBasicEligible bool     `json:"is_basic_eligible"`
	IsUnique        bool     `json:"is_unique"`
}

func newCatalogResponse(catalog overlay.Catalog) catalogResponse {
	response := catalogResponse{
		SchemaVersion:   schemaVersion,
		Rigblocks:       make([]rigblockDTO, 0, len(catalog.Items.Rigblocks)),
		Prefixes:        make([]prefixDTO, 0, len(catalog.Items.Prefixes)),
		Suffixes:        make([]suffixDTO, 0, len(catalog.Items.Suffixes)),
		EventNames:      nonNilNames(catalog.EventNames),
		EffectNames:     nonNilNames(catalog.EffectNames),
		DropCategories:  nonNilNames(catalog.DropCategories),
		SpawnCountLimit: catalog.SpawnCountLimit,
		LevelLimit:      catalog.LevelLimit,
	}
	for _, rigblock := range catalog.Items.Rigblocks {
		response.Rigblocks = append(response.Rigblocks, rigblockDTO{
			ID: rigblock.ID, Name: rigblock.Name, Slot: rigblock.Slot,
			Classes: nonNilNames(rigblock.Classes), Sciences: nonNilNames(rigblock.Sciences),
			MinimumLevel: rigblock.MinimumLevel, MaximumLevel: rigblock.MaximumLevel,
			IsUnique: rigblock.IsUnique,
		})
	}
	for _, prefix := range catalog.Items.Prefixes {
		response.Prefixes = append(response.Prefixes, prefixDTO{
			ID: prefix.ID, Name: prefix.Name, PartTypes: nonNilNames(prefix.PartTypes),
			Classes: nonNilNames(prefix.Classes), Sciences: nonNilNames(prefix.Sciences),
			MinimumLevel: prefix.MinimumLevel, MaximumLevel: prefix.MaximumLevel,
			IsUnique: prefix.IsUnique,
		})
	}
	for _, suffix := range catalog.Items.Suffixes {
		response.Suffixes = append(response.Suffixes, suffixDTO{
			ID: suffix.ID, Name: suffix.Name, PartTypes: nonNilNames(suffix.PartTypes),
			Classes: nonNilNames(suffix.Classes), Sciences: nonNilNames(suffix.Sciences),
			MinimumLevel: suffix.MinimumLevel, MaximumLevel: suffix.MaximumLevel,
			IsBasicEligible: suffix.IsBasicEligible, IsUnique: suffix.IsUnique,
		})
	}
	return response
}

// actionRequestDTO decodes into exact widths, so encoding/json rejects
// overflow. Absent fields decode as zero. decodeActionRequest checks every key
// exactly against the kind's allowlist before this typed decode.
type actionRequestDTO struct {
	SchemaVersion *int    `json:"schema_version"`
	Kind          string  `json:"kind"`
	Noun          string  `json:"noun"`
	Count         int     `json:"count"`
	Rigblock      uint16  `json:"rigblock"`
	Prefix1       uint16  `json:"prefix1"`
	Prefix2       uint16  `json:"prefix2"`
	Suffix        uint16  `json:"suffix"`
	Category      string  `json:"category"`
	Level         uint32  `json:"level"`
	DNA           uint32  `json:"dna"`
	Amount        float32 `json:"amount"`
	X             float32 `json:"x"`
	Y             float32 `json:"y"`
	Z             float32 `json:"z"`
	Name          string  `json:"name"`
	Area          string  `json:"area"`
}

func (e actionRequestDTO) actionRequest(kind overlay.ActionKind) overlay.ActionRequest {
	return overlay.ActionRequest{
		Kind: kind, Noun: e.Noun, Count: e.Count, Rigblock: e.Rigblock,
		PrimaryPrefix: e.Prefix1, SecondaryPrefix: e.Prefix2, Suffix: e.Suffix,
		Category: e.Category, Level: e.Level, DNA: e.DNA, Amount: e.Amount,
		X: e.X, Y: e.Y, Z: e.Z, Name: e.Name, Area: e.Area,
	}
}

// actionSpec pairs a wire kind with its feature kind, display label and the
// exact keys the kind accepts besides kind and schema_version.
type actionSpec struct {
	kind   overlay.ActionKind
	label  string
	fields []string
}

var actionKindsByName = map[string]actionSpec{
	"spawn": {
		kind: overlay.ActionSpawn, label: "Spawn", fields: []string{"noun", "count"},
	},
	"summon": {
		kind: overlay.ActionSummon, label: "Summon",
		fields: []string{"rigblock", "prefix1", "prefix2", "suffix"},
	},
	"drop":       {kind: overlay.ActionDrop, label: "Drop", fields: []string{"category"}},
	"level":      {kind: overlay.ActionLevel, label: "Level", fields: []string{"level"}},
	"dna":        {kind: overlay.ActionDNA, label: "DNA", fields: []string{"dna"}},
	"heal":       {kind: overlay.ActionHeal, label: "Heal"},
	"power_fill": {kind: overlay.ActionPowerFill, label: "Power fill"},
	"damage":     {kind: overlay.ActionDamage, label: "Damage", fields: []string{"amount"}},
	"power_drain": {
		kind: overlay.ActionPowerDrain, label: "Power drain", fields: []string{"amount"},
	},
	"goto":    {kind: overlay.ActionGoto, label: "Goto", fields: []string{"x", "y", "z"}},
	"event":   {kind: overlay.ActionEvent, label: "Event", fields: []string{"name"}},
	"kill":    {kind: overlay.ActionKill, label: "Kill"},
	"recap":   {kind: overlay.ActionRecap, label: "Recap"},
	"victory": {kind: overlay.ActionVictory, label: "Victory"},
	"reset":   {kind: overlay.ActionReset, label: "Reset"},
	"defeat":  {kind: overlay.ActionDefeat, label: "Defeat"},
	"warp":    {kind: overlay.ActionWarp, label: "Warp", fields: []string{"area"}},
	"effect":  {kind: overlay.ActionEffect, label: "Effect", fields: []string{"name"}},
}

var actionNamesByKind = func() map[overlay.ActionKind]string {
	namesByKind := make(map[overlay.ActionKind]string, len(actionKindsByName))
	for name, spec := range actionKindsByName {
		namesByKind[spec.kind] = name
	}
	return namesByKind
}()

var reasonNames = map[overlay.Reason]string{
	overlay.ReasonNone:         "",
	overlay.ReasonNoGame:       "no_game",
	overlay.ReasonNotDeployed:  "not_deployed",
	overlay.ReasonNotWarped:    "not_warped",
	overlay.ReasonWrongMode:    "wrong_mode",
	overlay.ReasonZoneTerminal: "zone_terminal",
	overlay.ReasonNotOnline:    "not_online",
	overlay.ReasonInvalidField: "invalid_field",
}

var resultCodeNames = map[overlay.ResultCode]string{
	overlay.ResultApplied:     "applied",
	overlay.ResultQueued:      "queued",
	overlay.ResultInvalid:     "invalid",
	overlay.ResultUnavailable: "unavailable",
	overlay.ResultOverflow:    "overflow",
	overlay.ResultInternal:    "internal",
}

var modeNames = map[overlay.Mode]string{
	overlay.ModeUnknown:  "unknown",
	overlay.ModeChain:    "chain",
	overlay.ModeTutorial: "tutorial",
	overlay.ModeArena:    "arena",
}

type actionResponse struct {
	SchemaVersion int    `json:"schema_version"`
	Code          string `json:"code"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	QueuedCount   int    `json:"queued_count"`
	DNATotal      uint32 `json:"dna_total"`
}

func newActionResponse(
	spec actionSpec, req overlay.ActionRequest, result overlay.ActionResult,
) actionResponse {
	code, isCodeFound := resultCodeNames[result.Code]
	if !isCodeFound {
		code = resultCodeNames[overlay.ResultInternal]
	}
	return actionResponse{
		SchemaVersion: schemaVersion, Code: code, Reason: reasonNames[result.Reason],
		Message:     actionMessage(spec, req, result),
		QueuedCount: result.QueuedCount, DNATotal: result.DNATotal,
	}
}

// actionMessage is a short, display-safe line. It echoes only typed values.
func actionMessage(
	spec actionSpec, req overlay.ActionRequest, result overlay.ActionResult,
) string {
	switch result.Code {
	case overlay.ResultApplied:
		return appliedMessage(spec, req, result)
	case overlay.ResultQueued:
		if req.Kind == overlay.ActionSpawn {
			return fmt.Sprintf("Spawn queued: %d of %d", result.QueuedCount, req.Count)
		}
		return spec.label + " queued"
	case overlay.ResultInvalid:
		return spec.label + ": invalid field"
	case overlay.ResultUnavailable:
		return unavailableMessage(spec, req, result)
	case overlay.ResultOverflow:
		if req.Kind == overlay.ActionDNA {
			return "DNA grant rejected: account total would overflow"
		}
		return spec.label + " rejected: inventory item IDs exhausted"
	default:
		return "internal error"
	}
}

func appliedMessage(
	spec actionSpec, req overlay.ActionRequest, result overlay.ActionResult,
) string {
	switch req.Kind {
	case overlay.ActionSummon:
		return fmt.Sprintf("Summoned rigblock %d to inventory", req.Rigblock)
	case overlay.ActionLevel:
		return fmt.Sprintf("Account level set to %d", req.Level)
	case overlay.ActionDNA:
		return fmt.Sprintf("Granted %d DNA | total=%d", req.DNA, result.DNATotal)
	case overlay.ActionWarp:
		return "Next campaign mission will warp to " + req.Area
	default:
		return spec.label + " done"
	}
}

func unavailableMessage(
	spec actionSpec, req overlay.ActionRequest, result overlay.ActionResult,
) string {
	if req.Kind == overlay.ActionLevel && result.Reason == overlay.ReasonNone {
		return "Level unavailable: level may have been applied; reopen to refresh"
	}
	switch result.Reason {
	case overlay.ReasonNoGame:
		return spec.label + " needs an active game"
	case overlay.ReasonNotDeployed:
		return spec.label + " needs a deployed hero"
	case overlay.ReasonNotWarped:
		return spec.label + " needs a mission entered with /warp"
	case overlay.ReasonWrongMode:
		return spec.label + " is unavailable in this game mode"
	case overlay.ReasonZoneTerminal:
		return spec.label + " is unavailable after the zone ended"
	case overlay.ReasonNotOnline:
		return "Account not online"
	default:
		return spec.label + " unavailable right now"
	}
}

// finiteNumber keeps JSON encoding total: a non-finite value is reported as 0.
func finiteNumber(number float32) float32 {
	if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
		return 0
	}
	return number
}

func nonNilNames(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}
