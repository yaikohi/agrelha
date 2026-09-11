package valheim

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/modpack"
	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

// ValheimWizardPage renders the Valheim creation wizard page.
func (h *Handler) ValheimWizardPage(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Valheim instance manager not configured")
	}

	instances, err := h.cfg.ValheimInstances.ListInstances(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("List instances failed: " + err.Error())
	}

	budget := h.cfg.ValheimInstances.Budget(instances)
	budgetUI := pages.BudgetUI{
		UsedGiB:        budget.UsedGiB,
		TotalBudgetGiB: budget.TotalBudgetGiB,
		RunningCount:   budget.RunningCount,
		MaxRunning:     budget.MaxRunning,
		TotalInstances: budget.TotalInstances,
		MaxInstances:   budget.MaxInstances,
	}

	return shared.Render(c, pages.ValheimWizard(budgetUI))
}

// ValheimWizardModsSearch queries Thunderstore for mod packages during wizard setup.
func (h *Handler) ValheimWizardModsSearch(c *fiber.Ctx) error {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" || h.cfg.TS == nil {
		return c.JSON([]any{})
	}

	results, err := h.cfg.TS.Search(c.UserContext(), q, 15)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	type hit struct {
		Name        string `json:"name"`
		Namespace   string `json:"namespace"`
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		IconURL     string `json:"icon_url"`
	}

	hits := make([]hit, 0, len(results))
	for _, r := range results {
		hits = append(hits, hit{
			Name:        r.Name,
			Namespace:   r.Owner,
			FullName:    fmt.Sprintf("%s-%s", r.Owner, r.Name),
			Description: r.Description,
			Version:     r.Version,
			IconURL:     r.Icon,
		})
	}

	return c.JSON(hits)
}

// ValheimWizardImport handles uploading an .r2z or export.r2x modpack profile.
func (h *Handler) ValheimWizardImport(c *fiber.Ctx) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return shared.SSEToast(c, "err", "File upload failed: "+err.Error(), map[string]any{
			"importError": err.Error(),
		})
	}

	f, err := fh.Open()
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to open uploaded file: "+err.Error(), map[string]any{
			"importError": err.Error(),
		})
	}
	defer f.Close()

	buf, err := io.ReadAll(f)
	if err != nil {
		return shared.SSEToast(c, "err", "Read upload failed: "+err.Error(), map[string]any{
			"importError": err.Error(),
		})
	}

	imported, err := modpack.ParseR2Z(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to parse modpack profile: "+err.Error(), map[string]any{
			"importError": err.Error(),
		})
	}

	signals := map[string]any{
		"raw_mods":    imported.RawMods,
		"source":      "scratch",
		"importError": "",
	}
	if imported.Name != "" {
		signals["name"] = imported.Name
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Imported %d mods from profile!", len(imported.Slugs)), signals)
}

// ValheimWizardCreate handles the final submission of the Valheim creation wizard.
func (h *Handler) ValheimWizardCreate(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	var req struct {
		Name     string   `json:"name" form:"name"`
		Password string   `json:"password" form:"password"`
		Seed     string   `json:"seed" form:"seed"`
		Tier     string   `json:"tier" form:"tier"`
		Source   string   `json:"source" form:"source"`
		RawMods  string   `json:"raw_mods" form:"raw_mods"`
		Mods     []string `json:"mods"`
	}
	_ = c.BodyParser(&req)

	if len(req.Mods) > 0 && req.RawMods == "" {
		req.RawMods = strings.Join(req.Mods, "\n")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(c.FormValue("name"))
	}
	if name == "" {
		return shared.SSEToast(c, "err", "World name is required.", nil)
	}

	password := strings.TrimSpace(req.Password)
	if password == "" {
		password = strings.TrimSpace(c.FormValue("password"))
	}
	if password != "" && len(password) < 5 {
		return shared.SSEToast(c, "err", "Valheim server password must be at least 5 characters.", nil)
	}

	seed := strings.TrimSpace(req.Seed)
	if seed == "" {
		seed = strings.TrimSpace(c.FormValue("seed"))
	}

	tierStr := strings.TrimSpace(req.Tier)
	if tierStr == "" {
		tierStr = strings.TrimSpace(c.FormValue("tier"))
	}
	tier := domain.NormalizeTier(tierStr)

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = strings.TrimSpace(c.FormValue("source"))
	}

	rawMods := req.RawMods
	if rawMods == "" {
		rawMods = c.FormValue("raw_mods")
	}

	var modsTxt string
	if source == "scratch" || source == "modpack" {
		var lines []string
		for line := range strings.SplitSeq(rawMods, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				lines = append(lines, line)
			}
		}
		modsTxt = strings.Join(lines, "\n")
	}

	inst := domain.Instance{
		GameID:   domain.GameValheim,
		Name:     name,
		Password: password,
		Seed:     seed,
		Tier:     tier,
		State:    domain.StateRunning,
	}

	created, err := h.cfg.ValheimInstances.CreateInstance(c.UserContext(), inst, modsTxt, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to create Valheim server: "+err.Error(), nil)
	}

	_ = h.cfg.ValheimInstances.StartInstance(c.UserContext(), created.Number, h.cfg.Actor(c))

	redirectURL := fmt.Sprintf("/valheim/%d", created.Number)
	return shared.SSEToast(c, "ok", fmt.Sprintf("Created Valheim server %q! Redirecting...", created.Name), map[string]any{
		"redirect": redirectURL,
	})
}
