// Package web serves the debug overlay API on the shared HTTP router. Every
// route answers only loopback requests that carry no Origin header and name a
// loopback host on the server port.
package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/developer/overlay"
	recaphttp "github.com/darkspinnet/darkspin/server/http"
)

const (
	statePath   = "/debug/v1/state"
	catalogPath = "/debug/v1/catalog"
	actionPath  = "/debug/v1/action"

	requestBodyLimit = 4 << 10
	stateTimeout     = 2 * time.Second
	catalogTimeout   = 10 * time.Second
)

// routeMethods excludes OPTIONS: the overlay never answers CORS preflights.
// Every other method reaches the guard, so a wrong method answers 405.
var routeMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete,
}

// Options wires the overlay HTTP adapter. Port is the server HTTP port the
// request Host must name.
type Options struct {
	OverlayService *overlay.Service
	Logger         *log.Logger
	Port           uint16
}

type api struct {
	overlayService *overlay.Service
	logger         *log.Logger
	port           string
	catalogMutex   sync.Mutex
	catalogBody    []byte
	catalogETag    string
}

// Register adds the overlay routes. Call it only when the overlay is enabled,
// so a disabled server answers 404.
func Register(router *recaphttp.Router, options Options) error {
	if router == nil || options.OverlayService == nil || options.Port == 0 {
		return errors.New("overlay web options incomplete")
	}
	logger := options.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	handler := &api{
		overlayService: options.OverlayService, logger: logger,
		port: strconv.FormatUint(uint64(options.Port), 10),
	}
	err := router.Add(statePath, routeMethods, handler.serveState)
	if err != nil {
		return fmt.Errorf("stateRoute: %w", err)
	}
	err = router.Add(catalogPath, routeMethods, handler.serveCatalog)
	if err != nil {
		return fmt.Errorf("catalogRoute: %w", err)
	}
	err = router.Add(actionPath, routeMethods, handler.serveAction)
	if err != nil {
		return fmt.Errorf("actionRoute: %w", err)
	}
	return nil
}

func (e *api) serveState(
	writer http.ResponseWriter, request *http.Request, _ *recaphttp.URI,
) {
	if !e.admit(writer, request, http.MethodGet) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), stateTimeout)
	defer cancel()
	actor, failureCode := e.authenticate(ctx, writer, request)
	if failureCode != "" {
		return
	}
	state, err := e.overlayService.State(ctx, actor)
	if err != nil {
		e.writeServiceError(writer, actor, "state", err)
		return
	}
	e.writeJSON(writer, http.StatusOK, newStateResponse(state))
}

func (e *api) serveCatalog(
	writer http.ResponseWriter, request *http.Request, _ *recaphttp.URI,
) {
	if !e.admit(writer, request, http.MethodGet) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), catalogTimeout)
	defer cancel()
	actor, failureCode := e.authenticate(ctx, writer, request)
	if failureCode != "" {
		return
	}
	body, etag, err := e.catalog(ctx)
	if err != nil {
		e.writeServiceError(writer, actor, "catalog", err)
		return
	}
	writer.Header().Set("ETag", etag)
	writer.Header().Set("Cache-Control", "no-cache")
	if isETagMatched(request.Header.Get("If-None-Match"), etag) {
		writer.WriteHeader(http.StatusNotModified)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	byteCount, err := writer.Write(body)
	if err != nil {
		e.logger.Printf(
			"Overlay catalog write failed account=%d after %d bytes: %v", actor.ID, byteCount, err,
		)
	}
}

// actionOutcome collects the fields of the single log line written per POST.
type actionOutcome struct {
	accountID int64
	gameID    uint32
	kind      string
	code      string
	err       error
}

