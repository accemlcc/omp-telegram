package omptelegram

import _ "embed"

// DefaultConfig is the bundled configuration used when no default file exists.
//
//go:embed config.toml
var DefaultConfig string
