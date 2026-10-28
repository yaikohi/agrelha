package pages

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderForAttrScan(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func TestNoEmptyDatastarAttributes(t *testing.T) {
	cases := map[string]templ.Component{
		"minecraft": MinecraftWizard([]string{"1.21.1"}, BudgetUI{}),
		"valheim":   ValheimWizard(BudgetUI{}),
		"gmod":      GModWizard(BudgetUI{}),
	}

	empty := regexp.MustCompile(`data-[a-z:_-]+=""`)
	valueless := regexp.MustCompile(`data-(bind|indicator|ref)[\s>]`)

	for name, c := range cases {
		html := renderForAttrScan(t, c)
		if m := empty.FindAllString(html, -1); len(m) > 0 {
			t.Errorf("%s: empty Datastar attributes %v\n"+
				"Datastar throws KeyOrValueRequired on these and stops processing the rest of the page, "+
				"which silently disables every data-show and data-on below it. "+
				"Guard the attribute with an if in the component instead of emitting it empty.", name, m)
		}
		if m := valueless.FindAllString(html, -1); len(m) > 0 {
			t.Errorf("%s: valueless exclusive-requirement attributes %v", name, m)
		}
	}
}
