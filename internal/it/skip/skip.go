/*
 * Copyright (c) 2008-2022, Hazelcast, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License")
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package skip

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/hazelcast/hazelcast-commandline-client/internal"
)

// see also the environment variables in it/util.go

const (
	skipHzVersion     = "hz"
	skipClientVersion = "Ver"
	skipOS            = "os"
	skipArch          = "arch"
	skipEnterprise    = "enterprise"
	skipNotEnterprise = "!enterprise"
	skipOSS           = "oss"
	skipNotOSS        = "!oss"
	skipRace          = "race"
	skipNotRace       = "!race"
	skipSSL           = "ssl"
	skipNotSSL        = "!ssl"
	skipSlow          = "slow"
	skipNotSlow       = "!slow"
	skipFlaky         = "flaky"
	skipNotFlaky      = "!flaky"
	skipViridian      = "viridian"
	skipNotViridian   = "!viridian"
	skipAll           = "all"
	skipNotAll        = "!all"
	enterpriseKey     = "HAZELCAST_ENTERPRISE_KEY"
	sslKey            = "ENABLE_SSL"
	slowKey           = "ENABLE_SLOW"
	flakyKey          = "ENABLE_FLAKY"
	viridianKey       = "ENABLE_VIRIDIAN"
	allKey            = "ENABLE_ALL"
)

var skipChecker = defaultSkipChecker()

/*
If can be used to skip a test case based on comma-separated conditions.
There are two kinds of conditions, comparisons and booleans.

# Comparison conditions

Comparison conditions are in the following format:

	KEY OP [VERSION|STRING]

KEY is one of the following keys:

	hz: Hazelcast version
	ver: Go Client version
	os: Operating system name, taken from runtime.GOOS
	arch: Operating system architecture, taken from runtime.GOARCH

hz and ver keys support the following operators:

	<, <=, =, !=, >=, >, ~=

os and arch key support the following operators:

	=, !=

VERSION has the following format:

	Major[.Minor[.Patch[...]]][-SUFFIX]

Tilde (~) operator uses the version in the right operand to set the precision, that is the number of version components to compare.
If the precision of the left operand is less than the right, then the missing version components on the left operand is set to zero.
Suffixes are not used in the comparison.

The following conditions are evaluated to true:

	(assuming hz == 5.1.2)
	hz ~ 5
	hz ~ 5.1
	hz ~ 5.1.2
	hz ~ 5.1.2-SNAPSHOT

	(assuming hz == 5.1-SNAPSHOT)
	hz ~ 5
	hz ~ 5.1
	hz ~ 5.1.0
	hz ~ 5.1.0-SNAPSHOT

The following conditions are evaluated to false:

	(assuming hz == 5.1.2)
	hz ~ 6
	hz ~ 5.2
	hz ~ 5.1.3
	hz ~ 5.1.3-SNAPSHOT

	(assuming hz == 5.1-SNAPSHOT)
	hz ~ 6
	hz ~ 5.2
	hz ~ 5.1.1

For other comparison operators, if minor, patch, etc. are not given, they are assumed to be 0.
A version with a suffix is less than a version without suffix, if their Major, Minor, Patch, ... are the same.

The following conditions are evaluated to true:

	(assuming hz == 5.1.2)
	hz > 5
	hz > 5.1
	hz = 5.1.2
	hz < 5.1.2-SNAPSHOT

	(assuming hz == 5.1-SNAPSHOT)
	hz > 5
	hz < 5.1
	hz < 5.1.0
	hz = 5.1.0-SNAPSHOT

The following conditions are evaluated to false:

	(assuming hz == 5.1.2)
	hz = 5
	hz = 5.1
	hz > 5.1.2

	(assuming hz == 5.1-SNAPSHOT)
	hz = 5
	hz = 5.1
	hz = 5.1.0

# Boolean conditions

Boolean conditions are in the following format:

	[!]KEY

KEY is one of the following keys:

	enterprise: Whether the Hazelcast cluster is enterprise
				(existence of HAZELCAST_ENTERPRISE_KEY environment variable)
	oss: Whether the Hazelcast cluster is open source
		 (non-existence of HAZELCAST_ENTERPRISE_KEY environment variable)
	race: existence of RACE_ENABLED environment variable with value "1"
	ssl: existence of ENABLE_SSL environment variable with value "1"
	slow: existence of SLOW_ENABLED environment variable with value "1"
	flaky: existence of FLAKY_ENABLED environment variable with value "1"

! operator negates the value of the key.

# Many Conditions

More than one condition may be specified by separating them with commas.
All conditions should be satisfied to skip.

	skip.If(t, "ver > 1.1, hz = 5, os != windows, !enterprise")

You can use multiple skip.If statements to skip when one of the conditions is satisfied:

	// skip if the OS is windows or client version is greater than 1.3.2 and the Hazelcast cluster is open source:
	skip.If(t, "os = windows")
	skip.If(t, "ver > 1.3.2, oss")
*/
func If(t *testing.T, conditions string) {
	if skipChecker.CanSkip(conditions) {
		t.Skipf("Skipping test since: %s holds", conditions)
	}
}

