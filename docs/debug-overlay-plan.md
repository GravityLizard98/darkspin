# In-game debug overlay (`/debug`): implementation plan

Status: implemented on the experimental branch `experimental/debug-overlay`,
which must never be merged into `main`. Section 13 records how the
implementation differs from this plan and what still needs an in-game check.
References are to the repository at commit `9cbdf5b`. This is revision 3. The previous
revision was fact-checked claim by claim (242 claims) and reviewed through six
lenses (repository rules, security, D3D9/Win32/cgo, simpler alternatives,
gaps, delivery). Each of the 52 resulting findings was re-verified by an
independent skeptic. 51 were confirmed and are incorporated below. Revision 3
was then checked again by five focused reviewers (session binding, per-mode
availability, Fang, consistency, security). All 37 issues they reported were
confirmed by skeptics and are fixed in this text. The main ones:
* the key binding outlives a relaunch;
* a `/debug` that is not the first word would have leaked the key into
  ordinary chat;
* apply-time failures can drop the RakNet peer;
* heal is unavailable in Arena.

Items marked **[verify]** cannot be settled from source alone, because no game
binary is available. Each of them names the spike or lookup that settles it.

### What changed since revision 2

| Area | Revision 2 | Revision 3 | Why |
|---|---|---|---|
| Overlay credential | Launcher-minted 12 h debug JWT passed through the environment | Per-process random key that Fang binds through the Blaze-authenticated `/debug` chat command, accepted only from a loopback Blaze peer; server-held and bound to the account's login token | The launcher has no issuer, several launch paths have no in-process server, the launch JWT expires after 2 minutes, and the shipped JWT secret is public. Blaze is plaintext here, so the old "TLS" rationale was wrong |
| Launcher work | Mint the token and pass it through the environment | None required | Removes environment leakage to child processes and the wrong-account risk |
| `/debug` handling | "Handled locally, not forwarded" | Forwarded like every Darkspin command, with a local toggle posted afterwards (the `/exit` and `/reset` precedent) | Fang cannot swallow chat text today, and adding that would be a new client change |
| Enemy and level lists | Served by the server as an "intersection" with Fang's list | Owned by Fang (its existing arrays). The server serves items, effects, events and drop categories | Those allowlists exist only in `fang.c`; the server checks only syntax |
| Package home | `server/debug` (collides with stdlib `runtime/debug`) | `server/developer/overlay`, next to the existing developer-command rules | `server/developer` already owns `PlanEvent` and `ApplyResource` |
| Validator refactor | Move about 200 lines of parsers into `server/chat` | Move about 55 lines of shared vocabulary (event aliases, effect table, drop categories) into `server/developer` | A typed JSON API does not need the text parsers |
| Policy layer | Multiplayer gate, rate limiter, `forbidden`/`rate_limited` codes | Removed | Every action is already open to any player through chat; Ping counted all online users, not players in the game |
| Results | `ok` | `applied` vs `queued`, plus server-computed per-action availability that includes the apply-time mode gates | Most commands only enqueue. Gameplay re-checks them later and may drop them, and an apply-time failure can disconnect the player |
| HTTP guard | "Loopback only" | Loopback peer + loopback/`localhost` Host + no `Origin` header | DNS rebinding; the server binds 0.0.0.0 in multiplayer |
| Delivery | One big bang after all spikes | Server track and Fang track in parallel, with three usable increments; a read-only Info panel ships before input capture | De-risks the riskiest part (input) last, with value earlier |
| D3D9 details | Present and Reset hooks, a state block, "wrap CreateDevice" | Present-only patch with a defined render-state sequence and no persistent resources; lazy device lookup preferred; Wine async load handled | Shared vtables, recursion through COM macros, Wine loads Fang late |

## 1. Decision summary

```text
 Darkspore.exe (client build 5.3.0.103)                Darkspinner / darkrun process (server)
 +----------------------------------------------+      +-------------------------------------------------+
 | Fang (fang.dll; C + Go, development build)   |      | server/blaze  /debug branch --+                 |
 |  chat hook: "/debug" -> "/debug <key>"  ------+-Blaze-> (authenticated session)     | BindOverlay     |
 |  posts OVERLAY_TOGGLE_MESSAGE (local)        |      |                               v                 |
 |  D3D9 Present patch -> microui panel         |      | server/chat  OverlayBinder port -> overlay.Service|
 |  WndProc input filter (only while open)      |      |                                                  |
 |  Go goroutine: HTTP poll + one action slot --+-HTTP-> overlay/web (loopback, no Origin, Bearer key)   |
 +----------------------------------------------+ JSON |   GET state, GET catalog, POST action            |
                                                       |     -> overlay.Service (server/developer/overlay)|
                                                       |        -> *chat.Service (same typed operations   |
                                                       |           the slash commands call)               |
                                                       |        -> gameplay state (read-only), PartCatalog|
                                                       +-------------------------------------------------+
```

* **Rendering:** Fang draws the overlay inside the game by patching the D3D9
  `Present` slot of the game's device. The UI library is microui with
  stb_easy_font, which is plain C, so Fang stays a C-only cgo build.
* **Server API:** a new feature, `server/developer/overlay`, exposes three
  loopback-only routes:
  * `GET /debug/v1/state`: live state.
  * `GET /debug/v1/catalog`: items, effects, events and drop categories.
  * `POST /debug/v1/action`: one typed action.

  Actions call the same `chat.Service` operations the slash commands call, so
  the server-side behavior matches typing the command: same operation, same
  validated arguments, same authorization. Some commands also have a client-side
  half in Fang (`/warp` and `/spawn` shorthand resolution, client HP/power on
  `/stat`, the local input reset after `/reset`, `/exit`). Sections 4.4 and 7
  say how each of those is reproduced or replaced; `/exit` has no overlay
  equivalent.
* **Authentication:** Fang generates a random 256-bit key per process. When
  the player types `/debug` as the first word, Fang appends the key to the
  command, the same way it already appends client values to `/stat`. It does
  this only when the server endpoint is loopback.
  * The server's `/debug` branch runs on the player's authenticated Blaze
    session. It binds the key to the account's current login token
    (`user.AuthToken`), and only when the Blaze peer is loopback.
  * The key then authenticates HTTP calls as a Bearer token.
  * It stays valid until one of three things happens: that token changes (an
    explicit logout followed by a fresh login), the server restarts, or a newer
    `/debug` from the same account replaces it.
* **Opening the overlay:** `/debug` toggles it. Fang forwards the command (it
  never swallows chat) and toggles visibility locally.
* **Enemy and level lists:** Fang's existing arrays (`fang_spawn_nouns`,
  `fang_warp_locations`, `fang_warp_aliases`). The server serves item, effect,
  event and drop-category data.
* **Off by default:** `[developer] is_overlay_enabled = false` registers no
  routes and makes `/debug` reply with the reason. Overlay code is compiled
  only into development Fang builds.

Rejected alternatives, with the corrected reasons:

* **Forging Blaze requests from Fang.** Blaze is plaintext under Fang: secure
  connect is forced off at `fang.c:7104`, and the redirector answers `SECU=0`
  (`server/blaze/components.go:75`). Fang already rewrites plaintext Blaze TDF
  in `hooked_send` (`fang.c:5946-6024`). It is still rejected, for three
  reasons:
  * Message IDs are allocated by the client's Blaze layer, so injected requests
    can collide with them.
  * Fang would have to find and strip each reply from the client's
    recv/WSARecv stream.
  * Results would come back only as TDF.
* **Reusing the Fang ServerEvent channel** (`DebugEffectPreviewMessage`,
  `server/raknet/application.go:677-705`). It runs server-to-client only, has
  fixed reflected fields, and is delivered only in a committed dungeon
  (`server/gameplay/handler.go:2950-2952`).
* **Passively capturing the session token** from plaintext Blaze or HTTP
  (`sporenet_components.go:137`, `server/game/api.go:937, 1300-1317`).
  Possible, but that token unlocks the whole account HTTP API, and capturing it
  makes Fang depend on parsing replies. Kept only as a documented fallback.
* **A launcher-minted debug token.** No launcher holds an issuer: launch JWTs
  are minted only by `LocalBroker.exchange` (`server/auth/local.go:270-302`).
  Detached instances and `app/darkspin` + `darkrun auth` have no in-process
  server. The shipped `jwt_secret` is public (`server/assets/darkspin.toml:37`).
* **Exchanging the 2-minute launch JWT at Fang startup.** Viable, but it fails
  for launches without a JWT and binds to a login name rather than a live
  session. The chat-bound key covers every launch path and can be re-bound by
  typing `/debug` again.
* **Chat injection** (open chat, paste, Enter) as the action path: no
  structured results, depends on chat UI state, and needs reverse engineering
  of the submit routine. If the server API is ever dropped, the starting point
  is the function containing the patched call at `exe+0xC998`
  (`fang.c:7111-7112`) **[verify in IDA]**.
* **Scaleform UI:** shipped SWFs are immutable and there is no authoring
  toolchain.
* **Companion launcher window:** not "inside the game". It could reuse the
  same API later.

## 2. Approvals (ask on day 0)

`AGENTS.md` ("Client reverse engineering") allows only observational Fang hooks
unless the user approves the exact hook. Ask for H0-H3 on day 0, and ask for
H4 only if S2b requires it. The server track and spike B0a need none of them.
Spike B0b installs H1's pass-through call-site capture, so it runs only after H1
is approved. Its pass-through Present counter is observational and does not
draw.

| ID | Hook | Kind | Needed from |
|---|---|---|---|
| H0 | Recognize `/debug` (command 30) in `hooked_chat_text_convert` in every Fang build, only as the first word of the text, and forward it through the existing 0x1F path. In overlay builds, when the server endpoint is loopback, append the session key (like `/stat`); in every overlay build, post `OVERLAY_TOGGLE_MESSAGE` to the game window afterwards (like `/exit` and `/reset`). Never swallow text. | Extends an existing rewrite hook | Increment 1 |
| H1 | Device acquisition. Preferred: lazily read the device pointer from the renderer singleton (getter `exe+0x3AEBF0`, vtable check `exe+0xC0F558`, field offset from S1a) the first time the overlay opens. Fallback: `patch_call` at `exe+0x912B7E` (expected target `exe+0xA8EB88`) to record the `IDirect3D9*`, plus a one-time patch of its `CreateDevice` vtable slot (16) that calls the original and records the device. Both are pass-through. | Observational | B0b and Increment 1 |
| H2 | Patch the `Present` slot (17) of the captured device's vtable, installed when the overlay first opens. It draws only when `this` is the captured device and the overlay is open, and restores the render target, depth-stencil and all device state. | Draws on top of the frame | Increment 1 |
| H3 | While the overlay is open, a filter at the top of `hooked_game_wndproc` consumes the mouse and keyboard messages it handles. Fang's own Enter poller (`poll_chat_key`) skips posting `CHAT_OPEN_MESSAGE` while an overlay text field owns the keyboard. | Changes input delivery | Increment 2 |
| H4 | *Only if S2b shows stuck input:* post the existing `/reset` input reset (`CHAT_RESET_MESSAGE` to `reset_combat_input_state`, which writes client state through `exe+0xD5EF0`) when the overlay opens and closes, and after an overlay Reset action. Today only `/reset` and scene changes trigger it. | New trigger of a client write | Optional |

Spike fallbacks are not pre-approved. Each needs its own approval of the exact
hook before it is implemented:
* a dummy device for vtable discovery;
* blocking polled input (`GetAsyncKeyState`/`GetCursorPos`);
* pausing game input while the overlay is open;
* chat-submit injection.

## 3. Verified facts the plan relies on

### 3.1 Server commands and operations

* **Command handling.** All slash commands are dispatched in
  `server/blaze/social_components.go` (chain `333-827`; Unknown-command list
  `:332`; help `darkspinChatHelp` `:1032`). The prefixes `\x1f`, `!` and `/`
  are all accepted (`:1045`).
* **Which branches call operations.** Every branch except `/help`, `/taunt`
  and `/exit` parses text and calls a typed, actor-authenticating
  `chat.Service` method (`server/chat/service.go`). Those methods delegate to
  ports implemented in `server/chat/local/developer.go` and `server/gameplay`.
  These exist but are out of scope:
  * `/bug` (alias `/b`);
  * `/ss`;
  * `/follow` (`Follow` `:633`), which queues following an ally in an active
    multiplayer game.

  `/mana` is an alias of `/power` (`social_components.go:685`).
