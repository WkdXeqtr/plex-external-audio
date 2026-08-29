# Code signing policy

Releases are not signed yet.
An application to [SignPath Foundation](https://signpath.org), who provide free certificates to open source projects, is planned; this policy exists because they require one to be published, and because it is worth writing down regardless.

Until a certificate is in place, SmartScreen will call the installer an unknown publisher.
The only check available meanwhile is the SHA-256 checksum published beside each release, which can be compared against the public build log of the run that produced it.

## Team roles

This is a one-person project, so one person holds all three roles: [WkdXeqtr](https://github.com/WkdXeqtr) is the only author with commit access, the only reviewer of changes proposed from outside, and the only person who may approve a release for signing.
Should anyone else join, this document is updated before they are given access.

## Accounts

Multi-factor authentication is required on every account that can reach the source or raise a signing request.

## Where the binaries come from

Released binaries are never built on a developer's machine.
Every release is produced by the `release` workflow in this repository, on a GitHub-hosted runner, from the source at the tag that triggered it.
The run is public and its log is kept, and a SHA-256 checksum of the installer is published beside it.

## No upstream to review

This is not a fork.
It carries no third-party code beyond its Go dependencies, which are pinned in `go.sum`.

## Privacy

The program sends nothing anywhere; see [PRIVACY.md](PRIVACY.md).
