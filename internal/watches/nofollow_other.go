//go:build !unix

package watches

// oNoFollow does nothing where there is no O_NOFOLLOW; the folder check and the
// regular-file check still refuse links.
const oNoFollow = 0
