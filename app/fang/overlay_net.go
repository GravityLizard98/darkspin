//go:build windows && cgo && fangoverlay && fangdebug

package main

/*
#include <stdlib.h>
#include "overlay_internal.h"
*/
import "C"

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/darkspinnet/darkspin/server/buildinfo"
)

const (
	overlayKeyByteCount      = 32
	overlaySchemaVersion     = 1
	overlayPollInterval      = 500 * time.Millisecond
	overlayUnboundGrace      = 5 * time.Second
	overlayCatalogRetry      = 5 * time.Second
	overlayDialTimeout       = 500 * time.Millisecond
	overlayRequestTimeout    = 2 * time.Second
	overlayIdleTimeout       = 30 * time.Second
	overlayStateByteLimit    = 256 << 10
	overlayActionByteLimit   = 16 << 10
	overlayStatePath         = "/debug/v1/state"
	overlayActionPath        = "/debug/v1/action"
	overlayUnboundMessage    = "Session not bound: type /debug again"
	overlayDisabledMessage   = "debug API disabled on this server"
	overlayUnreachableReason = "server unreachable"
)

var (
	errOverlayRedirect      = errors.New("redirect refused")
	errOverlayResponseLimit = errors.New("response too large")
	errOverlaySchema        = errors.New("schema version mismatch")
	errOverlayActionKind    = errors.New("unknown action kind")
)

// overlayActionKindNames maps enum overlay_action_kind to its section 12.2
// wire name.
var overlayActionKindNames = [C.OVERLAY_ACTION_KIND_COUNT]string{
	C.OVERLAY_ACTION_SPAWN:       "spawn",
	C.OVERLAY_ACTION_SUMMON:      "summon",
	C.OVERLAY_ACTION_DROP:        "drop",
	C.OVERLAY_ACTION_LEVEL:       "level",
	C.OVERLAY_ACTION_DNA:         "dna",
	C.OVERLAY_ACTION_HEAL:        "heal",
	C.OVERLAY_ACTION_POWER_FILL:  "power_fill",
	C.OVERLAY_ACTION_DAMAGE:      "damage",
	C.OVERLAY_ACTION_POWER_DRAIN: "power_drain",
	C.OVERLAY_ACTION_GOTO:        "goto",
	C.OVERLAY_ACTION_EVENT:       "event",
	C.OVERLAY_ACTION_KILL:        "kill",
	C.OVERLAY_ACTION_RECAP:       "recap",
	C.OVERLAY_ACTION_VICTORY:     "victory",
	C.OVERLAY_ACTION_RESET:       "reset",
	C.OVERLAY_ACTION_DEFEAT:      "defeat",
	C.OVERLAY_ACTION_WARP:        "warp",
	C.OVERLAY_ACTION_EFFECT:      "effect",
}

type overlayAccountDTO struct {
	DisplayName string `json:"display_name"`
	Level       uint32 `json:"level"`
	DNA         uint64 `json:"dna"`
	PendingWarp string `json:"pending_warp"`
}

type overlayGameDTO struct {
	GameID      uint64 `json:"game_id"`
	Mode        string `json:"mode"`
	IsWarped    bool   `json:"is_warped"`
	PlayerCount uint32 `json:"player_count"`
}

type overlayHeroDTO struct {
	IsDeployed    bool    `json:"is_deployed"`
	ObjectID      uint64  `json:"object_id"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	Z             float64 `json:"z"`
	HitPoint      float64 `json:"hit_point"`
	HitPointMax   float64 `json:"hit_point_max"`
	PowerPoint    float64 `json:"power_point"`
	PowerPointMax float64 `json:"power_point_max"`
}

type overlayNPCDTO struct {
	ObjectID    uint64  `json:"object_id"`
	Name        string  `json:"name"`
	HitPoint    float64 `json:"hit_point"`
	HitPointMax float64 `json:"hit_point_max"`
	Distance    float64 `json:"distance"`
	Direction   string  `json:"direction"`
}

type overlayAvailabilityDTO struct {
	IsAvailable bool   `json:"is_available"`
	Reason      string `json:"reason"`
}

type overlayStateDTO struct {
	SchemaVersion int                               `json:"schema_version"`
	BuildID       string                            `json:"build_id"`
	Version       string                            `json:"version"`
	Account       overlayAccountDTO                 `json:"account"`
	Game          *overlayGameDTO                   `json:"game"`
	Hero          *overlayHeroDTO                   `json:"hero"`
	AliveNPCCount uint32                            `json:"alive_npc_count"`
	NearestNPCs   []overlayNPCDTO                   `json:"nearest_npcs"`
	ActionStates  map[string]overlayAvailabilityDTO `json:"action_states"`
}

// overlayReplyDTO decodes both action results and error bodies; they share
// code and message.
type overlayReplyDTO struct {
	SchemaVersion int    `json:"schema_version"`
	Code          string `json:"code"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	QueuedCount   uint32 `json:"queued_count"`
	DNATotal      uint64 `json:"dna_total"`
}

