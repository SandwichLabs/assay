package watches

import "context"

// Store is where a workspace keeps its watches: one yaml file each under
// watches/. The FileStore keeps them in the folder; the Mock keeps them in
// memory for tests of packages that depend on one.
//
// Every watch carries Rev, the content hash of its one file as loaded. A write
// compares it against the file: a stale one is ErrConflict and changes
// nothing. A file that cannot be read still lists, under its file stem, with
// Error set; Get refuses it (ErrInvalid) and a write over it needs Overwrite.
type Store interface {
	ReadStore

	// Put validates and writes a watch. With neither a revision (the watch's
	// Rev or WithRev) nor Overwrite the write only creates: an existing file
	// is ErrConflict. On success the watch's Rev is updated.
	Put(ctx context.Context, w *Watch, opts ...WriteOption) error

	// PutSource validates file bytes with the strict decoder and writes them
	// exactly as given (comments and layout kept), under the same revision
	// rules as Put. It is how a bundle import keeps a hand-written file byte
	// for byte. It returns the new revision.
	PutSource(ctx context.Context, name string, data []byte, opts ...WriteOption) (rev string, err error)

	// Delete removes a watch; a missing name is ErrNotFound. With WithRev a
	// file that changed since is ErrConflict; without one it deletes whatever
	// is there.
	Delete(ctx context.Context, name string, opts ...WriteOption) error
}

// ReadStore is the read half of a Store. It is what a surface that must never
// change a watch (an agent's list and get tools) is given: the write methods
// are not part of its type.
type ReadStore interface {
	// List returns every watch ordered by name; never nil. An unreadable file
	// is listed with Error set, never hidden. More than MaxFiles files lists
	// the first MaxFiles and a stub saying how many were left out.
	List(ctx context.Context) ([]Watch, error)

	// Get returns one watch. A missing name satisfies
	// errors.Is(err, ErrNotFound); an unreadable or invalid file is an error
	// wrapping ErrInvalid.
	Get(ctx context.Context, name string) (*Watch, error)

	// Source returns a file's text exactly as stored, valid or not, with its
	// revision: what an editor or `slaw watch show` displays, so a broken
	// file can be read and fixed. A missing file is ErrNotFound.
	Source(ctx context.Context, name string) (*Source, error)
}

// Source is one file's text as stored.
type Source struct {
	Name string `json:"name"`
	Text string `json:"text"`
	Rev  string `json:"rev"`
}

// TruncatedStubName (never a valid watch name) names the stub a listing ends
// with when the folder holds more than MaxFiles files. Its Error is always set.
const TruncatedStubName = "(truncated)"

// WriteOption tunes one write.
type WriteOption func(*writeOpts)

type writeOpts struct {
	overwrite bool
	rev       string
	hasRev    bool
}

// WithOverwrite skips the conflict check ("overwrite" in the conflict banner),
// and is the only way to write over a file that could not be read.
func WithOverwrite() WriteOption { return func(o *writeOpts) { o.overwrite = true } }

// WithRev sets the revision the caller loaded, overriding the watch's Rev.
// WithRev("") says the caller loaded nothing: the write only creates.
func WithRev(rev string) WriteOption {
	return func(o *writeOpts) { o.rev, o.hasRev = rev, true }
}

func applyWriteOpts(opts []WriteOption) writeOpts {
	var o writeOpts
	for _, f := range opts {
		f(&o)
	}
	return o
}

// expect is the revision a write compares against.
func (o writeOpts) expect(itemRev string) string {
	if o.hasRev {
		return o.rev
	}
	return itemRev
}
