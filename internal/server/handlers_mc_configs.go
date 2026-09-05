package server

import (
	"regexp"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
)

const mcConfigsCM = "minecraft-neoforge-configs"

var mcCfgNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-/]*\.(toml|json|json5|yaml|yml|cfg|txt|properties|ini)$`)

func (s *FiberServer) mcConfigsList(c *fiber.Ctx) ([]string, error) {
	if s.mck8s == nil {
		return nil, nil
	}
	data, err := s.mck8s.ConfigMapData(c.UserContext(), mcConfigsCM)
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

func (s *FiberServer) mcConfigsPage(c *fiber.Ctx) error {
	files, _ := s.mcConfigsList(c)
	fk, fm := takeFlash(c)
	return render(c, pages.MinecraftConfigs(files, s.git != nil, fk, fm))
}

func (s *FiberServer) mcConfigNew(c *fiber.Ctx) error {
	return render(c, pages.MinecraftConfigEdit("", "", true, s.git != nil))
}

func (s *FiberServer) mcConfigEdit(c *fiber.Ctx) error {
	name := c.Query("f")
	if !mcCfgNameRe.MatchString(name) {
		setFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	var content string
	if s.mck8s != nil {
		if data, err := s.mck8s.ConfigMapData(c.UserContext(), mcConfigsCM); err == nil {
			content = data[name]
		}
	}
	return render(c, pages.MinecraftConfigEdit(name, content, false, s.git != nil))
}

func (s *FiberServer) mcConfigSave(c *fiber.Ctx) error {
	if s.git == nil {
		setFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !mcCfgNameRe.MatchString(file) {
		setFlash(c, "err", "File name must look like mod.toml or config.json (letters, digits, . _ - /).")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	content := strings.ReplaceAll(c.FormValue("content"), "\r\n", "\n")

	changed, err := s.git.SetData(c.UserContext(), s.cfg.MinecraftConfigsPath, file, content,
		"agrelha: edit minecraft config "+file)
	if err != nil {
		setFlash(c, "err", "Save failed: "+err.Error())
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	_ = s.store.RecordAudit(s.actor(c), "mc-config-edit", file)
	if changed {
		s.applyMinecraftAfterSync(mcConfigsCM, "", file, func(v string) bool { return v == content })
		setFlash(c, "ok", "Saved "+file+" — committed; the server will restart to apply.")
	} else {
		setFlash(c, "ok", file+" is unchanged.")
	}
	return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
}

func (s *FiberServer) mcConfigDelete(c *fiber.Ctx) error {
	if s.git == nil {
		setFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !mcCfgNameRe.MatchString(file) {
		setFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	changed, err := s.git.DeleteData(c.UserContext(), s.cfg.MinecraftConfigsPath, file,
		"agrelha: delete minecraft config "+file)
	if err != nil {
		setFlash(c, "err", "Delete failed: "+err.Error())
		return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
	}
	_ = s.store.RecordAudit(s.actor(c), "mc-config-delete", file)
	if changed {
		s.applyMinecraftAfterSync(mcConfigsCM, "", file, func(v string) bool { return v == "" })
		setFlash(c, "ok", "Deleted "+file+" — committed; the server will restart to apply.")
	} else {
		setFlash(c, "ok", file+" was not present.")
	}
	return c.Redirect("/minecraft/configs", fiber.StatusSeeOther)
}