// IfNot can be used to skip a test case if the list of comma-separated conditions does not hold.
// It is the reverse of skip.If.
// See the documentation about skip.If.
func IfNot(t *testing.T, conditions string) {
	if !skipChecker.CanSkip(conditions) {
		t.Skipf("Skipping test since: %s does not hold", conditions)
	}
}

type Checker struct {
	HzVer      string
	Ver        string
	OS         string
	Arch       string
	Enterprise bool
	Race       bool
	SSL        bool
	Slow       bool
	Flaky      bool
	Viridian   bool
	All        bool
}

// defaultSkipChecker creates and returns the default skip checker.
func defaultSkipChecker() Checker {
	_, enterprise := os.LookupEnv(enterpriseKey)
	return Checker{
		HzVer:      hzVersion(),
		Ver:        internal.Version,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Enterprise: enterprise,
		Race:       raceDetectorEnabled,
		SSL:        isConditionEnabled(sslKey),
		Slow:       isConditionEnabled(slowKey),
		Flaky:      isConditionEnabled(flakyKey),
		Viridian:   isConditionEnabled(viridianKey),
		All:        isConditionEnabled(allKey),
	}
}

// checkHzVer evaluates left OP right and returns the result.
// left is the actual Hazelcast server version.
// op is the comparison operator.
// right is the given Hazelcast server version.
// Hazelcast server version is retrieved from HZ_VERSION environment variable.
func (s Checker) checkHzVer(op, right string) bool {
	return internal.CheckVersion(s.HzVer, op, right)
}

// checkVer evaluates left OP right and returns the result.
// left is the actual client version.
// op is the comparison operator.
// right is the given client version.
func (s Checker) checkVer(op, right string) bool {
	return internal.CheckVersion(s.Ver, op, right)
}

// checkOS evaluates left OP right and returns the result.
// left is the actual operating system name.
// op is the comparison operator.
// right is the given operating system name.
// Consult runtime.GOOS for the valid operating system names.
func (s Checker) checkOS(op, right string) bool {
	return checkEquality(s.OS, op, right, skipOS)
}

// checkArch evaluates left OP right and returns the result.
// left is the actual operating system architecture.
// op is the comparison operator.
// right is the given operating system architecture.
// Consult runtime.GOARCH for the valid operating system architectures.
// As a special case, "arch ~ 32bit" condition is supported.
// That condition returns true for arch == 386, amd64p32, arm, armbe, mips, mips64p32, mips64p32le, mipsle, ppc, riscv, s390, sparc
func (s Checker) checkArch(op, right string) bool {
	b32 := [...]string{
		"386", "amd64p32", "arm", "armbe", "mips",
		"mips64p32", "mips64p32le", "mipsle", "ppc",
		"riscv", "s390", "sparc",
	}
	b64 := [...]string{
		"amd64", "arm64", "arm64be", "loong64", "mips64",
		"mips64le", "ppc64", "ppc64le", "riscv64",
		"s390x", "sparc64", "wasm",
	}
	if op == "~" {
		var b []string
		if right == "32bit" {
			b = b32[:]
		} else if right == "64bit" {
			b = b64[:]
		} else {
			panic("32bit and 64bit are the only valid values for arch ~ operator")
		}
		for _, a := range b {
			if s.Arch == a {
				return true
			}
		}
		return false
	}
	return checkEquality(s.Arch, op, right, skipArch)
}

// isEnterprise returns true if the actual Hazelcast server is Enterprise.
// The default skip checker considers non-blank HAZELCAST_ENTERPRISE_KEY as Hazelcast Enterprise.
func (s Checker) isEnterprise() bool {
	return s.Enterprise
}

// isOSS returns true if the actual Hazelcast server is open source.
// The default skip checker considers blank HAZELCAST_ENTERPRISE_KEY as Hazelcast open source.
func (s Checker) isOSS() bool {
	return !s.Enterprise
}

