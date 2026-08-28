package pitcrew

import _ "embed"

// MaximsText is the canonical MAXIMS.md embedded byte-for-byte at build time.
//
//go:embed MAXIMS.md
var MaximsText string

// InstallerScript is the canonical runtime installer embedded byte-for-byte at
// build time so the CLI can install PitCrew without a source checkout.
//
//go:embed scripts/install-templates.sh
var InstallerScript []byte
