package job

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v2"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hazelcast/hazelcast-go-client"

	"github.com/hazelcast/hazelcast-commandline-client/clc"
	"github.com/hazelcast/hazelcast-commandline-client/clc/cmd"
	"github.com/hazelcast/hazelcast-commandline-client/clc/paths"
	. "github.com/hazelcast/hazelcast-commandline-client/internal/check"
	"github.com/hazelcast/hazelcast-commandline-client/internal/jet"
	"github.com/hazelcast/hazelcast-commandline-client/internal/log"
	"github.com/hazelcast/hazelcast-commandline-client/internal/plug"
)

const (
	minServerVersion = "5.3.0"
)

type SubmitCmd struct{}

func (cm SubmitCmd) Init(cc plug.InitContext) error {
	cc.SetCommandUsage("submit [jar-file|yaml-file] [arg, ...]")
	long := fmt.Sprintf(`Submits a jar or yaml file to create a Jet job
	
This command requires a Viridian or a Hazelcast cluster having version %s or newer.
`, minServerVersion)
	short := "Submits a jar or yaml file to create a Jet job"
	cc.SetCommandHelp(long, short)
	cc.AddStringFlag(flagName, "", "", false, "override the job name")
	cc.AddStringFlag(flagSnapshot, "", "", false, "initial snapshot to start the job from")
	cc.AddStringFlag(flagClass, "", "", false, "the class that contains the main method that creates the Jet job (jar only)")
	cc.AddIntFlag(flagRetries, "", 0, false, "number of times to retry a failed upload attempt")
	cc.AddBoolFlag(flagWait, "", false, false, "wait for the job to be started")
	cc.SetPositionalArgCount(1, math.MaxInt)
	return nil
}

func (cm SubmitCmd) Exec(ctx context.Context, ec plug.ExecContext) error {
	path := ec.Args()[0]
	if !paths.Exists(path) {
		return fmt.Errorf("file does not exists: %s", path)
	}
	if strings.HasSuffix(path, ".jar") {
		ci, err := ec.ClientInternal(ctx)
		if err != nil {
			return err
		}
		if sv, ok := cmd.CheckServerCompatible(ci, minServerVersion); !ok {
			return fmt.Errorf("server (%s) does not support this command, at least %s is expected", sv, minServerVersion)
		}
		return submitJar(ctx, ci, ec, path)
	} else {
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
			ci, err := ec.ClientInternal(ctx)
			if err != nil {
				return err
			}
			if sv, ok := cmd.CheckServerCompatible(ci, minServerVersion); !ok {
				return fmt.Errorf("server (%s) does not support this command, at least %s is expected", sv, minServerVersion)
			}
			return submitYaml(ctx, ci, ec, path)
		} else {
			return fmt.Errorf("submitted file is neither a jar nor a yaml file: %s", path)
		}
	}
}

