package overlay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/developer"
)

const (
	// spawnCountLimit bounds one spawn action; chat spawns one NPC per command.
	spawnCountLimit = 10
	// levelLimit matches the /level 1-100 bound.
	levelLimit = 100
	// nameLengthLimit matches the game's noun and warp-destination limit.
	nameLengthLimit = 128
	dropCategoryAny = "any"
)

// ActionKind identifies one overlay action.
type ActionKind uint8

const (
	ActionSpawn ActionKind = iota + 1
	ActionSummon
	ActionDrop
	ActionLevel
	ActionDNA
	ActionHeal
	ActionPowerFill
	ActionDamage
	ActionPowerDrain
	ActionGoto
	ActionEvent
	ActionKill
	ActionRecap
	ActionVictory
	ActionReset
	ActionDefeat
	ActionWarp
	ActionEffect
)

// ActionKinds lists every action kind in catalog order.
func ActionKinds() []ActionKind {
	return []ActionKind{
		ActionSpawn, ActionSummon, ActionDrop, ActionLevel, ActionDNA,
		ActionHeal, ActionPowerFill, ActionDamage, ActionPowerDrain, ActionGoto,
		ActionEvent, ActionKill, ActionRecap, ActionVictory, ActionReset,
		ActionDefeat, ActionWarp, ActionEffect,
	}
}

// isAccountAction reports whether an action changes only the actor's account
// and therefore needs no game.
func isAccountAction(kind ActionKind) bool {
	return kind == ActionSummon || kind == ActionLevel || kind == ActionDNA ||
		kind == ActionWarp
}

// Reason explains why an action is unavailable or invalid.
type Reason uint8

const (
	ReasonNone Reason = iota
	ReasonNoGame
	ReasonNotDeployed
	ReasonNotWarped
	ReasonWrongMode
	ReasonZoneTerminal
	ReasonNotOnline
	ReasonInvalidField
)

// ResultCode classifies one action outcome.
type ResultCode uint8

const (
	// ResultApplied means the operation changed persistent state immediately.
	ResultApplied ResultCode = iota + 1
	// ResultQueued means gameplay applies the command on its next poll and may
	// still discard it.
	ResultQueued
	ResultInvalid
	ResultUnavailable
	ResultOverflow
	ResultInternal
)

// ActionRequest is one typed overlay action. Only the fields of Kind are
// read. Category is a /drop name ("any", "hand", ...). Name is an event or
// effect name, and Area is a Fang-resolved level name.
type ActionRequest struct {
	Kind            ActionKind
	Noun            string
	Count           int
	Rigblock        uint16
	PrimaryPrefix   uint16
	SecondaryPrefix uint16
	Suffix          uint16
	Category        string
	Level           uint32
	DNA             uint32
	Amount          float32
	X               float32
	Y               float32
	Z               float32
	Name            string
	Area            string
}

// ActionResult is the outcome of one action. QueuedCount counts queued
// commands, and DNATotal is the account total after a DNA grant.
type ActionResult struct {
	Code        ResultCode
	Reason      Reason
	QueuedCount int
	DNATotal    uint32
}

// Execute validates one action, checks its advisory availability, and runs
// the chat operation the matching slash command calls. Gameplay remains
// authoritative for queued commands.
func (e *Service) Execute(
	ctx context.Context, actor Actor, req ActionRequest,
) (ActionResult, error) {
	if !e.isEnabled {
		return ActionResult{}, fmt.Errorf("executeEnabled: %w", ErrDisabled)
	}
	isValid, err := e.isValidAction(ctx, req)
	if err != nil {
		return ActionResult{}, fmt.Errorf("actionValidate: %w", err)
	}
	if !isValid {
		return ActionResult{Code: ResultInvalid, Reason: ReasonInvalidField}, nil
	}
	if !isAccountAction(req.Kind) {
		gameState, stateErr := e.gameState(ctx, actor)
		if stateErr != nil {
			return ActionResult{}, fmt.Errorf("actionAvailability: %w", stateErr)
		}
		availability := actionStates(actor, gameState)[req.Kind]
		if !availability.IsAvailable {
			return ActionResult{Code: ResultUnavailable, Reason: availability.Reason}, nil
		}
	}
	if e.chatService == nil {
		return ActionResult{}, errors.New("action chat service unavailable")
	}
	result, err := e.runAction(ctx, actor, req)
	if err != nil {
		return result, fmt.Errorf("actionRun: %w", err)
	}
	return result, nil
}

