package main

// The logic behind OpenArtifactPage (docs/plans/html-artifacts.md, P3): issuing
// a page token only for a static page that is allowed right now, over the files
// that were hashed. The binding in app_artifact_views.go stays thin.

import (
	"context"
	"errors"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/semantics"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// errPageScripted is the fixed refusal to open a scripted page in a frame: it
// has script, so it runs in its own page window (not built yet).
var errPageScripted = errors.New("this page has script, so it opens in a page window")

// ArtifactPageGrant is what OpenArtifactPage returns: the token the page frame's
// URL is made from (/slaw-view/<token>/page), bound to one hash.
type ArtifactPageGrant struct {
	Token string `json:"token"`
	// Hash is the PageHash the token is bound to.
	Hash string `json:"hash"`
}

// openPageGrant checks, in ws, that the artifact is an HTML page that is
// allowed and static (workspace.AllowedStaticPage, the one check shared with
// `slaw artifact render`), which hands back the page rebuilt from the bytes that
// were hashed, and holds them under a new token. Its refusals map to Studio's
// fixed texts.
func openPageGrant(vs *viewServer, ws workspace.Workspace, name, origin string, runner func(m *artifacts.Manifest, pins map[string]string) artifacts.PageRunner) (*ArtifactPageGrant, error) {
	ok, err := workspace.AllowedStaticPage(ws, name)
	if err != nil {
		return nil, pageGrantError(err)
	}
	token, err := vs.issue(&viewGrant{name: name, hash: ok.Hash, origin: origin, ws: ws, page: ok.Page, run: runner(&ok.Page.Manifest, ok.Pins)})
	if err != nil {
		if errors.Is(err, errBadViewOrigin) {
			return nil, err
		}
		return nil, errors.New("the page could not be opened")
	}
	return &ArtifactPageGrant{Token: token, Hash: ok.Hash}, nil
}

// pageGrantError maps AllowedStaticPage's refusals to the fixed errors the
// bridge shows.
func pageGrantError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrPageScripted):
		return errPageScripted
	case errors.Is(err, workspace.ErrNotStaticPage), errors.Is(err, workspace.ErrPageNotAllowed), errors.Is(err, artifacts.ErrNotFound):
		return errViewNotAllowed
	}
	return viewError(err)
}

// pageRunner answers a page's queries from the manifest the page was hashed
// with (m, held by the grant), not from artifact.yaml as it is on disk when a
// lazy query runs: the same gates as every artifact query (vetted SQL at every
// run, Bind, read-only, the 500-row cap, the 30 s limit), never SQL from the
// page.
func (a *App) pageRunner(name string, m *artifacts.Manifest, pins map[string]string) artifacts.PageRunner {
	return func(ctx context.Context, query string, values map[string]any) (*artifacts.PageRows, error) {
		// The metric files and saved queries the page was allowed over are the
		// only revisions this run may read, whatever a cache last said (#282).
		res, err := a.runPageQuery(semantics.WithPins(ctx, pins), name, m, query, values)
		if err != nil {
			return nil, err
		}
		return &artifacts.PageRows{Columns: res.Columns, Rows: res.Rows, Truncated: res.Truncated}, nil
	}
}