// isRace returns true if RACE_ENABLED environment variable has the value "1".
func (s Checker) isRace() bool {
	return s.Race
}

// isSSL returns true if ENABLE_SSL environment variable has the value "1".
func (s Checker) isSSL() bool {
	return s.SSL
}

// isSlow returns true if ENABLE_SLOW environment variable has the value "1".
func (s Checker) isSlow() bool {
	return s.Slow
}

// isFlaky returns true if ENABLE_FLAKY environment variable has the value "1".
func (s Checker) isFlaky() bool {
	return s.Flaky
}

// isViridian returns true if ENABLE_VIRIDIAN environment variable has the value "1".
func (s Checker) isViridian() bool {
	return s.Viridian
}

// isAll returns true if ENABLE_ALL environment variable has the value "1".
func (s Checker) isAll() bool {
	return s.All
}

// CanSkip skips returns true if all the given conditions evaluate to true.
// Separate conditions with commas (,).
func (s Checker) CanSkip(condStr string) bool {
	conds := strings.Split(condStr, ",")
	for _, c := range conds {
		if !s.checkCondition(strings.TrimSpace(c)) {
			return false
		}
	}
	return true
}

func (s Checker) checkCondition(cond string) bool {
	parts := strings.Split(cond, " ")
	left := parts[0]
	switch left {
	case skipHzVersion:
		ensureLen(parts, 3, cond, "hz = 5.0")
		return s.checkHzVer(parts[1], parts[2])
	case skipClientVersion:
		ensureLen(parts, 3, cond, "Ver = 5.0")
		return s.checkVer(parts[1], parts[2])
	case skipOS:
		ensureLen(parts, 3, cond, "os = linux")
		return s.checkOS(parts[1], parts[2])
	case skipArch:
		ensureLen(parts, 3, cond, "arch = 386")
		return s.checkArch(parts[1], parts[2])
	case skipEnterprise:
		ensureLen(parts, 1, cond, skipEnterprise)
		return s.isEnterprise()
	case skipNotEnterprise:
		ensureLen(parts, 1, cond, skipNotEnterprise)
		return !s.isEnterprise()
	case skipOSS:
		ensureLen(parts, 1, cond, skipOSS)
		return s.isOSS()
	case skipNotOSS:
		ensureLen(parts, 1, cond, skipNotOSS)
		return !s.isOSS()
	case skipRace:
		ensureLen(parts, 1, cond, skipRace)
		return s.isRace()
	case skipNotRace:
		ensureLen(parts, 1, cond, skipNotRace)
		return !s.isRace()
	case skipSSL:
		ensureLen(parts, 1, cond, skipSSL)
		return s.isSSL()
	case skipNotSSL:
		ensureLen(parts, 1, cond, skipNotSSL)
		return !s.isSSL()
	case skipSlow:
		ensureLen(parts, 1, cond, skipSlow)
		return s.isSlow()
	case skipNotSlow:
		ensureLen(parts, 1, cond, skipNotSlow)
		return !s.isSlow()
	case skipFlaky:
		ensureLen(parts, 1, cond, skipFlaky)
		return s.isFlaky()
	case skipNotFlaky:
		ensureLen(parts, 1, cond, skipNotFlaky)
		return !s.isFlaky()
	case skipViridian:
		ensureLen(parts, 1, cond, skipViridian)
		return s.isViridian()
	case skipNotViridian:
		ensureLen(parts, 1, cond, skipNotViridian)
		return !s.isViridian()
	case skipAll:
		ensureLen(parts, 1, cond, skipAll)
		return s.isAll()
	case skipNotAll:
		ensureLen(parts, 1, cond, skipNotAll)
		return !s.isAll()
	default:
		panic(fmt.Errorf(`unexpected test skip constant "%s" in %s`, parts[0], cond))
	}
}

func ensureLen(parts []string, expected int, condition, example string) {
	if len(parts) != expected {
		panic(fmt.Errorf(`unexpected format for %s, example of expected condition: "%s"`, condition, example))
	}
}

func checkEquality(left, operator, right, key string) bool {
	switch operator {
	case "=":
		return left == right
	case "!=":
		return left != right
	default:
		panic(fmt.Errorf(`unexpected test skip operator "%s" in "%s" condition`, operator, key))
	}
}

func hzVersion() string {
	version := os.Getenv("HZ_VERSION")
	if version == "" {
		version = "5.2"
	}
	return version
}

func isConditionEnabled(envKey string) bool {
	return os.Getenv(envKey) == "1"
}
