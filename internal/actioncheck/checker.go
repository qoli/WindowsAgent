// Package actioncheck validates Rule-owned Action packages and their static
// child-Action dependency graph without involving the WindowsAgent runtime.
package actioncheck

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qoli/WindowsAgent/internal/inputaction"
	"github.com/qoli/WindowsAgent/internal/ocraction"
	"github.com/qoli/WindowsAgent/internal/ocrregionsaction"
	"github.com/qoli/WindowsAgent/internal/pointeraction"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"github.com/qoli/WindowsAgent/internal/streamaction"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

const SchemaVersion = 1

const (
	CodeRuleLoadFailed    = "RULE_LOAD_FAILED"
	CodePackageLoadFailed = "PACKAGE_LOAD_FAILED"
	CodeStarlarkInvalid   = "STARLARK_INVALID"
	CodeDynamicActionID   = "DYNAMIC_ACTION_ID"
	CodeIndirectActionUse = "INDIRECT_ACTION_USE"
	CodeMissingAction     = "MISSING_ACTION"
	CodeCrossRuleAction   = "CROSS_RULE_ACTION"
	CodeStreamingChild    = "STREAMING_CHILD_ACTION"
	CodeSelfDependency    = "SELF_DEPENDENCY"
	CodeDependencyCycle   = "DEPENDENCY_CYCLE"
)

// Result is the stable machine-readable validation report.
type Result struct {
	SchemaVersion   int     `json:"schemaVersion"`
	Valid           bool    `json:"valid"`
	RuleCount       int     `json:"ruleCount"`
	ActionCount     int     `json:"actionCount"`
	DependencyCount int     `json:"dependencyCount"`
	Issues          []Issue `json:"issues"`
}

// Issue describes one independently actionable validation failure.
type Issue struct {
	Code       string   `json:"code"`
	RuleID     string   `json:"ruleId,omitempty"`
	ActionID   string   `json:"actionId,omitempty"`
	Path       string   `json:"path,omitempty"`
	Line       int      `json:"line,omitempty"`
	Column     int      `json:"column,omitempty"`
	Primitive  string   `json:"primitive,omitempty"`
	Dependency string   `json:"dependency,omitempty"`
	Chain      []string `json:"chain,omitempty"`
	Message    string   `json:"message"`
}

type loadedAction struct {
	action     rules.Action
	entrypoint string
	script     []byte
}

type reference struct {
	caller     rules.Action
	primitive  string
	dependency string
	path       string
	line       int
	column     int
	dynamic    bool
	indirect   bool
}

