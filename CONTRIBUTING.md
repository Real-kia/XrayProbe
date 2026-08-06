# Contributing

1. Run `go test ./...` and `go vet ./...` before opening a pull request.
2. Do not add real share links, credentials, subscription URLs, or private endpoints to fixtures.
3. Keep temporary files, network clients, and child processes bounded by context cancellation.
4. New output fields should be added compatibly to the versioned JSON result schema.
5. Parser changes should include redacted unit fixtures for valid and invalid links.

Release maintainers create a `vX.Y.Z` tag. GitHub Actions runs tests and GoReleaser publishes checksums and platform archives.
