---
name: publish-windowsagent-release
description: "Validate, tag, publish, and verify a WindowsAgent GitHub Release through the repository's tag-driven workflow. Use when the user asks to publish, release, tag, or verify a public WindowsAgent executable release; do not use for live PC deployment or Rule-only distribution."
---

# Publish WindowsAgent Release

Publish one coherent WindowsAgent multi-process executable set through the
repository-owned GitHub Actions workflow. Keep release creation separate from
live Windows installation and acceptance.

## Hold the release boundary

The current authorities are:

- `.github/workflows/release.yml` for remote tests, build, packaging, and
  `gh release create`;
- `scripts/build-windows-capture-agent.sh`,
  `scripts/build-windows-assist-gui.ps1`, and
  `internal/releasecatalog.Specs` for the executable inventory, PE subsystem,
  catalog, and checksum contract; and
- `docs/design/assist-gui-release-distribution.md` for the public distribution
  model.

Publish every cataloged executable as an individual asset together with
`windowsagent-release.json`, `SHA256SUMS`, the runnable
`windows-assist-gui.zip`, and `windowsagent-user-skills.zip`. The AssistGUI
archive must contain the complete framework-dependent WinUI publish payload
plus the sibling `windows-assist-backend.exe`. The Skills archive must contain
exactly the repository-owned stable end-user Skills and their required files;
it must exclude repository developer Skills and experimental capabilities. Do
not create a Setup package or a complete-system archive. Do not treat the
individually published GUI apphost EXE as runnable without its adjacent payload
files.

The tag workflow owns release artifact production. Do not replace it with a
local `gh release create`, manually upload locally built binaries, or modify an
existing release to hide a failed run.

## Require release authorization

Creating and pushing a tag and publishing GitHub assets are external
mutations. Perform them only when the user explicitly asks to publish or
release. A request to build, test, commit, deploy to one PC, or prepare a
release does not authorize publication.

An authorized release includes pushing the intended commit and its new release
tag to `origin`; it does not authorize unrelated branch pushes, force-pushes,
tag replacement, release deletion, or live Windows installation.

## Resolve the release identity

Use an explicit user-supplied version when present. Otherwise derive the next
patch version from the highest existing stable `vMAJOR.MINOR.PATCH` tag and
state the selected tag before publishing. Do not infer a major, minor,
prerelease, or metadata change without user direction.

Require all of the following before tagging:

- the intended release changes are committed;
- `HEAD` is the exact commit to release;
- the tag does not exist locally, remotely, or as a GitHub Release;
- `origin` is the expected WindowsAgent repository; and
- GitHub CLI authentication can write repository contents and workflows.

Preserve unrelated dirty files. Do not stage them. If intended release changes
remain uncommitted or `HEAD` is ambiguous, stop rather than tagging the wrong
commit.

## Validate before publication

Run the portable validation locally before publication:

```bash
git diff --check
go test ./...
(cd runtimes/tailscale-adapter && GODEBUG=http2client=0 go test -mod=readonly ./...)
go run ./cmd/windows-action-check --rules-dir Rules
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
release_dir="$(mktemp -d)"
./scripts/build-windows-capture-agent.sh \
  --output-dir "$release_dir" \
  --version "${tag#v}" \
  --skip-assist-gui \
  --skip-catalog
python3 scripts/build-user-skills-bundle.py \
  --skills-dir .agents/skills \
  --output "$release_dir/windowsagent-user-skills.zip"
```

The tag workflow then builds the unpackaged framework-dependent WinUI frontend
on a Windows runner, combines its complete publish payload with the Go Assist
backend, generates the only complete executable catalog and checksum list, and
verifies the bootstrap ZIP against the WinUI publish output plus backend. When a
Windows build host with .NET 8 is available before tagging, additionally run
`scripts/build-windows-assist-gui.ps1` and then rerun the Go builder with
`--assist-gui-exe` to validate the complete local catalog. Report clearly when
that pre-tag Windows build was unavailable; do not substitute the retired Go
GUI. The Tailscale module test and build retain the repository's explicit
HTTP/2-disabled compatibility boundary; do not remove `GODEBUG=http2client=0`
or introduce HTTP/3 during release work.

Remove only the temporary directory created for this validation. A successful
local build is not a published release.

## Publish through the tag workflow

Push the exact release commit, create an annotated tag, and push only that tag:

```bash
git push origin HEAD
git tag -a "$tag" -m "$tag"
git push origin "$tag"
```

Find the workflow run whose `headBranch` is the tag and whose `headSha` is the
tagged commit. Follow that exact run with `gh run watch --exit-status`; do not
treat tag push or a queued workflow as completion.

If the run fails, preserve its URL, job, and exact failing step. Rerun only a
proven transient workflow failure for the same immutable commit. For a source,
test, catalog, packaging, or artifact defect, fix the repository and publish a
new version; never silently move a pushed release tag. Do not delete a partial
release or tag without explicit authorization.

## Verify the public result

After the workflow succeeds, read the release back through GitHub and require:

- the release is neither draft nor prerelease unless the user requested that;
- its tag and title match the selected version;
- its target commit is the intended commit;
- the asset set is exactly every cataloged `.exe` plus
  `windows-assist-gui.zip`, `windowsagent-user-skills.zip`,
  `windowsagent-release.json`, and `SHA256SUMS`;
- the catalog version and target are correct;
- `SHA256SUMS` covers the cataloged executables; and
- the downloaded bootstrap ZIP contains exactly the framework-dependent WinUI
  publish payload plus `windows-assist-backend.exe`; and
- the ZIP's GUI and backend EXEs each match their downloaded catalog entries.
- the Skills ZIP contains exactly `use-windows-pc`, `use-visual-log`, and
  `tailscale-one-off-auth-key`, including their referenced files, and contains
  no developer or experimental Skill.

Use a fresh temporary directory for downloaded verification assets. GitHub
workflow success alone is not public asset proof.

Do not claim Windows installability, Defender behavior, runtime health,
Tailscale enrollment, or GUI usability from release publication. Those require
separate signed-in Windows acceptance through `use-windows-pc` and, for runtime
changes, `maintain-windowsagent-runtime`.

## Report completion

Report the tag, commit, workflow conclusion and URL, GitHub Release URL,
verified asset count, bootstrap ZIP contents and digest result, and any checks
that were not run. State explicitly whether live Windows acceptance was or was
not part of the request.