type overlayRequest struct {
	method string
	path   string
	body   []byte
	etag   string
	limit  int64
}

type overlayResponse struct {
	status  int
	etag    string
	payload []byte
}

// overlayClient owns the loopback debug API session of this game process.
// Only its goroutine touches it after GoOverlayStart returns.
type overlayClient struct {
	baseURL         string
	authorization   string
	httpClient      *http.Client
	state           C.overlay_state
	buildID         string
	generation      uint32
	unboundSince    time.Time
	isStatusTraced  bool
	lastStateStatus int
	catalogETag     string
	catalogBuildID  string
	catalogAttempt  time.Time
	isCatalogLoaded bool
}

// GoOverlayStart generates the per-process session key and starts the
// network worker. The key is published to C, and so appended to /debug, only
// for a loopback endpoint; any other endpoint leaves the worker stopped.
//
//export GoOverlayStart
func GoOverlayStart(host *C.char, port C.ushort) C.int {
	if host == nil {
		C.overlay_trace_net(C.OVERLAY_TRACE_KEY_LENGTH, 0)
		return C.OVERLAY_NET_REMOTE
	}
	address, isLoopback := overlayLoopbackAddress(C.GoString(host), uint16(port))
	if !isLoopback {
		C.overlay_trace_net(C.OVERLAY_TRACE_KEY_LENGTH, 0)
		return C.OVERLAY_NET_REMOTE
	}
	key, err := newOverlayKey()
	if err != nil {
		// Without a key the overlay can only show that it is unavailable.
		C.overlay_trace_net(C.OVERLAY_TRACE_KEY_LENGTH, 0)
		return C.OVERLAY_NET_KEY_FAILED
	}
	C.overlay_publish_key((*C.char)(unsafe.Pointer(&key[0])), C.uint(len(key)))
	C.overlay_trace_net(C.OVERLAY_TRACE_KEY_LENGTH, C.uint(len(key)))
	client := newOverlayClient(address, string(key))
	clear(key)
	go client.run()
	return C.OVERLAY_NET_STARTED
}

// overlayLoopbackAddress accepts only a loopback IP literal or localhost,
// which is mapped to 127.0.0.1 to avoid ::1 resolution surprises.
func overlayLoopbackAddress(host string, port uint16) (string, bool) {
	if port == 0 {
		return "", false
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", false
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), true
}

func newOverlayKey() ([]byte, error) {
	secret := make([]byte, overlayKeyByteCount)
	readCount, err := rand.Read(secret)
	if err != nil {
		return nil, fmt.Errorf("keyRandom: %w", err)
	}
	if readCount != len(secret) {
		return nil, fmt.Errorf("keyLength: %w", io.ErrShortBuffer)
	}
	key := []byte(hex.EncodeToString(secret))
	clear(secret)
	return key, nil
}

func newOverlayClient(address, key string) *overlayClient {
	dialer := &net.Dialer{Timeout: overlayDialTimeout}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          2,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       overlayIdleTimeout,
		ResponseHeaderTimeout: overlayRequestTimeout,
	}
	return &overlayClient{
		baseURL:       "http://" + address,
		authorization: "Bearer " + key,
		httpClient: &http.Client{
			Transport:     transport,
			Timeout:       overlayRequestTimeout,
			CheckRedirect: refuseOverlayRedirect,
		},
	}
}

func refuseOverlayRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

func (e *overlayClient) run() {
	ctx := context.Background()
	for {
		e.serve(ctx)
	}
}

// serve waits for the render thread's signal or the poll interval. A queued
// action is sent even after the overlay closed; state is polled only while
// the overlay is open.
func (e *overlayClient) serve(ctx context.Context) {
	defer e.recoverServe()
	C.overlay_net_wait(C.uint(overlayPollInterval / time.Millisecond))
	e.serveAction(ctx)
	if C.is_overlay_open() == 0 {
		return
	}
	e.syncGeneration()
	e.pollState(ctx)
	e.syncCatalog(ctx)
}

