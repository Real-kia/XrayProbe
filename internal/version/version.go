package version

// Value is overridden at release build time via -ldflags
// "-X github.com/Real-kia/XrayProbe/internal/version.Value=...".
// It stays "dev" for `go install` and local builds.
var Value = "dev"
