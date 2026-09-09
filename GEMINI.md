# Agrelha Developer & Agent Guidelines

Before editing any file, read it first. Before modifying a function, grep for all callers. Research before you edit

## Tooling & Command Execution
- Always prefix shell commands with `rtk` (e.g. `rtk go test ./...`, `rtk git status`, `rtk kubectl ...`).

## Architecture & Networking
- **Gameserver Node:** `game-01` (bare-metal MiniPC).
- **Valheim:**
  - Deployment: `valheim` in namespace `valheim`.
  - Service: Cilium LoadBalancer `192.168.20.224` (UDP 2456-2457).
- **Minecraft Modded (NeoForge / Fabric):**
  - Deployment: `minecraft-neoforge` / `minecraft-fabric` in namespace `minecraft-neoforge` (migrating to `minecraft-modded`).
  - Service: Cilium LoadBalancer `192.168.20.225` (TCP/UDP 25565, RCON TCP 25575).
  - Traefik is NOT used for Minecraft game traffic. Minecraft connects directly via the Cilium L2 LoadBalancer IP.

## Minecraft Access & RCON Protocol Notes
- **In-Game Whitelist Status:**
  - Modern Minecraft (1.13 through 26.2.X) **does not support `/whitelist status`**. Attempting this command returns an error.
  - To probe whether whitelist is enforced without modifying state:
    - Send `/whitelist on`.
    - If output contains `"already"`, the whitelist was already active (return `true`).
    - If output contains `"now"`, it was previously disabled. Immediately send `/whitelist off` to restore its state (return `false`).
- **Whitelisting & Ops:**
  - Live access modifications must be applied via RCON (`/whitelist add <player>`, `/whitelist remove <player>`, `/op <player>`, `/deop <player>`) AND committed to GitOps access ConfigMaps for persistence.
  - Declarative ConfigMap values (`whitelist.txt`, `ops.txt`) must be passed to the `itzg/minecraft-server` container via the `WHITELIST` and `OPS` environment variables (not `WHITELIST_FILE` / `OPS_FILE`) so `mc-image-helper` resolves UUIDs via PlayerDB/Mojang API into valid JSON.
