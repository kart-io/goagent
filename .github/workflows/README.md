# GitHub Workflows

This directory contains GitHub Actions workflows for CI/CD automation.

## Workflows

### 🔄 CI (`ci.yml`)

**Trigger**: Push to `main`/`develop`, Pull Requests

**Purpose**: Continuous Integration

**Actions**:
- Run tests with race detection across Go 1.21, 1.22, 1.23
- Verify import layering compliance
- Run linters (golangci-lint)
- Build for multiple platforms
- Upload coverage to Codecov

**Usage**: Automatically runs on every push and PR

---

### 🏷️ Auto Tag (`auto-tag.yml`)

**Trigger**:
- Push to `master` branch (automatic)
- Manual dispatch via GitHub Actions UI

**Purpose**: Automatic version tagging based on conventional commits

**Actions**:
- Analyze commits since last tag
- Determine version bump type (major, minor, patch) based on commit messages
- Calculate new version number
- Generate comprehensive changelog
- Create and push new tag
- Trigger release workflow

**Supported Commit Types**:
- `feat:` → MINOR version bump (v1.0.0 → v1.1.0)
- `fix:`, `docs:`, `test:`, etc. → PATCH version bump (v1.0.0 → v1.0.1)
- `feat!:` or `BREAKING CHANGE:` → MAJOR version bump (v1.0.0 → v2.0.0)

**Usage (Automatic)**:
```bash
# Commits following conventional commit format automatically trigger releases
git commit -m "feat(llm): add Claude 3.5 Sonnet support"
git push origin master
# → Auto creates tag v1.1.0 (if current version is v1.0.0)
```

**Usage (Manual)**:
```bash
# Navigate to Actions → Auto Tag and Release → Run workflow
# Select version bump type: auto, major, minor, or patch
```

**See Also**: [VERSION_MANAGEMENT.md](../../VERSION_MANAGEMENT.md) for detailed documentation

---

### 🚀 Release (`release.yml`)

**Trigger**: Push tags matching `v*.*.*` (usually triggered by auto-tag workflow)

**Purpose**: Automated release creation and distribution

**Actions**:
- Run full test suite
- Verify import layering
- Build binaries for:
  - Linux (AMD64, ARM64)
  - macOS (AMD64, ARM64/Apple Silicon)
  - Windows (AMD64)
- Generate SHA256 checksums
- Create GitHub Release with binaries and release notes
- Publish to pkg.go.dev

**Usage (Automatic)**:
The auto-tag workflow automatically triggers this when it creates a new tag.

**Usage (Manual - Advanced)**:
```bash
# Create and push a tag manually
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3
```

**Pre-releases**:
```bash
# Alpha release (manual only)
git tag -a v1.3.0-alpha.1 -m "Alpha release v1.3.0-alpha.1"
git push origin v1.3.0-alpha.1

# Beta release (manual only)
git tag -a v1.3.0-beta.1 -m "Beta release v1.3.0-beta.1"
git push origin v1.3.0-beta.1

# Release candidate (manual only)
git tag -a v1.3.0-rc.1 -m "Release candidate v1.3.0-rc.1"
git push origin v1.3.0-rc.1
```

---

### 🔍 Pull Request (`pr.yml`)

**Trigger**: Pull Request events

**Purpose**: PR validation and feedback

**Actions**:
- Check code formatting
- Verify import layering (strict mode)
- Run go vet
- Run tests with coverage
- Validate coverage ≥ 80%
- Run security scanner (Gosec)
- Post coverage report as PR comment

**Coverage Report Example**:
```
## 📊 Test Coverage Report

**Coverage**: 85.3%
✅ Meets minimum threshold (80%)

---

### Checklist
- [x] All tests pass
- [x] Code is properly formatted
- [x] Import layering rules satisfied
- [x] Test coverage ≥ 80%
- [x] All linter checks pass
```

---

### 🌙 Nightly (`nightly.yml`)

**Trigger**: Daily at 2 AM UTC, or manual dispatch

**Purpose**: Nightly builds and monitoring

**Actions**:
- Run full test suite
- Execute benchmarks
- Check for dependency updates
- Upload artifacts (benchmarks, coverage)
- Create issue on failure

