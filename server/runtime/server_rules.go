package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Bump only when upstream defaults supersede this section's local overrides.
const allyAlertSectionVersion uint32 = 1

// ServerRulesConfiguration is the explicit launcher and persistence DTO.
type ServerRulesConfiguration struct {
	AllyAlert AllyAlertConfiguration `json:"allyAlert" toml:"ally_alert"`
}

type AllyAlertConfiguration struct {
	Version                    uint32 `json:"version" toml:"version"`
	IsEnabled                  bool   `json:"isEnabled" toml:"is_enabled"`
	RangePercent               uint32 `json:"rangePercent" toml:"range_percent"`
	RangeOwner                 string `json:"rangeOwner" toml:"range_owner"`
	MaxHops                    uint32 `json:"maxHops" toml:"max_hops"`
	IsDiagnosticLoggingEnabled bool   `json:"isDiagnosticLoggingEnabled" toml:"is_diagnostic_logging_enabled"`
}

func (e AllyAlertConfiguration) rules() zonenpc.AllyAlertRules {
	return zonenpc.AllyAlertRules{
		IsEnabled: e.IsEnabled, RangePercent: e.RangePercent, RangeOwner: e.RangeOwner,
		MaxHops: e.MaxHops, IsDiagnosticLoggingEnabled: e.IsDiagnosticLoggingEnabled,
	}
}

func serverRulesConfiguration(rules zonenpc.AllyAlertRules) ServerRulesConfiguration {
	return ServerRulesConfiguration{AllyAlert: AllyAlertConfiguration{
		Version:   allyAlertSectionVersion,
		IsEnabled: rules.IsEnabled, RangePercent: rules.RangePercent, RangeOwner: rules.RangeOwner,
		MaxHops: rules.MaxHops, IsDiagnosticLoggingEnabled: rules.IsDiagnosticLoggingEnabled,
	}}
}

// A single store is shared by every gameplay runtime copy and existing zone.
// Readers snapshot a whole policy; a failed save never changes live rules.
type serverRuleStore struct {
	mu       sync.RWMutex
	path     string
	rules    zonenpc.AllyAlertRules
	sections map[string]any
}

func (e *serverRuleStore) AllyAlertRules() zonenpc.AllyAlertRules {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.rules
}

func loadServerRuleStore(ctx context.Context, configPath string) (*serverRuleStore, error) {
	path := filepath.Join(filepath.Dir(configPath), "server.toml")
	payload, err := os.ReadFile(path)
	isNew := errors.Is(err, os.ErrNotExist)
	if err != nil && !isNew {
		return nil, fmt.Errorf("rulesRead: %w", err)
	}
	if isNew {
		// Import the earlier [ally_alert] section once. Thereafter server.toml
		// is authoritative, even if the old section remains in darkspin.toml.
		payload, err = os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("legacyRead: %w", err)
		}
	}
	document, isReplaced, err := decodeServerRules(payload, isNew)
	if err != nil {
		return nil, fmt.Errorf("rulesDecode: %w", err)
	}
	sections := make(map[string]any)
	if !isNew {
		metadata, decodeErr := toml.Decode(string(payload), &sections)
		if decodeErr != nil {
			return nil, fmt.Errorf("sectionsDecode: %w", decodeErr)
		}
		if !metadata.IsDefined("ally_alert") {
			isReplaced = true
		}
	}
	rules := document.AllyAlert.rules()
	err = rules.Validate()
	if err != nil {
		return nil, fmt.Errorf("rulesValidate: %w", err)
	}
	if isNew || isReplaced {
		err = writeServerRules(ctx, path, document, sections)
		if err != nil {
			return nil, fmt.Errorf("rulesCreate: %w", err)
		}
	}
	return &serverRuleStore{path: path, rules: rules, sections: sections}, nil
}

