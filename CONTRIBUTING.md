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