* **Developer-command rules.** `server/developer` already owns them:
  * `PlanEvent` (`event.go:35-57`), with the allowlist reset, goto,
    boss-start, boss-complete, security-next and recap.
  * `ApplyResource` (`resource.go:31-63`).
  * The `raknet103` packet builders.

  The events ai, drop-create, victory, defeat and kill are dispatched in
  `server/gameplay/handler.go:3069-3146`.

| Command | Operation | Result timing | Real precondition (checked when) |
|---|---|---|---|
| `/spawn <noun>` | `SpawnNPC` (`:608`) | queued | active game created from a consumed `/warp` (`binding.IsWarped`), deployed, zone has NPCs, and the noun has a targetable non-pet profile (`handler.go:3245-3260, 3418-3420`); MutationAgent also depends on the zone minion pool. Checked when the next poll drains the queue; a warp, zone or profile failure is returned as an error (`handler.go:3253, 3259`) and can drop the RakNet peer |
| `/summon r p1 p2 s` | `SummonItem` (`:763`) | applied (persistent); in-game presentation queued | none; the IDs must round-trip through `sporenet.NewPart` (`local/developer.go:35-40`); always level 1, Basic (`:30-31`) |
| `/drop create [cat]` | `TriggerEvent("drop-create")` | queued | ModeChain, checked at enqueue (`game/manager.go:917-919`) and again at apply time (`developer_drop.go:29-36`) |
| `/level n` | `SetLevel` (`:778`) | applied | 1-100. Exception: it saves before re-reading the user, so it can save and still return `ErrLevelUnavailable` (`local/developer.go:74-80`) |
| `/dna n` | `GrantDNA` (`:810`) | applied | amount > 0 (service); overflow rejected in `sporenet.UserManager.GrantDNA` (`user_features.go:138-140`) and mapped to `ErrDNAOverflow` |
| `/heal`, `/power [-n]`, `/damage n` | `MutateResource` (`:544`) | queued | dungeon stage, committed setup, deployed, squad present, zone not terminal (`handler.go:2986-2989, 3040-3043`). `PowerReduction` must be > 0; chat negates `/power -n` (`social_components.go:1147-1153`). `/heal` also needs ModeChain or ModeTutorial (`gameplay/session.go:4802-4804`, reached via `status.go:1262, 1365`); in Arena it is consumed and then fails with a resourceApply error (`handler.go:3019-3021`), not a logged discard |
| `/goto`, `/kill`, `/reset`, `/recap`, `/victory`, `/defeat`, `/event` | `TriggerEvent` (`:582`) | queued | dungeon, committed, deployed, not terminal. kill/defeat/goto/event/reset/recap and chain victory need ModeChain, checked only at apply time (`status.go:1393, 1689, 1731, 1768`, `recap.go:25`). victory has mode-specific effects: in Tutorial it marks completion in memory, persisted when the player returns to ship (`game/gameplay_session.go:845-847`, `blaze/game_manager_component.go:550-557`); in Arena it sets an in-memory result that ends the zone for every Arena player, with no PvP stats written (`gameplay/arena.go:1153-1167`); at the Overdrive chain level it persists the Overdrive unlock immediately (`gameplay/handler.go:3084-3097`) |
| `/ai` | `TriggerEvent("ai")` | queued | at least 2 players, Chain or Arena (`manager.go:920-923`). **Chat-only** in this plan |
| `/warp area` | `RequestWarp` (`:794`) | applied as a pending one-shot for the next campaign game | none; `WarpCommand` has no GameID; the server checks only the charset `[A-Za-z0-9_-]{1,128}` (`manager.go:1322-1339`) |
| `/effect name world` | `PreviewEffect` (`:519`) | queued | deployed hero in a committed dungeon; world mode only, attached is rejected (`social_components.go:504`) |
| `/hint`, `/loc`, `/stat` | `Hint`, `Location`, `ResourceStatus` (gameplay) | read | deployed hero |
| `/ping` | `Ping` (`:502`, in `chat.Service`) | read | online actor; `PlayerCount` counts **every online user** (`ScopeGlobal`), not players in the game |

* **Queueing.** `MutateResource`, `TriggerEvent`, `SpawnNPC` and
  `PreviewEffect` only enqueue onto `game.Instance`. When the next poll drains
  the queue, gameplay handles failures in two ways:
  * **Coarse eligibility** (dungeon, committed, deployed, squad, not terminal):
    ineligible commands are discarded with a log line only (`handler.go:2966,
    3006, 3059`).
  * **Mode gates and other apply-time checks** run after the command is
    consumed and return errors through `pendingRuntime.poll`
    (`handler.go:2713-2723`). These are `status.go:1393, 1689, 1731, 1768`,
    `recap.go:25`, `developer_drop.go:29-36`, `handler.go:3247-3260`, and the
    heal path's Chain/Tutorial gate (`session.go:4802-4803`).

  When that poll runs on a client ping or control packet, `HandleAndWriteDatagrams`
  **removes the RakNet peer** (`raknet/server.go:782-785`); the background poll
  loop only reports the error (`retransmit.go:289-292`). `/reset` also performs
  registry retirements before its ModeChain check (`handler.go:3166-3180`). The
  event queue is an unbounded slice (`manager.go:933`). All of this applies to
  the slash commands today; the overlay's availability pre-check (4.5) exists to
  avoid triggering it.
* **Side effects.** level, dna and summon persist to the account even in the
  ship hub (GameID 0). kill, recap, victory, spawn, drop and event affect the
  whole party or game, exactly as the slash command does. heal restores only
  the actor's own squad. goto, reset and defeat act on the actor; defeat causes
  a party Game Over only when every squad is down.
* **Shared vocabulary.** Three pieces of command vocabulary live only in the
  blaze transport and are needed by the API:
  * `developerEventName` (`:1082`);
  * `effectPreviewDefinition` / `effectPreviewDefinitionByName` /
    `effectPreviewAsset` (`:1095-1121`, which use `util.HashID(name +
    ".ServerEventDef")`);
  * `dropCategory` / `dropCategoryDisplay` (`:1055-1071`, hand↔grasper).

  The string parsers (`:1123-1153`) and `warpAreaCatalog` (`:946`, display
  text) are not needed by a typed API.
* **Fang's half of chat.** Fang resolves `/warp` numbers and partial names
  and `/spawn` partial nouns before the text reaches the server
  (`find_fang_*` `fang.c:4067-4161`, `normalize_fang_*` `:4292, :4383`). It
  appends client HP/power to `/stat` (`:4342`), posts the local input reset
  after `/reset` (`:4502-4506`), and closes the window on `/exit`
  (`:4492-4500`).

### 3.2 Catalog data

* **Enemies.** `fang_spawn_nouns[]` (`fang.c:3883`, 137 entries) is what chat
  accepts after Fang's resolution. The server has no Go copy and checks only
  noun syntax (`manager.go:943-959`). Profiles are global
  (`contentsqlite/director.go:181, 316-317`), so no per-zone list is needed.
  MutationAgent is the only zone-dependent noun.
* **Levels.** `fang_warp_locations[]` (`fang.c:3772`, 61 entries) and
  `fang_warp_aliases[]` (`:3839`, 37 numeric aliases). `warpAreaCatalog` is
  help text only, and `level_alias` holds text aliases.
* **Items.** Rigblocks and affixes are already in memory as `*game.PartCatalog`
  (`server/game/part_catalog.go:154`, loaded at `server/runtime/server.go:758`).
  Its fields are private and it has no enumerators.
  * Scale: 2,408 rigblocks and about 700 affixes.
  * Fields: `class`, `science` and `part_type` are CSV lists, and slot tokens
    include `grasper`.
  * Level ranges: `minimum_level` can be 0, and an empty range means the item
    is disabled.
  * Valid IDs (`sporenet/part.go:91-127`): rigblocks 1-1573 and 10001-10835,
    prefixes 1-338, suffixes 1-83 and 10001-10275.
  * `/summon` enforces no compatibility between rigblock and affixes.
* **Item names.** These are resolvable without importer changes:
  `localization_text` with `locale='en-us'`, `table_id =
  hashID('LootRigblockNames' | 'LootUniqueRigblockNames')`, and `locale_key =
  fmt.Sprintf("0x%08x", id)`. Suffixes use `LootSuffixNames` /
  `LootUniqueSuffixNames` (`content/loot_image.go:277-282`,
  `content/loot_affix.go:194`). The prefix table name is **[verify]** with one
  `darkrun db localization_text get table_id=<hash>` lookup. Fallback label:
  `#<id> <slot>`.

### 3.3 Live state available

* **Registry.** `gameplaySessionRegistry` keys sessions by remote address.
  Select the actor's session by `binding.UserID`/`binding.GameID`. Per-session
  data lives on `gameplayPeerSession`:
  * `binding` (UserID, GameID, IsWarped, Mode, ParticipantCount);
  * `stage`;
  * the embedded hero state (deployed object, position, HP/power and maxima);
  * zone membership (`zone.NPCs().Snapshots()`).
* **Existing reads:**
  * `ResourceStatus`, `Location` and `Hint` (gameplay).
  * `BugContext` (`gameplay/bug.go:14`): a combined hero/mission/squad view.
    It is heavy and fails when squad data is unavailable.
  * `SyncSnapshot` (`gameplay/snapshot.go:24`): every object, but it also
    serializes timelines and spins on `TryRLock`, so it is too heavy to poll.
  * None of them returns a bounded nearest-NPC list cheaply.
* **Reaching the registry.** `Lifecycle.syncSnapshot`
  (`server/gameplay/handler.go:813`) holds the registry. New gameplay reads
  reach it with a type assertion, as `scenario.go:34` does, so
  `handler.go` (6,204 lines) needs no new plumbing.
* **Player count.** `binding.ParticipantCount` and
  `Instance.ParticipantCount()` are the frozen combat-scaling count
  (`manager.go:530-540`), not live humans. Players in a game =
  `len(Instance.Players())`.

### 3.4 Transport and identity facts

* **Plaintext sessions.** Under Fang, Blaze and HTTP run in plaintext. The
  session token (`user.AuthToken`) is in the Blaze Auth reply and in HTTP API
  requests.
* **Launch JWT.** It lives 2 minutes (`server/auth/local.go:23`), is minted
  only by `LocalBroker.exchange`, and Fang unsets it after reading
  (`app/fang/main.go:52-54`).
* **Ports and binding.** HTTP and Blaze share `[server].port` on one socket
  (`server/runtime/server.go:796-813, 1284-1297`). One handler serves every
  port. The server binds `127.0.0.1`, or `0.0.0.0` when multiplayer is enabled
  (`server.go:503-506`).
* **Router.** `recaphttp.Router.Add` is mutex-guarded (`server/http/router.go:
  32-58`). It has no Host or Origin validation and answers OPTIONS with 404.
  The in-server broker (`NewAccountBroker`, `server/runtime/server.go:631`)
  rejects any `Origin`. For loopback peers it requires a localhost/loopback
  Host with the exact configured HTTP port (`server/auth/local.go:316-321`).
  It also admits private-LAN peers with no Host check (`local.go:323`), so the
  overlay guard must not reuse `requestAccess`; it keeps its own loopback-peer
  requirement. `game.isLoopbackRequest` is unexported.
* **Fang's endpoint.** Fang resolves the server endpoint from
  `DARKSPIN_SERVER_ADDRESS`, else `localhost:42127` (`app/fang/main.go:31-35`).
  The GUI local launch passes `127.0.0.1:<port>` (`app/darkspinner/app.go:
  540-541`).
* **Wiring point.** `chatService` is created at `server.go:669` and receives
  its ports through `Use*` setters (`:673-685`). Gameplay providers are wired
  at `:853-856`, before `application := &Server{` (`:881`).
* **Pre-existing leak (out of scope, see R5).** `api.panel.listUsers`
  (`server/game/api.go:226-227`) serializes `[]*sporenet.User`, including
  `AuthToken` and `Password`, with no authentication.

### 3.5 Fang facts

