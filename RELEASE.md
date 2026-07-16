# Release Workflow

## Steps

1. **Set the version**:
   ```sh
   VERSION="v0.2.0"
   echo "version=${VERSION}"
   ```

2. **Create a release branch**:
   ```sh
   echo "version=${VERSION}"
   git switch main && git pull
   git switch -c "prepare-release-${VERSION}"
   ```
3. **Update `version.txt`** with the new version (without the `v` prefix):
   ```sh
   echo "${VERSION#v}" > version.txt
   cat version.txt
   ```
4. **Update the container image tag in `README.md`** to match the new version (without the `v` prefix).
5. **Commit**:
   ```sh
   echo "version=${VERSION}"
   git add version.txt README.md
   git commit -m "release: ${VERSION}"
   ```
6. **Create a PR**:
   ```sh
   echo "version=${VERSION}"
   git push
   gh pr create --title "Prepare release ${VERSION}" --body "Update version.txt and README.md for release ${VERSION}."
   ```
7. **Merge the PR**.
8. **Create and push the tag**:
   ```sh
   echo "version=${VERSION}"
   git switch main && git pull
   git tag "${VERSION}"
   git push origin "${VERSION}"
   ```
9. **Build and publish**: The CI workflow creates a draft release with binary artifacts on the [GitHub releases page](https://github.com/ymettier/kopia_go_exporter/releases) and publishes the container image to `ghcr.io/ymettier/kopia_go_exporter:<version>`.
10. **Publish the release**: review the release draft on the [GitHub releases page](https://github.com/ymettier/kopia_go_exporter/releases) and publish it.
