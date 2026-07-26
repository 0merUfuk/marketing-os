package skills

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const vendoredUpstreamCommit = "67264763cb107d61749f418d081c56e5bcbc0209"

func TestVendoredDistributionContainsExactlyRequiredWorkflowSkills(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", "..", "third_party", "marketingskills"))
	loader := NewLoader(repo, filepath.Join("..", "..", "skills.lock.yaml"))
	status, err := loader.Status(context.Background())
	if err != nil {
		t.Fatalf("status vendored skills: %v", err)
	}
	if !status.PinValid || !status.InventoryMatches || !status.VendoredManifestMatches {
		t.Fatalf("vendored status = %+v", status)
	}
	indexed, err := loader.Index(context.Background())
	if err != nil {
		t.Fatalf("index vendored skills: %v", err)
	}
	if len(indexed) != 5 {
		t.Fatalf("vendored skill count = %d, want 5", len(indexed))
	}
	versions := map[string]string{}
	for _, skill := range indexed {
		versions[skill.Name] = skill.Version
	}
	want := map[string]string{
		"product-marketing": "2.1.0",
		"launch":            "2.0.1",
		"copywriting":       "2.0.1",
		"social":            "2.2.0",
		"emails":            "2.0.0",
	}
	for name, version := range want {
		if versions[name] != version {
			t.Errorf("skill %s version = %q, want %q", name, versions[name], version)
		}
	}
}