func (e *Service) isValidAction(ctx context.Context, req ActionRequest) (bool, error) {
	switch req.Kind {
	case ActionSpawn:
		return isValidNoun(req.Noun) && req.Count >= 1 && req.Count <= spawnCountLimit, nil
	case ActionSummon:
		index, err := e.catalogIndex(ctx)
		if err != nil {
			return false, fmt.Errorf("summonCatalog: %w", err)
		}
		return index.isSummonable(req), nil
	case ActionDrop:
		return slices.Contains(developer.DropCategories(), req.Category), nil
	case ActionLevel:
		return req.Level >= 1 && req.Level <= levelLimit, nil
	case ActionDNA:
		return req.DNA > 0, nil
	case ActionDamage, ActionPowerDrain:
		return isFinite(req.Amount) && req.Amount > 0, nil
	case ActionGoto:
		return isFinite(req.X) && isFinite(req.Y) && isFinite(req.Z), nil
	case ActionEvent:
		return slices.Contains(developer.EventNames(), req.Name), nil
	case ActionEffect:
		definition, isFound := developer.EffectPreviewAsset(req.Name)
		return isFound && definition.Asset != 0 && strings.TrimSpace(req.Name) == req.Name, nil
	case ActionWarp:
		return isValidWarpArea(req.Area), nil
	case ActionHeal, ActionPowerFill, ActionKill, ActionRecap, ActionVictory,
		ActionReset, ActionDefeat:
		return true, nil
	default:
		return false, nil
	}
}

func (e *Service) runAction(
	ctx context.Context, actor Actor, req ActionRequest,
) (ActionResult, error) {
	sender := chat.Participant{ID: actor.ID, Name: actor.DisplayName}
	switch req.Kind {
	case ActionSpawn:
		return e.spawn(ctx, sender, actor.GameID, req)
	case ActionSummon:
		err := e.chatService.SummonItem(ctx, chat.ItemSummonCommand{
			Sender: sender, GameID: actor.GameID, RigblockID: req.Rigblock,
			PrimaryPrefix: req.PrimaryPrefix, SecondaryPrefix: req.SecondaryPrefix,
			Suffix: req.Suffix,
		})
		return completeAction(req.Kind, ResultApplied, err, "summonRun")
	case ActionDrop:
		category := ""
		if req.Category != dropCategoryAny {
			category = developer.DropCategory(req.Category)
		}
		err := e.chatService.TriggerEvent(ctx, chat.EventCommand{
			Sender: sender, GameID: actor.GameID, Name: "drop-create", Category: category,
		})
		return completeAction(req.Kind, ResultQueued, err, "dropRun")
	case ActionLevel:
		err := e.chatService.SetLevel(ctx, chat.LevelCommand{
			Sender: sender, GameID: actor.GameID, Level: req.Level,
		})
		return completeAction(req.Kind, ResultApplied, err, "levelRun")
	case ActionDNA:
		dnaTotal, err := e.chatService.GrantDNA(ctx, chat.DNACommand{
			Sender: sender, GameID: actor.GameID, Amount: req.DNA,
		})
		result, resultErr := completeAction(req.Kind, ResultApplied, err, "dnaRun")
		if err == nil {
			result.DNATotal = dnaTotal
		}
		return result, resultErr
	case ActionHeal:
		err := e.chatService.MutateResource(ctx, chat.ResourceCommand{
			Sender: sender, GameID: actor.GameID, IsHeal: true,
		})
		return completeAction(req.Kind, ResultQueued, err, "healRun")
	case ActionPowerFill:
		err := e.chatService.MutateResource(ctx, chat.ResourceCommand{
			Sender: sender, GameID: actor.GameID, IsPowerFill: true,
		})
		return completeAction(req.Kind, ResultQueued, err, "powerFillRun")
	case ActionDamage:
		err := e.chatService.MutateResource(ctx, chat.ResourceCommand{
			Sender: sender, GameID: actor.GameID, Damage: req.Amount,
		})
		return completeAction(req.Kind, ResultQueued, err, "damageRun")
	case ActionPowerDrain:
		err := e.chatService.MutateResource(ctx, chat.ResourceCommand{
			Sender: sender, GameID: actor.GameID, PowerReduction: req.Amount,
		})
		return completeAction(req.Kind, ResultQueued, err, "powerDrainRun")
	case ActionGoto:
		err := e.chatService.TriggerEvent(ctx, chat.EventCommand{
			Sender: sender, GameID: actor.GameID, Name: "goto", X: req.X, Y: req.Y, Z: req.Z,
		})
		return completeAction(req.Kind, ResultQueued, err, "gotoRun")
	case ActionEvent:
		err := e.chatService.TriggerEvent(ctx, chat.EventCommand{
			Sender: sender, GameID: actor.GameID, Name: req.Name,
		})
		return completeAction(req.Kind, ResultQueued, err, "eventRun")
	case ActionKill:
		err := e.triggerNamedEvent(ctx, sender, actor.GameID, "kill")
		return completeAction(req.Kind, ResultQueued, err, "killRun")
	case ActionRecap:
		err := e.triggerNamedEvent(ctx, sender, actor.GameID, "recap")
		return completeAction(req.Kind, ResultQueued, err, "recapRun")
	case ActionVictory:
		err := e.triggerNamedEvent(ctx, sender, actor.GameID, "victory")
		return completeAction(req.Kind, ResultQueued, err, "victoryRun")
	case ActionReset:
		err := e.triggerNamedEvent(ctx, sender, actor.GameID, "reset")
		return completeAction(req.Kind, ResultQueued, err, "resetRun")
	case ActionDefeat:
		err := e.triggerNamedEvent(ctx, sender, actor.GameID, "defeat")
		return completeAction(req.Kind, ResultQueued, err, "defeatRun")
	case ActionWarp:
		err := e.chatService.RequestWarp(ctx, chat.WarpCommand{Sender: sender, Level: req.Area})
		return completeAction(req.Kind, ResultApplied, err, "warpRun")
	case ActionEffect:
		definition, isFound := developer.EffectPreviewAsset(req.Name)
		if !isFound {
			return ActionResult{Code: ResultInvalid, Reason: ReasonInvalidField}, nil
		}
		err := e.chatService.PreviewEffect(ctx, chat.EffectPreviewCommand{
			Sender: sender, GameID: actor.GameID, Asset: definition.Asset,
		})
		return completeAction(req.Kind, ResultQueued, err, "effectRun")
	default:
		return ActionResult{Code: ResultInvalid, Reason: ReasonInvalidField}, nil
	}
}

