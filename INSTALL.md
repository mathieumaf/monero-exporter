# Install

## Container image (recommended)

Multi-arch images (`linux/amd64`, `linux/arm64`) are published to GHCR on every
release:

```bash
docker pull ghcr.io/mathieumaf/monero-exporter:latest
# or pin a version:
docker pull ghcr.io/mathieumaf/monero-exporter:v0.1.0
```

The images are signed with [cosign] (keyless). You can verify provenance with:

```bash
cosign verify ghcr.io/mathieumaf/monero-exporter:latest \
  --certificate-identity-regexp 'https://github.com/mathieumaf/monero-exporter/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Using Go

With a recent [Go] toolchain you can build and install from source into
`$GOPATH/bin` (installs the latest tagged release):

```console
$ go install github.com/mathieumaf/monero-exporter/cmd/monero-exporter@latest

$ monero-exporter --help
Prometheus exporter for monero metrics
...
```

## From the releases page

Pre-compiled archives for each platform are attached to every release on the
[releases page], alongside a `checksums.txt`.

```bash
export VERSION=0.1.0

# download the archive for your platform (e.g. linux/amd64) and the checksums
curl -SOL https://github.com/mathieumaf/monero-exporter/releases/download/v${VERSION}/monero-exporter_${VERSION}_linux_amd64.tar.gz
curl -SOL https://github.com/mathieumaf/monero-exporter/releases/download/v${VERSION}/checksums.txt

# verify the download matches the published checksum
sha256sum --ignore-missing -c checksums.txt

# unpack and install
tar xzf monero-exporter_${VERSION}_linux_amd64.tar.gz
mv ./monero-exporter /usr/local/bin

monero-exporter version
```

[Go]: https://golang.org/dl/
[cosign]: https://github.com/sigstore/cosign
[releases page]: https://github.com/mathieumaf/monero-exporter/releases
