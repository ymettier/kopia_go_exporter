#! /bin/bash

set -x

if ! command -v kopia &>/dev/null; then
  echo "Command kopia not found. Please install kopia to run these tests"
  exit 1
fi

testdir=$1
if [ ! -d "${testdir}" ]; then
  echo "Tmpdir '${testdir}' does not exist. Exit"
  exit 1
fi

clean_up() {
  kill -TERM $PID
  wait $PID
  exit 0
}

# Set traps for SIGTERM and SIGINT
trap 'clean_up' SIGTERM SIGINT

kopia repository create filesystem --path="${testdir}/repo" -c -p kopia --cache-directory="${testdir}/cache" --no-check-for-updates

sed -e 's#KOPIA_GO_EXPORTER_DIR#'"${testdir}"'#' repository.config.template >"${testdir}/repo.config"

kopia --config-file="${testdir}/repo.config" snapshot create -p kopia $(pwd) --start-time="2025-05-01 15:20:01 CET" --end-time="2025-05-01 16:10:02 CET"

kopia --config-file="${testdir}/repo.config" server start -p kopia \
  --server-username=kopia \
  --server-password=Kopia \
  --tls-generate-cert \
  --tls-cert-file "${testdir}/my.cert" \
  --tls-key-file "${testdir}/my.key" &
PID=$!

wait "${PID}"
