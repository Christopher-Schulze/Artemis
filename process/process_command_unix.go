//go:build darwin || linux

package process

import (
	"os"
	"os/exec"
	"strconv"
)

const processGuardianScript = `
set -eu
ulimit -c 0
owner="$1"
profile="$2"
remove_profile="$3"
shift 3
"$@" &
child=$!
trap '' TERM INT HUP
group=$$
/bin/sh -c '
  trap "" TERM INT HUP
  owner="$1"
  group="$2"
  profile="$3"
  remove_profile="$4"
  while kill -0 "$owner" 2>/dev/null; do sleep 0.2; done
  kill -TERM "-$group" 2>/dev/null || true
  sleep 1
  rm -f -- "$profile/DevToolsActivePort" "$profile/.artemis-profile.lock"
  if [ "$remove_profile" = "1" ]; then rm -rf -- "$profile"; fi
  self=$$
  ps -axo pid=,pgid= | while read -r pid pgid; do
    if [ "$pgid" = "$group" ] && [ "$pid" != "$self" ]; then
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done
' artemis-process-guardian "$owner" "$group" "$profile" "$remove_profile" &
guard=$!
set +e
wait "$child"
status=$?
set -e
kill -KILL "$guard" 2>/dev/null || true
wait "$guard" 2>/dev/null || true
exit "$status"
`

func newProcessCommand(binaryPath, profileDir string, removeProfile bool, args []string) *exec.Cmd {
	remove := "0"
	if removeProfile {
		remove = "1"
	}
	commandArgs := []string{"-c", processGuardianScript, "artemis-process-owner", strconv.Itoa(os.Getpid()), profileDir, remove, binaryPath}
	commandArgs = append(commandArgs, args...)
	return exec.Command("/bin/sh", commandArgs...)
}