func decodeServerRules(payload []byte, isLegacy bool) (ServerRulesConfiguration, bool, error) {
	document := serverRulesConfiguration(zonenpc.DefaultAllyAlertRules())
	if isLegacy {
		metadata, err := toml.Decode(string(payload), &document)
		if err != nil {
			return ServerRulesConfiguration{}, false, fmt.Errorf("legacyDecode: %w", err)
		}
		// Other legacy darkspin.toml sections are deliberately left alone.
		if metadata.IsDefined("ally_alert", "version") && document.AllyAlert.Version > allyAlertSectionVersion {
			return ServerRulesConfiguration{}, false, errors.New("ally alert settings require a newer server")
		}
		document.AllyAlert.Version = allyAlertSectionVersion
		return document, true, nil
	}
	var raw struct {
		AllyAlert toml.Primitive `toml:"ally_alert"`
	}
	metadata, err := toml.Decode(string(payload), &raw)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("documentDecode: %w", err)
	}
	if !metadata.IsDefined("ally_alert") {
		return document, true, nil
	}
	var header struct {
		Version uint32 `toml:"version"`
	}
	err = metadata.PrimitiveDecode(raw.AllyAlert, &header)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("versionDecode: %w", err)
	}
	if header.Version > allyAlertSectionVersion {
		return ServerRulesConfiguration{}, false, errors.New("ally alert settings require a newer server")
	}
	if header.Version < allyAlertSectionVersion {
		// Discard the complete obsolete section before decoding its fields:
		// removed keys and old field types must not prevent an upstream reset.
		return document, true, nil
	}
	err = metadata.PrimitiveDecode(raw.AllyAlert, &document.AllyAlert)
	if err != nil {
		return ServerRulesConfiguration{}, false, fmt.Errorf("allyAlertDecode: %w", err)
	}
	return document, false, nil
}

func writeServerRules(ctx context.Context, path string, document ServerRulesConfiguration, sections map[string]any) (err error) {
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("rulesContext: %w", err)
	}
	// Preserve unrelated sections when resetting or saving this owned section.
	mergedSections := make(map[string]any, len(sections)+1)
	for name, section := range sections {
		mergedSections[name] = section
	}
	mergedSections["ally_alert"] = document.AllyAlert
	var payload bytes.Buffer
	err = toml.NewEncoder(&payload).Encode(mergedSections)
	if err != nil {
		return fmt.Errorf("rulesEncode: %w", err)
	}
	w, err := os.CreateTemp(filepath.Dir(path), "server-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("rulesTemp: %w", err)
	}
	temporaryPath := w.Name()
	isCommitted := false
	defer func() {
		if isCommitted {
			return
		}
		removeErr := os.Remove(temporaryPath)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("rulesCleanup: %w", removeErr))
		}
	}()
	writtenByteCount, writeErr := w.Write(payload.Bytes())
	if writeErr == nil && writtenByteCount != payload.Len() {
		writeErr = errors.New("incomplete server configuration write")
	}
	if writeErr == nil {
		writeErr = w.Sync()
	}
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("rulesWrite: %w", errors.Join(writeErr, closeErr))
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("rulesCommitContext: %w", err)
	}
	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("rulesReplace: %w", err)
	}
	isCommitted = true
	return nil
}

func (e *Server) ServerRulesConfiguration() (ServerRulesConfiguration, error) {
	if e == nil || e.serverRuleStore == nil {
		return ServerRulesConfiguration{}, errors.New("server rules unavailable")
	}
	return serverRulesConfiguration(e.serverRuleStore.AllyAlertRules()), nil
}

// SetServerRulesConfiguration persists before publishing the new policy. It
// takes effect on the next alert operation, including in already-running zones.
func (e *Server) SetServerRulesConfiguration(
	ctx context.Context, req ServerRulesConfiguration,
) (ServerRulesConfiguration, error) {
	if e == nil || e.serverRuleStore == nil {
		return ServerRulesConfiguration{}, errors.New("server rules unavailable")
	}
	if req.AllyAlert.Version != allyAlertSectionVersion {
		return ServerRulesConfiguration{}, errors.New("server settings version changed; reload before saving")
	}
	rules := req.AllyAlert.rules()
	err := rules.Validate()
	if err != nil {
		return ServerRulesConfiguration{}, fmt.Errorf("rulesValidate: %w", err)
	}
	store := e.serverRuleStore
	store.mu.Lock()
	defer store.mu.Unlock()
	err = ctx.Err()
	if err != nil {
		return ServerRulesConfiguration{}, fmt.Errorf("rulesContext: %w", err)
	}
	err = writeServerRules(ctx, store.path, req, store.sections)
	if err != nil {
		return ServerRulesConfiguration{}, fmt.Errorf("rulesSave: %w", err)
	}
	store.rules = rules
	e.logger.Printf("NPC ally alert policy applied enabled=%t range_percent=%d range_owner=%s max_hops=%d diagnostics=%t",
		rules.IsEnabled, rules.RangePercent, rules.RangeOwner, rules.MaxHops, rules.IsDiagnosticLoggingEnabled)
	return serverRulesConfiguration(rules), nil
}