// Check validates every Rule and Action below rulesRoot. The returned error is
// reserved for failures that prevent validation from starting; validation
// findings are returned in Result.Issues.
func Check(rulesRoot string) (Result, error) {
	result := Result{SchemaVersion: SchemaVersion, Issues: []Issue{}}
	store, err := rules.New(rulesRoot)
	if err != nil {
		return result, fmt.Errorf("initialize Rule store: %w", err)
	}
	ruleIDs, err := store.RuleIDs()
	if err != nil {
		return result, fmt.Errorf("enumerate Rules: %w", err)
	}
	result.RuleCount = len(ruleIDs)

	allActions := make(map[string]rules.Action)
	loaded := make([]loadedAction, 0)
	for _, ruleID := range ruleIDs {
		actions, _, readErr := store.ReadActions(ruleID)
		if readErr != nil {
			result.Issues = append(result.Issues, Issue{
				Code: CodeRuleLoadFailed, RuleID: ruleID,
				Message: fmt.Sprintf("load Rule Action catalog: %v", readErr),
			})
			continue
		}
		result.ActionCount += len(actions)
		for _, action := range actions {
			allActions[action.ID] = action
			entrypoint, script, loadErr := loadPackage(action)
			if loadErr != nil {
				result.Issues = append(result.Issues, Issue{
					Code: CodePackageLoadFailed, RuleID: action.RuleID, ActionID: action.ID,
					Path: relativePath(rulesRoot, action.Root), Message: loadErr.Error(),
				})
				continue
			}
			loaded = append(loaded, loadedAction{action: action, entrypoint: entrypoint, script: script})
		}
	}

	refs := make([]reference, 0)
	for _, item := range loaded {
		if len(item.script) == 0 {
			continue
		}
		path := filepath.Join(item.action.Root, filepath.FromSlash(item.entrypoint))
		reportPath := relativePath(rulesRoot, path)
		parsed, _, parseErr := starlark.SourceProgramOptions(
			&syntax.FileOptions{While: true},
			reportPath,
			item.script,
			func(name string) bool { return name == "action" || name == "stream" || name == "task" },
		)
		if parseErr != nil {
			result.Issues = append(result.Issues, Issue{
				Code: CodeStarlarkInvalid, RuleID: item.action.RuleID, ActionID: item.action.ID,
				Path: reportPath, Message: fmt.Sprintf("compile Starlark entrypoint: %v", parseErr),
			})
			continue
		}
		refs = append(refs, extractReferences(item.action, reportPath, parsed)...)
	}
	result.DependencyCount = len(refs)

	graph := make(map[string][]reference)
	for _, ref := range refs {
		if ref.indirect {
			result.Issues = append(result.Issues, issueForReference(ref, CodeIndirectActionUse,
				ref.primitive+" must be invoked directly and cannot be aliased or passed as a value"))
			continue
		}
		if ref.dynamic {
			result.Issues = append(result.Issues, issueForReference(ref, CodeDynamicActionID,
				ref.primitive+" id must be a static string literal"))
			continue
		}
		child, exists := allActions[ref.dependency]
		if !exists {
			result.Issues = append(result.Issues, issueForReference(ref, CodeMissingAction,
				fmt.Sprintf("referenced Action %q is not declared", ref.dependency)))
			continue
		}
		if !strings.EqualFold(child.RuleID, ref.caller.RuleID) {
			result.Issues = append(result.Issues, issueForReference(ref, CodeCrossRuleAction,
				fmt.Sprintf("referenced Action %q belongs to Rule %q", child.ID, child.RuleID)))
			continue
		}
		if child.Execution.Completion == rules.CompletionStream && (ref.caller.Execution.Completion != rules.CompletionStream ||
			(ref.primitive != "action.call" && ref.primitive != "action.try_call") ||
			child.Execution.Lifecycle != rules.LifecycleLinear ||
			!child.Execution.Interruptible) {
			result.Issues = append(result.Issues, issueForReference(ref, CodeStreamingChild,
				fmt.Sprintf("referenced streaming Action %q requires a streaming parent action.call/try_call and linear interruptible child", child.ID)))
			continue
		} else if child.Execution.Completion != rules.CompletionReturn && child.Execution.Completion != rules.CompletionStream {
			result.Issues = append(result.Issues, issueForReference(ref, CodeStreamingChild,
				fmt.Sprintf("referenced Action %q declares unsupported completion %q", child.ID, child.Execution.Completion)))
			continue
		}
		if child.ID == ref.caller.ID {
			result.Issues = append(result.Issues, issueForReference(ref, CodeSelfDependency,
				fmt.Sprintf("Action %q cannot call itself", child.ID)))
			continue
		}
		graph[ref.caller.ID] = append(graph[ref.caller.ID], ref)
	}
	result.Issues = append(result.Issues, cycleIssues(graph)...)
	sortIssues(result.Issues)
	result.Valid = len(result.Issues) == 0
	return result, nil
}

func loadPackage(action rules.Action) (entrypoint string, script []byte, err error) {
	switch action.Runtime {
	case rules.ObservationRuntimeV1:
		_, err = scriptpackage.Load(action.Root, action.ID)
	case rules.PpOcrActionRuntimeV1:
		_, err = ocraction.Load(action.Root)
	case rules.PpOcrTextRegionsActionRuntimeV1:
		_, err = ocrregionsaction.Load(action.Root)
	case rules.WindowsKeyActionRuntimeV1:
		_, err = inputaction.Load(action.Root)
	case rules.WindowsPointerActionRuntimeV1:
		_, err = pointeraction.Load(action.Root)
	case rules.CompositeActionRuntimeV1, rules.StreamingActionRuntimeV1:
		var pkg *streamaction.Package
		pkg, err = streamaction.Load(action.Root)
		if err == nil {
			entrypoint, script = pkg.Manifest.Entrypoint, pkg.Script
		}
	default:
		// Runtimes external to Core own their package contract. They cannot use
		// Core's Starlark action.* primitives and therefore add no dependency
		// edges for this checker.
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("load Action package: %w", err)
	}
	return entrypoint, script, nil
}

