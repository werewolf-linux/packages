// Package overlay applies reviewed changes to a captured Melange recipe.
// Package policy is data: edits select a field, check its old value, and change it.
package overlay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/werewolf-linux/packages/internal/transform"
	"gopkg.in/yaml.v3"
)

// Spec describes one package's changes to its Wolfi recipe.
type Spec struct {
	Title                 string                   `yaml:"title"`
	Sources               []string                 `yaml:"sources"`
	EpochMultiplier       int                      `yaml:"epoch-multiplier,omitempty"`
	StreamParts           int                      `yaml:"stream-parts,omitempty"`
	DescriptionSuffix     string                   `yaml:"description-suffix,omitempty"`
	Patches               string                   `yaml:"patches,omitempty"`
	UpstreamPatchPrefix   string                   `yaml:"upstream-patch-prefix,omitempty"`
	UpstreamPatches       map[string]UpstreamPatch `yaml:"upstream-patches,omitempty"`
	PatchWorkingDirectory string                   `yaml:"patch-working-directory,omitempty"`
	Context               map[string]string        `yaml:"context,omitempty"`
	Prepared              map[string]string        `yaml:"prepared,omitempty"`
	ContextEdits          []transform.File         `yaml:"context-edits,omitempty"`
	PreparedEdits         []transform.File         `yaml:"prepared-edits,omitempty"`
	MinKernel             string                   `yaml:"min-kernel,omitempty"`
	TestUnpinned          []string                 `yaml:"test-unpinned,omitempty"`
	TestPackages          []string                 `yaml:"test-packages,omitempty"`
	OriginMainOnly        bool                     `yaml:"origin-main-only,omitempty"`
	StableProvides        string                   `yaml:"stable-provides,omitempty"`
	ResultsSuffix         string                   `yaml:"results-suffix,omitempty"`
	TestSetup             yaml.Node                `yaml:"test-setup,omitempty"`
	SuiteSetup            yaml.Node                `yaml:"suite-setup,omitempty"`
	Checks                []Edit                   `yaml:"checks"`
	ReleaseEdits          []Edit                   `yaml:"release-edits,omitempty"`
	Edits                 []Edit                   `yaml:"edits"`
	TestEdits             []Edit                   `yaml:"test-edits,omitempty"`
}

// UpstreamPatch refreshes a captured Wolfi patch after checking its identity.
type UpstreamPatch struct {
	SHA256 string `yaml:"sha256"`
	Source string `yaml:"source"`
}

// Edit names a mapping key, sequence index, or unique field=value selector at
// each level of Path. Expect and Absent guard replacements and deletions.
type Edit struct {
	Path    []string      `yaml:"path"`
	Expect  yaml.Node     `yaml:"expect,omitempty"`
	Absent  bool          `yaml:"absent,omitempty"`
	Exists  bool          `yaml:"exists,omitempty"`
	Set     yaml.Node     `yaml:"set,omitempty"`
	Delete  bool          `yaml:"delete,omitempty"`
	Append  []yaml.Node   `yaml:"append,omitempty"`
	Prepend []yaml.Node   `yaml:"prepend,omitempty"`
	Remove  []yaml.Node   `yaml:"remove,omitempty"`
	Replace []Replacement `yaml:"replace,omitempty"`
	Before  string        `yaml:"before,omitempty"`
	After   string        `yaml:"after,omitempty"`
	Shape   []string      `yaml:"shape,omitempty"`
	Join    *Join         `yaml:"join,omitempty"`
}

// Join keeps related shell steps in one runner PID namespace.
type Join struct {
	From   string `yaml:"from"`
	Name   string `yaml:"name"`
	Prefix string `yaml:"prefix,omitempty"`
}

// Replacement changes exactly Count occurrences of literal text. Count defaults
// to one; a changed upstream command fails instead of being partially edited.
type Replacement struct {
	Old   string `yaml:"old"`
	New   string `yaml:"new"`
	Count int    `yaml:"count,omitempty"`
}

// Read rejects misspelled fields and multiple YAML documents.
func Read(path string) (*Spec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var spec Spec
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s: expected one YAML document", path)
	}
	if spec.Title == "" || len(spec.Sources) == 0 {
		return nil, fmt.Errorf("%s: title and sources are required", path)
	}
	if spec.EpochMultiplier == 0 {
		spec.EpochMultiplier = 1000
	}
	if spec.EpochMultiplier != 1 && spec.EpochMultiplier != 1000 {
		return nil, fmt.Errorf("%s: epoch-multiplier must be 1 or 1000", path)
	}
	return &spec, nil
}

