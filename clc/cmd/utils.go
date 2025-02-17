package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hazelcast/hazelcast-go-client"
	"github.com/hazelcast/hazelcast-go-client/hzerrors"

	"github.com/hazelcast/hazelcast-commandline-client/clc"
	cmderrors "github.com/hazelcast/hazelcast-commandline-client/errors"
	"github.com/hazelcast/hazelcast-commandline-client/internal"
)

func ExtractStartupArgs(args []string) (cfgPath, logFile, logLevel string, err error) {
	var i int
	ln := len(args)
	for i < ln {
		switch args[i] {
		case fmt.Sprintf("--%s", clc.PropertyConfig), fmt.Sprintf("-%s", clc.ShortcutConfig):
			if ln <= i+1 {
				return cfgPath, logFile, logLevel, fmt.Errorf("%s requires the configuration name or path", args[i])
			}
			cfgPath = args[i+1]
			i++
		case fmt.Sprintf("--%s", clc.PropertyLogPath):
			if ln <= i+1 {
				return cfgPath, logFile, logLevel, fmt.Errorf("%s requires a path", args[i])
			}
			logFile = args[i+1]
			i++
		case fmt.Sprintf("--%s", clc.PropertyLogLevel):
			if ln <= i+1 {
				return cfgPath, logFile, logLevel, fmt.Errorf("%s requires a level", args[i])
			}
			logLevel = args[i+1]
			i++
		}
		i++
	}
	return
}

func CheckServerCompatible(ci *hazelcast.ClientInternal, targetVersion string) (string, bool) {
	conn := ci.ConnectionManager().RandomConnection()
	if conn == nil {
		return "UNKNOWN", false
	}
	sv := conn.ServerVersion()
	if os.Getenv(clc.EnvSkipServerVersionCheck) == "1" {
		return sv, true
	}
	ok := internal.CheckVersion(sv, ">=", targetVersion)
	return sv, ok
}

func MakeErrStr(err error) string {
	var httpErr cmderrors.HTTPError
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, hzerrors.ErrTimeout) {
		return "Timeout"
	}
	var errStr string
	if errors.As(err, &httpErr) {
		errStr = makeErrorStringFromHTTPResponse(httpErr.Text())
	} else {
		errStr = err.Error()
	}
	return fmt.Sprintf("Error: %s", errStr)
}

func parseDuration(duration string) (time.Duration, error) {
	// input can be like: 10_000_000 or 10_000_000ms, so remove underscores
	ds := strings.ReplaceAll(duration, "_", "")
	if ds == "" {
		return 0, nil
	}
	// if it can be parsed to int, then it means it does not have any prefix ms, s, m, h (default is millisecond)
	d, err := strconv.Atoi(ds)
	if err == nil {
		return time.Duration(d) * time.Millisecond, nil
	}
	pd, err := time.ParseDuration(ds)
	if err != nil {
		return 0, err
	}
	return pd, nil
}