* **Build.** Fang is the Go `c-shared` package `./app/fang`
  (`GOARCH=386`). `buildFang` sets `CC=i686-w64-mingw32-gcc` only on
  non-Windows hosts (`magefile.go:272-279`). It is C only; no C++ compiler is
  required. cgo compiles only `.c` files at the top level of the package
  directory.
* **Diagnostics in every build.** Every current `buildFang` caller passes
  `isDiagnostics=true` (`fangdebug`), including the CI release build
  (`magefile.go:745`). Darkspinner embeds `bin/game/fang.dll` (`:345-346`), and
  `ensureFang` rewrites `cache/fang.dll` on every launch
  (`app/darkspinner/main.go:675-690`). `integrity.json` lists only shipped game
  files.
* **Launch abort.** Any nonzero `fang_install` result aborts the Windows launch
  (`app/darkspinner/launch_windows.go:328-330`).
* **Wine.** Fang is loaded from a thread created in the `VERSION.dll` proxy's
  DllMain while the game keeps starting (`app/fangproxy/proxy.c:325-340`). The
  Wine launcher forces builtin d3d9, i.e. wined3d (`launch_wine.go:165-171`),
  and also serves macOS.
* **Chat hook.** `hooked_chat_text_convert` (`fang.c:4433-4508`) never drops
  text. Recognized commands get the 0x1F prefix (`:4482`) and are converted
  and forwarded. Local side effects run after forwarding: `/exit` posts
  `WM_CLOSE`, `/reset` posts `CHAT_RESET_MESSAGE` (`WM_APP+0x46`, `:380`).
* **Window procedure.** `hooked_game_wndproc` starts at `fang.c:2081`; the
  Ctrl+V and Enter branches come first. `install_chat_wndproc` runs on the
  branding thread, possibly late.
* **Enter poller.** `poll_chat_key` (`:2153-2174`, started at `:7063`) polls
  `GetAsyncKeyState(VK_RETURN)` on its own thread in every build and posts
  `CHAT_OPEN_MESSAGE`.
* **Input reset.** `reset_combat_input_state` (static, `:2063`) runs only for
  `/reset` and on scene change (`:4912`). It does not run when chat opens.
* **Display hooks.** `display.c` hooks `App::Update` and the Alt+Enter
  borderless toggle only when `DARKSPIN_BORDERLESS_FULLSCREEN=1` (development
  channel plus `[game] is_borderless_fullscreen_enabled`; default off). By
  default, Alt+Enter is the native exclusive-fullscreen toggle with device
  Reset.
* **Frame hook.** The always-installed per-frame client-thread hook is
  `hooked_frame_delta` at `exe+0x5BF1F6` (`fang.c:1190-1197, 7167-7168`).
* **Scenario collision.** `scenario_renderer.c` already patches the
  `Direct3DCreate9` call at `exe+0x912B7E`, but only in `FANG_SCENARIO`
  builds. `patch_call` refuses a site whose current target is not the
  expected one.
* **Helpers** (`hook.h`): `readable_range`, `patch_call`, `patch_pointer`
  (sets `PAGE_READWRITE`), `trace_client_state` (one integer per key; written
  only when the launch sets a client trace path, which Darkspinner does by
  default as `game.jsonl`).
* **Fail-closed patterns.** The existing patterns are PE header,
  `SizeOfImage` and byte-signature checks (`display.c:509-531`,
  `camera.c:144-161`), plus `patch_call`'s opcode and target check
  (`fang.c:6484-6496`). The supported client is 5.3.0.103
  (`content/sqlite/build.go:21`). `5.3.0.127` in `server/game/api.go` is a
  server-side default string.
* **No precedent.** No Fang Go code currently runs goroutines or network I/O.

### 3.6 Repository rules that shape the work

* Immutable game files.
* Observational Fang hooks unless approved.
* No git staging/commit by the agent.
* No tests created or run; verify with production builds and the real path.
* Changelog only when finished.
* No new behavior in files over 5,000 lines (`fang.c` 7,284;
  `gameplay/handler.go` 6,204; `gameplay/session.go` 5,248).
* Package by feature, ports defined by the consumer, adapters in
  subpackages, transports only decode/call/encode, explicit DTOs.
* Go style: breadcrumbs, guard clauses, no inline error initializers, no `, _`.
* Naming: `is`/`are` booleans, plural only for collections, `e` receivers,
  `req` payloads.
* Themed launcher UI only.
* Output under `bin/game/logs/<topic>` (never `bin/game/logs/bugs`).

## 4. Server design

### 4.1 Package layout and change surface

```text
server/developer/vocabulary.go        moved from blaze, unchanged logic: developer event aliases,
                                      effect preview table, drop categories (+ ordered lists for the catalog)
server/developer/overlay/             feature (package overlay): Service, session store, actions, state,
                                      catalog, ports. No HTTP/JSON/SQL/TDF knowledge.
server/developer/overlay/local/       adapters: ActorSource over *sporenet.UserManager + *game.Manager;
                                      ItemSource over *game.PartCatalog (+ optional name lookup)
server/developer/overlay/web/         HTTP adapter: Register(router, Options), request guard, DTOs,
                                      code->message mapping, action log line
server/gameplay/overlay_state.go      StateSource: Lifecycle.OverlayState via the syncSnapshot assertion
server/gameplay/eligibility.go        behavior-preserving extraction of the four inline eligibility
                                      predicates from handler.go (shrinks the oversized file)
server/chat/service.go                OverlayBindCommand{Sender, Key, IsLocalPeer}, OverlayBinder port,
                                      UseOverlayBinder, BindOverlay operation; sentinels
                                      ErrOverlayUnavailable (no binder or feature off),
                                      ErrOverlayKeyMissing, ErrOverlayKeyInvalid, ErrOverlayPeerRemote
server/blaze/social_components.go     /debug branch; imports the moved vocabulary
server/game/part_catalog.go           read-only enumerators (copies, catalog order)
server/game/config.go + assets        [developer] is_overlay_enabled (six edits, 4.9)
content/sqlite/                       optional: one read-only item-name lookup (uppercase SQL)
server/runtime/server.go              one wiring block after line 856
```

Dependency direction:
* `overlay` imports `chat` and `developer`.
* `chat` defines `OverlayBinder`, which `overlay.Service` implements.
* `gameplay` imports `overlay` for its result types, as it already does
  with `chat`.
* The composition root wires the adapters.
* `overlay` imports neither `gameplay` nor `game`. There is no import cycle.

### 4.2 Ports (defined in `server/developer/overlay`)

```go
// ActorSource resolves an online account into the facts the overlay needs.
// The adapter hashes the session token; the raw token never enters the feature.
type ActorSource interface {
    Actor(ctx context.Context, accountID int64) (Actor, error) // ErrActorOffline when not active
}

// StateSource reads the actor's live gameplay state without mutating it.
type StateSource interface {
    OverlayState(ctx context.Context, req StateRequest) (GameState, error)
}

// ItemSource lists summonable items for the catalog.
type ItemSource interface {
    Items(ctx context.Context) (ItemCatalog, error)
}
```

* `Actor` carries:
  * ID and DisplayName. DisplayName is never empty: an empty `Name` makes
    every `chat.Service` operation return `ErrSenderNotMember`.
  * GameID (`CurrentGameID()`).
  * SessionDigest (SHA-256 of `AuthToken`).
  * Level and DNA.
  * PendingWarp (`Manager.CampaignWarp`, non-consuming).
  * Game facts read from the actor's instance when it exists:
    * Mode, from `Info.Mode`, which is set at game creation and is the value
      `binding.Mode` copies at join (`game/gameplay_session.go:394`). It is
      mapped to an overlay-owned value: `ModeChain`/`ModeTutorial`/`ModeArena`
      → `chain`/`tutorial`/`arena`, anything else → `unknown`. There is no
      "none" mode, and `ModeChain` is the zero value.
    * IsWarped (`Instance.IsWarped()`).
    * PlayerCount (`len(Instance.Players())`).
* `overlay.Service` takes the concrete `*chat.Service` for actions, as blaze
  does. A mirror interface with one implementation would be an abstraction the
  rules discourage.

### 4.3 Session binding (`/debug <key>`)

1. **Fang generates the key.** In an overlay build, Fang generates 32 random
   bytes at startup with Go `crypto/rand`. It hex-encodes them into a 64-char
   key and keeps it in memory only.
2. **The player types `/debug` as the first word.** Fang rewrites it to
   `/debug <key>` (the `normalize_fang_stat_command` pattern) if the server
   endpoint is loopback, forwards it, and posts `OVERLAY_TOGGLE_MESSAGE`.
   If `/debug` appears later in the text, Fang leaves the text untouched
   (6.2).
3. **The blaze branch.** It handles `/debug` like every other command, and
   with one call:
   * It calls `messenger.BindOverlay(ctx, chat.OverlayBindCommand{Sender,
     Key, IsLocalPeer})`. `Key` is empty when no argument was given.
     `IsLocalPeer` is `net.ParseIP(sessionRemoteIP(request.Session)).IsLoopback()`;
     an empty or unparsable address yields false.
   * More than one argument gets the syntax reply.
   * `chat.Service` validates the sender. With no binder it returns
     `ErrOverlayUnavailable`; otherwise it delegates to the `OverlayBinder`
     port without checking the key itself.
   * Blaze maps each sentinel to a reply row (table below). Only unexpected
     errors return `fmt.Errorf("commandDebug: %w", err)`, which becomes a Blaze
     ErrorSystem reply (`blaze/registry.go:197-201`).
4. **`overlay.Service.BindOverlay`** checks in this order, wrapping each
   sentinel with a breadcrumb (as `gameplay/hint.go:41` does), so blaze maps
   them without importing `overlay`:
   1. Feature disabled → `chat.ErrOverlayUnavailable`. This check comes first,
      so a disabled server always answers "disabled".
   2. `IsLocalPeer == false` → `chat.ErrOverlayPeerRemote`. The store is not
      touched, so an existing binding is kept.
   3. Empty key → `chat.ErrOverlayKeyMissing`.
   4. Not exactly 64 lowercase hex characters → `chat.ErrOverlayKeyInvalid`.
   5. Resolve the actor through `ActorSource`; `ErrActorOffline` maps to
      `chat.ErrSenderNotMember`.
   6. Store `{accountID, sessionDigest}` keyed by `SHA-256(key)`. A new binding
      for the same account replaces the old one. The store is a small
      feature-owned type guarded by a mutex, held in memory only.
5. **Each HTTP request.**
   * The adapter extracts the Bearer key and calls
     `Service.Authenticate(ctx, key)`, which looks up `SHA-256(key)`.
   * It re-resolves the actor and requires the account to be active, with the
     same `SessionDigest`.
   * Otherwise it deletes the binding and returns `ErrSessionExpired`, which
     the adapter maps to HTTP 401.
6. **Binding lifetime.** A Blaze disconnect alone does not log the account
   out (`blaze/server.go:277-284, 613-652`). A later login while the account
   is still active returns the same user and `AuthToken`
   (`sporenet/user.go:547-555`). The binding therefore ends only when one of
   these happens:
   * an explicit Blaze or HTTP logout (`sporenet_components.go:146-156`,
     `game/api.go:293-300`);
   * a server restart (the store is memory-only);
   * the next `/debug` bind for the account, which replaces it.

   A Fang-generated key does not survive its process. Its only possible copy
   is the local chat echo checked in S2a. A manually chosen key stays valid
   after the game closes until one of the three events above. If the binding
   should end with the Blaze connection (decision in section 10):
   * include the Blaze session ID in `OverlayBindCommand`;
   * have `Authenticate` ask a small consumer-defined port, implemented in
     blaze over `Server.sessionsForUser` (`blaze/server.go:654`), whether that
     session is still connected.
7. **Replies.** The reply never echoes the key. The system reply replaces the
   body (`queueMessagingSystemResponse` builds the payload from the reply
   text, `social_components.go:1155-1167`). First-token commands return before
   `messenger.Send` (`social_components.go:834-840`), so they never reach the
   chat `Recorder`. Blaze protocol traces record keys only.