// Parse decodes one recipe while retaining scalar types and field order.
func Parse(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("recipe must be a mapping")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("expected one recipe document")
	}
	if err := plain(doc.Content[0]); err != nil {
		return nil, err
	}
	return doc.Content[0], nil
}

func plain(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return errors.New("YAML aliases and anchors are not supported")
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || seen[key.Value] {
				return fmt.Errorf("invalid or duplicate YAML key %q", key.Value)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := plain(child); err != nil {
			return err
		}
	}
	return nil
}

// Clone makes a recipe safe to edit without changing its captured baseline.
func Clone(n *yaml.Node) *yaml.Node {
	copy := *n
	copy.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		copy.Content[i] = Clone(child)
	}
	return &copy
}

// Equal compares YAML values without comments, quoting, or source locations.
func Equal(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value || len(a.Content) != len(b.Content) {
		return false
	}
	if a.Kind == yaml.MappingNode {
		for i := 0; i < len(a.Content); i += 2 {
			if !Equal(a.Content[i+1], Field(b, a.Content[i].Value)) {
				return false
			}
		}
		return true
	}
	for i := range a.Content {
		if !Equal(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

// Field returns a mapping value or nil if the key is absent.
func Field(n *yaml.Node, key string) *yaml.Node {
	if n != nil && n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				return n.Content[i+1]
			}
		}
	}
	return nil
}

// Value returns a scalar mapping value, or the empty string if absent.
func Value(n *yaml.Node, key string) string {
	if f := Field(n, key); f != nil {
		return f.Value
	}
	return ""
}

// Put adds or replaces a mapping value.
func Put(n *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1] = value
			return
		}
	}
	n.Content = append(n.Content, String(key), value)
}

// String constructs a YAML string scalar.
func String(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

// Node encodes a typed Go value as YAML.
func Node(value any) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(value); err != nil {
		return nil, err
	}
	return &n, nil
}

// Select resolves a path. Selectors must identify exactly one sequence item.
func Select(n *yaml.Node, path []string) (*yaml.Node, error) {
	for _, key := range path {
		index, err := position(n, key)
		if err != nil {
			return nil, err
		}
		if index < 0 {
			return nil, fmt.Errorf("missing field %q", key)
		}
		n = n.Content[index]
	}
	return n, nil
}

func position(n *yaml.Node, key string) (int, error) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				return i + 1, nil
			}
		}
		return -1, nil
	case yaml.SequenceNode:
		field, value, selector := strings.Cut(key, "=")
		if !selector {
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(n.Content) {
				return -1, fmt.Errorf("invalid sequence index %q", key)
			}
			return i, nil
		}
		found := -1
		for i, child := range n.Content {
			if f := Field(child, field); f != nil && f.Value == value {
				if found >= 0 {
					return -1, fmt.Errorf("ambiguous selector %q", key)
				}
				found = i
			}
		}
		return found, nil
	default:
		return -1, fmt.Errorf("cannot select %q from a scalar", key)
	}
}

var variable = regexp.MustCompile(`\$\{([^{}]+)\}`)

// Expand substitutes explicit variables and package-relative file contents.
// Melange's ${{...}} expressions are left for Melange to evaluate.
func Expand(n *yaml.Node, directory string, vars map[string]string) error {
	if err := plain(n); err != nil {
		return err
	}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		var failure error
		n.Value = variable.ReplaceAllStringFunc(n.Value, func(token string) string {
			key := token[2 : len(token)-1]
			kind, path, include := strings.Cut(key, ":")
			if include && (kind == "file" || kind == "template") {
				if !filepath.IsLocal(path) {
					failure = fmt.Errorf("unsafe include %q", path)
					return token
				}
				base, err := filepath.EvalSymlinks(directory)
				if err != nil {
					failure = err
					return token
				}
				resolved, err := filepath.EvalSymlinks(filepath.Join(base, path))
				if err != nil {
					failure = err
					return token
				}
				rel, err := filepath.Rel(base, resolved)
				if err != nil || !filepath.IsLocal(rel) {
					failure = fmt.Errorf("include escapes package: %s", path)
					return token
				}
				data, err := os.ReadFile(resolved)
				if err != nil {
					failure = err
					return token
				}
				if kind == "file" && strings.Contains(string(data), "${{") {
					failure = fmt.Errorf("include contains Melange interpolation: %s", path)
					return token
				}
				return string(data)
			}
			if value, ok := vars[key]; ok {
				return value
			}
			// Ordinary shell ${...} expressions belong to the recipe.
			if strings.HasPrefix(key, "lock.") || strings.HasPrefix(key, "baseline.") || strings.HasPrefix(key, "upstream.") || strings.HasPrefix(key, "policy.") || strings.HasPrefix(key, "test.") || strings.HasPrefix(key, "shared:") {
				failure = fmt.Errorf("unknown overlay variable %q", key)
			}
			return token
		})
		if failure != nil {
			return failure
		}
	}
	for _, child := range n.Content {
		if err := Expand(child, directory, vars); err != nil {
			return err
		}
	}
	return nil
}

