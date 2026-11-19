package process

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// profileLeaseTokenEnv carries the lease token into the browser's environment;
// every helper inherits it, which proves a process belongs to this lease.
const profileLeaseTokenEnv = "ARTEMIS_PROFILE_LEASE"

// profileLease is the content of a profile lease file: the owning process with
// its start identity, the random token its browser carries and the browser's
// process group once it started. A lease written before tokens existed has an
// owner and maybe a group only.
type profileLease struct {
	owner      int
	ownerStart string
	token      string
	group      int
}

// newProfileLease describes a lease held by this process.
func newProfileLease() (profileLease, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return profileLease{}, fmt.Errorf("profile lease token: %w", err)
	}
	start, err := processStartID(os.Getpid())
	if err != nil {
		return profileLease{}, fmt.Errorf("profile lease owner identity: %w", err)
	}
	return profileLease{owner: os.Getpid(), ownerStart: start, token: hex.EncodeToString(raw)}, nil
}

func (l profileLease) encode() []byte {
	var out strings.Builder
	fmt.Fprintf(&out, "owner=%d\n", l.owner)
	if l.ownerStart != "" {
		fmt.Fprintf(&out, "owner_start=%s\n", l.ownerStart)
	}
	if l.token != "" {
		fmt.Fprintf(&out, "token=%s\n", l.token)
	}
	if l.group > 0 {
		fmt.Fprintf(&out, "group=%d\n", l.group)
	}
	return []byte(out.String())
}

// parseProfileLease reads both the keyed form and the legacy form of bare
// owner and group numbers.
func parseProfileLease(raw []byte) profileLease {
	var lease profileLease
	fields := strings.Fields(string(raw))
	for i, field := range fields {
		key, value, keyed := strings.Cut(field, "=")
		if !keyed {
			number, err := strconv.Atoi(field)
			if err != nil {
				continue
			}
			switch i {
			case 0:
				lease.owner = number
			case 1:
				lease.group = number
			}
			continue
		}
		switch key {
		case "owner":
			lease.owner = leaseNumber(value)
		case "owner_start":
			lease.ownerStart = value
		case "token":
			lease.token = value
		case "group":
			lease.group = leaseNumber(value)
		}
	}
	return lease
}

// leaseNumber is a positive pid or group, or 0 for anything else.
func leaseNumber(value string) int {
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return 0
	}
	return number
}

// readProfileLease reads the lease file of a started browser.
func readProfileLease(path string) (profileLease, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the fixed lease file inside the leased profile.
	if err != nil {
		return profileLease{}, fmt.Errorf("read profile lease: %w", err)
	}
	return parseProfileLease(raw), nil
}

// browserEnvironment is the environment the leased browser starts with: the
// inherited one plus the lease token.
func browserEnvironment(lease profileLease) []string {
	env := os.Environ()
	if lease.token != "" {
		env = append(env, profileLeaseTokenEnv+"="+lease.token)
	}
	return env
}
