package server

import (
	"regexp"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
)

const configsCM = "valheim-mod-configs"

var cfgNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.cfg$`)

func (s *FiberServer) configsList(c *fiber.Ctx) ([]string, error) {
	if s.k8s == nil {
		return nil, nil
	}
	data, err := s.k8s.ConfigMapData(c.UserContext(), configsCM)
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

func (s *FiberServer) configsPage(c *fiber.Ctx) error {
	files, _ := s.configsList(c)
	fk, fm := takeFlash(c)
	return render(c, pages.Configs(files, s.git != nil, fk, fm))
}

func (s *FiberServer) configNew(c *fiber.Ctx) error {
	return render(c, pages.ConfigEdit("", "", true, s.git != nil))
}

func (s *FiberServer) configEdit(c *fiber.Ctx) error {
	name := c.Query("f")
	if !cfgNameRe.MatchString(name) {
		setFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	var content string
	if s.k8s != nil {
		if data, err := s.k8s.ConfigMapData(c.UserContext(), configsCM); err == nil {
			content = data[name]
		}
	}
	return render(c, pages.ConfigEdit(name, content, false, s.git != nil))
}

func (s *FiberServer) configSave(c *fiber.Ctx) error {
	if s.git == nil {
		setFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !cfgNameRe.MatchString(file) {
		setFlash(c, "err", "File name must look like Some.Mod.cfg (letters, digits, . _ -).")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	content := strings.ReplaceAll(c.FormValue("content"), "\r\n", "\n")

	changed, err := s.git.SetData(c.UserContext(), s.cfg.ModConfigsPath, file, content,
		"agrelha: edit mod config "+file)
	if err != nil {
		setFlash(c, "err", "Save failed: "+err.Error())
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	_ = s.store.RecordAudit(s.actor(c), "mod-config-edit", file)
	if changed {
		s.applyAfterSync(configsCM, file, func(v string) bool { return v == content })
		setFlash(c, "ok", "Saved "+file+" — committed; the server will restart to apply.")
	} else {
		setFlash(c, "ok", file+" is unchanged.")
	}
	return c.Redirect("/configs", fiber.StatusSeeOther)
}

func (s *FiberServer) configDelete(c *fiber.Ctx) error {
	if s.git == nil {
		setFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	file := strings.TrimSpace(c.FormValue("file"))
	if !cfgNameRe.MatchString(file) {
		setFlash(c, "err", "Invalid config file name.")
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	changed, err := s.git.DeleteData(c.UserContext(), s.cfg.ModConfigsPath, file,
		"agrelha: delete mod config "+file)
	if err != nil {
		setFlash(c, "err", "Delete failed: "+err.Error())
		return c.Redirect("/configs", fiber.StatusSeeOther)
	}
	_ = s.store.RecordAudit(s.actor(c), "mod-config-delete", file)
	if changed {
		s.applyAfterSync(configsCM, file, func(v string) bool { return v == "" })
		setFlash(c, "ok", "Deleted "+file+" — committed; the server will restart to apply.")
	} else {
		setFlash(c, "ok", file+" was not present.")
	}
	return c.Redirect("/configs", fiber.StatusSeeOther)
}
