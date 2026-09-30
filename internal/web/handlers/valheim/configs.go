package valheim

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	appbepinex "agrelha/internal/app/bepinex"
	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

var valheimCfgNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-/]*\.(cfg|json|yaml|yml|txt|ini|properties)$`)

// searchLimit caps how many Settings one fragment renders. Therzie.Warfare.cfg
// declares 1398; sending them all is megabytes of HTML and a browser that stops
// responding. Truncation is always stated, never silent - a clipped list that
// looks complete reads as "this mod has no such setting".
const searchLimit = 200

// ValheimInstanceConfigGet opens one mod's config: its sections, and either the
// settings the operator already manages or the raw override set.
func (h *Handler) ValheimInstanceConfigGet(c *fiber.Ctx) error {
	svc, num, name, err := h.configRequest(c)
	if err != nil {
		return ssePatchElements(c, "#cfg-panel", configError(err))
	}
	inst, err := h.instanceFor(c, num)
	if err != nil {
		return ssePatchElements(c, "#cfg-panel", configError(err))
	}

	view, err := svc.File(c.UserContext(), *inst, name)
	if err != nil {
		return ssePatchElements(c, "#cfg-panel", configError(err))
	}
	return ssePatchElements(c, "#cfg-panel", renderPanel(num, name, view))
}

// ValheimInstanceConfigSettings renders the Settings matching a section or a
// search. It is a separate endpoint because the whole point is not to render
// them all at once.
func (h *Handler) ValheimInstanceConfigSettings(c *fiber.Ctx) error {
	svc, num, name, err := h.configRequest(c)
	if err != nil {
		return ssePatchElements(c, "#cfg-settings", configError(err))
	}
	inst, err := h.instanceFor(c, num)
	if err != nil {
		return ssePatchElements(c, "#cfg-settings", configError(err))
	}
	view, err := svc.File(c.UserContext(), *inst, name)
	if err != nil {
		return ssePatchElements(c, "#cfg-settings", configError(err))
	}

	q := domain.SearchQuery{
		Text:        strings.TrimSpace(c.Query("q")),
		Section:     c.Query("section"),
		OnlyChanged: c.Query("changed") == "true",
		Limit:       searchLimit,
	}
	// With no filter at all, show what the operator manages rather than 1398
	// rows they did not ask for.
	onlyManaged := q.Text == "" && q.Section == "" && !q.OnlyChanged
	hits, truncated := view.Config.Search(q)
	return ssePatchElements(c, "#cfg-settings", renderSettings(num, name, view, hits, truncated, onlyManaged))
}

// ValheimInstanceConfigSave records the operator's edits as Overrides.
func (h *Handler) ValheimInstanceConfigSave(c *fiber.Ctx) error {
	svc, num, name, err := h.configRequest(c)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}
	inst, err := h.instanceFor(c, num)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}

	var req struct {
		File   string            `json:"file"`
		SHA256 string            `json:"sha256"`
		Values map[string]string `json:"values"`
		Forget []string          `json:"forget"`
	}
	_ = c.BodyParser(&req)
	if req.File != "" {
		name = strings.Clone(strings.TrimSpace(req.File))
	}

	view, err := svc.File(c.UserContext(), *inst, name)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}

	changes, err := decodeChanges(view, req.Values, req.Forget)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}
	if len(changes) == 0 {
		return shared.SSEToast(c, "ok", "Nothing to save.", nil)
	}

	changed, err := svc.Apply(c.UserContext(), *inst, name, req.SHA256, changes, h.cfg.Actor(c))
	return h.configSaveResult(c, name, changed, err)
}

// ValheimInstanceConfigReset pins a Setting back to the value the mod ships
// with.
//
// It WRITES the default rather than dropping the Override, because dropping one
// restores nothing: the file on the server keeps whatever value was last
// written to it. A reset that only forgot would visibly do nothing.
func (h *Handler) ValheimInstanceConfigReset(c *fiber.Ctx) error {
	svc, num, name, err := h.configRequest(c)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}
	inst, err := h.instanceFor(c, num)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}

	var req struct {
		File    string `json:"file"`
		SHA256  string `json:"sha256"`
		Section string `json:"section"`
		Setting string `json:"setting"`
	}
	_ = c.BodyParser(&req)
	if req.File != "" {
		name = strings.Clone(strings.TrimSpace(req.File))
	}
	if req.Section == "" || req.Setting == "" {
		return shared.SSEToast(c, "err", "Which setting?", nil)
	}

	changed, err := svc.ResetToDefault(c.UserContext(), *inst, name, req.SHA256,
		strings.Clone(req.Section), strings.Clone(req.Setting), h.cfg.Actor(c))
	return h.configSaveResult(c, name, changed, err)
}

// ValheimInstanceConfigDelete forgets every Override for a file. The server's
// own values are untouched; this only stops agrelha managing them.
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
	name := strings.Clone(strings.TrimSpace(req.File))
	if name == "" {
		name = strings.Clone(strings.TrimSpace(c.Query("file")))
	}
	if !valheimCfgNameRe.MatchString(name) {
		return shared.SSEToast(c, "err", "Invalid config file name.", nil)
	}

	changed, err := h.cfg.ValheimInstances.DeleteConfig(c.UserContext(), num, name, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Failed: "+err.Error(), nil)
	}
	if !changed {
		return shared.SSEToast(c, "ok", fmt.Sprintf("agrelha was not managing any values in %s.", name), nil)
	}
	return shared.SSEToast(c, "ok",
		fmt.Sprintf("Stopped managing %s. The server keeps its current values until something writes over them.", name), nil)
}

// ValheimInstanceConfigRaw edits the Override set as text, for a file agrelha
// could not parse.
func (h *Handler) ValheimInstanceConfigRaw(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}
	var req struct {
		File    string `json:"file"`
		Content string `json:"content"`
	}
	_ = c.BodyParser(&req)
	name := strings.Clone(strings.TrimSpace(req.File))
	if !valheimCfgNameRe.MatchString(name) {
		return shared.SSEToast(c, "err", "Invalid config file name.", nil)
	}

	// What is edited here is the Override set, never the generated file. Storing
	// a whole generated config would put up to 207 KB into a ConfigMap that caps
	// at 1 MiB, and make git claim to know settings it cannot.
	body := strings.Clone(strings.ReplaceAll(req.Content, "\r\n", "\n"))
	if strings.TrimSpace(body) == "" {
		// Emptying the box stops agrelha managing the file. It does not reset
		// anything: the server keeps the values it already has.
		changed, err := h.cfg.ValheimInstances.DeleteConfig(c.UserContext(), num, name, h.cfg.Actor(c))
		if err != nil {
			return shared.SSEToast(c, "err", "Failed: "+err.Error(), nil)
		}
		if !changed {
			return shared.SSEToast(c, "ok", fmt.Sprintf("agrelha was not managing any values in %s.", name), nil)
		}
		return shared.SSEToast(c, "ok",
			fmt.Sprintf("Stopped managing %s. The server keeps its current values until something writes over them.", name), nil)
	}
	changed, err := h.cfg.ValheimInstances.SaveConfig(c.UserContext(), num, name, body, h.cfg.Actor(c))
	return h.configSaveResult(c, name, changed, err)
}

func (h *Handler) configSaveResult(c *fiber.Ctx, name string, changed bool, err error) error {
	switch {
	case errors.Is(err, appbepinex.ErrStaleSnapshot):
		return shared.SSEToast(c, "err",
			"The server rewrote this config while you were editing. Reopen it and try again.", nil)
	case err != nil:
		return shared.SSEToast(c, "err", err.Error(), nil)
	case !changed:
		return shared.SSEToast(c, "ok", fmt.Sprintf("%s is already set that way.", name), nil)
	}
	// The change is in git but the running server has not read it: mods load
	// their config once, at startup. Queue the restart rather than forcing one,
	// so nobody is disconnected over a setting they did not ask about.
	h.requestRestart(c, name)
	return shared.SSEToast(c, "ok",
		fmt.Sprintf("Saved %s. It applies on the next restart.", name),
		map[string]any{"cfgDirty": map[string]any{}})
}

func (h *Handler) requestRestart(c *fiber.Ctx, reason string) {
	if h.cfg.ValheimRestarts == nil {
		return
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return
	}
	slug := ""
	if inst, err := h.instanceFor(c, num); err == nil {
		slug = inst.Slug
	}
	h.cfg.ValheimRestarts.Request(num, slug, "config change: "+reason)
}

// ValheimForceRestart restarts now, whoever is connected. It is the operator
// overruling the wait, so it does not consult occupancy.
func (h *Handler) ValheimForceRestart(c *fiber.Ctx) error {
	if h.cfg.ValheimRestarts == nil {
		return shared.SSEToast(c, "err", "Restart queue unavailable.", nil)
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}
	if err := h.cfg.ValheimRestarts.Force(c.UserContext(), num); err != nil {
		return shared.SSEToast(c, "err", "Restart failed: "+err.Error(), nil)
	}
	return shared.SSEToast(c, "ok", "Restarting now.", nil)
}

// decodeChanges turns the form's index-keyed values back into Settings.
//
// BepInEx keys contain spaces, dots and brackets ("Mining Yield Factor" under
// "[2 - Mining]"), none of which are valid in a Datastar signal name, so the
// form is keyed by the Setting's position in the file and resolved here against
// the same parse the form was rendered from.
func decodeChanges(view appbepinex.FileView, values map[string]string, forget []string) ([]appbepinex.Change, error) {
	all := view.Config.Settings()
	var out []appbepinex.Change

	resolve := func(key string) (domain.Setting, error) {
		i, err := strconv.Atoi(key)
		if err != nil || i < 0 || i >= len(all) {
			return domain.Setting{}, fmt.Errorf("this form is out of date; reopen the file")
		}
		return all[i], nil
	}

	for key, v := range values {
		s, err := resolve(key)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(s.Value) == strings.TrimSpace(v) && !view.Overrides.Has(s.Section, s.Name) {
			continue
		}
		out = append(out, appbepinex.Change{Section: s.Section, Name: s.Name, Value: v})
	}
	for _, key := range forget {
		s, err := resolve(key)
		if err != nil {
			return nil, err
		}
		out = append(out, appbepinex.Change{Section: s.Section, Name: s.Name, Forget: true})
	}
	return out, nil
}

// configRequest resolves the shared preconditions of every config endpoint.
func (h *Handler) configRequest(c *fiber.Ctx) (*appbepinex.Service, int, string, error) {
	// Validate the request before the dependency. A bad instance number or a
	// name trying to escape the config directory is wrong whatever agrelha is
	// configured with, and reporting "unavailable" for it hides a real answer.
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return nil, 0, "", errors.New("Invalid instance number.")
	}
	name := strings.Clone(strings.TrimSpace(c.Query("f")))
	if name == "" {
		name = strings.Clone(strings.TrimSpace(c.Query("file")))
	}
	if name != "" && !valheimCfgNameRe.MatchString(name) {
		return nil, 0, "", errors.New("Invalid config file name.")
	}
	if h.cfg.ValheimConfigs == nil {
		return nil, 0, "", errors.New("Mod configuration is unavailable: no backups directory is configured.")
	}
	return h.cfg.ValheimConfigs, num, name, nil
}

func (h *Handler) instanceFor(c *fiber.Ctx, num int) (*domain.Instance, error) {
	if h.cfg.ValheimInstances == nil {
		return nil, errors.New("Valheim instance manager unconfigured.")
	}
	inst, err := h.cfg.ValheimInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("instance %d not found", num)
	}
	return inst, nil
}

func configError(err error) string {
	return fmt.Sprintf(
		`<div class="rounded-xl border border-red-900/60 bg-red-950/30 p-4 text-xs text-red-300">%s</div>`,
		html.EscapeString(err.Error()))
}

// configTabState fills the Configs tab. Nothing published and no configs at all
// are shown differently: the first means the world has not run yet, the second
// means its mods expose no settings, and conflating them tells the operator
// something false.
func (h *Handler) configTabState(c *fiber.Ctx, d *pages.InstanceDetailUI, inst domain.Instance) {
	if p, ok := h.cfg.ValheimRestarts.Pending(inst.Number); ok {
		d.RestartPending = true
		d.RestartReason = p.Reason
		d.RestartWaiting = p.Blocked
		d.RestartSince = domain.FormatDuration(time.Since(p.Since))
	}

	if h.cfg.ValheimConfigs == nil {
		d.ConfigsError = "Mod configuration is unavailable: no backups directory is configured."
		return
	}
	files, snap, err := h.cfg.ValheimConfigs.Files(c.UserContext(), inst)
	switch {
	case errors.Is(err, appbepinex.ErrNotPublished):
		d.ConfigsUnpublished = true
		return
	case err != nil:
		d.ConfigsError = "Could not read the published configs: " + err.Error()
		return
	}
	if snap != nil && !snap.PublishedAt.IsZero() {
		d.ConfigsPublished = domain.FormatDuration(time.Since(snap.PublishedAt)) + " ago"
	}
	for _, f := range files {
		d.Configs = append(d.Configs, pages.ConfigFileUI{
			Name:       f.Name,
			Plugin:     f.PluginName,
			Version:    f.PluginVersion,
			Settings:   f.Settings,
			Changed:    f.Changed,
			Overridden: f.Overridden,
			Size:       f.Size,
			Parsable:   f.Parsable,
		})
	}
}

func (h *Handler) ValheimInstanceConfigImportPreview(c *fiber.Ctx) error {
	svc, num, _, err := h.configRequest(c)
	if err != nil {
		return ssePatchElements(c, "#cfg-import-preview", configError(err))
	}
	inst, err := h.instanceFor(c, num)
	if err != nil {
		return ssePatchElements(c, "#cfg-import-preview", configError(err))
	}

	var req struct {
		File    string `json:"file"`
		Content string `json:"content"`
	}
	_ = c.BodyParser(&req)
	body := strings.Clone(strings.ReplaceAll(req.Content, "\r\n", "\n"))
	if strings.TrimSpace(body) == "" {
		return ssePatchElements(c, "#cfg-import-preview", "")
	}

	name := strings.Clone(strings.TrimSpace(req.File))
	if name == "" {
		name, err = svc.MatchFile(c.UserContext(), *inst, body)
		if err != nil {
			return ssePatchElements(c, "#cfg-import-preview", configError(err))
		}
	}
	if !valheimCfgNameRe.MatchString(name) {
		return ssePatchElements(c, "#cfg-import-preview", configError(errors.New("Invalid config file name.")))
	}

	plan, view, err := svc.PlanImport(c.UserContext(), *inst, name, body)
	if err != nil {
		return ssePatchElements(c, "#cfg-import-preview", configError(err))
	}
	return ssePatchElements(c, "#cfg-import-preview", renderImportPreview(num, name, view, plan))
}

func (h *Handler) ValheimInstanceConfigImportApply(c *fiber.Ctx) error {
	svc, num, _, err := h.configRequest(c)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}
	inst, err := h.instanceFor(c, num)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}

	var req struct {
		File    string `json:"file"`
		SHA256  string `json:"sha256"`
		Content string `json:"content"`
	}
	_ = c.BodyParser(&req)
	body := strings.Clone(strings.ReplaceAll(req.Content, "\r\n", "\n"))
	name := strings.Clone(strings.TrimSpace(req.File))
	if name == "" {
		name, err = svc.MatchFile(c.UserContext(), *inst, body)
		if err != nil {
			return shared.SSEToast(c, "err", err.Error(), nil)
		}
	}
	if !valheimCfgNameRe.MatchString(name) {
		return shared.SSEToast(c, "err", "Invalid config file name.", nil)
	}

	n, changed, err := svc.ApplyImport(c.UserContext(), *inst, name, req.SHA256, body, h.cfg.Actor(c))
	switch {
	case errors.Is(err, appbepinex.ErrStaleSnapshot):
		return shared.SSEToast(c, "err", "The server rewrote this config while you were importing. Reopen it and try again.", nil)
	case err != nil:
		return shared.SSEToast(c, "err", err.Error(), nil)
	case n == 0:
		return shared.SSEToast(c, "ok", fmt.Sprintf("Nothing to import into %s: every value matches what the mod already uses.", name), nil)
	case !changed:
		return shared.SSEToast(c, "ok", fmt.Sprintf("%s is already set that way.", name), nil)
	}

	h.requestRestart(c, "imported "+name)
	return shared.SSEToast(c, "ok",
		fmt.Sprintf("Imported %d value(s) into %s. Applies on the next restart.", n, name),
		map[string]any{"cfgImport": "", "cfgImportOpen": false})
}

func (h *Handler) ValheimInstanceConfigImportOpen(c *fiber.Ctx) error {
	_, num, _, err := h.configRequest(c)
	if err != nil {
		return ssePatchElements(c, "#cfg-panel", configError(err))
	}
	return ssePatchElements(c, "#cfg-panel", renderImportPanel(num))
}
