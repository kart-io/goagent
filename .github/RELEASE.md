# Release Management

This document describes the release process for GoAgent.

## Versioning

GoAgent follows [Semantic Versioning 2.0.0](https://semver.org/):

- **MAJOR** version: Incompatible API changes
- **MINOR** version: New functionality in a backward-compatible manner
- **PATCH** version: Backward-compatible bug fixes

### Version Format

- Release versions: `v1.2.3`
- Pre-release versions:
  - Alpha: `v1.2.3-alpha.1`
  - Beta: `v1.2.3-beta.1`
  - Release Candidate: `v1.2.3-rc.1`

## Release Process

### 1. Prepare for Release

Before creating a release, ensure:

1. All tests pass locally:
   ```bash
   make test
   ```

2. Import layering is valid:
   ```bash
   ./verify_imports.sh
   ```

3. Code is properly formatted:
   ```bash
   make fmt
   ```

4. Linter passes:
   ```bash
   make lint
   ```

5. Update CHANGELOG.md with release notes
6. Update version references in documentation

### 2. Create a Release Tag

#### For a Standard Release

```bash
# Make sure you're on the main branch
git checkout main
git pull origin main

# Create and push the tag
git tag -a v1.2.3 -m "Release v1.2.3

- Feature: Add new reasoning patterns
- Fix: Improve error handling
- Docs: Update API documentation"

git push origin v1.2.3
```

#### For a Pre-release

```bash
# For alpha
git tag -a v1.3.0-alpha.1 -m "Release v1.3.0-alpha.1 - Early preview"
git push origin v1.3.0-alpha.1

# For beta
git tag -a v1.3.0-beta.1 -m "Release v1.3.0-beta.1 - Feature complete"
git push origin v1.3.0-beta.1

# For release candidate
git tag -a v1.3.0-rc.1 -m "Release v1.3.0-rc.1 - Final testing"
git push origin v1.3.0-rc.1
```

### 3. Automated Release Process

When you push a tag, GitHub Actions automatically:

1. Runs all tests with race detection
2. Verifies import layering compliance
3. Builds binaries for multiple platforms:
   - Linux (AMD64, ARM64)
   - macOS (AMD64, ARM64)
   - Windows (AMD64)
4. Generates SHA256 checksums
5. Creates a GitHub Release with:
   - Release notes (from CHANGELOG.md or auto-generated)
   - Binary artifacts
   - Checksums file
6. Publishes to pkg.go.dev

### 4. Monitor the Release

1. Check the [Actions tab](https://github.com/kart-io/goagent/actions) to monitor the release workflow
2. Verify the [Releases page](https://github.com/kart-io/goagent/releases) shows the new release
3. Confirm the package appears on [pkg.go.dev](https://pkg.go.dev/github.com/kart-io/goagent)

### 5. Post-Release

1. Announce the release in:
   - Project README
   - Discussion boards
   - Social media (if applicable)

2. Update documentation website (if applicable)

3. Close related milestone on GitHub

## Emergency Hotfix Release

For critical bugs that need immediate fixes:

```bash
# Create a hotfix branch from the tag
git checkout v1.2.3
git checkout -b hotfix/v1.2.4

# Make your fixes
git add .
git commit -m "Fix critical bug in agent execution"

# Merge to main
git checkout main
git merge hotfix/v1.2.4

# Create and push tag
git tag -a v1.2.4 -m "Hotfix v1.2.4 - Fix critical execution bug"
git push origin main
git push origin v1.2.4

# Clean up
git branch -d hotfix/v1.2.4
```

## Yanking a Release

If a release has critical issues:

1. Delete the tag locally and remotely:
   ```bash
   git tag -d v1.2.3
   git push origin :refs/tags/v1.2.3
   ```

2. Delete the GitHub Release from the web interface

3. Create a new patched release immediately

## Version Compatibility

### Go Version Support

We officially support the **last 3 major Go versions**. For example, if the latest is Go 1.23:
- ✅ Go 1.23 (latest)
- ✅ Go 1.22
- ✅ Go 1.21
- ⚠️ Older versions may work but are not tested

### Breaking Changes

When introducing breaking changes:

1. **Deprecation Period**: Mark features as deprecated in a minor release
2. **Documentation**: Clearly document migration path
3. **Examples**: Provide before/after code examples
4. **Major Version**: Remove deprecated features in next major version

### API Stability

- **Stable**: `v1.x.x` - Production ready, no breaking changes in minor/patch versions
- **Beta**: `v1.x.x-beta.x` - API may change, not recommended for production
- **Alpha**: `v1.x.x-alpha.x` - Experimental, API will likely change

## Changelog Guidelines

Update `CHANGELOG.md` before each release following [Keep a Changelog](https://keepachangelog.com/):

```markdown
## [1.2.3] - 2024-01-15

### Added
- New Graph-of-Thought (GoT) reasoning pattern
- Support for parallel thought execution

### Changed
- Improved error messages in agent execution
- Updated dependencies

### Fixed
- Fixed race condition in ToT agent
- Corrected import layering violations

### Deprecated
- `OldMethod()` - Use `NewMethod()` instead

### Removed
- Removed deprecated `LegacyAgent`

### Security
- Updated crypto dependencies to address CVE-2024-XXXX
```

## Useful Commands

```bash
# List all tags
git tag -l

# Show tag details
git show v1.2.3

# List tags by pattern
git tag -l "v1.2.*"

# Delete local tag
git tag -d v1.2.3

# Delete remote tag
git push origin :refs/tags/v1.2.3

# Fetch all tags
git fetch --tags

# Check out a specific version
git checkout v1.2.3

# See what's changed since last tag
git log $(git describe --tags --abbrev=0)..HEAD --oneline
```

## Release Checklist

Use this checklist for each release:

- [ ] All tests pass locally
- [ ] Import layering verified
- [ ] Code formatted (`make fmt`)
- [ ] Linter passes (`make lint`)
- [ ] CHANGELOG.md updated
- [ ] Version bumped in relevant files
- [ ] Documentation updated
- [ ] Tag created with proper message
- [ ] Tag pushed to origin
- [ ] GitHub Actions workflow succeeds
- [ ] Release appears on GitHub
- [ ] Package indexed on pkg.go.dev
- [ ] Release announcement posted

## Troubleshooting

### Workflow Failed

If the release workflow fails:

1. Check the Actions tab for error details
2. Fix the issue locally
3. Delete the tag: `git push origin :refs/tags/v1.2.3`
4. Increment the patch version
5. Create a new tag: `git tag -a v1.2.4 -m "Release v1.2.4"`
6. Push again: `git push origin v1.2.4`

### pkg.go.dev Not Updating

If the package doesn't appear on pkg.go.dev:

1. Wait 15-30 minutes (indexing takes time)
2. Try fetching explicitly:
   ```bash
   GOPROXY=proxy.golang.org go list -m github.com/kart-io/goagent@v1.2.3
   ```
3. Visit: `https://pkg.go.dev/github.com/kart-io/goagent@v1.2.3`

### Wrong Tag Pushed

If you pushed the wrong tag:

```bash
# Delete local and remote tag
git tag -d v1.2.3
git push origin :refs/tags/v1.2.3

# Delete the GitHub Release (from web interface)

# Create correct tag
git tag -a v1.2.3 -m "Correct release message"
git push origin v1.2.3
```

## References

- [Semantic Versioning](https://semver.org/)
- [Keep a Changelog](https://keepachangelog.com/)
- [GitHub Releases](https://docs.github.com/en/repositories/releasing-projects-on-github)
- [Go Modules](https://go.dev/blog/publishing-go-modules)
