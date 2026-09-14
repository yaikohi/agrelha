package valheim

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web/shared"
)

var valheimCfgNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-/]*\.(cfg|json|yaml|yml|txt|ini|properties)$`)

// ValheimInstanceConfigGet reads a config file for a specific Valheim instance into the SSE editor.
func (h *Handler) ValheimInstanceConfigGet(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Valheim instance manager unavailable")
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	fileName := strings.TrimSpace(c.Query("f"))
	if fileName == "" {
		return c.Status(fiber.StatusBadRequest).SendString("File name required")
	}

	content, _ := h.cfg.ValheimInstances.GetConfig(c.UserContext(), num, fileName)

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
	if err := patchSignals(w, signals); err != nil {
		return err
	}
	return c.Send(buf.Bytes())
}

// ValheimInstanceConfigSave saves a config file for a specific Valheim instance.
func (h *Handler) ValheimInstanceConfigSave(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		File    string `json:"file" form:"file"`
		Content string `json:"content" form:"content"`
	}
	_ = c.BodyParser(&req)

	fileName := strings.Clone(strings.TrimSpace(req.File))
	if fileName == "" {
		fileName = strings.Clone(strings.TrimSpace(c.FormValue("file")))
	}
	if !valheimCfgNameRe.MatchString(fileName) {
		return shared.SSEToast(c, "err", "Invalid config file name. Must end in .cfg, .json, .yaml, etc.", nil)
	}

	content := strings.Clone(strings.ReplaceAll(req.Content, "\r\n", "\n"))

	changed, err := h.cfg.ValheimInstances.SaveConfig(c.UserContext(), num, fileName, content, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Save failed: "+err.Error(), nil)
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

// ValheimInstanceConfigDelete deletes a config file for a specific Valheim instance.
func (h *Handler) ValheimInstanceConfigDelete(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		File string `json:"file" form:"file"`
	}
	_ = c.BodyParser(&req)

	fileName := strings.Clone(strings.TrimSpace(req.File))
	if fileName == "" {
		fileName = strings.Clone(strings.TrimSpace(c.FormValue("file")))
	}
	if fileName == "" {
		fileName = strings.Clone(strings.TrimSpace(c.Query("file")))
	}
	if !valheimCfgNameRe.MatchString(fileName) {
		return shared.SSEToast(c, "err", "Invalid config file name.", nil)
	}

	changed, err := h.cfg.ValheimInstances.DeleteConfig(c.UserContext(), num, fileName, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Delete failed: "+err.Error(), nil)
	}

	if changed {
		return shared.SSEToast(c, "ok", fmt.Sprintf("Deleted %s.", fileName), map[string]any{
			"showEditor": false,
		})
	}
	return shared.SSEToast(c, "ok", fmt.Sprintf("%s was not present.", fileName), map[string]any{
		"showEditor": false,
	})
}