func submitJar(ctx context.Context, ci *hazelcast.ClientInternal, ec plug.ExecContext, path string) error {
	wait := ec.Props().GetBool(flagWait)
	jobName := ec.Props().GetString(flagName)
	snapshot := ec.Props().GetString(flagSnapshot)
	className := ec.Props().GetString(flagClass)
	if wait && jobName == "" {
		return fmt.Errorf("--wait requires the --name to be set")
	}
	tries := int(ec.Props().GetInt(flagRetries))
	if tries < 0 {
		tries = 0
	}
	tries++
	_, fn := filepath.Split(path)
	fn = strings.TrimSuffix(fn, ".jar")
	args := ec.Args()[1:]
	_, stop, err := ec.ExecuteBlocking(ctx, func(ctx context.Context, sp clc.Spinner) (any, error) {
		j := jet.New(ci, sp, ec.Logger())
		err := retry(tries, ec.Logger(), func(try int) error {
			msg := "Submitting the job"
			if try == 0 {
				sp.SetText(msg)
			} else {
				sp.SetText(fmt.Sprintf("%s: retry %d", msg, try))
			}
			br := jet.CreateBinaryReaderForPath(path)
			return j.SubmitJob(ctx, path, jobName, className, snapshot, args, br)
		})
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("submitting the job: %w", err)
	}
	stop()
	if wait {
		msg := fmt.Sprintf("Waiting for job %s to start", jobName)
		ec.Logger().Info(msg)
		err = WaitJobState(ctx, ec, msg, jobName, jet.JobStatusRunning, 2*time.Second)
		if err != nil {
			return err
		}
	}
	return nil
}

func submitYaml(ctx context.Context, ci *hazelcast.ClientInternal, ec plug.ExecContext, path string) error {

	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading job definition: %w", err)
	}
	definition := string(b)

	// handling --define FOO=1 --define BAR=2 seems to be complex for ExecContext
	// ie 'define' is a 'flag' and can only have one single value, it seems
	// so we're going with positional arguments here
	defines := make(map[string]string)
	for _, arg := range ec.Args()[1:] { // skip the first one which is the path
		//fmt.Println("arg: ", arg)
		pair := strings.Split(arg, "=")
		defines[pair[0]] = pair[1]
	}

	re := regexp.MustCompile("(.|^)\\$([A-Z_]*)")
	definition = re.ReplaceAllStringFunc(definition, func(m string) string {
		// meh - in Go the 'match' is the whole string - match again
		sub := re.FindStringSubmatch(m)
		if sub[1] == "$" {
			return m
		} else {
			value, defined := defines[sub[2]]
			if defined {
				return sub[1] + value
			} else {
				return m
			}
		}
	})

	// now validate the yaml content
	m := make(map[interface{}]interface{})
	err = yaml.Unmarshal([]byte(definition), m)
	if err != nil {
		return fmt.Errorf("reading job definition: %w", err)
	}

	// validate job
	job, exists := m["job"]
	if !exists {
		return fmt.Errorf("missing job element")
	}
	jobm, ok := job.(map[interface{}]interface{})
	if !ok {
		return fmt.Errorf("invalid job element")
	}
	// validate resources
	res, exists := jobm["resources"]
	if exists {
		var resl []interface{}
		resl, ok = res.([]interface{})
		if !ok {
			return fmt.Errorf("invalid resources element")
		}
		for _, r := range resl {
			var rm map[interface{}]interface{}
			rm, ok = r.(map[interface{}]interface{})
			if !ok {
				return fmt.Errorf("invalid resource element")
			}
			id := rm["id"].(string)
			t := rm["type"].(string)
			if t != "DIRECTORY" {
				return fmt.Errorf("invalid resource type for %s", id)
			}
			d := rm["path"].(string)
			_, err = os.Stat(d)
			if os.IsNotExist(err) {
				return fmt.Errorf("invalid resource path for %s: %s", id, d)
			}
		}
	}

	wait := ec.Props().GetBool(flagWait)
	jobName := ec.Props().GetString(flagName)
	snapshot := ec.Props().GetString(flagSnapshot)
	if wait && jobName == "" {
		return fmt.Errorf("--wait requires the --name to be set")
	}
	tries := int(ec.Props().GetInt(flagRetries))
	if tries < 0 {
		tries = 0
	}
	tries++
	args := ec.Args()[1:]
	_, stop, err := ec.ExecuteBlocking(ctx, func(ctx context.Context, sp clc.Spinner) (any, error) {
		j := jet.New(ci, sp, ec.Logger())
		err = retry(tries, ec.Logger(), func(try int) error {
			msg := "Submitting the job"
			if try == 0 {
				sp.SetText(msg)
			} else {
				sp.SetText(fmt.Sprintf("%s: retry %d", msg, try))
			}
			return j.SubmitYamlJob(ctx, definition, jobm, jobName, snapshot, args)
		})
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("submitting the job: %w", err)
	}
	stop()
	if wait {
		msg := fmt.Sprintf("Waiting for job %s to start", jobName)
		ec.Logger().Info(msg)
		err = WaitJobState(ctx, ec, msg, jobName, jet.JobStatusRunning, 2*time.Second)
		if err != nil {
			return err
		}
	}
	return nil
}

func retry(times int, lg log.Logger, f func(try int) error) error {
	var err error
	for i := 0; i < times; i++ {
		err = f(i)
		if err != nil {
			lg.Error(err)
			time.Sleep(5 * time.Second)
			continue
		}
		return nil
	}
	return fmt.Errorf("failed after %d tries: %w", times, err)
}

func init() {
	Must(plug.Registry.RegisterCommand("job:submit", &SubmitCmd{}))
}