func TestVendoredMarkdownLinksResolve(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", "..", "third_party", "marketingskills"))
	links, problems, err := inspectMarkdownLinks(filepath.Join(repo, "skills"))
	if err != nil {
		t.Fatalf("inspect vendored Markdown: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("vendored Markdown link problems:\n%s", strings.Join(problems, "\n"))
	}

	var localCount int
	var pinned []string
	for _, link := range links {
		switch link.class {
		case markdownLinkLocal:
			localCount++
		case markdownLinkPinnedExternal:
			pinned = append(pinned, link.destination)
		}
	}
	if localCount == 0 {
		t.Fatal("vendored Markdown contained no local links to verify")
	}
	sort.Strings(pinned)
	wantPinned := []string{
		"https://github.com/coreyhaines31/marketingskills/blob/" + vendoredUpstreamCommit + "/tools/REGISTRY.md",
		"https://github.com/coreyhaines31/marketingskills/blob/" + vendoredUpstreamCommit + "/tools/integrations/customer-io.md",
		"https://github.com/coreyhaines31/marketingskills/blob/" + vendoredUpstreamCommit + "/tools/integrations/introw.md",
		"https://github.com/coreyhaines31/marketingskills/blob/" + vendoredUpstreamCommit + "/tools/integrations/kit.md",
		"https://github.com/coreyhaines31/marketingskills/blob/" + vendoredUpstreamCommit + "/tools/integrations/mailchimp.md",
		"https://github.com/coreyhaines31/marketingskills/blob/" + vendoredUpstreamCommit + "/tools/integrations/nitrosend.md",
		"https://github.com/coreyhaines31/marketingskills/blob/" + vendoredUpstreamCommit + "/tools/integrations/resend.md",
		"https://github.com/coreyhaines31/marketingskills/blob/" + vendoredUpstreamCommit + "/tools/integrations/sendgrid.md",
	}
	if !reflect.DeepEqual(pinned, wantPinned) {
		t.Fatalf("commit-pinned links = %#v, want %#v", pinned, wantPinned)
	}
}

func TestMarkdownLinkInspectionHandlesSyntaxAndContainment(t *testing.T) {
	temp := t.TempDir()
	root := filepath.Join(temp, "vendored")
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatalf("create fixture directories: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docs, "Guide One.md"), []byte("# Guide\n"), 0o600); err != nil {
		t.Fatalf("write encoded-path target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(temp, "outside.md"), []byte("# Outside\n"), 0o600); err != nil {
		t.Fatalf("write containment target: %v", err)
	}
	fixture := strings.Join([]string{
		"# Links",
		"[encoded](Guide%20One.md?mode=read#intro)",
		"[reference][guide]",
		"[guide][]",
		"[pinned](https://github.com/coreyhaines31/marketingskills/blob/" + vendoredUpstreamCommit + "/tools/REGISTRY.md)",
		"[external](https://example.com/guide.md)",
		"```markdown",
		"[ignored](missing-in-fence.md)",
		"```",
		"[escape](%2e%2e/%2e%2e/outside.md)",
		"[missing](z-missing.md#section)",
		"[guide]: <Guide%20One.md?from=reference#intro>",
	}, "\n")
	if err := os.WriteFile(filepath.Join(docs, "links.md"), []byte(fixture), 0o600); err != nil {
		t.Fatalf("write Markdown fixture: %v", err)
	}

	links, problems, err := inspectMarkdownLinks(root)
	if err != nil {
		t.Fatalf("inspect fixture Markdown: %v", err)
	}
	var counts = map[markdownLinkClass]int{}
	for _, link := range links {
		counts[link.class]++
	}
	if counts[markdownLinkLocal] != 3 {
		t.Errorf("local link count = %d, want 3", counts[markdownLinkLocal])
	}
	if counts[markdownLinkPinnedExternal] != 1 {
		t.Errorf("commit-pinned link count = %d, want 1", counts[markdownLinkPinnedExternal])
	}
	if counts[markdownLinkExternal] != 1 {
		t.Errorf("external link count = %d, want 1", counts[markdownLinkExternal])
	}
	if len(problems) != 2 {
		t.Fatalf("problems = %#v, want containment and missing-target errors", problems)
	}
	if !sort.StringsAreSorted(problems) {
		t.Fatalf("problems are not sorted: %#v", problems)
	}
	if !strings.Contains(problems[0], "docs/links.md:10:") ||
		!strings.Contains(problems[0], "escapes vendored Markdown root") {
		t.Errorf("containment problem = %q", problems[0])
	}
	if !strings.Contains(problems[1], "docs/links.md:11:") ||
		!strings.Contains(problems[1], "target does not exist") {
		t.Errorf("missing-target problem = %q", problems[1])
	}
	for _, problem := range problems {
		if strings.Contains(problem, "missing-in-fence") {
			t.Errorf("fenced link was inspected: %q", problem)
		}
	}
}

func TestVendoredUpstreamProvenance(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", "..", "third_party", "marketingskills"))
	data, err := os.ReadFile(filepath.Join(repo, "UPSTREAM.yaml"))
	if err != nil {
		t.Fatalf("read UPSTREAM.yaml: %v", err)
	}
	var provenance upstreamProvenance
	if err := yaml.Unmarshal(data, &provenance); err != nil {
		t.Fatalf("decode UPSTREAM.yaml: %v", err)
	}
	if provenance.SourceURL != "https://github.com/coreyhaines31/marketingskills.git" {
		t.Errorf("source_url = %q", provenance.SourceURL)
	}
	if provenance.Ref != vendoredUpstreamCommit || provenance.Commit != vendoredUpstreamCommit {
		t.Errorf("ref/commit = %q/%q, want %s", provenance.Ref, provenance.Commit, vendoredUpstreamCommit)
	}
	if provenance.RepositoryVersion != "2.8.12" {
		t.Errorf("repository_version = %q, want 2.8.12", provenance.RepositoryVersion)
	}

	actualPaths, err := vendoredSkillFilePaths(filepath.Join(repo, "skills"))
	if err != nil {
		t.Fatalf("enumerate vendored skill files: %v", err)
	}
	if len(actualPaths) != 23 {
		t.Fatalf("vendored skill file count = %d, want 23", len(actualPaths))
	}

	inventoryPaths := make([]string, 0, len(provenance.Inventory))
	inventoryHashes := make(map[string]string, len(provenance.Inventory))
	for index, entry := range provenance.Inventory {
		if err := validateProvenancePath(entry.Path); err != nil {
			t.Errorf("inventory[%d] path %q: %v", index, entry.Path, err)
		}
		if index > 0 && provenance.Inventory[index-1].Path >= entry.Path {
			t.Errorf("inventory is not strictly sorted and unique at %q", entry.Path)
		}
		inventoryPaths = append(inventoryPaths, entry.Path)
		inventoryHashes[entry.Path] = entry.SHA256
		actualHash, err := sha256File(filepath.Join(repo, filepath.FromSlash(entry.Path)))
		if err != nil {
			t.Errorf("hash inventory file %q: %v", entry.Path, err)
			continue
		}
		if entry.SHA256 != actualHash {
			t.Errorf("inventory hash for %q = %s, want %s", entry.Path, entry.SHA256, actualHash)
		}
	}
	if !reflect.DeepEqual(inventoryPaths, actualPaths) {
		t.Errorf("inventory paths do not exactly match vendored skill files\ninventory: %#v\nactual: %#v", inventoryPaths, actualPaths)
	}

	if provenance.License.Path != "LICENSE" {
		t.Errorf("license path = %q, want LICENSE", provenance.License.Path)
	}
	licenseHash, err := sha256File(filepath.Join(repo, provenance.License.Path))
	if err != nil {
		t.Fatalf("hash vendored license: %v", err)
	}
	if provenance.License.SHA256 != licenseHash {
		t.Errorf("license hash = %s, want %s", provenance.License.SHA256, licenseHash)
	}

	if provenance.LocalModifications.UpstreamContent != "link-target-only" {
		t.Errorf("local_modifications.upstream_content = %q, want link-target-only", provenance.LocalModifications.UpstreamContent)
	}
	wantOriginalHashes := map[string]string{
		"skills/emails/SKILL.md": "1fbd54df4c59f48408057e7db0dfb1a53c033e6a4ca5cd1927a47c51ec28d8f1",
		"skills/launch/SKILL.md": "6d8bbd61449c48c458b20457c0b698386fe808e7063a7baadc197fc179bd94bb",
	}
	if len(provenance.LocalModifications.Files) != len(wantOriginalHashes) {
		t.Fatalf("local modification count = %d, want %d", len(provenance.LocalModifications.Files), len(wantOriginalHashes))
	}
	for index, modification := range provenance.LocalModifications.Files {
		if err := validateProvenancePath(modification.Path); err != nil {
			t.Errorf("local_modifications.files[%d] path %q: %v", index, modification.Path, err)
		}
		if index > 0 && provenance.LocalModifications.Files[index-1].Path >= modification.Path {
			t.Errorf("local modifications are not strictly sorted and unique at %q", modification.Path)
		}
		wantOriginal, ok := wantOriginalHashes[modification.Path]
		if !ok {
			t.Errorf("unexpected local modification record for %q", modification.Path)
			continue
		}
		if modification.Date != "2026-07-26" {
			t.Errorf("%s modification date = %q, want 2026-07-26", modification.Path, modification.Date)
		}
		if modification.ModificationType != "link-target-only" {
			t.Errorf("%s modification type = %q, want link-target-only", modification.Path, modification.ModificationType)
		}
		if !strings.Contains(strings.ToLower(modification.Reason), "link") ||
			!strings.Contains(strings.ToLower(modification.Summary), "link") {
			t.Errorf("%s reason/summary must describe a link-only change", modification.Path)
		}
		if modification.OriginalUpstreamSHA256 != wantOriginal {
			t.Errorf("%s original hash = %s, want %s", modification.Path, modification.OriginalUpstreamSHA256, wantOriginal)
		}
		wantResult := inventoryHashes[modification.Path]
		if modification.ResultingVendoredSHA256 != wantResult {
			t.Errorf("%s resulting hash = %s, want inventory hash %s", modification.Path, modification.ResultingVendoredSHA256, wantResult)
		}
		actualResult, err := sha256File(filepath.Join(repo, filepath.FromSlash(modification.Path)))
		if err != nil {
			t.Errorf("hash locally modified file %q: %v", modification.Path, err)
		} else if modification.ResultingVendoredSHA256 != actualResult {
			t.Errorf("%s resulting hash = %s, want actual hash %s", modification.Path, modification.ResultingVendoredSHA256, actualResult)
		}
	}
}

type upstreamProvenance struct {
	SourceURL         string `yaml:"source_url"`
	Ref               string `yaml:"ref"`
	Commit            string `yaml:"commit"`
	RepositoryVersion string `yaml:"repository_version"`
	License           struct {
		Path   string `yaml:"path"`
		SHA256 string `yaml:"sha256"`
	} `yaml:"license"`
	Inventory          []upstreamInventoryEntry `yaml:"inventory"`
	LocalModifications struct {
		UpstreamContent string                      `yaml:"upstream_content"`
		Statement       string                      `yaml:"statement"`
		Files           []upstreamLocalModification `yaml:"files"`
	} `yaml:"local_modifications"`
}

type upstreamInventoryEntry struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
}

type upstreamLocalModification struct {
	Path                    string `yaml:"path"`
	Date                    string `yaml:"date"`
	ModificationType        string `yaml:"modification_type"`
	Reason                  string `yaml:"reason"`
	Summary                 string `yaml:"summary"`
	OriginalUpstreamSHA256  string `yaml:"original_upstream_sha256"`
	ResultingVendoredSHA256 string `yaml:"resulting_vendored_sha256"`
}

func vendoredSkillFilePaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("vendored skill file %s is a symlink", filePath)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("vendored skill path %s is not a regular file", filePath)
		}
		relative, err := filepath.Rel(filepath.Dir(root), filePath)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func validateProvenancePath(value string) error {
	if value == "" {
		return errors.New("path is empty")
	}
	if value != path.Clean(value) || strings.Contains(value, `\`) {
		return errors.New("path is not normalized")
	}
	if path.IsAbs(value) || value == ".." || strings.HasPrefix(value, "../") {
		return errors.New("path is not repository-relative")
	}
	return nil
}

func sha256File(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type markdownLinkClass string

const (
	markdownLinkLocal          markdownLinkClass = "local"
	markdownLinkExternal       markdownLinkClass = "external"
	markdownLinkPinnedExternal markdownLinkClass = "commit-pinned-external"
)

type inspectedMarkdownLink struct {
	file        string
	line        int
	destination string
	class       markdownLinkClass
}

type markdownLine struct {
	number int
	text   string
}

var (
	inlineMarkdownLinkPattern  = regexp.MustCompile(`!?\[[^]]*\]\(\s*(?:<([^>]+)>|([^\s)]+))`)
	referenceDefinitionPattern = regexp.MustCompile(`^\s{0,3}\[([^]]+)\]:\s*(?:<([^>]+)>|(\S+))`)
	referenceLinkPattern       = regexp.MustCompile(`!?\[([^]]+)\]\[([^]]*)\]`)
)

func inspectMarkdownLinks(root string) ([]inspectedMarkdownLink, []string, error) {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	var links []inspectedMarkdownLink
	var problems []string
	err = filepath.WalkDir(cleanRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(cleanRoot, filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		lines := markdownLinesOutsideFences(data)
		definitions := make(map[string]string)
		for _, line := range lines {
			if match := referenceDefinitionPattern.FindStringSubmatch(line.text); match != nil {
				definitions[normalizeReferenceLabel(match[1])] = firstNonempty(match[2], match[3])
			}
		}
		inspect := func(line int, destination string) {
			class, problem := classifyMarkdownDestination(cleanRoot, filePath, destination)
			if problem != "" {
				problems = append(problems, fmt.Sprintf("%s:%d: destination %q %s", relative, line, destination, problem))
				return
			}
			links = append(links, inspectedMarkdownLink{
				file:        relative,
				line:        line,
				destination: destination,
				class:       class,
			})
		}
		for _, line := range lines {
			for _, match := range inlineMarkdownLinkPattern.FindAllStringSubmatch(line.text, -1) {
				inspect(line.number, firstNonempty(match[1], match[2]))
			}
			for _, match := range referenceLinkPattern.FindAllStringSubmatch(line.text, -1) {
				label := match[2]
				if label == "" {
					label = match[1]
				}
				destination, ok := definitions[normalizeReferenceLabel(label)]
				if !ok {
					problems = append(problems, fmt.Sprintf("%s:%d: reference %q has no definition", relative, line.number, label))
					continue
				}
				inspect(line.number, destination)
			}
		}
		return nil
	})
	sort.Slice(links, func(i, j int) bool {
		if links[i].file != links[j].file {
			return links[i].file < links[j].file
		}
		if links[i].line != links[j].line {
			return links[i].line < links[j].line
		}
		return links[i].destination < links[j].destination
	})
	sort.Strings(problems)
	return links, problems, err
}

func markdownLinesOutsideFences(data []byte) []markdownLine {
	var lines []markdownLine
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var fence byte
	var fenceLength int
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " \t")
		if marker, length := markdownFence(trimmed); length >= 3 {
			if fence == 0 {
				fence = marker
				fenceLength = length
			} else if marker == fence && length >= fenceLength {
				fence = 0
				fenceLength = 0
			}
			continue
		}
		if fence == 0 {
			lines = append(lines, markdownLine{number: lineNumber, text: line})
		}
	}
	return lines
}

func markdownFence(line string) (byte, int) {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return 0, 0
	}
	marker := line[0]
	var length int
	for length < len(line) && line[length] == marker {
		length++
	}
	return marker, length
}

func classifyMarkdownDestination(root, sourcePath, destination string) (markdownLinkClass, string) {
	parsed, err := url.Parse(destination)
	if err != nil {
		return "", "is not a valid URL"
	}
	if parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(destination, "//") {
		if parsed.Scheme == "https" &&
			strings.EqualFold(parsed.Host, "github.com") &&
			strings.HasPrefix(parsed.Path, "/coreyhaines31/marketingskills/blob/"+vendoredUpstreamCommit+"/") {
			return markdownLinkPinnedExternal, ""
		}
		return markdownLinkExternal, ""
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", "contains invalid URL encoding"
	}
	target := sourcePath
	if decodedPath != "" {
		target = filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(decodedPath))
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", "cannot be resolved relative to the vendored Markdown root"
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", "escapes vendored Markdown root"
	}
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "target does not exist"
		}
		return "", "target cannot be inspected"
	}
	if info.IsDir() {
		return "", "target is a directory"
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "vendored Markdown root cannot be resolved"
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", "target cannot be resolved"
	}
	resolvedRelative, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil ||
		resolvedRelative == ".." ||
		strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(resolvedRelative) {
		return "", "escapes vendored Markdown root through a symlink"
	}
	return markdownLinkLocal, ""
}

func normalizeReferenceLabel(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(label), " "))
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
