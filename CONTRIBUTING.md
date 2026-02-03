# Contributing to amux

## Development

```bash
git clone https://github.com/andyrewlee/amux.git
cd amux
make run
```

## Release

The release workflow is tag-driven. Pushing a tag like `v0.0.5` triggers the GitHub Actions release job.

Fast path:

```bash
make release VERSION=v0.0.5
```

Manual steps:

```bash
make release-check
git tag -a v0.0.5 -m "v0.0.5"
git push origin v0.0.5
```

### Homebrew tap

The Homebrew tap lives in `andyrewlee/homebrew-amux` and auto-bumps the formula after a release.

- After `make release VERSION=vX.Y.Z`, the tap workflow updates `Formula/amux.rb` (daily at 06:00 UTC).
- To update immediately, run the **Bump amux formula** workflow in the tap repo.
- Users upgrade with `brew upgrade amux`.