func (e *api) serveAction(
	writer http.ResponseWriter, request *http.Request, _ *recaphttp.URI,
) {
	startedAt := time.Now()
	outcome := actionOutcome{kind: "-", code: "forbidden"}
	if request.Method == http.MethodPost {
		defer e.logAction(&outcome, startedAt)
	}
	if !e.admit(writer, request, http.MethodPost) {
		return
	}
	ctx := request.Context()
	actor, failureCode := e.authenticate(ctx, writer, request)
	if failureCode != "" {
		outcome.code = failureCode
		return
	}
	outcome.accountID = actor.ID
	outcome.gameID = actor.GameID
	outcome.code = "invalid"
	dto, message, isDecoded := decodeActionRequest(writer, request)
	if !isDecoded {
		e.writeError(writer, http.StatusBadRequest, "invalid", message)
		return
	}
	spec, isKindFound := actionKindsByName[dto.Kind]
	if !isKindFound {
		outcome.kind = "unknown"
		e.writeError(writer, http.StatusBadRequest, "invalid", "unknown action kind")
		return
	}
	outcome.kind = dto.Kind
	req := dto.actionRequest(spec.kind)
	result, err := e.overlayService.Execute(ctx, actor, req)
	if errors.Is(err, overlay.ErrDisabled) {
		outcome.code = "disabled"
		http.Error(writer, "404 page not found", http.StatusNotFound)
		return
	}
	if err != nil {
		outcome.code = "internal"
		outcome.err = err
		e.writeError(writer, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	response := newActionResponse(spec, req, result)
	outcome.code = response.Code
	e.writeJSON(writer, http.StatusOK, response)
}

func (e *api) logAction(outcome *actionOutcome, startedAt time.Time) {
	duration := time.Since(startedAt).Round(time.Microsecond)
	if outcome.err != nil {
		e.logger.Printf(
			"Overlay action account=%d game=%d kind=%s code=%s duration=%s error=%v",
			outcome.accountID, outcome.gameID, outcome.kind, outcome.code, duration, outcome.err,
		)
		return
	}
	e.logger.Printf(
		"Overlay action account=%d game=%d kind=%s code=%s duration=%s",
		outcome.accountID, outcome.gameID, outcome.kind, outcome.code, duration,
	)
}

// decodeActionRequest reads at most 4 KiB of strict JSON. Every key must
// match kind, schema_version or one of the kind's fields exactly, because
// encoding/json alone matches keys case-insensitively and accepts the fields of
// every kind. An unknown kind returns only its name, which serveAction rejects.
// The returned message is safe to show and never echoes the body.
func decodeActionRequest(
	writer http.ResponseWriter, request *http.Request,
) (actionRequestDTO, string, bool) {
	dto := actionRequestDTO{}
	if request.Body == nil {
		return dto, "request body required", false
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, requestBodyLimit))
	if err != nil {
		return dto, decodeMessage(err), false
	}
	fieldsByKey := map[string]json.RawMessage{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	err = decoder.Decode(&fieldsByKey)
	if err != nil {
		return dto, decodeMessage(err), false
	}
	trailing := json.RawMessage{}
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		return dto, "request body must hold one JSON object", false
	}
	kind := ""
	kindField, isKindFound := fieldsByKey["kind"]
	if isKindFound {
		err = json.Unmarshal(kindField, &kind)
		if err != nil {
			return dto, "field kind has the wrong type or range", false
		}
	}
	spec, isSpecFound := actionKindsByName[kind]
	if !isSpecFound {
		return actionRequestDTO{Kind: kind}, "", true
	}
	for key := range fieldsByKey {
		if key == "kind" || key == "schema_version" || slices.Contains(spec.fields, key) {
			continue
		}
		return dto, "unknown field", false
	}
	decoder = json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&dto)
	if err != nil {
		return dto, decodeMessage(err), false
	}
	if dto.SchemaVersion != nil && *dto.SchemaVersion != schemaVersion {
		return dto, "unsupported schema_version", false
	}
	return dto, "", true
}

func decodeMessage(err error) string {
	bodyLimitErr := &http.MaxBytesError{}
	if errors.As(err, &bodyLimitErr) {
		return "request body exceeds 4 KiB"
	}
	typeErr := &json.UnmarshalTypeError{}
	if errors.As(err, &typeErr) && typeErr.Field == "" {
		return "request body must be a JSON object"
	}
	if errors.As(err, &typeErr) {
		return "field " + typeErr.Field + " has the wrong type or range"
	}
	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		return "unknown field"
	}
	return "malformed JSON"
}

// admit runs the request guard first, then the method check.
func (e *api) admit(writer http.ResponseWriter, request *http.Request, method string) bool {
	if !isLocalOverlayRequest(request, e.port) {
		e.writeError(writer, http.StatusForbidden, "forbidden", "forbidden")
		return false
	}
	if request.Method != method {
		writer.Header().Set("Allow", method)
		e.writeError(writer, http.StatusMethodNotAllowed, "invalid", "method not allowed")
		return false
	}
	return true
}

