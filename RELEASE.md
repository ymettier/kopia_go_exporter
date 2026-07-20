# Release Workflow

## Release kopia-go-exporter

1. **Set the version**:
    ```sh
    VERSION="v0.2.0"
    ```

2. **Create a release branch**:
    ```sh
    git switch main && git pull
    git switch -c "prepare-release-${VERSION}"
    ```
3. **Update `version.txt`** with the new version (without the `v` prefix):
    ```sh
    echo "${VERSION#v}" > version.txt
    cat version.txt
    ```
4. **Update the container image tag in `README.md`** to match the new version (without the `v` prefix):
    ```sh
    sed -i "s|ghcr\.io/ymettier/kopia_go_exporter:[0-9]\+\.[0-9]\+\.[0-9]\+\(-[a-z]*[0-9]\+\)\?|ghcr.io/ymettier/kopia_go_exporter:${VERSION#v}|g" README.md
    ```
5. **Commit**:
    ```sh
    git add version.txt README.md
    git commit -m "release: ${VERSION}"
    ```
6. **Create a PR**:
    ```sh
    git push
    gh pr create --title "Prepare release ${VERSION}" --body "Update version.txt and README.md for release ${VERSION}."
    # Alternative without gh CLI:
    xdg-open "https://github.com/ymettier/kopia_go_exporter/compare/prepare-release-${VERSION}?title=Prepare%20release%20${VERSION}&body=Update%20version.txt%20and%20README.md%20for%20release%20${VERSION}."
    ```
7. **Merge the PR**.
8. **Create and push the tag**:
    ```sh
    git switch main && git pull
    git tag "${VERSION}"
    git push origin "${VERSION}"
    ```
9. **Build and publish**: The CI workflow creates a draft release with binary artifacts on the [GitHub releases page](https://github.com/ymettier/kopia_go_exporter/releases) and publishes the container image to `ghcr.io/ymettier/kopia_go_exporter:<version>`.
10. **Publish the release**: review the release draft on the [GitHub releases page](https://github.com/ymettier/kopia_go_exporter/releases) and publish it.

## Release the helm chart

1. **Set the helm chart version**:
    ```sh
    HELM_VERSION="v0.1.0"
    ```

2. **Create a release branch**:
    ```sh
    git switch main && git pull
    git switch -c "prepare-helm-release-${HELM_VERSION}"
    ```
3. **Update `charts/kopia_go_exporter/Chart.yaml`** with the new version:
    ```sh
    # Update chart version
    VERSION_PLAIN="${HELM_VERSION#v}"
    sed -i "s/^version: .*/version: ${VERSION_PLAIN}/" charts/kopia_go_exporter/Chart.yaml
    ```
4. **Set `appVersion` in Chart.yaml** from the application release:
    ```sh
    # appVersion should match the latest kopia-go-exporter release (from version.txt)
    APP_VERSION=$(cat version.txt)
    sed -i "s/^appVersion: .*/appVersion: \"${APP_VERSION}\"/" charts/kopia_go_exporter/Chart.yaml
    ```
5. **Update `README.md`** helm install command with the new chart version:
    ```sh
    sed -i -E "s|--version [0-9]+.[0-9]+.[0-9]+|--version ${HELM_VERSION#v}|g" README.md
    ```
6. **Commit**:
    ```sh
    git add charts/kopia_go_exporter/Chart.yaml README.md
    git commit -m "release(helm): ${HELM_VERSION}"
    ```
7. **Create a PR**:
    ```sh
    git push
    gh pr create --title "Prepare helm release ${HELM_VERSION}" --body "Update Chart.yaml and README.md for helm release ${HELM_VERSION}."
    # Alternative without gh CLI:
    xdg-open "https://github.com/ymettier/kopia_go_exporter/compare/prepare-helm-release-${HELM_VERSION}?title=Prepare%20helm%20release%20${HELM_VERSION}&body=Update%20Chart.yaml%20and%20README.md%20for%20helm%20release%20${HELM_VERSION}."
    ```
8. **Merge the PR**.
9. **Push the helm chart tag**:
    ```sh
    git switch main && git pull
    git tag "helm-${HELM_VERSION}"
    git push origin "helm-${HELM_VERSION}"
    ```
   The CI workflow publishes the chart to `ghcr.io/ymettier/charts/kopia-go-exporter` on `helm-v*` tags.