| `/debug` reply | Returned by `BindOverlay` |
|---|---|
| `Debug overlay connected` | nil |
| `Debug overlay: API disabled on this server ([developer] is_overlay_enabled = false)` | `ErrOverlayUnavailable` |
| `Debug overlay: the game client must run on the server machine` | `ErrOverlayPeerRemote` |
| `Debug overlay: this Fang build has no overlay (development build required); API is enabled` | `ErrOverlayKeyMissing` |
| `Debug overlay: invalid session key` | `ErrOverlayKeyInvalid` |
| `Debug overlay: account not online` | `ErrSenderNotMember` |

**Manual binding for development.**
* Typing `!debug <64 hex>` in game chat works with any Fang build, because the
  server accepts the `!` prefix.
* After that, `curl -H "Authorization: Bearer <hex>"
  http://127.0.0.1:<port>/debug/v1/state` exercises the API without the
  overlay.
* This is how the server track is verified before Fang work lands. The client
  must run on the server machine.
* **[verify]** that the client forwards `!`-prefixed text as chat.

**Security framing.**
* The key binds an actor; it is not the security boundary.
* The boundary is the loopback-only request guard (4.8) plus the
  off-by-default switch.
* A local process can already obtain launch JWTs for any account from the
  loopback broker (`server/auth/local.go:195-215`).
* The debug API adds no capability beyond the slash commands any logged-in
  player can type. The one exception is the bounded spawn count.

### 4.4 Operations and actions

```text
BindOverlay(ctx, chat.OverlayBindCommand) error                 implements chat.OverlayBinder
Authenticate(ctx, key string) (Actor, error)
State(ctx, Actor) (State, error)                                 Actor facts + StateSource
Catalog(ctx) (Catalog, error)                                    built once per process, lazily
Execute(ctx, Actor, ActionRequest) (ActionResult, error)
```

**Execute pipeline:**
1. Check that the feature is enabled.
2. Statically validate the typed fields (bounds below). On failure return
   `invalid` and call no operation.
3. Run the advisory availability pre-check from `State` (4.5). If the action
   is unavailable, return `unavailable` with its reason code.
4. Call the `chat.Service` operation with `Sender: chat.Participant{ID:
   Actor.ID, Name: Actor.DisplayName}`, the same mapping blaze uses
   (`social_components.go:344`). Also pass `Actor.GameID` where the command has
   a GameID field.
5. Map the result.

The gameplay consumer stays authoritative: state can change between the check
and the next packet.

| Kind | Fields and bounds | Calls | Result | Scope |
|---|---|---|---|---|
| `spawn` | `noun` (Fang canonical name; same syntax check as chat), `count` 1-10 (constant `spawnCountLimit`) | `SpawnNPC` × count, stop at first error | queued (`queued_count`) | whole game |
| `summon` | `rigblock` in the item catalog; `prefix1`/`prefix2` 0 or catalog prefix; `suffix` 0 or catalog suffix (ID ranges in 3.2) | `SummonItem` | applied | account |
| `drop` | `category` ∈ any, weapon, hand, foot, offense, defense, utility; `any` → `""`, others via `developer.DropCategory` (hand → grasper) | `TriggerEvent("drop-create")` | queued | whole game |
| `level` | 1-100 | `SetLevel` | applied | account |
| `dna` | 1-4294967295 | `GrantDNA` | applied (`dna_total`) | account |
| `heal` | none | `MutateResource{IsHeal}` | queued | actor's squad |
| `power_fill` | none | `MutateResource{IsPowerFill}` | queued | actor's hero |
| `damage` | `amount` float32, finite, > 0 | `MutateResource{Damage}` | queued | actor's hero |
| `power_drain` | `amount` float32, finite, > 0 (a positive magnitude) | `MutateResource{PowerReduction: amount}` | queued | actor's hero |
| `goto` | `x`,`y`,`z` finite | `TriggerEvent("goto")` | queued | actor's hero |
| `event` | `name` ∈ `developer.EventNames` | `TriggerEvent` | queued | whole game |
| `kill`, `recap`, `victory` | none | `TriggerEvent` | queued | whole game |
| `reset` | none | `TriggerEvent` | queued | actor's session (also restarts enemies targeting the actor's hero) |
| `defeat` | none | `TriggerEvent` | queued | actor's squad (party Game Over only when every squad is down) |
| `warp` | `area` = Fang canonical level name (Fang resolves numbers and partial names, 3.2); the server applies only the chat charset rule `[A-Za-z0-9_-]{1,128}` (`manager.go:1322-1339`), with no level lookup, matching `/warp` | `RequestWarp` | applied (pending) | account, next campaign game |
| `effect` | `name` ∈ effect table; world mode only | `PreviewEffect{Asset: developer.EffectAsset(name)}` | queued | actor's hero |

* **Bounds.** Decode numbers into the exact Go widths (`uint16`, `uint32`,
  `float32`) so `encoding/json` rejects overflow. Reject non-finite values and
  float32 values that underflow to 0. `chat.Service` only checks `> 0`
  (`service.go:550-553`), and `+Inf` would otherwise fail later in
  `developer.ApplyResource`.
* **Apply-time failures.** The gameplay consumer stays authoritative, because
  state can change between the pre-check and the next poll. A command that then
  fails at apply time returns an error that can drop the RakNet peer (3.1). The
  pre-check therefore includes the apply-time mode gates (4.5).
* **Reset.** The overlay's Reset sends only the server half of `/reset`.
  Mirroring the local input reset needs H4.
* **/stat client half.** There is no action for it. The Info tab (section 7)
  reads Fang's existing client-received HP/power values locally
  (`diagnostic_hit_point_bits`, `diagnostic_power_point_bits`,
  `diagnostic_resource_mask`, which are what `normalize_fang_stat_command`
  appends) and shows them next to the server values.
* **/exit.** There is no overlay action. Closing the game stays a chat command
  handled by Fang's existing `WM_CLOSE` branch.

**Result type (feature-owned):**
```go
type ResultCode uint8 // Applied, Queued, Invalid, Unavailable, Overflow, Internal
type ActionResult struct {
    Code        ResultCode
    Reason      Reason   // typed reason for Unavailable/Invalid
    QueuedCount int
    DNATotal    uint32
}
```
The web adapter owns wire codes and messages. No presentation text lives in
the feature.

| Sentinel or error | Code |
|---|---|
| failed static validation | `invalid` |
| `ErrSenderNotMember` (stale GameID: game gone or player left), `ErrNPCSpawnUnavailable`, `ErrEventUnavailable`, `ErrResourceUnavailable`, `ErrEffectPreviewUnavailable`, `ErrWarpUnavailable`, `ErrItemSummonUnavailable`, `ErrLevelUnavailable` (bad input was already rejected statically) | `unavailable` |
| `ErrDNAOverflow`, `ErrItemExists` (ID space exhausted; `GrantPart` assigns the next free ID, so duplicates cannot occur) | `overflow` |
| anything else | `internal` (logged; generic message) |

* **No storage writes on rejection, except one.** `SetLevel` may save and
  still return `ErrLevelUnavailable`. The message says so: "level may have
  been applied; reopen to refresh".
* **Spawn count.** A count > 1 spawn that stops early reports `queued N of
  M`.

### 4.5 Availability (server-computed, advisory)

* **Source.** `gameplay/overlay_state.go` computes an `action_states` map
  keyed by action kind, each entry `{is_available, reason}`. It uses the same
  predicates the RakNet consumers use. To keep them identical, extract these
  into named `gameplayPeerSession` methods in `gameplay/eligibility.go`, and
  call them from both the consumers and the pre-check:
  * the four inline coarse eligibility expressions (`handler.go:2949-2950,
    2986-2989, 3040-3043, 3245-3248`);
  * the apply-time mode gates listed in 3.1:
    * event/kill/victory/defeat/recap/reset/goto/drop need ModeChain (except
      victory in Tutorial and Arena);
    * spawn needs IsWarped;
    * heal needs Chain or Tutorial.

  The extraction is behavior-preserving and shrinks the oversized file.
  Availability then does not report "yes" where a static gate would make the
  apply step return an error. A profile lookup or a state change between the
  check and the poll can still fail.
* **Mode.** It comes from `binding.Mode` of the found session, which always
  equals `Info.Mode`. Without a session, availability does not consult mode.

| Action | Hub (GameID 0) | Tutorial | Chain | Warped chain | Arena/PvP |
|---|---|---|---|---|---|
| level, dna, summon, warp | yes (persists; no in-game presentation) | yes | yes | yes | yes |
| spawn | no | no | no (`not_warped`) | if deployed | no |
| drop, kill, defeat, goto, event, reset, recap | no | no (`wrong_mode`) | if deployed | if deployed | no (`wrong_mode`) |
| victory | no | if deployed (completes tutorial; persisted on Return to Ship) | if deployed | if deployed | if deployed (in-memory result for all Arena players) |
| heal | no | if deployed | if deployed | if deployed | no (`wrong_mode`) |
| power_fill, damage, power_drain, effect | no | if deployed | if deployed | if deployed | if deployed |

**Co-op.** Co-op games follow their mode column. Player count is not an input
to any eligibility predicate. It changes only who is affected:
* spawn, drop, event, kill, recap and victory affect every player in the game;
* heal, power_fill, damage, power_drain, goto, reset, defeat and effect affect
  only the actor's squad, hero or session;
* level, dna, summon and warp affect only the actor's account (scopes as in
  4.4).

Reason codes: `no_game`, `not_deployed`, `not_warped`, `wrong_mode`,
`zone_terminal`, `busy`.

### 4.6 State DTO (`GET /debug/v1/state`)

```json
{
  "schema_version": 1,
  "build_id": "…", "version": "0.5.0",
  "account": { "level": 12, "dna": 3400, "pending_warp": "cryos_1_SM" },
  "game": { "game_id": 17, "mode": "chain", "is_warped": true, "player_count": 1 },
  "hero": { "is_deployed": true, "object_id": 123, "x": 12.3, "y": -4.1, "z": 0.0,
            "hit_point": 840.0, "hit_point_max": 1000.0,
            "power_point": 55.0, "power_point_max": 100.0 },
  "alive_npc_count": 14,
  "nearest_npcs": [ { "object_id": 456, "name": "Boomer", "hit_point": 90.0,
                      "hit_point_max": 120.0, "distance": 8.2, "direction": "Northeast" } ],
  "action_states": { "spawn": { "is_available": true, "reason": "" } }
}
```

Field sources:

