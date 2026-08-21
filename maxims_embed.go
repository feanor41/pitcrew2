package pitcrew

import _ "embed"

// MaximsText is the canonical MAXIMS.md embedded byte-for-byte at build time.
//
//go:embed MAXIMS.md
var MaximsText string
