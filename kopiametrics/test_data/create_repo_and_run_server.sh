#! /bin/bash

testdir="$(mktemp -d /tmp/kopia-go-exporter-testdir-XXXXXX)"

kopia repository create filesystem --path="${testdir}/repo" -c -p kopia --cache-directory="${testdir}/cache" --no-check-for-updates

sed -e 's#KOPIA_GO_EXPORTER_DIR#'"${testdir}"'#' repository.config.template >"${testdir}/repo.config"

kopia --config-file="${testdir}/repo.config" snapshot create -p kopia $(pwd) --start-time="2025-05-01 15:20:01 CET" --end-time="2025-05-01 16:10:02 CET"
kopia --config-file="${testdir}/repo.config" server start -p kopia --server-username=kopia --server-password=Kopia --insecure

echo "rm -rf ${testdir}"
