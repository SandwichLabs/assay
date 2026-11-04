package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/engine"
	"github.com/SandwichLabs/slaw/internal/filestore"
	"github.com/SandwichLabs/slaw/internal/hub"
	"github.com/SandwichLabs/slaw/internal/pagetheme"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

var artifactRenderCmd = &cobra.Command{
	Use:   "render <name>",
	Short: "Draw an allowed static HTML page to a folder of files",
	Long: `Draw a static HTML page (view: html, no script) the way Studio's page frame
does and write it to a folder: index.html, the host stylesheet _slaw/theme.css
(the built-in light theme, linked first in the page's head) and the page's own
stylesheets, images and fonts. Open index.html in a browser or publish the
folder.

It draws only a static page that you have allowed as it is now ("slaw artifact
view status|allow"). A page with script, an A2UI view and a script view are
refused: those open in Studio. Its queries run through the same read-only,
policy-checked path as in Studio, and a -p value is bound as a parameter, never
put into SQL text. The page is drawn once with those values: its links and
forms back to itself ("page?...") do not work from a file.

The folder defaults to ./<name>-render. A folder that already has files in it is
refused unless you pass --force, which overwrites only the files this writes.
Nothing is written outside the folder. A failed draw writes nothing.`,
	Example: `  slaw artifact render sales
  slaw artifact render sales --out ./public -p region=east -p from=2026-01-01`,
	Args:         cobra.ExactArgs(1),
	RunE:         runArtifactRender,
	SilenceUsage: true,
}

func init() {
	f := artifactRenderCmd.Flags()
	f.StringP("out", "o", "", "folder to write (default: ./<name>-render)")
	f.StringArrayP("param", "p", nil, "a parameter of the page, name=value (repeatable)")
	f.Bool("force", false, "write into a folder that already has files in it")
	addWorkspaceFlag(artifactRenderCmd)
	artifactCmd.AddCommand(artifactRenderCmd)
}

// renderFile is one file of a render, by its slash path under the output folder.
type renderFile struct {
	rel  string
	data []byte
}

func runArtifactRender(cmd *cobra.Command, args []string) error {
	name := args[0]
	if !filestore.ValidID(name) {
		return fmt.Errorf("artifact %q: %w", name, artifacts.ErrNotFound)
	}
	out, _ := cmd.Flags().GetString("out")
	if out == "" {
		out = name + "-render"
	}
	force, _ := cmd.Flags().GetBool("force")
	pairs, _ := cmd.Flags().GetStringArray("param")
	dest, err := prepareRenderOut(out, force)
	if err != nil {
		return err
	}
	query, err := renderQuery(pairs)
	if err != nil {
		return err
	}
	dir, _ := cmd.Flags().GetString("workspace")
	o, err := cliOpenWith(cmdCtx(cmd), dir, hub.OpenOptions{App: "slaw artifact render", Mode: hub.ModeReadOnly, PolicyGatedRuns: true}, false, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer o.Close()

	page, pins, err := workspace.AllowedStaticPage(o.WS, name)
	if err != nil {
		return err
	}
	doc, err := drawPage(cmd, o, name, page, pins, query)
	if err != nil {
		return err
	}
	files, err := renderedFiles(page, doc)
	if err != nil {
		return err
	}
	if err := writeRenderFiles(dest, files); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Rendered %s to %s (%d files)\n", name, dest, len(files))
	for _, f := range files {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", f.rel)
	}
	return nil
}

// renderQuery turns the -p name=value flags into the query string a page
// request carries; artifacts.ParsePageQuery then reads it exactly as Studio's
// page route does (a declared name, once, of its type).
func renderQuery(pairs []string) (url.Values, error) {
	q := url.Values{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid -p %q: want name=value", truncateArg(p))
		}
		q.Add(k, v)
	}
	return q, nil
}

