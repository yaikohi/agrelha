package arch

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

const (
	wiringFile  = "internal/wiring/wiring.go"
	managerFile = "internal/app/instances/manager.go"

	hostScoped     = "host-scoped"
	clusterOnly    = "cluster-only"
	notPerInstance = "not-per-instance"
)

type hostCapability struct {
	field  string
	scope  string
	reason string
}

var clusterBackedCapabilities = map[string]hostCapability{
	"WithModsReader": {
		field: "modsReader",
		scope: hostScoped,
	},
	"WithCurseForgeReader": {
		field: "cfModsReader",
		scope: hostScoped,
	},
	"WithConfigsReader": {
		field: "configsReader",
		scope: hostScoped,
	},
	"WithGlobalConfigsReader": {
		scope:  notPerInstance,
		reason: "global configs belong to the deployment, not to an Instance, so no Instance Host can select a source for them",
	},
	"WithJobRunner": {
		scope:  clusterOnly,
		reason: "ports.JobRunner has no Compose implementation; a Docker Host has no backup Job path and uses the host's own timer units instead",
	},
	"WithSourceReconciler": {
		field: "sourceReconciler",
		scope: hostScoped,
	},
}

func TestEveryClusterBackedCapabilityIsClassified(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	found, err := clusterBackedInstalls(filepath.Join(root, wiringFile))
	if err != nil {
		t.Fatalf("parse %s: %v", wiringFile, err)
	}
	if len(found) == 0 {
		t.Fatalf("no capability installs found under a Kubernetes guard in %s; has the composition root changed shape?", wiringFile)
	}

	for _, name := range found {
		cap, ok := clusterBackedCapabilities[name]
		if !ok {
			t.Errorf("%s installs %s behind a Kubernetes client with no entry in clusterBackedCapabilities;\n"+
				"a capability wired on \"the process has a cluster\" answers for every Instance, including one on a Docker Host,\n"+
				"and a stale cluster read is indistinguishable from a correct one.\n"+
				"classify it as %q (the manager gates it on the Instance Host), %q or %q, with a reason.",
				wiringFile, name, hostScoped, clusterOnly, notPerInstance)
			continue
		}
		switch cap.scope {
		case hostScoped:
			if cap.field == "" {
				t.Errorf("%s is classified %q but names no InstanceManager field, so nothing can check it is gated", name, hostScoped)
			}
		case clusterOnly, notPerInstance:
			if cap.reason == "" {
				t.Errorf("%s is classified %q with no reason; say why a Docker Host world does without it", name, cap.scope)
			}
		default:
			t.Errorf("%s has scope %q, which is not one of %q, %q, %q", name, cap.scope, hostScoped, clusterOnly, notPerInstance)
		}
	}

	installed := map[string]bool{}
	for _, name := range found {
		installed[name] = true
	}
	for name := range clusterBackedCapabilities {
		if !installed[name] {
			t.Errorf("clusterBackedCapabilities has an entry for %s, which %s no longer installs behind a Kubernetes client; delete the entry", name, wiringFile)
		}
	}
}

func TestEveryHostScopedCapabilityIsGatedOnTheInstanceHost(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(root, managerFile), nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", managerFile, err)
	}

	gated := map[string]bool{}
	for _, cap := range clusterBackedCapabilities {
		if cap.scope == hostScoped && cap.field != "" {
			gated[cap.field] = true
		}
	}

	var ungated []string
	ast.Inspect(f, func(n ast.Node) bool {
		cond := conditionOf(n)
		if cond == nil {
			return true
		}
		src := nodeSource(fset, cond)
		for field := range gated {
			if !strings.Contains(src, "m."+field+" != nil") {
				continue
			}
			if strings.Contains(src, "readsFromCluster(") || strings.Contains(src, "IsCluster()") {
				continue
			}
			ungated = append(ungated, fset.Position(cond.Pos()).String()+": "+src)
		}
		return true
	})

	sort.Strings(ungated)
	for _, site := range ungated {
		t.Errorf("%s consults a host-scoped reader without gating on the Instance Host:\n  %s\n"+
			"a Docker Host world materialises its Mod list and configs through the state store;\n"+
			"answering from the cluster there returns whatever ArgoCD last synced, which for a migrated world is frozen forever.\n"+
			"add \"&& m.readsFromCluster(instanceNumber)\", or \"&& inst.EffectiveHost().IsCluster()\" where an Instance is already in hand.",
			managerFile, site)
	}

	if len(gated) == 0 {
		t.Fatal("no host-scoped capabilities in clusterBackedCapabilities; this ratchet is asserting nothing")
	}
}

func conditionOf(n ast.Node) ast.Expr {
	switch s := n.(type) {
	case *ast.IfStmt:
		return s.Cond
	case *ast.CaseClause:
		if len(s.List) == 1 {
			return s.List[0]
		}
	}
	return nil
}

func nodeSource(fset *token.FileSet, n ast.Node) string {
	var sb strings.Builder
	if err := printer.Fprint(&sb, fset, n); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

func clusterBackedInstalls(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		for _, body := range clusterBackedBlocks(n) {
			for _, name := range capabilityNames(body) {
				seen[name] = true
			}
		}
		return true
	})

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func clusterBackedBlocks(n ast.Node) []*ast.BlockStmt {
	var out []*ast.BlockStmt
	if block, ok := n.(*ast.BlockStmt); ok && assignsClusterClient(block) {
		out = append(out, block)
	}
	ifs, ok := n.(*ast.IfStmt)
	if !ok {
		return out
	}
	if !mentionsClusterClient(ifs.Cond) {
		return out
	}
	out = append(out, ifs.Body)
	if elseBlock, ok := ifs.Else.(*ast.BlockStmt); ok {
		out = append(out, elseBlock)
	}
	return out
}

func assignsClusterClient(block *ast.BlockStmt) bool {
	for _, stmt := range block.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}
		if slices.ContainsFunc(assign.Lhs, isClusterClient) {
			return true
		}
	}
	return false
}

func mentionsClusterClient(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if expr, ok := n.(ast.Expr); ok && isClusterClient(expr) {
			found = true
		}
		return !found
	})
	return found
}

func isClusterClient(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != "d" {
		return false
	}
	return sel.Sel.Name == "K8s" || sel.Sel.Name == "MCK8s"
}

func capabilityNames(block *ast.BlockStmt) []string {
	var out []string
	ast.Inspect(block, func(n ast.Node) bool {
		if goStmt, ok := n.(*ast.GoStmt); ok {
			if name := selectorName(goStmt.Call.Fun); name != "" {
				out = append(out, name)
			}
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !strings.HasPrefix(sel.Sel.Name, "With") {
			return true
		}
		if _, ok := sel.X.(*ast.Ident); !ok {
			return true
		}
		out = append(out, sel.Sel.Name)
		return true
	})
	return out
}

func selectorName(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name + "." + sel.Sel.Name
}