// Apply executes ordered, checked edits. It returns a new recipe; a failure
// leaves the input untouched and reports the package policy path that drifted.
func Apply(input *yaml.Node, edits []Edit, directory string, vars map[string]string) (*yaml.Node, error) {
	result := Clone(input)
	for i, source := range edits {
		edit := source
		edit.Path = slices.Clone(source.Path)
		edit.Shape = slices.Clone(source.Shape)
		edit.Replace = slices.Clone(source.Replace)
		edit.Append = slices.Clone(source.Append)
		edit.Prepend = slices.Clone(source.Prepend)
		edit.Remove = slices.Clone(source.Remove)
		edit.Expect = *Clone(&source.Expect)
		edit.Set = *Clone(&source.Set)
		values := []*string{&edit.Before, &edit.After}
		for j := range edit.Path {
			values = append(values, &edit.Path[j])
		}
		for j := range edit.Shape {
			values = append(values, &edit.Shape[j])
		}
		for j := range edit.Replace {
			values = append(values, &edit.Replace[j].Old, &edit.Replace[j].New)
		}
		for _, value := range values {
			n := String(*value)
			if err := Expand(n, directory, vars); err != nil {
				return nil, fmt.Errorf("edit %d: %w", i+1, err)
			}
			*value = n.Value
		}
		nodes := []*yaml.Node{&edit.Expect, &edit.Set}
		if source.Join != nil {
			join, err := Node(source.Join)
			if err != nil {
				return nil, err
			}
			if err := Expand(join, directory, vars); err != nil {
				return nil, err
			}
			edit.Join = &Join{}
			if err := join.Decode(edit.Join); err != nil {
				return nil, err
			}
		}
		for _, list := range [][]yaml.Node{edit.Append, edit.Prepend, edit.Remove} {
			for j := range list {
				list[j] = *Clone(&list[j])
				nodes = append(nodes, &list[j])
			}
		}
		for _, n := range nodes {
			if err := Expand(n, directory, vars); err != nil {
				return nil, fmt.Errorf("edit %d: %w", i+1, err)
			}
		}
		if err := apply(result, edit); err != nil {
			return nil, fmt.Errorf("edit %d at %v: %w", i+1, edit.Path, err)
		}
	}
	return result, nil
}