func (e *Service) triggerNamedEvent(
	ctx context.Context, sender chat.Participant, gameID uint32, name string,
) error {
	err := e.chatService.TriggerEvent(ctx, chat.EventCommand{
		Sender: sender, GameID: gameID, Name: name,
	})
	if err != nil {
		return fmt.Errorf("eventTrigger: %w", err)
	}
	return nil
}

// spawn queues one NPC per chat command and stops at the first failure, so a
// partial result reports how many commands were queued.
func (e *Service) spawn(
	ctx context.Context, sender chat.Participant, gameID uint32, req ActionRequest,
) (ActionResult, error) {
	queuedCount := 0
	for queuedCount < req.Count {
		err := e.chatService.SpawnNPC(ctx, chat.NPCSpawnCommand{
			Sender: sender, GameID: gameID, NounName: req.Noun,
		})
		if err == nil {
			queuedCount++
			continue
		}
		code, reason, isKnown := classifyActionError(req.Kind, err)
		if !isKnown {
			return ActionResult{Code: ResultInternal}, fmt.Errorf("spawnRun[%d]: %w", queuedCount, err)
		}
		if queuedCount > 0 {
			return ActionResult{Code: ResultQueued, QueuedCount: queuedCount}, nil
		}
		return ActionResult{Code: code, Reason: reason}, nil
	}
	return ActionResult{Code: ResultQueued, QueuedCount: queuedCount}, nil
}

// completeAction maps a chat operation result. Known sentinels become typed
// outcomes; anything else is an internal error for the caller to log.
func completeAction(
	kind ActionKind, successCode ResultCode, err error, step string,
) (ActionResult, error) {
	if err == nil {
		result := ActionResult{Code: successCode}
		if successCode == ResultQueued {
			result.QueuedCount = 1
		}
		return result, nil
	}
	code, reason, isKnown := classifyActionError(kind, err)
	if !isKnown {
		return ActionResult{Code: ResultInternal}, fmt.Errorf("%s: %w", step, err)
	}
	return ActionResult{Code: code, Reason: reason}, nil
}

func classifyActionError(kind ActionKind, err error) (ResultCode, Reason, bool) {
	if errors.Is(err, chat.ErrSenderNotMember) {
		// A game-scoped command fails membership when the game ended or the
		// player left it after the actor was resolved.
		if isAccountAction(kind) {
			return ResultUnavailable, ReasonNotOnline, true
		}
		return ResultUnavailable, ReasonNoGame, true
	}
	if errors.Is(err, chat.ErrDNAOverflow) || errors.Is(err, chat.ErrItemExists) {
		return ResultOverflow, ReasonNone, true
	}
	if errors.Is(err, chat.ErrNPCSpawnUnavailable) || errors.Is(err, chat.ErrEventUnavailable) ||
		errors.Is(err, chat.ErrResourceUnavailable) ||
		errors.Is(err, chat.ErrEffectPreviewUnavailable) ||
		errors.Is(err, chat.ErrWarpUnavailable) || errors.Is(err, chat.ErrItemSummonUnavailable) ||
		errors.Is(err, chat.ErrLevelUnavailable) || errors.Is(err, chat.ErrDNAUnavailable) {
		return ResultUnavailable, ReasonNone, true
	}
	return ResultInternal, ReasonNone, false
}

// isValidNoun mirrors the game's spawn-noun syntax rule
// (game.isValidPlayerEventNoun): word characters followed by ".Noun".
func isValidNoun(nounName string) bool {
	const nounSuffix = ".Noun"
	if len(nounName) <= len(nounSuffix) || len(nounName) > nameLengthLimit ||
		!strings.EqualFold(nounName[len(nounName)-len(nounSuffix):], nounSuffix) {
		return false
	}
	for index := 0; index < len(nounName)-len(nounSuffix); index++ {
		character := nounName[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' {
			continue
		}
		return false
	}
	return true
}

// isValidWarpArea mirrors the /warp charset rule (game.Manager.RequestCampaignWarp).
func isValidWarpArea(area string) bool {
	if area == "" || len(area) > nameLengthLimit {
		return false
	}
	for index := 0; index < len(area); index++ {
		character := area[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func isFinite(number float32) bool {
	return !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
}
