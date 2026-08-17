# Contributing

LongHub Manager accepts focused bug fixes, tests and documentation updates.
Please open an issue before proposing a behavioral or architectural change.

All changes from non-maintainers must be submitted through a pull request and
reviewed by the project maintainer. Release workflow, installer, dependency and
code-signing changes require explicit maintainer approval. Contributors must
not commit credentials, signing keys, certificates, generated installers or
private Cloud Skill implementation details.

Run the required checks before submitting a change:

```powershell
go test ./...
go vet ./...
go build ./cmd/longhub-manager
```

By contributing, you agree that your contribution is licensed under the
Apache License 2.0 in this repository.
