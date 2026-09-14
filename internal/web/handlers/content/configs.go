package content

import (
	"regexp"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/ports"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

const configsCM = "valheim-mod-configs"

var cfgNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.cfg$`)

// ConfigsList returns the sorted list of mod config file names from the ConfigMap or StateStore.
func (h *Handler) ConfigsList(c *fiber.Ctx) ([]string, error) {
	data, err := h.configData(c.UserContext())
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
	return shared.Render(c, pages.Configs(files, h.cfg.StateStore != nil, fk, fm))
}

// ConfigNew renders the page to create a new config file.
func (h *Handler) ConfigNew(c *fiber.Ctx) error {
	return shared.Render(c, pages.ConfigEdit("", "", true, h.cfg.StateStore != nil))
}

// ConfigEdit renders the editor for an existing config file.
func (h *Handler) ConfigEdit(c *fiber.Ctx) error {
	name := c.Query("f")
	if !cfgNameRe.MatchString(name) {
		shared.SetFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	var content string
	if data, err := h.configData(c.UserContext()); err == nil && data != nil {
		content = data[name]
	}
	return shared.Render(c, pages.ConfigEdit(name, content, false, h.cfg.StateStore != nil))
}

// ConfigSave handles creating or updating a mod config file.
func (h *Handler) ConfigSave(c *fiber.Ctx) error {
	if h.cfg.StateStore == nil || h.cfg.ModConfigsPath == "" {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !cfgNameRe.MatchString(file) {
		shared.SetFlash(c, "err", "File name must look like Some.Mod.cfg (letters, digits, . _ -).")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	content := strings.ReplaceAll(c.FormValue("content"), "\r\n", "\n")

	changed, err := h.cfg.StateStore.Patch(c.UserContext(), h.cfg.ModConfigsPath,
		"agrelha: edit mod config "+file, func(doc *ports.Document) (bool, error) {
			if doc.Data == nil {
				doc.Data = make(map[string]string)
			}
			if doc.Data[file] == content {
				return false, nil
			}
			doc.Data[file] = content
			return true, nil
		})
	if err != nil {
		shared.SetFlash(c, "err", "Save failed: "+err.Error())
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	if h.cfg.Audit != nil {
		_ = h.cfg.Audit.RecordAudit(h.cfg.Actor(c), "mod-config-edit", file)
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
	if h.cfg.StateStore == nil || h.cfg.ModConfigsPath == "" {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !cfgNameRe.MatchString(file) {
		shared.SetFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.StateStore.Patch(c.UserContext(), h.cfg.ModConfigsPath,
		"agrelha: delete mod config "+file, func(doc *ports.Document) (bool, error) {
			if doc.Data == nil {
				return false, nil
			}
			if _, ok := doc.Data[file]; !ok {
				return false, nil
			}
			delete(doc.Data, file)
			return true, nil
		})
	if err != nil {
		shared.SetFlash(c, "err", "Delete failed: "+err.Error())
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	if h.cfg.Audit != nil {
		_ = h.cfg.Audit.RecordAudit(h.cfg.Actor(c), "mod-config-delete", file)
	}
	if changed {
		h.cfg.ApplyAfterSync(configsCM, file, func(v string) bool { return v == "" })
		shared.SetFlash(c, "ok", "Deleted "+file+" — committed; the server will restart to apply.")
	} else {
		shared.SetFlash(c, "ok", file+" was not present.")
	}
	return c.Redirect("/configs", fiber.StatusSeeOther)
}
