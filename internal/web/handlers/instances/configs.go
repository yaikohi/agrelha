package instances

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
	"agrelha/internal/web/sse"
)

const mcConfigsCM = "minecraft-neoforge-configs"

var mcCfgNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-/]*\.(toml|json|json5|yaml|yml|cfg|txt|properties|ini)$`)

// MCConfigsList lists all global Minecraft config file names from the ConfigMap.
func (h *Handler) MCConfigsList(c *fiber.Ctx) ([]string, error) {
	if h.cfg.MCK8s == nil {
		return nil, nil
	}
	data, err := h.cfg.MCK8s.ConfigMapData(c.UserContext(), mcConfigsCM)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(data))
	for k := range data {
		files = append(files, k)
	}
	sort.Strings(files)
	return files, nil
}

// MCConfigsPage renders the list of Minecraft config files.
func (h *Handler) MCConfigsPage(c *fiber.Ctx) error {
	files, _ := h.MCConfigsList(c)
	fk, fm := shared.TakeFlash(c)
	return shared.Render(c, pages.MinecraftConfigs(files, h.cfg.Git != nil, fk, fm))
}

// MCConfigNew renders the new Minecraft config file form.
func (h *Handler) MCConfigNew(c *fiber.Ctx) error {
	return shared.Render(c, pages.MinecraftConfigEdit("", "", true, h.cfg.Git != nil))
}

// MCConfigEdit renders the edit form for an existing Minecraft config file.
func (h *Handler) MCConfigEdit(c *fiber.Ctx) error {
	name := c.Query("f")
	if !mcCfgNameRe.MatchString(name) {
		shared.SetFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	var content string
	if h.cfg.MCK8s != nil {
		if data, err := h.cfg.MCK8s.ConfigMapData(c.UserContext(), mcConfigsCM); err == nil {
			content = data[name]
		}
	}
	return shared.Render(c, pages.MinecraftConfigEdit(name, content, false, h.cfg.Git != nil))
}

// MCConfigSave saves changes to a global Minecraft config file.
func (h *Handler) MCConfigSave(c *fiber.Ctx) error {
	if h.cfg.Git == nil || h.cfg.Cfg == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !mcCfgNameRe.MatchString(file) {
		shared.SetFlash(c, "err", "File name must look like mod.toml or config.json (letters, digits, . _ - /).")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	content := strings.ReplaceAll(c.FormValue("content"), "\r\n", "\n")

	changed, err := h.cfg.Git.SetData(c.UserContext(), h.cfg.Cfg.MinecraftConfigsPath, file, content,
		"agrelha: edit minecraft config "+file)
	if err != nil {
		shared.SetFlash(c, "err", "Save failed: "+err.Error())
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-config-edit", file)
	}
	if changed {
		h.cfg.ApplyMinecraftAfterSync(mcConfigsCM, "", file, func(v string) bool { return v == content })
		shared.SetFlash(c, "ok", "Saved "+file+" — committed; the server will restart to apply.")
	} else {
		shared.SetFlash(c, "ok", file+" is unchanged.")
	}
	return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
}

// MCConfigDelete deletes a global Minecraft config file.
func (h *Handler) MCConfigDelete(c *fiber.Ctx) error {
	if h.cfg.Git == nil || h.cfg.Cfg == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !mcCfgNameRe.MatchString(file) {
		shared.SetFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Git.DeleteData(c.UserContext(), h.cfg.Cfg.MinecraftConfigsPath, file,
		"agrelha: delete minecraft config "+file)
	if err != nil {
		shared.SetFlash(c, "err", "Delete failed: "+err.Error())
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-config-delete", file)
	}
	if changed {
		h.cfg.ApplyMinecraftAfterSync(mcConfigsCM, "", file, func(v string) bool { return v == "" })
		shared.SetFlash(c, "ok", "Deleted "+file+" — committed; the server will restart to apply.")
	} else {
		shared.SetFlash(c, "ok", file+" was not present.")
	}
	return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
}

// MCInstanceConfigGet reads a config file for a specific instance into the SSE editor.
func (h *Handler) MCInstanceConfigGet(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil || h.cfg.MCK8s == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Service unavailable")
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}
	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	fileName := strings.TrimSpace(c.Query("f"))
	if fileName == "" {
		return c.Status(fiber.StatusBadRequest).SendString("File name required")
	}

	content := ""
	if data, err := h.cfg.MCK8s.ConfigMapData(c.UserContext(), inst.ConfigsCMName()); err == nil {
		content = data[fileName]
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	signals := map[string]any{
		"selectedFile": fileName,
		"fileContent":  content,
		"isNew":        false,
		"showEditor":   true,
	}
	if err := sse.PatchSignals(w, signals); err != nil {
		return err
	}
	return c.Send(buf.Bytes())
}

// MCInstanceConfigSave saves a config file for a specific instance.
func (h *Handler) MCInstanceConfigSave(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil || h.cfg.Git == nil {
		return shared.SSEToast(c, "err", "Instance manager or Git committer unconfigured.", nil)
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}
	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return shared.SSEToast(c, "err", "Instance not found.", nil)
	}

	var req struct {
		File    string `json:"file" form:"file"`
		Content string `json:"content" form:"content"`
	}
	_ = c.BodyParser(&req)

	fileName := strings.TrimSpace(req.File)
	if fileName == "" {
		fileName = strings.TrimSpace(c.FormValue("file"))
	}
	if !mcCfgNameRe.MatchString(fileName) {
		return shared.SSEToast(c, "err", "Invalid config file name. Must end in .toml, .json, .yaml, .cfg, .snbt, etc.", nil)
	}

	content := strings.ReplaceAll(req.Content, "\r\n", "\n")
	relPath := fmt.Sprintf("manifests/minecraft-modded/instance-%02d/configs.yaml", inst.Number)

	changed, err := h.cfg.Git.SetData(c.UserContext(), relPath, fileName, content,
		fmt.Sprintf("agrelha: edit config %s for instance #%02d", fileName, inst.Number))
	if err != nil {
		return shared.SSEToast(c, "err", "Save failed: "+err.Error(), nil)
	}

	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-config-edit", fmt.Sprintf("Saved %s on #%02d", fileName, num))
	}
	if changed {
		return shared.SSEToast(c, "ok", fmt.Sprintf("Saved %s — committed to git. Restarts apply.", fileName), map[string]any{
			"showEditor": false,
		})
	}
	return shared.SSEToast(c, "ok", fmt.Sprintf("%s is unchanged.", fileName), map[string]any{
		"showEditor": false,
	})
}

// MCInstanceConfigDelete deletes a config file for a specific instance.
func (h *Handler) MCInstanceConfigDelete(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil || h.cfg.Git == nil {
		return shared.SSEToast(c, "err", "Instance manager or Git committer unconfigured.", nil)
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}
	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return shared.SSEToast(c, "err", "Instance not found.", nil)
	}

	var req struct {
		File string `json:"file" form:"file"`
	}
	_ = c.BodyParser(&req)

	fileName := strings.TrimSpace(req.File)
	if fileName == "" {
		fileName = strings.TrimSpace(c.FormValue("file"))
	}
	if fileName == "" {
		return shared.SSEToast(c, "err", "File name required.", nil)
	}

	relPath := fmt.Sprintf("manifests/minecraft-modded/instance-%02d/configs.yaml", inst.Number)
	changed, err := h.cfg.Git.DeleteData(c.UserContext(), relPath, fileName,
		fmt.Sprintf("agrelha: delete config %s for instance #%02d", fileName, inst.Number))
	if err != nil {
		return shared.SSEToast(c, "err", "Delete failed: "+err.Error(), nil)
	}

	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-config-delete", fmt.Sprintf("Deleted %s on #%02d", fileName, num))
	}
	if changed {
		return shared.SSEToast(c, "ok", fmt.Sprintf("Deleted %s — committed to git.", fileName), nil)
	}
	return shared.SSEToast(c, "ok", fmt.Sprintf("%s was not present.", fileName), nil)
}