// isLocalOverlayRequest requires a loopback peer, no Origin header, and a
// localhost or loopback Host on the server port. This closes DNS rebinding and
// LAN access while multiplayer binds every interface.
func isLocalOverlayRequest(request *http.Request, port string) bool {
	if len(request.Header.Values("Origin")) != 0 {
		return false
	}
	peerAddress, err := netip.ParseAddrPort(request.RemoteAddr)
	if err != nil || !peerAddress.Addr().Unmap().IsLoopback() {
		return false
	}
	requestHost, requestPort, err := net.SplitHostPort(request.Host)
	if err != nil || requestPort != port {
		return false
	}
	if strings.EqualFold(requestHost, "localhost") {
		return true
	}
	requestIP := net.ParseIP(requestHost)
	return requestIP != nil && requestIP.IsLoopback()
}

// authenticate resolves the Bearer key. Only the Authorization header carries
// the key; it is never logged. On failure it writes the response and returns
// the failure code it answered; an empty code means success.
func (e *api) authenticate(
	ctx context.Context, writer http.ResponseWriter, request *http.Request,
) (overlay.Actor, string) {
	scheme, key, isFound := strings.Cut(request.Header.Get("Authorization"), " ")
	if !isFound || !strings.EqualFold(scheme, "Bearer") {
		e.writeError(writer, http.StatusUnauthorized, "unauthorized", "type /debug again")
		return overlay.Actor{}, "unauthorized"
	}
	actor, err := e.overlayService.Authenticate(ctx, strings.TrimSpace(key))
	if errors.Is(err, overlay.ErrSessionExpired) {
		e.writeError(writer, http.StatusUnauthorized, "unauthorized", "type /debug again")
		return overlay.Actor{}, "unauthorized"
	}
	if errors.Is(err, overlay.ErrDisabled) {
		http.Error(writer, "404 page not found", http.StatusNotFound)
		return overlay.Actor{}, "disabled"
	}
	if err != nil {
		e.logger.Printf("Overlay authentication failed: %v", err)
		e.writeError(writer, http.StatusInternalServerError, "internal", "internal error")
		return overlay.Actor{}, "internal"
	}
	return actor, ""
}

// catalog encodes the catalog once. catalog_hash is the SHA-256 of the body
// encoded with an empty hash, and the ETag quotes it.
func (e *api) catalog(ctx context.Context) ([]byte, string, error) {
	e.catalogMutex.Lock()
	defer e.catalogMutex.Unlock()
	if e.catalogBody != nil {
		return e.catalogBody, e.catalogETag, nil
	}
	catalog, err := e.overlayService.Catalog(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("catalogRead: %w", err)
	}
	response := newCatalogResponse(catalog)
	unhashedBody, err := json.Marshal(response)
	if err != nil {
		return nil, "", fmt.Errorf("catalogHashEncode: %w", err)
	}
	digest := sha256.Sum256(unhashedBody)
	response.CatalogHash = hex.EncodeToString(digest[:])
	body, err := json.Marshal(response)
	if err != nil {
		return nil, "", fmt.Errorf("catalogEncode: %w", err)
	}
	e.catalogBody = body
	e.catalogETag = `"` + response.CatalogHash + `"`
	return e.catalogBody, e.catalogETag, nil
}

func isETagMatched(ifNoneMatch string, etag string) bool {
	for candidate := range strings.SplitSeq(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

func (e *api) writeServiceError(
	writer http.ResponseWriter, actor overlay.Actor, route string, err error,
) {
	if errors.Is(err, overlay.ErrDisabled) {
		http.Error(writer, "404 page not found", http.StatusNotFound)
		return
	}
	e.logger.Printf("Overlay %s failed account=%d game=%d: %v", route, actor.ID, actor.GameID, err)
	e.writeError(writer, http.StatusInternalServerError, "internal", "internal error")
}

func (e *api) writeError(writer http.ResponseWriter, status int, code string, message string) {
	e.writeJSON(writer, status, errorResponse{
		SchemaVersion: schemaVersion, Code: code, Message: message,
	})
}

func (e *api) writeJSON(writer http.ResponseWriter, status int, response any) {
	body, err := json.Marshal(response)
	if err != nil {
		e.logger.Printf("Overlay response encode failed status=%d: %v", status, err)
		status = http.StatusInternalServerError
		body = []byte(`{"schema_version":1,"code":"internal","message":"internal error"}`)
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	byteCount, err := writer.Write(body)
	if err != nil {
		e.logger.Printf(
			"Overlay response write failed status=%d after %d bytes: %v", status, byteCount, err,
		)
	}
}