| Fields | Source |
|---|---|
| `build_id`, `version` | `buildinfo.ID` / `buildinfo.Version`, passed in by the composition root. Fang compares `build_id` with its own to flag mismatches |
| `account.*`, `game.*` | `ActorSource` (the actor's `game.Instance`; mode mapped to `chain`/`tutorial`/`arena`/`unknown`) |
| `hero.*`, `action_states` | the registry session |
| `nearest_npcs` | the `hintForSession` filter (published, not defeated, not fixture, non-player-aligned), `hintNPCName`, `hintDirection`, `hit_point_max` from `Plan.NPCProfile.HitPoint`; capped at 16, nearest first |

* **Locking.** `OverlayState` takes the registry read lock once, using the
  ctx-bounded `TryRLock` loop from `snapshot.go:37-43`. It reuses the
  per-session helpers rather than calling the providers one after another,
  which would mean several locks and an inconsistent frame. It does not reuse
  `BugContext` or `SyncSnapshot` (too heavy).
* **No game.** `game` is null only when GameID is 0 or the instance no longer
  exists. Between Blaze `ClaimGame` (`game/manager.go:200-290`) and the RakNet
  join there is no registry session. In that window `game` is present, `hero`
  is null, and every in-game action reports `not_deployed`. Never read a zero
  `gameplayPeerSession{}` returned with `isFound == false`.
* **Latency.** Fang shows the request round-trip time it measures itself.
  There is no server "processing" field.

### 4.7 Catalog DTO (`GET /debug/v1/catalog`)

```json
{
  "schema_version": 1, "catalog_hash": "sha256-of-encoded-dto",
  "rigblocks": [ { "id": 1234, "name": "…", "slot": "weapon", "classes": ["all"],
                   "sciences": ["plasma"], "minimum_level": 1, "maximum_level": 40,
                   "is_unique": false } ],
  "prefixes":  [ { "id": 11, "name": "…", "part_types": ["weapon"], "classes": ["all"],
                   "sciences": ["all"], "minimum_level": 1, "maximum_level": 100,
                   "is_unique": false } ],
  "suffixes":  [ { "id": 21, "name": "…", "part_types": ["…"], "classes": ["…"],
                   "sciences": ["…"], "minimum_level": 1, "maximum_level": 100,
                   "is_basic_eligible": true, "is_unique": false } ],
  "event_names": ["security-next", "boss-start", "boss-complete"],
  "effect_names": ["…"],
  "drop_categories": ["any", "weapon", "hand", "foot", "offense", "defense", "utility"],
  "spawn_count_limit": 10, "level_limit": 100
}
```

* **No enemies or levels.** Nouns and levels are not in the catalog; Fang
  owns them (3.2).
* **When it is built.** Once per process, on the first request. content.db is
  opened once at startup and never reloaded at runtime.
* **Caching.** `catalog_hash` is the hex SHA-256 of the DTO encoded with
  `catalog_hash` set to the empty string. On the first request the adapter
  computes it, sets the field, re-encodes and caches the bytes. It sends the
  same value as the `ETag` header (`If-None-Match` → 304).
* **Size.** About 0.4-0.5 MB of plain JSON, fetched once per process over
  loopback.

### 4.8 HTTP adapter (`server/developer/overlay/web`)

* **Registration.** `func Register(router *recaphttp.Router, options Options)
  error`, mirroring `game.RegisterAPI` and `readiness.Register`. It is called
  from the wiring block only when `[developer] is_overlay_enabled` is true, so
  a disabled server has no route (404).
* **Request guard** (`isLocalOverlayRequest`), run before anything else.
  * It requires all of:
    * no `Origin` header;
    * a `RemoteAddr` IP that is loopback;
    * a `request.Host` hostname of `localhost` (case-insensitive) or a
      loopback IP literal, with a port equal to `[server].port`. Every HTTP
      port is that one port (`server/runtime/server.go:1284-1297`), and this
      mirrors the broker's loopback check (`server/auth/local.go:316-321`).
      Do not reuse `requestAccess`, which also admits private-LAN peers.
  * Failure returns 403 with a generic body.
  * Never emit CORS headers or register OPTIONS.
  * The check is mandatory, not defensive: it closes DNS rebinding, and in
    multiplayer the listener binds `0.0.0.0`.
* **Authentication.** `Authorization: Bearer <key>` only, never in the query,
  a cookie or the body. A missing or unknown key returns 401.
* **Decoding.** JSON only; strict decoding (unknown fields rejected); body
  ≤ 4 KiB; fixed envelope with `schema_version`. The adapter maps
  `ResultCode`/`Reason` to the wire `code`, `reason` and a short adapter-owned
  `message`. Internal errors are logged, never returned.
* **Logging.** One line per POST through the server logger, for example:
  `Overlay action account=%d game=%d kind=%s code=%s duration=%s`. Rejections
  are logged too. Never log the key or the request body.
* **Traces.** State polls appear in the protocol trace (path, keys and status
  only; headers are never traced) only while the overlay is open, at about
  2 lines per second. This is accepted.

### 4.9 Configuration

```toml
[developer]
is_overlay_enabled = false
```

In `server/game/config.go`:
1. Add `ConfigIsDeveloperOverlayEnabled ConfigKey =
   "IS_DEVELOPER_OVERLAY_ENABLED"`, named like `ConfigIsChatStdoutEnabled`
   for a boolean key outside the game section.
2. Add a `defaultConfigValues` entry with the value `"false"`.
3. Add a `Developer configDeveloper` field (toml tag `developer`) plus a
   `configDeveloper` struct with `IsOverlayEnabled *bool` (toml tag
   `is_overlay_enabled`).
4. Add a `setBool` call in `LoadConfig`.
5. Emit the section in `encodeConfigDocument`. Without this, every launcher
   Settings save silently deletes the section (`app/darkspinner/config.go:159,
   174, 217`).
6. Add the section to `server/assets/darkspin.toml`.

Routes are registered at server start, so a hand edit takes effect after
restarting Darkspinner or `darkrun server`. An optional development-only themed
Settings checkbox is listed in section 10.

### 4.10 Shared vocabulary move

Move into `server/developer/vocabulary.go`, exported, with the logic unchanged:
* `developerEventName`;
* the effect preview type, table and `effectPreviewAsset` (adds the
  `server/util` import);
* `dropCategory` / `dropCategoryDisplay`.

Also add ordered lists for the catalog. Blaze imports them, so chat and the
overlay share one vocabulary. The move is about 55-100 lines. Chat replies,
the protocol and behavior are unchanged.

### 4.11 Wiring (`server/runtime/server.go`, after line 856)

1. Construct `overlay.NewService(...)` with:
   * `chatService`;
   * `overlaylocal.NewActorSource(userManager, gameManager)`;
   * `gameplayLifecycle`;
   * `overlaylocal.NewItemSource(partCatalog, contentStore)`;
   * the build info;
   * `IsEnabled`.
2. `chatService.UseOverlayBinder(overlayService)`.
3. If enabled, `overlayweb.Register(router, overlayweb.Options{Service,
   Logger, Port})`, with `Port` set to `ports.http` so the request guard knows
   which Host port to require.

Late registration is safe: `Router.Add` is mutex-guarded and no listener has
started inside `New`.

## 5. Launcher

* **No changes required.** There is no token minting and no new environment
  variable. Fang derives the API base URL from the Blaze endpoint it already
  resolves.
* **Coverage.** Every launch path that reaches a local server works:
  Darkspinner Play, detached instances, `app/darkspin` + `darkrun`, and CLI
  launches. `mage scenario:build` rewrites `bin/game/fang.dll` without the
  overlay; run `mage build` afterwards.
* **Optional.** A development-only themed Settings checkbox for
  `[developer] is_overlay_enabled`, modeled on borderless. It needs:
  * a `ServerConfiguration` field;
  * a `SetServerConfiguration` argument with regenerated `wailsjs` bindings;
  * inclusion in the no-change check, rollback and restart condition
    (`app/darkspinner/config.go:126-129, 164-165, 195-215`).

  It must use themed UI only.

## 6. Fang design (`app/fang`)

### 6.1 Files (flat in `app/fang`; cgo ignores subdirectories)

```text
overlay.h / overlay.c          state machine, toggle, install, trace reasons, public entry points
overlay_device.c               device acquisition (lazy renderer read; H1 call-site fallback),
                               vtable-slot patch helper that preserves execute permission
overlay_draw.c                 Present hook (H2), render-state sequence, quads, text, integer scale
overlay_input.c                WndProc filter (H3), SPSC input ring, coordinate mapping, focus/capture
overlay_ui.c                   microui tabs and widgets
overlay_net.go                 key generation, HTTP goroutine, JSON -> C buffers, action mailbox
overlay_vendor.c               #if FANG_OVERLAY: #include "thirdparty/microui/microui.c"
thirdparty/microui/            microui.c/.h (MIT, rxi), unmodified, upstream commit noted in overlay.h
thirdparty/stb_easy_font.h     public domain/MIT, unmodified; included only by overlay_draw.c
command_catalog.c / .h         behavior-preserving extraction of fang_spawn_nouns/fang_warp_* and the
                               find_fang_* resolvers out of fang.c; shared by chat and the overlay UI
overlay_debug.go / overlay_release.go   build-tag switch defining FANG_OVERLAY (scenario_*.go pattern)
```

* **Vendored C.** cgo does not track files in `thirdparty/`, and the Go build
  cache is keyed on file content, not mtime, so touching a file has no effect.
  When a vendored file changes, update the upstream-commit note in
  `overlay.h` in the same change. That header is a top-level package file, so
  the whole package rebuilds. Otherwise run `go clean -cache` before `mage
  build`.
* **stb_easy_font.** It is header-only and all its functions are static, so
  never define an implementation macro.

### 6.2 `/debug` command (H0)

* **Recognition.** `/debug` gets command number 30 in
  `find_darkspin_chat_command` (`fang.c:4163`).
  * Numbers 1 and 3-29 are taken (`fang.c:4173-4282`); 2 is an unused gap that
    stays unused.
  * It is recognized in every Fang build, so it is always forwarded
    deterministically with the 0x1F prefix.
* **First word only.** Unlike the other Fang commands, `/debug` counts only
  when it is the first token of the text (after leading spaces and tabs).
  * This is the server's own rule: `messagingCommandName` checks only
    `strings.Fields(body)[0]` (`social_components.go:1039-1050`).
  * Fang's recognizer also matches after a space, tab or `]`
    (`fang.c:4170-4171`), and the `/stat` normalizer keeps the text before the
    command (`fang.c:4355-4361`).
  * When `/debug` appears later in the text, Fang leaves it alone: no
    normalize, no key, no 0x1F, no toggle. The original text goes to
    `original_chat_text_convert` unchanged.
  * Otherwise `type /debug to open it` would be sent as `type \x1fdebug <key>`.
    The server treats that as ordinary chat, which is broadcast and recorded
    (`social_components.go:840-872`, `chat/service.go:489-497`).
* **Non-overlay builds** forward the text unchanged, and the server replies
  that the build has no overlay.
* **Overlay builds:**
  * In a `command == 30` branch next to the `/stat` branch
    (`fang.c:4464-4473`), normalize to `/debug <key>`. The 0x1F rewrite
    (`fang.c:4482`) then applies to the normalized buffer.
  * The key is appended only when `overlay_net.go` has published one, and it
    publishes one only when the endpoint passes the 6.7 loopback predicate.
    Otherwise the text is forwarded bare, and the Info tab says "overlay
    needs a local server".
  * After forwarding, post `OVERLAY_TOGGLE_MESSAGE` (`WM_APP+n`) to
    `chat_game_window`, next to the `/exit` and `/reset` branches.
  * `hooked_game_wndproc` handles the message by calling `overlay_toggle()`.
* **No hotkey in the MVP.** It is deferred to keep H0-H3 minimal. A hotkey
  other than F10 or an Alt combination would arrive as an ordinary
  `WM_KEYDOWN` in `hooked_game_wndproc`, so it can be added later.
* **[verify] in S2a:** whether the client echoes the typed command (with the
  key) in its own chat log. That would be acceptable because the key is sent
  only to a loopback server and bound only from a loopback Blaze peer, but
  record it.

### 6.3 Device acquisition (H1)

* **Preferred: lazy, no startup hook.** On the first toggle, read the device
  pointer from the renderer singleton (getter `exe+0x3AEBF0`, vtable check
  `exe+0xC0F558` as in `resolution.c:167-178`; device field offset from S1a).
  * Validate it with `readable_range`.
  * Check that its vtable pointer lies inside the loaded `d3d9.dll` image.
  * This works even when Fang loads late under Wine.
* **Fallback (if S1a finds no stable offset):** the H1 call-site capture.
  * It must be installed before device creation and re-capture on every
    successful `CreateDevice`.
  * Under Wine it may install too late. In that case the overlay is
    unavailable, with `overlay_device_source=0`.
* **Trace value.** `overlay_device_source` is an integer, because
  `trace_client_state` records one unsigned value per key: 0 = unavailable,
  1 = renderer singleton, 2 = call-site capture.
* **Non-fatal.** Overlay installation never sets bits in `fang_install`'s
  result. Success and failure are reported only through
  `trace_client_state("overlay_*")` and an internal disabled flag. The
  installer runs after the `#if FANG_SCENARIO` block.
* **Patch helper.** Add a vtable-slot variant of `patch_pointer`. It calls
  `VirtualQuery` and uses `PAGE_EXECUTE_READWRITE` when the page is
  executable, then restores the old protection. Existing callers are
  unchanged.
* **Fail closed.** Reuse the existing PE, `SizeOfImage` and signature checks
  for every new RVA.

### 6.4 Drawing (H2)

* **Patch.** Patch only `Present` (slot 17), when the overlay first opens.
  Keep the original pointer and call it directly; never call the
  `IDirect3DDevice9_Present` macro, which would re-enter the hook. Draw only
  when `this` is the captured device, because the vtable is shared by every
  device of the class.
* **Present path.** S1a counts device `Present` vs
  `IDirect3DSwapChain9::Present` per frame. If the game presents through a
  swap chain, patch that slot instead.
* **Per-frame sequence** (only while open and the device is usable):
  1. `GetRenderTarget(0)` and `GetDepthStencilSurface`.
  2. `CreateStateBlock(D3DSBT_ALL)`.
  3. `GetBackBuffer(0,0)` + `GetDesc`. Use the backbuffer size; never trust
     the CreateDevice parameters, which can be 0×0 in windowed mode.
  4. `SetRenderTarget(0, backbuffer)` and `SetDepthStencilSurface(NULL)`.
  5. Set a full-backbuffer viewport.
  6. No shaders; `SetFVF(XYZRHW|DIFFUSE)`; no texture; stage 0 = diffuse,
     stage 1 disabled; `SetStreamSourceFreq(0,1)`.
  7. Set the render states explicitly:
     * off: Z, Z-write, stencil, alpha test, fog, lighting, sRGB write;
     * cull NONE, fill SOLID, color write `0xF`;
     * alpha blend `SRCALPHA`/`INVSRCALPHA` with `ADD`;
     * scissor test on.
  8. `BeginScene` → `DrawIndexedPrimitiveUP` → `EndScene`.
  9. Restore RT0 and the depth-stencil surface first (this resets viewport and
     scissor), then `Apply` the state block.
  10. Release every reference.
* **No persistent device resources.** Draw only from user memory (the
  `*PrimitiveUP` calls). Nothing needs handling on `Reset`, so there is no
  Reset hook. That covers native Alt+Enter, resolution apply, display.c
  toggles and lost-device recovery.
* **Device loss.** After `Present` returns `D3DERR_DEVICELOST`, skip drawing
  until `TestCooperativeLevel` returns `D3D_OK`.
* **Scale.** Use the integer UI scale `s = max(1, round(bb_height / 1080))`,
  with a −/+ override in the window. microui lays out in unscaled units, and
  the renderer multiplies every rect, every glyph quad and every scissor rect
  by `s`.
* **Text.** stb_easy_font quads are converted to `XYZRHW` vertices (rhw 1,
  −0.5 pixel offset, `D3DCOLOR_ARGB`). For microui's `text_width(font, str,
  len)`, measure directly when `len < 0`; otherwise copy at most `len` bytes
  into a bounded stack buffer before measuring. `text_height` returns 12
  (stb_easy_font's unscaled line pitch). Like `text_width`, it is in unscaled
  layout units, and only the renderer applies `s`.
* **Frame time.** Measured with `QueryPerformanceCounter` deltas between
  overlay Present calls (1-second moving average). It does not depend on
  `App::Update`, which is hooked only in development borderless launches.
* **Coordinates.** Each frame, publish the backbuffer size and Present's
  `pDestRect` (if non-null) for input mapping.

### 6.5 Input (H3, Increment 2)

* **Placement.** The filter is the first statement of `hooked_game_wndproc`,
  ahead of the Ctrl+V and Enter branches: `if (overlay_window_message(...))
  return 0;`. It never consumes `CHAT_OPEN_MESSAGE`, `CHAT_RESET_MESSAGE`,
  `OVERLAY_TOGGLE_MESSAGE`, `WM_TIMER` or `WM_INPUT`.
* **Where events are processed.** The WndProc only pushes compact events into
  a fixed-size SPSC ring. Present drains the ring into microui at the start
  of the frame. The consume decision uses the overlay rectangles and the
  "text field focused" flag, which Present publishes each frame under a small
  seqlock.
* **What is consumed while open:**
  * mouse messages (`WM_MOUSEMOVE`, `WM_*BUTTONDOWN/UP/DBLCLK`,
    `WM_MOUSEWHEEL`) inside overlay windows, plus the matching up message of
    any button that went down inside;
  * keyboard messages (`WM_KEYDOWN/UP`, `WM_SYSKEYDOWN/UP`, `WM_CHAR`) while
    an overlay text field has focus. Otherwise keys pass through.
* **Mouse capture.** `SetCapture` on an overlay button-down only when
  `GetCapture()==NULL`; release only a capture the overlay took.
* **Focus loss.** `WM_KILLFOCUS`, `WM_ACTIVATEAPP(FALSE)` and
  `WM_CAPTURECHANGED` release microui mouse, drag and text focus.
* **Enter poller.** While an overlay text field owns the keyboard, an
  interlocked `is_overlay_keyboard_owned` flag makes `poll_chat_key` skip its
  post. That is one guarded line in `fang.c`. Enter in an overlay field then
  never opens game chat.
* **Mapping to UI units.** `x_ui = (x_client · bb_w / client_w) / s`, and the
  same for y. `client_w`/`client_h` come from `GetClientRect` (refreshed on
  `WM_SIZE`). `WM_MOUSEWHEEL` uses screen coordinates: pass only
  `GET_WHEEL_DELTA_WPARAM` and hit-test with the last mapped mouse position.
  Never reuse the game's internal input scale.
* **Text.** ANSI `WM_CHAR` (the subclass is the A variant) is converted to
  UTF-8 before `mu_input_text`.
* **Cursor.** Whether `WM_MOUSEMOVE`/`WM_SETCURSOR` pass through, and whether
  the overlay draws its own cursor, is decided from the S2a results.
* **No automatic input reset.** H4 is only added if S2b shows held
  move/attack sticking across a toggle.

### 6.6 Threading and data flow

| Path | Mechanism |
|---|---|
| WndProc → Present | SPSC input-event ring |
| Present → WndProc | overlay rectangles and focus flag under a seqlock |
| Present → Go | single-slot action mailbox: a fixed-size struct (kind, numeric arguments, bounded name) whose state word moves free → ready → taken → free via `InterlockedCompareExchange`. The render thread never blocks. While an action is in flight, action buttons are disabled with reason `busy` |
| Go → Present (state) | fixed-size state struct under a seqlock |
| Go → Present (catalog) | immutable block allocated with `C.malloc`, published with `InterlockedExchangePointer`. A replaced block is retired and never freed during the process; `catalog_hash` only changes with a different server build |

* **Goroutine.** The overlay installer (in `overlay.c`, under `FANG_OVERLAY`,
  run after the `#if FANG_SCENARIO` block of `fang_install`) starts it through
  the exported `GoOverlayStart(host, port)`. This follows the existing
  `scenario_thread.c` call to `GoScenarioCapability`. `GoOverlayStart`
  generates the key, publishes it to C only for a loopback endpoint, and
  starts the goroutine. The goroutine waits on a Win32 auto-reset event with a
  500 ms timeout:
  * the render thread signals it after filling the mailbox;
  * on timeout it polls state only while the overlay is open.