// recoverServe keeps a decoding or conversion bug from terminating the game
// process; the next iteration starts from the published state.
func (e *overlayClient) recoverServe() {
	recovered := recover()
	if recovered == nil {
		return
	}
	C.overlay_trace_net(C.OVERLAY_TRACE_NET_RECOVERED, 1)
}

func (e *overlayClient) syncGeneration() {
	generation := uint32(C.overlay_open_generation())
	if generation == e.generation {
		return
	}
	// A reopen follows a new /debug, whose bind may land after the first poll.
	e.generation = generation
	e.unboundSince = time.Time{}
}

func (e *overlayClient) send(ctx context.Context, req overlayRequest) (overlayResponse, error) {
	var body io.Reader
	if req.body != nil {
		body = bytes.NewReader(req.body)
	}
	request, err := http.NewRequestWithContext(ctx, req.method, e.baseURL+req.path, body)
	if err != nil {
		return overlayResponse{}, fmt.Errorf("requestBuild: %w", err)
	}
	request.Header.Set("Authorization", e.authorization)
	request.Header.Set("Accept", "application/json")
	if req.body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if req.etag != "" {
		request.Header.Set("If-None-Match", req.etag)
	}
	response, err := e.httpClient.Do(request)
	if err != nil {
		return overlayResponse{}, fmt.Errorf("requestDo: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, req.limit+1))
	closeErr := response.Body.Close()
	reply := overlayResponse{status: response.StatusCode, etag: response.Header.Get("ETag")}
	if readErr != nil {
		return reply, fmt.Errorf("responseRead: %w", readErr)
	}
	if closeErr != nil {
		return reply, fmt.Errorf("responseClose: %w", closeErr)
	}
	if int64(len(payload)) > req.limit {
		return reply, fmt.Errorf("responseSize: %w", errOverlayResponseLimit)
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 &&
		response.StatusCode != http.StatusNotModified {
		return reply, fmt.Errorf("responseStatus: %w", errOverlayRedirect)
	}
	reply.payload = payload
	return reply, nil
}

func (e *overlayClient) pollState(ctx context.Context) {
	started := time.Now()
	reply, err := e.send(ctx, overlayRequest{
		method: http.MethodGet,
		path:   overlayStatePath,
		limit:  overlayStateByteLimit,
	})
	latency := time.Since(started)
	if err != nil && reply.status == 0 {
		e.traceStateStatus(0)
		e.publishConnection(C.OVERLAY_CONNECTION_UNREACHABLE, 0)
		return
	}
	e.traceStateStatus(reply.status)
	if err != nil {
		// Oversized, unreadable or redirected replies are server faults.
		e.publishConnection(C.OVERLAY_CONNECTION_FAILED, reply.status)
		return
	}
	switch reply.status {
	case http.StatusOK:
		err = e.applyState(reply.payload, latency)
		if err != nil {
			e.publishConnection(C.OVERLAY_CONNECTION_SCHEMA, reply.status)
			return
		}
		e.unboundSince = time.Time{}
	case http.StatusUnauthorized:
		e.publishUnbound(reply.status)
	case http.StatusNotFound:
		e.publishConnection(C.OVERLAY_CONNECTION_DISABLED, reply.status)
	case http.StatusForbidden:
		e.publishConnection(C.OVERLAY_CONNECTION_FORBIDDEN, reply.status)
	default:
		e.publishConnection(C.OVERLAY_CONNECTION_FAILED, reply.status)
	}
}

// traceStateStatus records the poll status only when it changes, so an open
// overlay does not add two trace lines per second.
func (e *overlayClient) traceStateStatus(status int) {
	if e.isStatusTraced && status == e.lastStateStatus {
		return
	}
	e.isStatusTraced = true
	e.lastStateStatus = status
	C.overlay_trace_net(C.OVERLAY_TRACE_NET_STATUS, C.uint(status))
}

// publishUnbound tolerates 401 for a short grace period: the Blaze /debug
// bind can land after the first poll.
func (e *overlayClient) publishUnbound(status int) {
	if e.unboundSince.IsZero() {
		e.unboundSince = time.Now()
	}
	if time.Since(e.unboundSince) < overlayUnboundGrace {
		e.publishConnection(C.OVERLAY_CONNECTION_CONNECTING, status)
		return
	}
	e.publishConnection(C.OVERLAY_CONNECTION_UNBOUND, status)
}

// publishConnection keeps the last good state visible and updates only the
// connection status.
func (e *overlayClient) publishConnection(connection int, status int) {
	e.state.connection = C.uint(connection)
	e.state.http_status = C.int(status)
	C.overlay_publish_state(&e.state)
}

func (e *overlayClient) applyState(payload []byte, latency time.Duration) error {
	var dto overlayStateDTO
	err := json.Unmarshal(payload, &dto)
	if err != nil {
		return fmt.Errorf("stateDecode: %w", err)
	}
	if dto.SchemaVersion != overlaySchemaVersion {
		return fmt.Errorf("stateSchema: %w", errOverlaySchema)
	}
	var state C.overlay_state
	state.connection = C.OVERLAY_CONNECTION_CONNECTED
	state.http_status = http.StatusOK
	state.latency_ms = C.uint(latency / time.Millisecond)
	state.is_received = 1
	if dto.BuildID != "" && dto.BuildID == buildinfo.ID {
		state.is_build_match = 1
	}
	copyOverlayText(state.build_id[:], dto.BuildID)
	copyOverlayText(state.version[:], dto.Version)
	copyOverlayText(state.display_name[:], dto.Account.DisplayName)
	state.level = C.uint(dto.Account.Level)
	state.dna = C.ulonglong(dto.Account.DNA)
	copyOverlayText(state.pending_warp[:], dto.Account.PendingWarp)
	if dto.Game != nil {
		state.is_game = 1
		state.game_id = C.ulonglong(dto.Game.GameID)
		copyOverlayText(state.mode[:], dto.Game.Mode)
		state.is_warped = overlayFlag(dto.Game.IsWarped)
		state.player_count = C.uint(dto.Game.PlayerCount)
	}
	if dto.Hero != nil {
		state.is_hero = 1
		state.is_deployed = overlayFlag(dto.Hero.IsDeployed)
		state.object_id = C.ulonglong(dto.Hero.ObjectID)
		state.x = C.float(dto.Hero.X)
		state.y = C.float(dto.Hero.Y)
		state.z = C.float(dto.Hero.Z)
		state.hit_point = C.float(dto.Hero.HitPoint)
		state.hit_point_max = C.float(dto.Hero.HitPointMax)
		state.power_point = C.float(dto.Hero.PowerPoint)
		state.power_point_max = C.float(dto.Hero.PowerPointMax)
	}
	state.alive_npc_count = C.uint(dto.AliveNPCCount)
	for index, npc := range dto.NearestNPCs {
		if index >= len(state.npcs) {
			break
		}
		destination := &state.npcs[index]
		destination.object_id = C.ulonglong(npc.ObjectID)
		destination.hit_point = C.float(npc.HitPoint)
		destination.hit_point_max = C.float(npc.HitPointMax)
		destination.distance = C.float(npc.Distance)
		copyOverlayText(destination.name[:], npc.Name)
		copyOverlayText(destination.direction[:], npc.Direction)
		state.npc_count++
	}
	for kind, name := range overlayActionKindNames {
		availability, isFound := dto.ActionStates[name]
		if !isFound {
			copyOverlayText(state.actions[kind].reason[:], "unknown")
			continue
		}
		state.actions[kind].is_available = overlayFlag(availability.IsAvailable)
		copyOverlayText(state.actions[kind].reason[:], availability.Reason)
	}
	e.state = state
	e.buildID = dto.BuildID
	C.overlay_publish_state(&e.state)
	return nil
}

// serveAction forwards at most one mailbox action. The result is published
// on every path, including a recovered panic, so the slot is always freed.
func (e *overlayClient) serveAction(ctx context.Context) {
	var action C.overlay_action
	if C.overlay_take_action(&action) == 0 {
		return
	}
	result := C.overlay_result{action_sequence: action.sequence, kind: action.kind}
	copyOverlayText(result.code[:], "internal")
	copyOverlayText(result.message[:], "overlay request failed")
	defer publishOverlayResult(&result)
	e.performAction(ctx, action, &result)
}

func publishOverlayResult(result *C.overlay_result) {
	C.overlay_publish_result(result)
}

func (e *overlayClient) performAction(ctx context.Context, action C.overlay_action,
	result *C.overlay_result) {
	if action.kind == C.OVERLAY_ACTION_SPAWN {
		result.requested_count = action.count
	}
	body, err := overlayActionBody(action)
	if err != nil {
		copyOverlayText(result.code[:], "invalid")
		copyOverlayText(result.message[:], "invalid overlay action")
		return
	}
	reply, err := e.send(ctx, overlayRequest{
		method: http.MethodPost,
		path:   overlayActionPath,
		body:   body,
		limit:  overlayActionByteLimit,
	})
	result.http_status = C.int(reply.status)
	C.overlay_trace_net(C.OVERLAY_TRACE_ACTION_STATUS, C.uint(reply.status))
	if err != nil {
		copyOverlayText(result.code[:], "transport")
		copyOverlayText(result.message[:], overlayUnreachableReason)
		return
	}
	var dto overlayReplyDTO
	decodeErr := json.Unmarshal(reply.payload, &dto)
	switch {
	case reply.status == http.StatusUnauthorized:
		copyOverlayText(result.code[:], "unauthorized")
		copyOverlayText(result.message[:], overlayUnboundMessage)
	case reply.status == http.StatusNotFound:
		copyOverlayText(result.code[:], "disabled")
		copyOverlayText(result.message[:], overlayDisabledMessage)
	case decodeErr != nil:
		// A reply without the documented body is reported by status only.
		copyOverlayText(result.code[:], "http")
		copyOverlayText(result.message[:], "HTTP "+strconv.Itoa(reply.status))
	default:
		copyOverlayText(result.code[:], dto.Code)
		copyOverlayText(result.reason[:], dto.Reason)
		copyOverlayText(result.message[:], dto.Message)
		result.queued_count = C.uint(dto.QueuedCount)
		result.dna_total = C.ulonglong(dto.DNATotal)
	}
}

// overlayActionBody encodes kind plus exactly that kind's section 12.2
// fields; the server rejects unknown fields.
func overlayActionBody(action C.overlay_action) ([]byte, error) {
	kind := int(action.kind)
	if kind < 0 || kind >= len(overlayActionKindNames) {
		return nil, fmt.Errorf("actionKind: %w", errOverlayActionKind)
	}
	text := overlayGoText(action.text[:])
	fields := map[string]any{"kind": overlayActionKindNames[kind]}
	switch kind {
	case C.OVERLAY_ACTION_SPAWN:
		fields["noun"] = text
		fields["count"] = uint32(action.count)
	case C.OVERLAY_ACTION_SUMMON:
		fields["rigblock"] = uint16(action.rigblock)
		fields["prefix1"] = uint16(action.prefix1)
		fields["prefix2"] = uint16(action.prefix2)
		fields["suffix"] = uint16(action.suffix)
	case C.OVERLAY_ACTION_DROP:
		fields["category"] = text
	case C.OVERLAY_ACTION_LEVEL:
		fields["level"] = uint32(action.level)
	case C.OVERLAY_ACTION_DNA:
		fields["dna"] = uint32(action.dna)
	case C.OVERLAY_ACTION_DAMAGE, C.OVERLAY_ACTION_POWER_DRAIN:
		fields["amount"] = float32(action.amount)
	case C.OVERLAY_ACTION_GOTO:
		fields["x"] = float32(action.x)
		fields["y"] = float32(action.y)
		fields["z"] = float32(action.z)
	case C.OVERLAY_ACTION_EVENT, C.OVERLAY_ACTION_EFFECT:
		fields["name"] = text
	case C.OVERLAY_ACTION_WARP:
		fields["area"] = text
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("actionMarshal: %w", err)
	}
	return payload, nil
}

func overlayFlag(isSet bool) C.uint {
	if isSet {
		return 1
	}
	return 0
}

// copyOverlayText copies at most len(destination)-1 bytes and terminates.
// The renderer replaces anything outside printable ASCII.
func copyOverlayText(destination []C.char, text string) {
	if len(destination) == 0 {
		return
	}
	count := min(len(text), len(destination)-1)
	for index := 0; index < count; index++ {
		destination[index] = C.char(text[index])
	}
	destination[count] = 0
}

func overlayGoText(source []C.char) string {
	length := 0
	for length < len(source) && source[length] != 0 {
		length++
	}
	text := make([]byte, length)
	for index := 0; index < length; index++ {
		text[index] = byte(source[index])
	}
	return string(text)
}
