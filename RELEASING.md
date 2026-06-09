# Releasing

Releases are fully automated. Pushing a `v*` tag triggers
[`.github/workflows/release.yml`](.github/workflows/release.yml), which:

1. runs **GoReleaser** to cross-compile the binaries, build the archives and
   checksums, and create the GitHub Release with an auto-generated changelog;
2. builds the **multi-arch container image** (`linux/amd64`, `linux/arm64`) and
   pushes it to `ghcr.io/mathieumaf/monero-exporter`, tagged `vX.Y.Z`,
   `X.Y` and (for non-prereleases) `latest`;
3. **signs the image** with cosign (keyless / OIDC).

No secrets need to be configured: the workflow authenticates to GHCR and to
sigstore with the repository's built-in `GITHUB_TOKEN` and OIDC identity.

## Cutting a release

```bash
# make sure master is green and up to date, then:
git tag v1.2.3
git push origin v1.2.3
```

Use a pre-release suffix (e.g. `v1.2.3-rc.1`) to publish a GitHub pre-release;
the `latest` image tag is skipped automatically for those.

## Testing locally

```bash
# build a snapshot of the binaries/archives without publishing anything
make snapshot          # -> goreleaser release --snapshot --clean (output in dist/)

# validate the GoReleaser config
goreleaser check

# build the image locally (single arch)
docker build -t monero-exporter:dev .
```
