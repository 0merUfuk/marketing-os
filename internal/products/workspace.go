package products

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/omerufuk/marketing-os/internal/domain"
	"gopkg.in/yaml.v3"
)

type Workspace struct {
	root string
}

func NewWorkspace(root string) *Workspace { return &Workspace{root: root} }

func (w *Workspace) Initialize(product domain.Product) error {
	if err := product.Validate(); err != nil {
		return err
	}
	dir, err := w.productDir(product.ID, true)
	if err != nil {
		return err
	}
	for _, name := range []string{"evidence", "research", "drafts", "reports", "approvals", "state", ".agents"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
			return fmt.Errorf("create product workspace directory %s: %w", name, err)
		}
	}
	data, err := yaml.Marshal(product)
	if err != nil {
		return fmt.Errorf("encode product config: %w", err)
	}
	if err := atomicWrite(filepath.Join(dir, "product.yaml"), data, 0o600); err != nil {
		return err
	}
	for _, file := range []string{"sources.yaml", "workflows.yaml", "integrations.yaml"} {
		path := filepath.Join(dir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := atomicWrite(path, []byte("{}\n"), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Workspace) WriteContextDraft(productID string, version int, content string) error {
	dir, err := w.productDir(productID, false)
	if err != nil {
		return err
	}
	if version <= 0 {
		return fmt.Errorf("context version must be positive")
	}
	path := filepath.Join(dir, ".agents", fmt.Sprintf("product-marketing.v%d.draft.md", version))
	return atomicWrite(path, []byte(content), 0o600)
}

func (w *Workspace) WriteApprovedContext(productID string, version int, content, actor string) error {
	dir, err := w.productDir(productID, false)
	if err != nil {
		return err
	}
	if version <= 0 || strings.TrimSpace(actor) == "" {
		return fmt.Errorf("positive version and approval actor are required")
	}
	agentDir := filepath.Join(dir, ".agents")
	if err := atomicWrite(filepath.Join(agentDir, "product-marketing.md"), []byte(content), 0o600); err != nil {
		return err
	}
	entry := fmt.Sprintf("- %s: approved v%d by %s\n", time.Now().UTC().Format(time.RFC3339), version, actor)
	changelog := filepath.Join(agentDir, "product-marketing.changelog.md")
	f, err := os.OpenFile(changelog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open context changelog: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("append context changelog: %w", err)
	}
	return f.Sync()
}

func (w *Workspace) WriteApproval(productID, approvalID, body string) error {
	dir, err := w.productDir(productID, false)
	if err != nil {
		return err
	}
	if strings.TrimSpace(approvalID) == "" || strings.ContainsAny(approvalID, `/\\`) {
		return fmt.Errorf("approval id is invalid")
	}
	return atomicWrite(filepath.Join(dir, "approvals", approvalID+".md"), []byte(body), 0o600)
}

func (w *Workspace) WriteAssets(productID, approvalID string, assets []domain.GeneratedAsset) error {
	dir, err := w.productDir(productID, false)
	if err != nil {
		return err
	}
	if strings.TrimSpace(approvalID) == "" || strings.ContainsAny(approvalID, `/\\`) {
		return fmt.Errorf("approval id is invalid")
	}
	data, err := json.MarshalIndent(assets, "", "  ")
	if err != nil {
		return fmt.Errorf("encode generated assets: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(dir, "drafts", approvalID+".json"), data, 0o600)
}

func (w *Workspace) ProductPath(productID string) (string, error) {
	return w.productDir(productID, false)
}

func (w *Workspace) productDir(productID string, create bool) (string, error) {
	if err := domain.ValidateProductID(productID); err != nil {
		return "", err
	}
	root, err := filepath.Abs(w.root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	if create {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", fmt.Errorf("create workspace root: %w", err)
		}
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root symlinks: %w", err)
	}
	dir := filepath.Join(resolvedRoot, productID)
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("create product directory: %w", err)
		}
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve product directory: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("product path escapes workspace")
	}
	return resolvedDir, nil
}

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".marketing-os-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