func truncateArg(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// drawPage runs the page's queries through the read-only policy-gated runner
// and returns the finished document, with the host stylesheet linked.
func drawPage(cmd *cobra.Command, o *openedWorkspace, name string, page *artifacts.Artifact, pins map[string]string, query url.Values) ([]byte, error) {
	values, err := artifacts.ParsePageQuery(&page.Manifest, query)
	if err != nil {
		return nil, fmt.Errorf("invalid parameter: %w", err)
	}
	run, err := hub.ArtifactPageRunner(o.WS, o.Sess, logger, name, &page.Manifest, pins)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	if err := artifacts.RenderHTMLPage(cmdCtx(cmd), run, page, values, &body); err != nil {
		return nil, renderFailed(name, err)
	}
	return pagetheme.LinkInto(body.Bytes()), nil
}

// renderFailed words a failed draw. The renderer's text is fixed on purpose
// (the cause can hold SQL, a path or a policy reason), so only a bad value shows
// its reason, which names a parameter and a limit; the rest go to --verbose.
func renderFailed(name string, err error) error {
	var re *artifacts.RenderError
	if !errors.As(err, &re) {
		return fmt.Errorf("failed to render %s: %w", name, err)
	}
	if cause := errors.Unwrap(re); cause != nil {
		logger.Debug("page render failed", "artifact", name, "error", engine.ErrorForLog(cause))
		if re.Kind == artifacts.RenderBadValues {
			return fmt.Errorf("failed to render %s: %s: %w", name, re.Error(), cause)
		}
	}
	return fmt.Errorf("failed to render %s: %w (--verbose logs the reason)", name, re)
}

// renderedFiles is everything one render writes: the document, the host
// stylesheet and the page's static assets, as Studio's page frame serves them.
func renderedFiles(page *artifacts.Artifact, doc []byte) ([]renderFile, error) {
	files := []renderFile{
		{rel: "index.html", data: doc},
		{rel: pagetheme.Path, data: []byte(pagetheme.Stylesheet(pagetheme.DefaultRoles()))},
	}
	taken := map[string]bool{"index.html": true, pagetheme.Path: true}
	for _, f := range page.Files {
		if !artifacts.IsStaticAsset(f.Path) {
			continue
		}
		if taken[f.Path] {
			return nil, fmt.Errorf("the page has a file %q that the render writes itself; rename it", f.Path)
		}
		taken[f.Path] = true
		files = append(files, renderFile{rel: f.Path, data: f.Data})
	}
	return files, nil
}

// prepareRenderOut resolves the output folder and refuses one that is a link,
// not a folder, or has files in it (without force). It creates nothing.
func prepareRenderOut(out string, force bool) (string, error) {
	abs, err := filepath.Abs(out)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", out, err)
	}
	fi, err := os.Lstat(abs)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return abs, nil
	case err != nil:
		return "", fmt.Errorf("failed to check %s: %w", abs, err)
	case fi.Mode()&os.ModeSymlink != 0:
		return "", fmt.Errorf("%s is a symbolic link; pass the folder it points to", abs)
	case !fi.IsDir():
		return "", fmt.Errorf("%s exists and is not a folder", abs)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", abs, err)
	}
	if len(entries) > 0 && !force {
		return "", fmt.Errorf("%s already has files in it; use --force to write into it (only the files this renders are overwritten)", abs)
	}
	return abs, nil
}

// writeRenderFiles writes every file under dest, creating folders as needed.
// All paths are checked before the first write; each write refuses to go through
// a symbolic link, and replaces its file by rename, so an existing link is
// replaced, never followed.
func writeRenderFiles(dest string, files []renderFile) error {
	targets := make([]string, len(files))
	for i, f := range files {
		rel := filepath.FromSlash(f.rel)
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("refusing to write %q outside the output folder", f.rel)
		}
		targets[i] = filepath.Join(dest, rel)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dest, err)
	}
	for i, f := range files {
		if err := writeInside(dest, targets[i], f.data); err != nil {
			return fmt.Errorf("failed to write %s: %w", f.rel, err)
		}
	}
	return nil
}

func writeInside(root, target string, data []byte) error {
	dir := filepath.Dir(target)
	if err := mkdirNoLinks(root, dir); err != nil {
		return err
	}
	if fi, err := os.Lstat(target); err == nil && fi.IsDir() {
		return fmt.Errorf("%s is a folder", target)
	}
	tmp, err := os.CreateTemp(dir, ".render-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// mkdirNoLinks creates dir (inside root) one element at a time and refuses an
// element that exists as a link or as a file.
func mkdirNoLinks(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return fmt.Errorf("%s is outside the output folder", dir)
	}
	cur := root
	for _, el := range strings.Split(rel, string(filepath.Separator)) {
		if el == "." || el == "" {
			continue
		}
		cur = filepath.Join(cur, el)
		fi, err := os.Lstat(cur)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(cur, 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		case !fi.IsDir():
			return fmt.Errorf("%s is a link or a file, not a folder", cur)
		}
	}
	return nil
}