func extractReferences(action rules.Action, path string, file *syntax.File) []reference {
	refs := make([]reference, 0)
	directMembers := make(map[*syntax.DotExpr]struct{})
	memberModules := make(map[*syntax.Ident]struct{})
	walkSyntax(file, func(node syntax.Node) {
		if _, module, _, ok := actionMember(node); ok {
			memberModules[module] = struct{}{}
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return
		}
		dot, module, _, ok := actionMember(call.Fn)
		if !ok {
			return
		}
		directMembers[dot] = struct{}{}
		memberModules[module] = struct{}{}
	})
	walkSyntax(file, func(node syntax.Node) {
		if call, ok := node.(*syntax.CallExpr); ok {
			_, _, primitive, dependencyMember := actionMember(call.Fn)
			if !dependencyMember || primitive == "action.clear_on_failure" {
				return
			}
			value, position, found := actionIDArgument(call)
			ref := reference{caller: action, primitive: primitive, path: path, dynamic: true}
			if !found {
				position, _ = call.Span()
			} else if literal, ok := value.(*syntax.Literal); ok && literal.Token == syntax.STRING {
				ref.dependency, ref.dynamic = literal.Value.(string), false
			}
			ref.line, ref.column = int(position.Line), int(position.Col)
			refs = append(refs, ref)
			return
		}
		if dot, _, primitive, member := actionMember(node); member {
			if _, direct := directMembers[dot]; !direct {
				position, _ := dot.Span()
				refs = append(refs, reference{
					caller: action, primitive: primitive, path: path,
					line: int(position.Line), column: int(position.Col), indirect: true,
				})
			}
			return
		}
		ident, ok := node.(*syntax.Ident)
		if !ok || ident.Name != "action" {
			return
		}
		if _, member := memberModules[ident]; member {
			return
		}
		position, _ := ident.Span()
		refs = append(refs, reference{
			caller: action, primitive: "action", path: path,
			line: int(position.Line), column: int(position.Col), indirect: true,
		})
	})
	return refs
}

// walkSyntax is kept local because the upstream syntax.Walk in the pinned
// Starlark version does not handle WhileStmt even when While is enabled.
func walkSyntax(node syntax.Node, visit func(syntax.Node)) {
	if node == nil {
		return
	}
	visit(node)
	walkStatements := func(statements []syntax.Stmt) {
		for _, statement := range statements {
			walkSyntax(statement, visit)
		}
	}
	switch node := node.(type) {
	case *syntax.File:
		walkStatements(node.Stmts)
	case *syntax.ExprStmt:
		walkSyntax(node.X, visit)
	case *syntax.BranchStmt, *syntax.Ident, *syntax.Literal:
		return
	case *syntax.IfStmt:
		walkSyntax(node.Cond, visit)
		walkStatements(node.True)
		walkStatements(node.False)
	case *syntax.AssignStmt:
		walkSyntax(node.LHS, visit)
		walkSyntax(node.RHS, visit)
	case *syntax.DefStmt:
		walkSyntax(node.Name, visit)
		for _, parameter := range node.Params {
			walkSyntax(parameter, visit)
		}
		walkStatements(node.Body)
	case *syntax.ForStmt:
		walkSyntax(node.Vars, visit)
		walkSyntax(node.X, visit)
		walkStatements(node.Body)
	case *syntax.WhileStmt:
		walkSyntax(node.Cond, visit)
		walkStatements(node.Body)
	case *syntax.ReturnStmt:
		walkSyntax(node.Result, visit)
	case *syntax.LoadStmt:
		walkSyntax(node.Module, visit)
		for _, from := range node.From {
			walkSyntax(from, visit)
		}
		for _, to := range node.To {
			walkSyntax(to, visit)
		}
	case *syntax.ListExpr:
		for _, expression := range node.List {
			walkSyntax(expression, visit)
		}
	case *syntax.ParenExpr:
		walkSyntax(node.X, visit)
	case *syntax.CondExpr:
		walkSyntax(node.Cond, visit)
		walkSyntax(node.True, visit)
		walkSyntax(node.False, visit)
	case *syntax.IndexExpr:
		walkSyntax(node.X, visit)
		walkSyntax(node.Y, visit)
	case *syntax.DictEntry:
		walkSyntax(node.Key, visit)
		walkSyntax(node.Value, visit)
	case *syntax.SliceExpr:
		walkSyntax(node.X, visit)
		walkSyntax(node.Lo, visit)
		walkSyntax(node.Hi, visit)
		walkSyntax(node.Step, visit)
	case *syntax.Comprehension:
		walkSyntax(node.Body, visit)
		for _, clause := range node.Clauses {
			walkSyntax(clause, visit)
		}
	case *syntax.IfClause:
		walkSyntax(node.Cond, visit)
	case *syntax.ForClause:
		walkSyntax(node.Vars, visit)
		walkSyntax(node.X, visit)
	case *syntax.TupleExpr:
		for _, expression := range node.List {
			walkSyntax(expression, visit)
		}
	case *syntax.DictExpr:
		for _, entry := range node.List {
			walkSyntax(entry, visit)
		}
	case *syntax.UnaryExpr:
		walkSyntax(node.X, visit)
	case *syntax.BinaryExpr:
		walkSyntax(node.X, visit)
		walkSyntax(node.Y, visit)
	case *syntax.DotExpr:
		walkSyntax(node.X, visit)
		walkSyntax(node.Name, visit)
	case *syntax.CallExpr:
		walkSyntax(node.Fn, visit)
		for _, argument := range node.Args {
			walkSyntax(argument, visit)
		}
	case *syntax.LambdaExpr:
		for _, parameter := range node.Params {
			walkSyntax(parameter, visit)
		}
		walkSyntax(node.Body, visit)
	}
}