* **Readiness.** The overlay refuses to open (with a trace reason) while
  `chat_game_window` is NULL.
* **Thread IDs.** S1a records the thread IDs of Present, `hooked_game_wndproc`
  and `hooked_frame_delta` to confirm this model.

### 6.7 Networking (`overlay_net.go`)

* **Base URL.** `http://<host>:<port>` from `fang_install`'s `hostname` and
  `port` arguments (the endpoint `GoRecapInitialize` already resolved), passed
  into `GoOverlayStart`. The host must be a loopback
  IP literal. `localhost` is mapped to `127.0.0.1` to avoid `::1` resolution
  surprises. Anything else (for example a LAN server in remote mode) leaves
  the network goroutine stopped, and the Info tab shows "overlay needs a local
  server".
* **Client.** One dedicated `http.Client`:
  * its own `Transport` with `Proxy: nil` and a 500 ms dial timeout;
  * a 2 s request timeout;
  * `CheckRedirect` returns `http.ErrUseLastResponse`, and any 3xx is an
    error.
* **Key handling.** The key is sent only in the `Authorization` header.
* **On 401.** Retry for up to 5 s, because the Blaze `/debug` bind may land
  after the first poll. Then show "Session not bound: type /debug again".
* **Traces.** Integers only, through `trace_client_state`:
  * `overlay_key_length` (0 means no key);
  * the HTTP status per route as `overlay_net_status` (state poll),
    `overlay_net_catalog_status` and `overlay_net_action_status`, with 0 for a
    transport error.

  The request path and the key are never traced as text.
* **No precedent.** This is the first goroutine and the first network I/O in
  Fang; hardening covers both.

### 6.8 Build gating

* **`isOverlay` flag.** Add an `isOverlay` parameter to `buildFang` and
  `buildDarkSpinnerTarget`.
  * Pass true from `mage build` (`magefile.go:159`), `mage darkspinner:build`
    (`:661`) and `mage darkspin:build` (`:1081`). These are the development
    builds, and the embedded DLL includes the overlay. `darkspin:build` is a
    local development target that CI never runs, and it writes the same
    `bin/game/fang.dll` that `app/darkspin` loads (`app/darkspin/main.go:416`),
    so it must keep the overlay.
  * Pass false from `scenario:build` (`:614`) and the CI target (`:745`).
* **Guards**, modeled on `magefile.go:257`:
  * overlay requires diagnostics;
  * overlay and scenario are mutually exclusive (same call site);
  * Darkspinner rejects overlay without `isDevelopment`.
* **Sources.**
  * Go overlay files: `//go:build windows && cgo && fangoverlay && fangdebug`.
  * `overlay_debug.go` carries `#cgo CFLAGS: -DFANG_OVERLAY=1`.
  * `overlay_release.go` (`//go:build windows && cgo && (!fangoverlay ||
    !fangdebug)`) contains only the `#cgo CFLAGS: -DFANG_OVERLAY=0` preamble.
  * C overlay files are wrapped in `#if FANG_OVERLAY`. `overlay.h` defaults
    `FANG_OVERLAY` to 0, as `scenario_dispatch.h:4-6` does for its macro.
  * `/debug` recognition (H0) is outside the tag.
* **Release linkage.**
  * In `fang.c`, wrap each new overlay call site in `#if FANG_OVERLAY`, as
    `fang.c` already guards `FANG_SCENARIO` (`fang.c:4-11, 1198-1200,
    7000-7019`). That covers the include, the WndProc filter call, the
    `OVERLAY_TOGGLE_MESSAGE` case, the `poll_chat_key` check, the `/debug`
    key-normalize branch and the installer call. C names never reach the
    release DLL, because `-s` is passed to the external linker.
  * The untagged `main.go` stays unchanged and calls no overlay code. A
    `C.*overlay*` call would emit a Go wrapper whose name survives `-s -w` and
    would fail the `strings` check below. The installer in `overlay.c` calls
    the exported `GoOverlayStart` instead (6.6).
* **Runtime.** With the lazy device read, no overlay hook exists until the
  first `/debug`. With the H1 fallback, only the pass-through capture is
  installed at startup.
* **Release check** (hardening):
  * `GOOS=windows GOARCH=386 CGO_ENABLED=1 go list -deps -tags fangdebug
    ./app/fang` lists no `net/http`;
  * `strings` on the CI `fang.dll` finds no `overlay_` literals.

  Do not check symbols, because Fang links with `-s -w`.
* **Server routes** compile into every build and are registered only when
  enabled, matching today's ungated slash commands.

### 6.9 Oversized-file containment

* **`fang.c` (7,284 lines)** gains only:
  * `#include "overlay.h"`;
  * the `/debug` table entry and its normalize/post branch (H0);
  * the first-statement filter call in `hooked_game_wndproc` and the
    toggle-message case;
  * the one-line `is_overlay_keyboard_owned` check in `poll_chat_key`;
  * the installer call.

  Everything else lives in `overlay_*.c`. Moving `CHAT_RESET_MESSAGE` to
  `fang.h` and extracting the command catalog into `command_catalog.c` are
  behavior-preserving extractions that shrink the file.
* **`gameplay/handler.go` (6,204 lines)** gains no plumbing; it only loses the
  extracted eligibility predicates.
* **`gameplay/session.go`** is untouched.

## 7. UI specification

* **Window.** One draggable, resizable window. Its position is remembered in
  memory only.
* **Disabled controls** use `MU_OPT_NOINTERACT` and dimmed text, with the
  reason shown as an inline label. microui has no tooltips.
* **Lists** are a `mu_textbox` filter (append and backspace only) over a
  scrolling panel that renders only the visible rows (2,408 rigblocks).
* **Pickers** use `mu_open_popup`. Numbers use `mu_slider_ex`/`mu_number_ex`
  with clamped ranges.
* **Pending requests** show a "Pending…" label.
* **Confirmations.** Destructive or party-wide actions (Kill, Victory, Defeat,
  Recap, Level) need an in-window second click ("Confirm?"). The Victory text
  names its effect for the current mode.

