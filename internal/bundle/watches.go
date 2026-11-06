package bundle

import (
	"context"
	"fmt"

	"github.com/SandwichLabs/slaw/internal/watches"
)

// A watch item's id is its name; the entry is watches/<name>.yaml. Only the
// file travels: a watch's history lives under .slaw-local/ and no bundle path
// can name it (classify refuses anything else).

// validateWatch decodes one watch entry with the store's own decoder, so a file
// that would list with an error in the workspace refuses the whole bundle
// instead. The title is what the preview shows.
func validateWatch(name string, data []byte) (string, error) {
	w, err := watches.Decode(name, data)
	if err != nil {
		return "", err
	}
	if w.Description != "" {
		return w.Description, nil
	}
	return w.Name, nil
}

// watchRev is the revision of the watch file now in the workspace, "" when
// there is none or it cannot be read.
func watchRev(ctx context.Context, t Target, name string) string {
	if w, err := t.Watches().Get(ctx, name); err == nil {
		return w.Rev
	}
	return ""
}

// watchIDs lists the names of every watch file an export can take, unreadable
// files included (validation then skips or refuses them).
func watchIDs(ctx context.Context, t Target) ([]string, error) {
	list, err := t.Watches().List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, w := range list {
		if w.Name != watches.TruncatedStubName {
			ids = append(ids, w.Name)
		}
	}
	return ids, nil
}

// watchItems lists the items ContentsOf offers: every readable watch.
func watchItems(ctx context.Context, t Target) ([]ContentItem, error) {
	list, err := t.Watches().List(ctx)
	if err != nil {
		return nil, err
	}
	out := []ContentItem{}
	for _, w := range list {
		if w.Error != "" {
			continue
		}
		title := w.Description
		if title == "" {
			title = w.Name
		}
		out = append(out, ContentItem{ID: w.Name, Title: title})
	}
	return out, nil
}

// writeWatch stores one watch under finalID, exactly as the bundle held it
// (comments and layout kept), after the store's decoder has read it again:
// create-only, or against the revision just read for an overwrite.
func (p *Package) writeWatch(ctx context.Context, t Target, it item, finalID string, overwrite bool) (string, error) {
	var opts []watches.WriteOption
	if overwrite {
		rev := p.revFor(ctx, t, KindWatch, finalID)
		if rev == "" {
			return "", fmt.Errorf("cannot overwrite the existing watch %q: it could not be read", finalID)
		}
		opts = append(opts, watches.WithRev(rev))
	}
	_, err := t.Watches().PutSource(ctx, finalID, it.Data, opts...)
	return finalID, err
}

// renameWatch finds a free name for a clashing watch: "stuck_orders_2".
func (r *itemImporter) renameWatch(it item) (string, error) {
	for i := 2; i < 1000; i++ {
		suffix := fmt.Sprintf("_%d", i)
		base := it.ID
		if len(base)+len(suffix) > 63 {
			base = base[:63-len(suffix)]
		}
		cand := base + suffix
		has, err := storeHas(r.t, KindWatch, cand)
		if err != nil {
			return "", err
		}
		if !has && !r.isTaken(KindWatch, cand) {
			return cand, nil
		}
	}
	return "", nil
}

// rewriteWatchRefs points a watch at the saved query or metric that the same
// bundle brought in under a new name; without it a renamed "q" -> "q_2" would
// leave the imported watch asking the workspace's own "q". Items are imported
// in path order, so queries/ and semantics/ are done before watches/. A rewrite
// re-encodes the document (comments are not kept) and says so in the notes.
func (r *itemImporter) rewriteWatchRefs(it item) item {
	w, err := watches.Decode(it.ID, it.Data)
	if err != nil {
		return it
	}
	changed := false
	if q := w.Question.Query; q != "" {
		if newQ, ok := r.renamed[KindQuery][q]; ok {
			r.notes = append(r.notes, fmt.Sprintf("watch %s now asks the saved query %q imported with it (the workspace's own %q was kept)", it.ID, newQ, q))
			w.Question.Query, changed = newQ, true
		} else if r.skipped[KindQuery][q] {
			r.notes = append(r.notes, fmt.Sprintf("watch %s asks the saved query %q, which was skipped because the workspace already has one by that name: it now asks the workspace's %q, which may be a different query", it.ID, q, q))
		}
	}
	if m := w.Question.Metric; m != "" {
		if final, ok := r.renamed[KindSemantics]["metrics/"+m]; ok {
			_, newM := splitSemanticsID(final)
			r.notes = append(r.notes, fmt.Sprintf("watch %s now asks the metric %q imported with it (the workspace's own %q was kept)", it.ID, newM, m))
			w.Question.Metric, changed = newM, true
		}
	}
	if !changed {
		return it
	}
	data, err := watches.Encode(w)
	if err != nil {
		r.notes = append(r.notes, fmt.Sprintf("watch %s could not be pointed at the renamed items: %v", it.ID, err))
		return it
	}
	it.Data = data
	return it
}