func actionMember(node syntax.Node) (*syntax.DotExpr, *syntax.Ident, string, bool) {
	expr, ok := node.(syntax.Expr)
	if !ok {
		return nil, nil, "", false
	}
	dot, ok := expr.(*syntax.DotExpr)
	if !ok {
		return nil, nil, "", false
	}
	module, ok := dot.X.(*syntax.Ident)
	if !ok || module.Name != "action" {
		return nil, nil, "", false
	}
	switch dot.Name.Name {
	case "call", "try_call", "on_failure", "clear_on_failure":
		return dot, module, "action." + dot.Name.Name, true
	default:
		return nil, nil, "", false
	}
}

func actionIDArgument(call *syntax.CallExpr) (syntax.Expr, syntax.Position, bool) {
	for _, arg := range call.Args {
		binary, ok := arg.(*syntax.BinaryExpr)
		if !ok || binary.Op != syntax.EQ {
			continue
		}
		name, ok := binary.X.(*syntax.Ident)
		if ok && name.Name == "id" {
			position, _ := binary.Y.Span()
			return binary.Y, position, true
		}
	}
	if len(call.Args) > 0 {
		if binary, ok := call.Args[0].(*syntax.BinaryExpr); !ok || binary.Op != syntax.EQ {
			position, _ := call.Args[0].Span()
			return call.Args[0], position, true
		}
	}
	position, _ := call.Span()
	return nil, position, false
}

func issueForReference(ref reference, code, message string) Issue {
	return Issue{
		Code: code, RuleID: ref.caller.RuleID, ActionID: ref.caller.ID,
		Path: ref.path, Line: ref.line, Column: ref.column,
		Primitive: ref.primitive, Dependency: ref.dependency, Message: message,
	}
}

func cycleIssues(graph map[string][]reference) []Issue {
	for caller := range graph {
		sort.Slice(graph[caller], func(i, j int) bool {
			return graph[caller][i].dependency < graph[caller][j].dependency
		})
	}
	nodes := make([]string, 0, len(graph))
	for node := range graph {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	state := make(map[string]uint8)
	stack := make([]string, 0)
	stackIndex := make(map[string]int)
	seen := make(map[string]struct{})
	issues := make([]Issue, 0)
	var visit func(string)
	visit = func(node string) {
		state[node] = 1
		stackIndex[node] = len(stack)
		stack = append(stack, node)
		for _, edge := range graph[node] {
			switch state[edge.dependency] {
			case 0:
				visit(edge.dependency)
			case 1:
				start := stackIndex[edge.dependency]
				cycle := append([]string(nil), stack[start:]...)
				signature := canonicalCycle(cycle)
				if _, exists := seen[signature]; exists {
					continue
				}
				seen[signature] = struct{}{}
				chain := append(append([]string(nil), cycle...), edge.dependency)
				issue := issueForReference(edge, CodeDependencyCycle, "Action dependency cycle: "+strings.Join(chain, " -> "))
				issue.Chain = chain
				issues = append(issues, issue)
			}
		}
		stack = stack[:len(stack)-1]
		delete(stackIndex, node)
		state[node] = 2
	}
	for _, node := range nodes {
		if state[node] == 0 {
			visit(node)
		}
	}
	return issues
}

func canonicalCycle(cycle []string) string {
	if len(cycle) == 0 {
		return ""
	}
	best := ""
	for index := range cycle {
		rotated := append(append([]string(nil), cycle[index:]...), cycle[:index]...)
		candidate := strings.Join(rotated, "\x00")
		if best == "" || candidate < best {
			best = candidate
		}
	}
	return best
}

func relativePath(root, name string) string {
	relative, err := filepath.Rel(root, name)
	if err != nil {
		return filepath.ToSlash(name)
	}
	return filepath.ToSlash(relative)
}

func sortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		left := fmt.Sprintf("%s\x00%s\x00%s\x00%09d\x00%09d\x00%s\x00%s",
			issues[i].RuleID, issues[i].ActionID, issues[i].Path, issues[i].Line, issues[i].Column, issues[i].Code, issues[i].Dependency)
		right := fmt.Sprintf("%s\x00%s\x00%s\x00%09d\x00%09d\x00%s\x00%s",
			issues[j].RuleID, issues[j].ActionID, issues[j].Path, issues[j].Line, issues[j].Column, issues[j].Code, issues[j].Dependency)
		return left < right
	})
}
