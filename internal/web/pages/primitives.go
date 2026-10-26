package pages

import "agrelha/internal/web/components"

type tierOptionUI struct {
	Value string
	Label string
}

func valheimTiers() []tierOptionUI {
	return []tierOptionUI{
		{"small", "Small (4 GiB) - Up to 4 players"},
		{"medium", "Medium (6 GiB) - Standard (Recommended)"},
		{"large", "Large (8 GiB) - Heavy modded"},
	}
}

func instanceVariant(kind string) components.Variant {
	if kind == "quiet" {
		return components.VariantSecondary
	}
	return components.VariantPrimary
}

func managerVariant(kind string) components.Variant {
	switch kind {
	case "danger":
		return components.VariantDanger
	case "blocked":
		return components.VariantDisabled
	default:
		return components.VariantPrimary
	}
}

func primaryVariant(primary bool) components.Variant {
	if primary {
		return components.VariantPrimary
	}
	return components.VariantSecondary
}

func isNavAdmin(flags []bool) bool {
	return len(flags) > 0 && flags[0]
}