| Tab | Content | Increment |
|---|---|---|
| Info | game id, mode, players in this game, account level and DNA, pending warp, hero position, HP/power bars (server values, plus Fang's client-received values as `/stat` shows them), alive NPC count, nearest hostiles (name, distance, direction), frame time, state request latency, build id match, connection status | 1 |
| Enemies | filter box, noun list from Fang's `fang_spawn_nouns` (MutationAgent labelled zone-dependent), count 1-10, **Spawn**, result line ("Queued 3 of 3") | 2 |
| Items | filters: slot, class, science, level; rigblock list; prefix1/prefix2/suffix pickers filtered by `part_types` and science (advisory; affixes a level-1 Basic item could not roll are tagged, not hidden); **Summon**; **Random drop** per category | 2 |
| Player | level 1-100 + Apply, DNA amount + Grant, Heal, Fill power, Damage n, Drain power n, Recap, Reset (server half; local half only with H4) | 3 |
| World | warp list from Fang's `fang_warp_locations`, grouped by kind (campaign, SM, PvP, test, editor, hub; any warp loads as a Chain game), current pending warp, event buttons, goto x/y/z prefilled from the hero, Kill all, Victory, Defeat, effect picker | 3 |

* **Results.** "Done" for `applied`, "Queued" for `queued`, otherwise the
  adapter message. A queued action that gameplay later drops is visible only
  through state (for example `alive_npc_count` unchanged) and the server log.

## 8. Delivery plan

**Day 0.** Ask the H0-H3 approvals and the platform decision (section 10).
Then run two tracks in parallel.

### Track A: server (no approvals needed)

| Step | Work | Exit (observed in the real build, no tests) |
|---|---|---|
| A1 Foundation | Vocabulary move (4.10); `chat` port + `BindOverlay`; blaze `/debug` branch; overlay feature with session store, `State`, `ActorSource` adapter; `gameplay/overlay_state.go` (+ eligibility extraction); web adapter with the guard and `GET /debug/v1/state`; config (six edits); wiring; action log helper | `mage build` passes. In a running client: `/event 1`, `/effect <name> world`, `/drop create` and `/drop create hand` reply as before; `!debug <64 hex>` replies "connected"; `curl -H "Authorization: Bearer <hex>" http://127.0.0.1:<port>/debug/v1/state` returns hero HP/power matching `/stat`; with an `Origin` header or a wrong Host port → 403; feature off → `/debug` says disabled and the route is 404; a second `!debug <other 64 hex>` → the first key returns 401; in-game logout (or `api.account.logout`) → 401; server restart → 401; one log line per POST |
| A2 Core actions | `PartCatalog` enumerators; `ItemSource` (+ optional name lookup after the `darkrun db` check); `GET /debug/v1/catalog`; `POST /debug/v1/action` for spawn, summon, drop | curl spawn in a warped mission → `queued`, then `alive_npc_count` rises; summon → `applied` and the item appears in inventory; drop → `queued`; an invalid request → `invalid`; each POST writes one log line |
| A3 Remaining actions | The other 15 kinds; availability per mode (4.5) | Each kind via curl in its eligible mode; hub, tutorial and arena return the expected `unavailable` reasons; level, DNA and summon from the hub persist |

### Track B: Fang

| Step | Work | Approval | Exit |
|---|---|---|---|
| B0 Build gating | 6.8: `isOverlay` on `buildFang` and `buildDarkSpinnerTarget` (true from `magefile.go:159, :661, :1081`; false from `:614, :745`); the `fangoverlay` tag; `overlay_debug.go` / `overlay_release.go`; the three guards; the `/debug` recognition entry (command 30), forwarded bare in every build | none (no hook) | `mage build` and `mage darkspinner:build` succeed and build Fang with `-tags fangdebug,fangoverlay`; `mage scenario:build` still uses `fangdebug,scenario`; the CI target's Fang builds with `FANG_OVERLAY=0`; typing `/debug` reaches the server's `/debug` branch |
| B0a Spike S2a | Observational input survey: read-only listing of the exe's PE imports (`GetAsyncKeyState`, `GetKeyState`, `GetCursorPos`, raw input, `DirectInput8Create`, `SetCursor`/`ShowCursor`), plus traced calls attributed by caller module (excluding Fang's own poller); whether the typed `/debug` text is echoed locally | none | Summary under `bin/game/logs/overlay/s2a/` |
| B0b Spike S1a | Observational device survey (pass-through capture in a development build): whether CreateDevice uses the IDirect3D9 from `0x912B7E`, CreateDevice parameters, device vs swap-chain Present counts per frame, thread IDs, renderer-singleton device offset; run on Windows and, if in scope, Wine (capture order vs Fang load) | H1 (asked on day 0; the capture is pass-through and the Present counter does not draw) | `game.jsonl` keys `overlay_s1_*`; summary under `bin/game/logs/overlay/s1a/`; decision lazy vs call-site acquisition |
| B1 Increment 1: read-only Info panel | `command_catalog.c` extraction; H0 command + key + toggle; device acquisition; H2 Present draw; `overlay_net.go` state polling; Info tab | H0, H1, H2 | `game.jsonl` shows `overlay_device_source=1` or `=2`, `overlay_present_hook=1`, `overlay_net_status=200`; `/debug` with leading text (`see /debug`) is sent as plain chat with no key and no toggle; `/debug` shows and hides a live Info panel in a mission; text is legible with even strokes at 720p, 1080p, 1440p and 4K; the panel survives: (a) default launch, windowed and exclusive: Alt+Enter both ways, Alt+Tab ×10 while exclusive, minimize/restore, a Graphics resolution change; (b) development borderless launch: Alt+Enter ×10, resolution change; a Wine run if in scope; with H1 unavailable the game still launches and the trace records the reason |
| B2 Increment 2: core ask | H3 input; Enemies and Items tabs; catalog fetch; action mailbox | H3 | Clicks inside the window never move the hero; clicks hit the intended control at all four corners in windowed, borderless and after a resolution change, at 100% and 150% desktop scaling; Enter or Ctrl+V in an overlay field neither opens nor pastes into game chat; Alt+Tab during a drag leaves no stuck drag; spawn, summon and drop work from the overlay |
| B3 Increment 3 | Player and World tabs | none new | Every section 7 row works in its eligible mode, and unavailable reasons show elsewhere |

### Then

| Step | Work | Exit |
|---|---|---|
| C Hardening | Server restart, game relaunch (new unbound key) or explicit logout → 401 → "type /debug again" → re-type `/debug` reconnects; Wine pass if in scope; release check (6.8); leak check | Handle count and private bytes of `Darkspore.exe` show no steady growth over 30 minutes of open/close cycling (Task Manager/Process Explorer); the CI `fang.dll` has no `overlay_` literals and no `net/http` dependency |
| D Docs and changelog | `/debug` in `darkspinChatHelp` (`:1032`), the Unknown-command list (`:332`), the field manual's Developer tools (`server/game/api.go:185-190`); `INSTALL.md` if needed; one changelog line under that day's `### YYYY-MM-DD` | After the whole feature is finished (no per-increment changelog lines) |

* **Order.** Day 0 → (A1 ∥ (B0 → (B0a ∥ B0b))) → B1 (needs A1) →
  (A2 ∥ B2) → (A3 ∥ B3) → C → D.
* **Spike code.** After B0, spike code is written under `FANG_OVERLAY`, so it
  becomes the B1 code rather than throwaway code.
* **Builds.** Run `mage build` after any server, launcher or Fang change. It
  also rebuilds Darkspinner with the new `fang.dll` embedded (`magefile.go:159,
  175, 345-346`). Run `mage darkspinner:build` only after `app/darkspinner`
  changes. Both compile the overlay once B0 (6.8) is in.

## 9. Spikes and risks

