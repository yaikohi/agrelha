# How Minecraft Whitelist Management Works in Agrelha {#mc-whitelist}

Agrelha manages access through a non-destructive RCON probe, dual-write GitOps persistence, and environment-based UUID pre-resolution.

- Probing uses a temporary `/whitelist on` probe to infer status because modern Minecraft dropped `/whitelist status`.[](source:access_src)
- Add and remove actions apply instantly via RCON and save to GitOps for restart persistence.[](source:access_src)
- The container resolves player names to UUIDs at boot using the `WHITELIST` environment variable.[](source:gemini_src)
- Every access change emits an immutable audit record with actor identity and target player.[](source:access_src)

@answer scope="Minecraft 1.13+ · RCON protocol · GitOps dual-write"
@meta locale=en repo_url="ssh://git@codeberg.org/ykhi/agrelha.git" repo_revision="14438fdf84e36ba519ca9cb46974e45fec97e8f1"
@source access_src title="AccessManager implementation" path=internal/app/access/access.go lines=164:319
@source gemini_src title="Agrelha Guidelines" path=GEMINI.md lines=20:27

## How does Agrelha probe whitelist status without modifying state? {#probing-state}

Modern Minecraft removed `/whitelist status`. Agrelha issues `/whitelist on` and checks the response text. If the server answers with `already`, the whitelist was already active. If it answers with `now`, Agrelha immediately sends `/whitelist off` to restore disabled state.

@sequence id=probe-flow title="Non-destructive whitelist probe"
| actor agrelha | Agrelha Engine | backend |
| actor rcon | Minecraft Server | backend |
| agrelha | rcon | /whitelist on | emphasis |
| rcon | agrelha | Whitelist is already turned on | return |
| agrelha | rcon | /whitelist on (if was off) |
| rcon | agrelha | Whitelist is now turned on | return |
| agrelha | rcon | /whitelist off (immediate rollback) | security |

@steps id=probe-steps
| send-probe | Probe Command | Agrelha transmits `/whitelist on` across RCON. | probe-flow | agrelha |
| evaluate | Branch Check | The parser inspects the response for `already` or `now`. | probe-flow | agrelha |
| revert | State Revert | If `now` appeared, Agrelha sends `/whitelist off` instantly. | probe-flow | rcon |

## How are player access modifications kept in sync? {#dual-write-flow}

Live server state and declarative GitOps configurations stay aligned through dual writes. A single API call modifies the in-memory game state and commits to `whitelist.txt`.

@sequence id=sync-flow title="Dual-write GitOps & live RCON update"
| actor admin | Operator UI | frontend |
| actor control | Agrelha Core | backend |
| actor git | GitOps Store | database |
| actor mc | Minecraft Server | backend |
| admin | control | Add player request |
| control | git | Patch whitelist.txt commit | security |
| control | mc | /whitelist add player | emphasis |
| control | mc | /whitelist reload |
| mc | control | Reloaded whitelist | return |

@steps id=sync-steps
| commit | GitOps Patch | Agrelha writes the username into `whitelist.txt` in Git. | sync-flow | git |
| rcon-add | Live RCON Add | Agrelha issues `/whitelist add` to admit the player immediately. | sync-flow | mc |
| rcon-reload | Live Reload | Agrelha reloads server memory so changes take effect without restart. | sync-flow | mc |

## Why does container startup require WHITELIST rather than WHITELIST_FILE? {#container-resolution}

The container entrypoint `mc-image-helper` handles usernames differently based on configuration format. Passing usernames via environment variable triggers PlayerDB and Mojang UUID lookups before server launch.

| Approach | Startup UUID Resolution | Offline / Modded Safety | GitOps Compatibility |
| --- | --- | --- | --- |
| `WHITELIST` env var | Automatic lookup {good:Online UUID} | High resilience {good:Pre-cached} | ConfigMap key mapping |
| `WHITELIST_FILE` mount | Skips image helper {bad:Raw names} | Risk of join errors {bad:Unresolved} | Direct file volume |

@callout id=uuid-note level=note title="UUID resolution requirement"
Passing declarative values through `WHITELIST` lets `mc-image-helper` query Mojang APIs before server startup. Mounting files directly leaves raw strings without UUID mappings in `whitelist.json`.

## Access management checklist {#checklist}

- [ ] Confirm RCON port `25575` responds with authenticated session
- [ ] Verify `WhitelistEnforced()` returns accurate boolean without leaving whitelist enabled
- [ ] Inspect Git repository history for automated `mc-access` commits
- [ ] Ensure deployment passes `WHITELIST` environment variable from ConfigMap