**Manual Trigger**:
Go to Actions → Nightly Build → Run workflow

---

## Quick Reference

### Recommended Release Workflow (Automatic)

**For most releases, use the automatic workflow:**

1. **Follow Conventional Commit format**:
   ```bash
   # New feature
   git commit -m "feat(llm): add Claude 3.5 Sonnet support"

   # Bug fix
   git commit -m "fix(retry): handle crypto/rand failure"

   # Breaking change
   git commit -m "feat(core)!: redesign middleware system

   BREAKING CHANGE: Middleware signature changed"
   ```

2. **Push to master**:
   ```bash
   git push origin master
   ```

3. **Automatic release**:
   - Auto-tag workflow analyzes commits
   - Creates appropriate version tag (v1.x.x)
   - Release workflow builds and publishes

4. **Monitor**:
   - Check [Actions](https://github.com/kart-io/goagent/actions)
   - Verify [Release](https://github.com/kart-io/goagent/releases)

### Manual Release (Advanced)

**Only use this for special cases (hotfixes, pre-releases, etc.)**:

1. **Update CHANGELOG.md** (optional):
   ```bash
   vim CHANGELOG.md
   ```

2. **Run pre-flight checks**:
   ```bash
   make test
   ./scripts/verify_imports.sh
   make lint
   ```

3. **Create and push tag manually**:
   ```bash
   git tag -a v1.2.3 -m "Release v1.2.3"
   git push origin v1.2.3
   ```

4. **Or manually trigger auto-tag workflow**:
   - Go to Actions → Auto Tag and Release → Run workflow
   - Select version bump type (major, minor, patch)

### Checking Workflow Status

```bash
# View workflow runs
gh run list

# Watch a specific run
gh run watch

# View logs
gh run view <run-id> --log
```

### Secrets Required

The following secrets should be configured in repository settings:

- `CODECOV_TOKEN` (optional) - For coverage uploads
- `GITHUB_TOKEN` - Automatically provided by GitHub

---

## Workflow Files

| File | Description | Triggers |
|------|-------------|----------|
| `ci.yml` | Main CI pipeline | Push, PR |
| `auto-tag.yml` | Automatic version tagging | Push to master, manual |
| `release.yml` | Automated releases | Tag push (v*.*.*) |
| `pr.yml` | PR validation | PR events |
| `nightly.yml` | Nightly builds | Schedule, manual |

---

## Best Practices

### Before Creating a PR

```bash
# Format code
make fmt

# Run tests locally
make test

# Verify imports
./verify_imports.sh

# Run linter
make lint
```

### Before Creating a Release

```bash
# All of the above, plus:

# Update CHANGELOG.md
vim CHANGELOG.md

# Update version references in docs
# (if applicable)

# Use the release script
./scripts/create_release.sh <version>
```

---

## Troubleshooting

### CI Failed

1. Check the Actions tab for error details
2. Run the same commands locally
3. Fix issues and push again

### Release Failed

1. Check workflow logs
2. Delete the tag if needed:
   ```bash
   git tag -d v1.2.3
   git push origin :refs/tags/v1.2.3
   ```
3. Fix issues
4. Create new tag with incremented patch version

### Coverage Below Threshold

Add more tests to increase coverage:
```bash
# Check current coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Import Layering Violation

Fix the import structure:
```bash
# Check violations
./verify_imports.sh

# See ARCHITECTURE.md for layer rules
```

---

## Additional Resources

- **[Version Management Guide](../../VERSION_MANAGEMENT.md)** - Comprehensive guide on automatic versioning and releases
- [Architecture Documentation](../docs/architecture/ARCHITECTURE.md)
- [Import Layering Rules](../docs/architecture/IMPORT_LAYERING.md)
- [Conventional Commits Specification](https://www.conventionalcommits.org/)
- [Semantic Versioning](https://semver.org/)
- [GitHub Actions Documentation](https://docs.github.com/en/actions)

---

## Support

For issues with workflows:
1. Check workflow logs in the Actions tab
2. Consult this README
3. Review `.github/RELEASE.md`
4. Open an issue with the `ci` label
