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

### 🚀 Release (`release.yml`)

**Trigger**: Push tags matching `v*.*.*`

**Purpose**: Automated release creation

**Actions**:
- Run full test suite
- Verify import layering
- Build binaries for:
  - Linux (AMD64, ARM64)
  - macOS (AMD64, ARM64/Apple Silicon)
  - Windows (AMD64)
- Generate SHA256 checksums
- Create GitHub Release with binaries
- Publish to pkg.go.dev

**Usage**:
```bash
# Create and push a tag
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3

# Or use the helper script
./create_release.sh 1.2.3
```

**Pre-releases**:
```bash
# Alpha release
./create_release.sh 1.3.0 alpha

# Beta release
./create_release.sh 1.3.0 beta

# Release candidate
./create_release.sh 1.3.0 rc
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

### Creating a Release

1. **Update CHANGELOG.md**:
   ```bash
   # Add entry for new version
   vim CHANGELOG.md
   ```

2. **Run pre-flight checks**:
   ```bash
   make test
   ./verify_imports.sh
   make lint
   ```

3. **Create release**:
   ```bash
   # Interactive script (recommended)
   ./create_release.sh 1.2.3

   # Manual
   git tag -a v1.2.3 -m "Release v1.2.3

   - Feature: New reasoning patterns
   - Fix: Import layering issues
   - Docs: Comprehensive documentation"

   git push origin v1.2.3
   ```

4. **Monitor**:
   - Check [Actions](https://github.com/kart-io/goagent/actions)
   - Verify [Release](https://github.com/kart-io/goagent/releases)
   - Confirm [pkg.go.dev](https://pkg.go.dev/github.com/kart-io/goagent)

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
./create_release.sh <version>
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

- [Release Management Guide](.github/RELEASE.md)
- [Architecture Documentation](../docs/architecture/ARCHITECTURE.md)
- [Import Layering Rules](../docs/architecture/IMPORT_LAYERING.md)
- [GitHub Actions Documentation](https://docs.github.com/en/actions)

---

## Support

For issues with workflows:
1. Check workflow logs in the Actions tab
2. Consult this README
3. Review `.github/RELEASE.md`
4. Open an issue with the `ci` label
