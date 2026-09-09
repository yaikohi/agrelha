package content

import (
	"regexp"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

var cfgNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.cfg$`)

// ConfigsList returns the sorted list of mod config file names from the ConfigMap.
func (h *Handler) ConfigsList(c *fiber.Ctx) ([]string, error) {
	if h.cfg.K8s == nil {
		return nil, nil
	}
	data, err := h.cfg.K8s.ConfigMapData(c.UserContext(), configsCM)
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

// ConfigsPage renders the list of mod config files.
func (h *Handler) ConfigsPage(c *fiber.Ctx) error {
	files, _ := h.ConfigsList(c)
	fk, fm := shared.TakeFlash(c)
	return shared.Render(c, pages.Configs(files, h.cfg.Git != nil, fk, fm))
}

// ConfigNew renders the page to create a new config file.
func (h *Handler) ConfigNew(c *fiber.Ctx) error {
	return shared.Render(c, pages.ConfigEdit("", "", true, h.cfg.Git != nil))
}

// ConfigEdit renders the editor for an existing config file.
func (h *Handler) ConfigEdit(c *fiber.Ctx) error {
	name := c.Query("f")
	if !cfgNameRe.MatchString(name) {
		shared.SetFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	var content string
	if h.cfg.K8s != nil {
		if data, err := h.cfg.K8s.ConfigMapData(c.UserContext(), configsCM); err == nil {
			content = data[name]
		}
	}
	return shared.Render(c, pages.ConfigEdit(name, content, false, h.cfg.Git != nil))
}

// ConfigSave handles creating or updating a mod config file.
func (h *Handler) ConfigSave(c *fiber.Ctx) error {
	if h.cfg.Git == nil || h.cfg.Cfg == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !cfgNameRe.MatchString(file) {
		shared.SetFlash(c, "err", "File name must look like Some.Mod.cfg (letters, digits, . _ -).")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	content := strings.ReplaceAll(c.FormValue("content"), "\r\n", "\n")

	changed, err := h.cfg.Git.SetData(c.UserContext(), h.cfg.Cfg.ModConfigsPath, file, content,
		"agrelha: edit mod config "+file)
	if err != nil {
		shared.SetFlash(c, "err", "Save failed: "+err.Error())
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mod-config-edit", file)
	}
	if changed {
		h.cfg.ApplyAfterSync(configsCM, file, func(v string) bool { return v == content })
		shared.SetFlash(c, "ok", "Saved "+file+" — committed; the server will restart to apply.")
	} else {
		shared.SetFlash(c, "ok", file+" is unchanged.")
	}
	return c.Redirect("/configs", fiber.StatusSeeOther)
}

// ConfigDelete handles deleting a mod config file.
func (h *Handler) ConfigDelete(c *fiber.Ctx) error {
	if h.cfg.Git == nil || h.cfg.Cfg == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !cfgNameRe.MatchString(file) {
		shared.SetFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Git.DeleteData(c.UserContext(), h.cfg.Cfg.ModConfigsPath, file,
		"agrelha: delete mod config "+file)
	if err != nil {
		shared.SetFlash(c, "err", "Delete failed: "+err.Error())
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mod-config-delete", file)
	}
	if changed {
		h.cfg.ApplyAfterSync(configsCM, file, func(v string) bool { return v == "" })
		shared.SetFlash(c, "ok", "Deleted "+file+" — committed; the server will restart to apply.")
	} else {
		shared.SetFlash(c, "ok", file+" was not present.")
	}
	return c.Redirect("/configs", fiber.StatusSeeOther)
}