func apply(root *yaml.Node, e Edit) error {
	if len(e.Path) == 0 {
		return errors.New("edit needs a path")
	}
	parent, err := Select(root, e.Path[:len(e.Path)-1])
	if err != nil {
		return err
	}
	key := e.Path[len(e.Path)-1]
	i, err := position(parent, key)
	if err != nil {
		return err
	}
	var target *yaml.Node
	if i >= 0 {
		target = parent.Content[i]
	}
	if e.Absent && target != nil {
		return errors.New("expected an absent field")
	}
	if e.Exists && target == nil {
		return errors.New("expected an existing field")
	}
	if e.Expect.Kind != 0 && !Equal(target, &e.Expect) {
		return errors.New("upstream value changed; review the overlay")
	}
	if e.Set.Kind != 0 || e.Delete {
		if e.Expect.Kind == 0 && !e.Absent && !e.Exists && !(e.Delete && strings.Contains(key, "=")) {
			return errors.New("set/delete requires expect, absent, or a unique selector")
		}
		if e.Delete {
			if target == nil {
				return errors.New("cannot delete an absent field")
			}
			start := i
			if parent.Kind == yaml.MappingNode {
				start--
			}
			parent.Content = slices.Delete(parent.Content, start, i+1)
		} else if i >= 0 {
			parent.Content[i] = Clone(&e.Set)
		} else if parent.Kind == yaml.MappingNode {
			Put(parent, key, Clone(&e.Set))
		} else {
			return errors.New("cannot add an absent sequence item")
		}
		return nil
	}
	if target == nil {
		return errors.New("missing edit target")
	}
	if e.Shape != nil {
		if target.Kind != yaml.SequenceNode {
			return errors.New("shape needs a sequence")
		}
		var shape []string
		for _, child := range target.Content {
			id := "runs"
			if name := Value(child, "name"); name != "" {
				id = "name=" + name
			} else if uses := Value(child, "uses"); uses != "" {
				id = "uses=" + uses
			}
			shape = append(shape, id)
		}
		if !slices.Equal(shape, e.Shape) {
			return fmt.Errorf("upstream sequence changed: %v", shape)
		}
	}
	for _, r := range e.Replace {
		if target.Kind != yaml.ScalarNode || target.Tag != "!!str" || r.Old == "" {
			return errors.New("replace needs a string and nonempty old text")
		}
		count := r.Count
		if count == 0 {
			count = 1
		}
		if strings.Count(target.Value, r.Old) != count {
			return fmt.Errorf("expected %d occurrences of %q", count, r.Old)
		}
		target.Value = strings.ReplaceAll(target.Value, r.Old, r.New)
	}
	if e.Join != nil {
		return joinRuns(target, *e.Join)
	}
	if len(e.Append)+len(e.Prepend)+len(e.Remove) == 0 {
		return nil
	}
	if target.Kind != yaml.SequenceNode {
		return errors.New("append/prepend/remove needs a sequence")
	}
	for _, value := range e.Remove {
		found := -1
		for j, child := range target.Content {
			if Equal(child, &value) {
				if found >= 0 {
					return errors.New("remove matched multiple items")
				}
				found = j
			}
		}
		if found < 0 {
			return errors.New("remove did not match an item")
		}
		target.Content = slices.Delete(target.Content, found, found+1)
	}
	at := len(target.Content)
	if e.Before != "" || e.After != "" {
		selector := e.Before
		if e.After != "" {
			selector = e.After
		}
		at, err = position(target, selector)
		if err != nil {
			return err
		}
		if at < 0 {
			return fmt.Errorf("missing insertion point %q", selector)
		}
		if e.After != "" {
			at++
		}
	}
	for j := range e.Append {
		target.Content = slices.Insert(target.Content, at+j, Clone(&e.Append[j]))
	}
	for j := len(e.Prepend) - 1; j >= 0; j-- {
		target.Content = slices.Insert(target.Content, 0, Clone(&e.Prepend[j]))
	}
	return nil
}

func joinRuns(pipeline *yaml.Node, join Join) error {
	if pipeline.Kind != yaml.SequenceNode || join.Name == "" || !strings.HasPrefix(join.From, "name=") {
		return errors.New("join requires a pipeline, a name, and a name= selector")
	}
	start, err := position(pipeline, join.From)
	if err != nil {
		return err
	}
	if start < 0 {
		return fmt.Errorf("missing lifecycle start %q", join.From)
	}
	packages := map[string]bool{}
	runs := join.Prefix
	for _, step := range pipeline.Content[start:] {
		if step.Kind != yaml.MappingNode || Field(step, "runs") == nil {
			return errors.New("review changed lifecycle tests: expected shell steps")
		}
		for i := 0; i < len(step.Content); i += 2 {
			if !slices.Contains([]string{"name", "runs", "needs"}, step.Content[i].Value) {
				return errors.New("review changed lifecycle test fields")
			}
		}
		if needs := Field(Field(step, "needs"), "packages"); needs != nil {
			for _, pkg := range needs.Content {
				packages[pkg.Value] = true
			}
		}
		runs += "# " + Value(step, "name") + "\n" + Value(step, "runs") + "\n"
	}
	names := make([]string, 0, len(packages))
	for pkg := range packages {
		names = append(names, pkg)
	}
	slices.Sort(names)
	step, err := Node(map[string]any{"name": join.Name, "needs": map[string]any{"packages": names}, "runs": runs})
	if err != nil {
		return err
	}
	pipeline.Content = append(pipeline.Content[:start], step)
	return nil
}

// EditFiles expands locked version variables before applying guarded file edits.
func EditFiles(edits []transform.File, root, directory string, vars map[string]string) error {
	if len(edits) == 0 {
		return nil
	}
	node, err := Node(edits)
	if err != nil {
		return err
	}
	if err := Expand(node, directory, vars); err != nil {
		return err
	}
	var expanded []transform.File
	if err := node.Decode(&expanded); err != nil {
		return err
	}
	return transform.Apply(transform.Plan{Actions: map[string]transform.Action{"files": {Files: expanded}}}, "files", root)
}
