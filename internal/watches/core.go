package watches

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// backend is where a core keeps file bytes: the workspace folder (FileStore)
// or a map (Mock). rel is slash-separated under the workspace root. read,
// write and remove report revisions as filestore does (the content hash) and
// satisfy its conflict and not-found errors, so the two stores behave alike.
type backend interface {
	read(rel string) (data []byte, rev string, err error)
	// write is compare-and-write: expect "" means the file must not exist.
	write(rel string, data []byte, expect string, overwrite bool) (rev string, err error)
	// remove deletes rel; expect "" deletes whatever is there.
	remove(rel, expect string, overwrite bool) error
	// names lists the files in watches/ that end in Extension, sorted. Hidden
	// and temp files are left out; a symbolic link is listed (link set) so it
	// can be shown with an error instead of silently missing.
	names() ([]fileName, error)
}

// fileName is one directory entry of watches/.
type fileName struct {
	name string
	link bool
}

// core is the whole Store over a backend.
type core struct {
	b      backend
	logger *slog.Logger
}

var _ Store = (*core)(nil)

func rel(name string) string { return Dir + "/" + name + Extension }

func (c *core) load(name string) (*Watch, error) {
	data, rev, err := c.b.read(rel(name))
	if err != nil {
		return nil, err
	}
	w, err := Decode(name, data)
	if err != nil {
		return nil, err
	}
	w.Rev = rev
	return w, nil
}

// stub is the listing entry of a file that could not be read.
func stub(name, msg string) Watch { return Watch{Name: name, Meta: Meta{Error: msg}} }

// List implements ReadStore.
func (c *core) List(ctx context.Context) ([]Watch, error) {
	names, err := c.b.names()
	if err != nil {
		return nil, fmt.Errorf("failed to list watches: %w", err)
	}
	more := 0
	if len(names) > MaxFiles {
		more = len(names) - MaxFiles
		names = names[:MaxFiles]
	}
	out := make([]Watch, 0, len(names)+1)
	for _, fn := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		stem := strings.TrimSuffix(fn.name, Extension)
		if w, ok := c.listOne(fn, stem); ok {
			out = append(out, w)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if more > 0 {
		out = append(out, stub(TruncatedStubName, fmt.Sprintf("%d more files in %s/ are not listed (limit %d)", more, Dir, MaxFiles)))
	}
	return out, nil
}

// listOne loads one listed file; ok is false for a file that vanished.
func (c *core) listOne(fn fileName, stem string) (Watch, bool) {
	if fn.link {
		return stub(stem, fn.name+" is a symbolic link; it is not read"), true
	}
	if err := ValidateName(stem); err != nil {
		return stub(stem, errorText(err)), true
	}
	w, err := c.load(stem)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Watch{}, false // vanished between the listing and the read
		}
		c.logger.Warn("unreadable watch file", "name", stem, "error", err)
		st := stub(stem, errorText(err))
		// The stub carries the revision of the bytes that could not be read, so a
		// surface can replace or delete exactly that file and nothing newer.
		if _, rev, rerr := c.b.read(rel(stem)); rerr == nil {
			st.Rev = rev
		}
		return st, true
	}
	return *w, true
}

// Get implements ReadStore.
func (c *core) Get(_ context.Context, name string) (*Watch, error) {
	if err := ValidateName(name); err != nil {
		return nil, fmt.Errorf("failed to get watch %q: %w", name, err)
	}
	w, err := c.load(name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, &notFoundError{name: name}
		}
		return nil, fmt.Errorf("failed to get watch %q: %w", name, err)
	}
	return w, nil
}

// Source implements ReadStore.
func (c *core) Source(_ context.Context, name string) (*Source, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	data, rev, err := c.b.read(rel(name))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, &notFoundError{name: name}
		}
		return nil, fmt.Errorf("failed to read watch %q: %w", name, err)
	}
	return &Source{Name: name, Text: string(data), Rev: rev}, nil
}

// caseVariantError says a differently-cased file would be the same file on a
// case-insensitive filesystem, so a write is refused rather than risk it.
type caseVariantError struct{ want, have string }

func (e *caseVariantError) Error() string {
	return fmt.Sprintf("%s exists with different capitalisation (%s); on a case-insensitive filesystem a write would replace it. Rename or remove it first", e.have, e.want)
}
func (e *caseVariantError) Is(target error) bool { return target == ErrConflict }

// checkCaseVariant refuses a write when the folder holds a file whose name
// equals the target's ignoring case but not exactly.
func (c *core) checkCaseVariant(name string) error {
	names, err := c.b.names()
	if err != nil {
		return err
	}
	want := name + Extension
	for _, fn := range names {
		if fn.name != want && strings.EqualFold(fn.name, want) {
			return &caseVariantError{want: want, have: fn.name}
		}
	}
	return nil
}

// refuseUnreadable keeps a store from replacing a file it could not parse. A
// caller that names the revision of that very file (the one a listing showed
// with its error) may replace it: the write is still compare-and-write.
func (c *core) refuseUnreadable(name string, w writeOpts, expect string) error {
	if w.overwrite {
		return nil
	}
	cur, curRev, err := c.b.read(rel(name))
	if err != nil {
		return nil
	}
	if expect != "" && expect == curRev {
		return nil
	}
	if _, derr := Decode(name, cur); derr != nil {
		return fmt.Errorf("failed to write watch %q: not saved, because that would replace a file that could not be read (%s); fix the file first or overwrite it", name, errorText(derr))
	}
	return nil
}

// Put implements Store.
func (c *core) Put(_ context.Context, w *Watch, opts ...WriteOption) error {
	wo := applyWriteOpts(opts)
	data, err := Encode(w)
	if err != nil {
		return err
	}
	if err := c.checkCaseVariant(w.Name); err != nil {
		return fmt.Errorf("failed to write watch %q: %w", w.Name, err)
	}
	if err := c.refuseUnreadable(w.Name, wo, wo.expect(w.Rev)); err != nil {
		return err
	}
	rev, err := c.b.write(rel(w.Name), data, wo.expect(w.Rev), wo.overwrite)
	if err != nil {
		return fmt.Errorf("failed to write watch %q: %w", w.Name, err)
	}
	w.Rev, w.Error = rev, ""
	return nil
}

// PutSource implements Store.
func (c *core) PutSource(_ context.Context, name string, data []byte, opts ...WriteOption) (string, error) {
	wo := applyWriteOpts(opts)
	if _, err := Decode(name, data); err != nil {
		return "", err
	}
	if err := c.checkCaseVariant(name); err != nil {
		return "", fmt.Errorf("failed to write watch %q: %w", name, err)
	}
	if err := c.refuseUnreadable(name, wo, wo.expect("")); err != nil {
		return "", err
	}
	rev, err := c.b.write(rel(name), data, wo.expect(""), wo.overwrite)
	if err != nil {
		return "", fmt.Errorf("failed to write watch %q: %w", name, err)
	}
	return rev, nil
}

// Delete implements Store.
func (c *core) Delete(_ context.Context, name string, opts ...WriteOption) error {
	wo := applyWriteOpts(opts)
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("failed to delete watch %q: %w", name, err)
	}
	if _, _, err := c.b.read(rel(name)); err != nil && errors.Is(err, ErrNotFound) {
		return &notFoundError{name: name}
	}
	if err := c.b.remove(rel(name), wo.rev, wo.overwrite); err != nil {
		return fmt.Errorf("failed to delete watch %q: %w", name, err)
	}
	return nil
}