| ID | Question | Settled by | Fallback (new hooks need their own approval) |
|---|---|---|---|
| S1a | Device acquisition, Present path, threads, Wine load order | B0b | Call-site capture (H1 fallback); otherwise the overlay is unavailable with a trace reason |
| S2a | Does the client poll input (`GetAsyncKeyState`, `GetCursorPos`, raw input, DirectInput)? Cursor type? Local echo of `/debug <key>`? | B0a | Ship display-plus-buttons with the limitation documented; blocking polled input or pausing game input is a separate approval |
| S2b | Do consumed messages stop the hero? | B2 exit | Same as S2a; H4 only for stuck held input |
| S3 | Wine (wined3d forced by the launcher; DXVK only if a user overrides): lazy acquisition and Present correct? macOS? | B1 Wine run | Overlay documented as Windows-only; the server API is unaffected |
| S4 | Glyph readability and widget fit of microui + stb_easy_font | B1 and B2 exits | Nuklear (single C header, has tooltips, combos and a full text editor; render its commands through the same quad path, or bake its atlas into a `D3DPOOL_MANAGED` texture); cimgui last (needs a 32-bit MinGW C++ compiler). Not ID3DXFont (needs Reset handling and the client's D3DX DLL) |

| ID | Risk | Mitigation |
|---|---|---|
| R1 | Item names (prefix table name unknown) | `darkrun db` lookup in A2; fallback `#id slot` labels |
| R2 | Build-103 offsets on another client | Existing fail-closed checks; overlay install is non-fatal |
| R3 | Cheating surface | No capability beyond chat, which every logged-in player already has (also remote guests when multiplayer binds `0.0.0.0`); off by default; development builds only; loopback guard. Gating developer chat commands on shared servers is a separate, existing issue (section 10) |
| R4 | Key exposure | 256-bit random key, memory-only in Fang, stored as a SHA-256 digest server-side and bound to the account's login token. Replaced on rebind; ends on explicit logout or server restart. Never logged: H0 appends it only to a first-token `/debug`, which the server handles as a command and does not record. Sent only to a loopback server, in the Blaze `/debug` body and the HTTP `Authorization` header. Binds are accepted only from loopback Blaze peers, so a LAN peer logged in as the same account cannot replace the host's binding. A possible local chat echo is checked in S2a |
| R5 | Pre-existing: `api.panel.listUsers` leaks `AuthToken` and `Password` without authentication (`server/game/api.go:226-227`) | Not part of this feature. Fix separately before enabling the overlay on any machine others can reach: remove the method (nothing calls it) or return an allowlisted DTO |
| R6 | Queued actions dropped by gameplay. A coarse-eligibility discard is only logged. An apply-time failure (mode gate, warp/zone/profile check) returns an error that can **drop the player's RakNet connection** when the poll runs on a ping or control packet. Typed slash commands do the same today | `queued` vs `applied` wording; server-side availability pre-check that includes the extracted apply-time mode gates (4.5); server log; optional later ring of recent consumer outcomes exposed in state. Making apply-time failures non-fatal for the peer would be a separate gameplay change affecting chat too |
| R7 | Unbounded event queue | Spawn count ≤ 10 per action and one action in flight (mailbox); optional read-only pending-count check later |
| R8 | First goroutine and network I/O in Fang; Go runtime under Wine | Goroutine only in overlay builds, started by the overlay installer through `GoOverlayStart`; loopback-only client without a proxy; hardening covers Wine |

## 10. Decisions

**Blocking (day 0):**

1. Approve H0, H1 (also needed for spike B0b), H2 (Increment 1) and H3
   (Increment 2)? H4 is asked only if S2b requires it.
2. Platform scope. Recommended: native Windows required; Linux and macOS
   through Wine (listed as supported in `INSTALL.md:86`) best effort via S3.

**Defaults (override if you disagree):**

* The overlay is compiled only into development Fang/Darkspinner builds. The
  server routes are always compiled and registered only when `[developer]
  is_overlay_enabled = true` (default false).
* There is no multiplayer gate in the API. Should developer slash commands be
  restricted on shared servers? That is a separate behavior change to existing
  commands, best done once in `chat.Service` so chat and the API are covered
  together.
* The UI is microui + stb_easy_font (C only), with Nuklear as the C-only
  fallback.
* `/summon` keeps level 1 / Basic. Level and rarity selection would extend
  `chat.ItemSummonCommand` later.
* Spawn count is 1-10 per action, as a constant, not a config key.
* There is no hotkey in the MVP; `/debug` is the toggle.
* The key binding lives as long as the account's login token: it ends on
  explicit logout, server restart, or a newer `/debug` (4.3 step 6). Tying it
  to the Blaze connection instead needs the small blaze session port described
  there.
* The development Settings checkbox for the flag is optional (section 5).
* Fix the R5 credential leak as a separate change before Phase A1 ships.

## 11. Repository-rule checklist

* **Git and tests:** no staging or committing by the agent; no tests
  created, edited or run (repository policy). Verify with `mage build`, `mage
  darkspinner:build` (after `app/darkspinner` changes) and the real
  client/server path.
* **Game files and hooks:** no edits to shipped game files. Fang hooks only as
  approved in section 2; spike fallbacks need their own approval.
* **Oversized files:** no new behavior in files over 5,000 lines (6.9);
  extractions that shrink them are allowed.
* **Architecture:**
  * The feature owns its data and rules, with ports defined where they are
    consumed and adapters in subpackages (`overlay/local`, `overlay/web`).
  * Transports only decode, call and encode; DTOs are explicit allowlists, and
    feature types carry no JSON/SQL tags.
  * `context.Context` comes first on I/O operations.
* **Go style:**
  * Error breadcrumbs on every propagated error (`fmt.Errorf("step: %w",
    err)`); guard clauses; no inline error initializers; no discarded return
    slots. Fang's existing `_ = os.Unsetenv` is not copied.
  * Booleans `is_`/`Is` (config key `ConfigIsDeveloperOverlayEnabled`); plural
    only for collections (`nearest_npcs`, `rigblocks`, `action_states`);
    singular package names; `e` receivers; `req` payloads; no
    generic `value`.
  * SQL keywords in uppercase; the only possible new SQL is the optional
    item-name lookup.
* **Launcher:** any launcher message (optional checkbox) uses the themed modal
  or inline status, never a native dialog.
* **Output locations:** spike and reverse-engineering output goes under
  `bin/game/logs/overlay/<spike>`, never directly in `bin` or under
  `bin/game/logs/bugs`; protocol traces stay under
  `bin/server/darkspin/logs/traces`.
* **Changelog:** one `- ` line under the current `### YYYY-MM-DD` heading,
  only when the whole feature is finished. Holding back the `/help`,
  Unknown-command and manual text until step D is this plan's policy, not a
  repository rule.

## 12. Wire contract (implemented on branch `experimental/debug-overlay`)

This section is the single source of truth shared by the server and Fang
implementations. Both sides must match it exactly.

### 12.1 Chat binding

* Fang sends `\x1fdebug <key>` (via the existing 0x1F rewrite). `<key>` is
  exactly 64 lowercase hex characters, from 32 `crypto/rand` bytes.
  * Fang sends it only when `/debug` is the first token, the build is an
    overlay build, and the endpoint is loopback with a published key.
  * In every other case Fang sends bare `\x1fdebug`.
* The server accepts `/debug`, `!debug` and `\x1fdebug`:
  * zero arguments → `BindOverlay` with an empty key;
  * one argument → `BindOverlay` with that key;
  * more than one argument → the syntax reply
    `Syntax: /debug (development overlay builds add their session key)`.
* Reply lines are exactly the table in 4.3.

### 12.2 HTTP

* **Base URL:** `http://127.0.0.1:<port>`, where `<port>` is the port Fang
  already uses for Blaze (`[server].port`).
* **Every request** carries `Authorization: Bearer <key>`, no `Origin`, and
  `Host: 127.0.0.1:<port>` or `localhost:<port>`.
* **Every JSON body** (success or error) carries `"schema_version": 1`.

| Situation | HTTP status | Body |
|---|---|---|
| Feature disabled | 404 (route not registered) | router default |
| Guard failure (Origin present, non-loopback peer, bad Host) | 403 | `{"schema_version":1,"code":"forbidden","message":"forbidden"}` |
| Missing, unknown or expired key | 401 | `{"schema_version":1,"code":"unauthorized","message":"type /debug again"}` |
| Malformed JSON, unknown field, body over 4 KiB, wrong method | 400 / 405 | `{"schema_version":1,"code":"invalid","message":"..."}` |
| Unexpected server error | 500 | `{"schema_version":1,"code":"internal","message":"internal error"}` |

#### `GET /debug/v1/state` → 200

```json
{
  "schema_version": 1,
  "build_id": "development",
  "version": "0.5.0",
  "account": { "display_name": "Alice", "level": 12, "dna": 3400, "pending_warp": "" },
  "game": { "game_id": 17, "mode": "chain", "is_warped": true, "player_count": 1 },
  "hero": { "is_deployed": true, "object_id": 123, "x": 1.0, "y": 2.0, "z": 0.0,
            "hit_point": 840.0, "hit_point_max": 1000.0,
            "power_point": 55.0, "power_point_max": 100.0 },
  "alive_npc_count": 14,
  "nearest_npcs": [ { "object_id": 456, "name": "Boomer", "hit_point": 90.0,
                      "hit_point_max": 120.0, "distance": 8.2, "direction": "Northeast" } ],
  "action_states": { "spawn": { "is_available": false, "reason": "not_warped" } }
}
```

* **Nullability.** `game` is `null` when GameID is 0 or the instance is gone.
  `hero` is `null` without a registry session. `nearest_npcs` is `[]`, never
  `null`.
* **`mode`** is one of `chain`, `tutorial`, `arena`, `unknown`.
* **`action_states`** always contains every action kind below. `reason` is
  `""` when available, otherwise one of `no_game`, `not_deployed`,
  `not_warped`, `wrong_mode`, `zone_terminal`.

#### `GET /debug/v1/catalog` → 200

* The response has an `ETag` header equal to `"<catalog_hash>"`. A matching
  `If-None-Match` returns 304 with an empty body.
* The body matches 4.7 exactly. `name` is never empty; the fallback is
  `#<id> <slot-or-part_types>`. Arrays are never `null`.

#### `POST /debug/v1/action` → 200

The body is JSON with `kind` plus that kind's fields. Unknown fields are
rejected.

| `kind` | Fields |
|---|---|
| `spawn` | `noun` (string, e.g. `"Boomer.Noun"`), `count` (int 1-10) |
| `summon` | `rigblock`, `prefix1`, `prefix2`, `suffix` (uint16; affixes 0 = none) |
| `drop` | `category` (`any`, `weapon`, `hand`, `foot`, `offense`, `defense`, `utility`) |
| `level` | `level` (1-100) |
| `dna` | `dna` (uint32 > 0) |
| `heal`, `power_fill`, `kill`, `reset`, `recap`, `victory`, `defeat` | none |
| `damage`, `power_drain` | `amount` (finite float32 > 0) |
| `goto` | `x`, `y`, `z` (finite float32) |
| `event` | `name` (a catalog `event_names` entry) |
| `effect` | `name` (a catalog `effect_names` entry) |
| `warp` | `area` (Fang canonical level name) |

Response (HTTP 200 for every application-level outcome):

```json
{ "schema_version": 1, "code": "queued", "reason": "", "message": "Spawn queued: 3 of 3",
  "queued_count": 3, "dna_total": 0 }
```

* `code` ∈ `applied`, `queued`, `invalid`, `unavailable`, `overflow`,
  `internal`. `reason` uses the reason codes above, plus `not_online` and
  `invalid_field`.
* `message` is short, adapter-owned and safe to display.

## 13. Implementation notes (branch `experimental/debug-overlay`)

### 13.1 Using it

1. Build with `mage build` (or `mage darkspinner:build`). On this branch both
   compile Fang with `fangdebug,fangoverlay`. The CI target and `mage
   scenario:build` never include the overlay.
2. In the server configuration Darkspinner uses (`darkspin.toml` next to the
   launcher, or the file passed with `--config`), add:

   ```toml
   [developer]
   is_overlay_enabled = true
   ```

   Then restart Darkspinner or the server.
3. In game, open chat and type `/debug` as the first word. The server replies
   `Debug overlay connected` and the overlay window opens. Typing `/debug`
   again closes it. If the panel says `Session not bound: type /debug again`,
   typing `/debug` rebinds the session instead of closing the panel.

### 13.2 Differences from the plan

**Device acquisition (H1)**
* Only the install-time call-site capture is implemented: `patch_call` at
  `exe+0x912B7E`, plus the `CreateDevice` slot patch. Only devices created
  through the game's own `IDirect3D9` are recorded.
* The lazy renderer-singleton read waits for S1a to record a fixed device
  offset. A heuristic scan was implemented first and then removed in review,
  because it made COM calls on unverified pointers.
* Consequence: where Fang loads after the device was created (possible under
  Wine, where Fang is loaded asynchronously), the overlay stays unavailable.
  The trace shows `overlay_device_source=0` and `overlay_open_refused=4`. The
  game is unaffected.

**Drawing (H2)**
* Only device `Present` (slot 17) is patched. A swap-chain `Present` hook was
  removed in review, because it is not part of H2.
* If the game presents through a swap chain, nothing is drawn, and the trace
  has no `overlay_first_frame`.

**Server behavior**
* The `event` action accepts only canonical names (`security-next`,
  `boss-start`, `boss-complete`), not the numeric chat aliases.
* A static bounds failure returns HTTP 200 with `code=invalid` and
  `reason=invalid_field`. Malformed JSON, unknown or other-kind fields, a body
  over 4 KiB and an unknown kind return HTTP 400. A wrong method returns 405.
* `queued_count` is 1 for every queued action other than spawn.
* Availability applies each apply-time gate exactly as the consumer does,
  including squad presence for event-family kinds, an Arena team for Arena
  victory, and a zone for chain victory.
* Item names come from `localization_text` for rigblocks and suffixes,
  assuming `table_id = util.HashID("<table name>")` and the lowercase
  `0x%08x` key. Prefix names always use the `#<id> <part_types>` fallback.
  **[verify]** with `darkrun db localization_text get table_id=<hash>`.
* `/debug` is listed in `/help`, in the Unknown-command reply and in the field
  manual.

**Fang UI and input**
* The UI is split across `overlay_ui.c`, `overlay_items.c` and
  `overlay_actions.c`.
* There is no hotkey. Ctrl+V does not paste into overlay fields.
* Shift is not passed to microui, which disables number-edit mode.
* A press is applied one frame after a mouse move to a new position, so hover
  is current.
* A 2-pixel pointer marker is drawn over the panel.
* microui `abort()`s if its fixed stacks overflow. The UI stays far below
  them.

**Server-only changes beyond the plan**
* `server/gameplay/eligibility.go` also replaces the apply-time gates at
  their original sites in `status.go`, `recap.go`, `developer_drop.go`,
  `arena.go` and `handler.go` (tutorial victory). The replacements are exact
  extractions.
* The heal gate in `session.go` (over 5,000 lines) is duplicated as
  `isDeveloperHealMode` rather than extracted.

### 13.3 Verified here, and what still needs the game

**Verified in this container (no tests, by repository policy):**
* Server: `go build ./...` and `-tags scenario`; `go vet` and `gofmt` clean on
  the touched packages.
* Fang: c-shared cross-compiles with `fangdebug,fangoverlay`, `fangdebug` and
  `fangdebug,scenario`; `go vet` clean for both overlay and release tag sets.
* Release shape: `strings` on the release-shape DLL finds no `overlay_` or
  `GoOverlay`, and `go list -deps` has no `net/http`.
* Magefile: `go vet -tags mage ./magefile.go` passes.
* Review: two adversarial rounds; 15 and then 7 confirmed findings, all
  fixed.

**Not verified (needs the real client):**
* Everything in section 8's B1-B3 exits, and the A1-A3 curl exits against a
  running server with content.db.
* Device capture, the Present path, render-state restore, input consumption,
  Wine behavior and readability.

**Trace keys to check in `game.jsonl`:**
`overlay_install`, `overlay_capture_hook`, `overlay_create_device_hook`,
`overlay_device_captured`, `overlay_device_source`, `overlay_present_hook`,
`overlay_first_frame`, `overlay_visible`, `overlay_open_refused` (1 not
installed, 2 disabled, 3 no window, 4 no device, 5 Present patch failed),
`overlay_toggle_post`, `overlay_rebind`, `overlay_key_length`,
`overlay_net_mode`, `overlay_net_status`, `overlay_net_catalog_status`,
`overlay_net_action_status`, `overlay_device_lost`.
