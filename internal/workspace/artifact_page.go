package workspace

import (
	"errors"
	"fmt"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/trustfile"
)

// The ways a person's surface refuses to draw an artifact as a static page.
var (
	// ErrNotStaticPage: the artifact is not a view: html page (an A2UI view or a
	// script view).
	ErrNotStaticPage = errors.New("not a static page")
	// ErrPageScripted: the page has script, which only runs in Studio's page window.
	ErrPageScripted = errors.New("the page has script")
	// ErrPageNotAllowed: the person has not allowed this page as it is now
	// (never decided, denied, or changed since it was allowed).
	ErrPageNotAllowed = errors.New("the page is not allowed")
)

// AllowedStaticPage is what Studio's OpenArtifactPage checks before it serves a
// page, for a surface with no window (`slaw artifact render`): the artifact must
// be a view: html page, static, and allowed for this person right now. It returns
// the page rebuilt from the bytes the allowed hash covers (checked again over
// them, so what is drawn is what was reviewed) and the metric and saved-query
// revisions the allow pinned (for semantics.WithPins). A refusal names the
// `slaw artifact view` command that fixes it.
func AllowedStaticPage(ws Workspace, name string) (*artifacts.Artifact, map[string]string, error) {
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return nil, nil, err
	}
	if st.Error != "" {
		return nil, nil, fmt.Errorf("artifact %q cannot be read: %s", name, st.Error)
	}
	if err := refuseNonStatic(name, st); err != nil {
		return nil, nil, err
	}
	if err := refuseUnallowed(name, st); err != nil {
		return nil, nil, err
	}
	// The bytes held are the bytes the allowed hash covers, checked here rather
	// than assumed.
	page, err := artifacts.PageFromText(name, []byte(st.Manifest), st.PageFiles)
	if err != nil || page.Page.Hash != st.ContentHash {
		return nil, nil, fmt.Errorf("artifact %q: %w (its files changed while it was read; review it with `slaw artifact view status %s`)", name, ErrPageNotAllowed, name)
	}
	return page, st.Pins, nil
}

func refuseNonStatic(name string, st ArtifactViewStatus) error {
	switch {
	case st.Kind == artifacts.ViewHTML && st.PageKind == artifacts.PageStatic:
		return nil
	case st.Kind == artifacts.ViewHTML:
		return fmt.Errorf("artifact %q: %w, so it runs only in a Studio page window; render draws static pages", name, ErrPageScripted)
	case st.Kind == artifacts.ViewScript:
		return fmt.Errorf("artifact %q: %w: it is a script view (view: script); render draws only static view: html pages, open it in Studio", name, ErrNotStaticPage)
	default:
		return fmt.Errorf("artifact %q: %w: it is an A2UI view (%s); render draws only static view: html pages, open it in Studio", name, ErrNotStaticPage, viewKindLabel(st.Kind))
	}
}

func viewKindLabel(kind string) string {
	if kind == "" {
		return "view: a2ui"
	}
	return "view: " + kind
}

func refuseUnallowed(name string, st ArtifactViewStatus) error {
	review := fmt.Sprintf("review it with `slaw artifact view status %s`, then `slaw artifact view allow %s`", name, name)
	switch trustfile.State(st.State) {
	case trustfile.StateAllowed:
		if st.Hash != "" {
			return nil
		}
	case trustfile.StateChanged:
		return fmt.Errorf("artifact %q: %w: it changed since you allowed it; %s", name, ErrPageNotAllowed, review)
	case trustfile.StateDenied:
		return fmt.Errorf("artifact %q: %w: you denied it; `slaw artifact view revoke %s` forgets that, then %s", name, ErrPageNotAllowed, name, review)
	}
	return fmt.Errorf("artifact %q: %w: it has not been allowed; %s", name, ErrPageNotAllowed, review)
}
