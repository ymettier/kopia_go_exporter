# Release Workflow

## Steps

1. **Update `version.txt`** with the new version:
   ```sh
   echo "0.2.0" > version.txt
   ```
2. **Update the container image tag in `README.md`** to match the new version.
3. **Commit**:
   ```sh
   git add version.txt README.md
   git commit -m "release: 0.2.0"
   ```
4. **Create and push the tag**:
   ```sh
   git tag 0.2.0
   git push origin 0.2.0
   ```
5. **Build and publish**: The CI workflow creates a draft release with binary artifacts on the [GitHub releases page](https://github.com/ymettier/kopia-go-exporter/releases) and publishes the container image to `ghcr.io/ymettier/kopia_go_exporter:<version>`.
6. **Publish the release**: review the release draft on the [GitHub releases page](https://github.com/ymettier/kopia-go-exporter/releases) and publish it.
