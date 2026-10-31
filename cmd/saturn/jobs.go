package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	logtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	saturnv "github.com/xKDR/saturn-v"
)

func languageOf(script string) (lang, helper string, err error) {
	switch {
	case strings.HasSuffix(script, ".jl"):
		return "julia", "saturn.jl", nil
	case strings.HasSuffix(script, ".R"), strings.HasSuffix(script, ".r"):
		return "r", "saturn.R", nil
	case strings.HasSuffix(script, ".py"):
		return "python", "saturn.py", nil
	}
	return "", "", fail("don't know how to run %s (expected .jl, .R or .py)", script)
}

// rTaskDefinition: the R version the script's renv.lock was made with (read from S3, as
// the job will see it), R_VERSION if set, or the newest available.
func (s *session) rTaskDefinition(ctx context.Context, script string) (string, error) {
	r := s.dep.Languages["r"]
	version := os.Getenv("R_VERSION")
	if version == "" {
		key := "renv.lock"
		if dir := path.Dir(script); dir != "." {
			key = dir + "/renv.lock"
		}
		if lock, err := s.getObject(ctx, key); err == nil {
			var l struct{ R struct{ Version string } }
			if json.Unmarshal(lock, &l) == nil && l.R.Version != "" {
				parts := strings.SplitN(l.R.Version, ".", 3)
				version = strings.Join(parts[:min(2, len(parts))], ".")
				fmt.Fprintf(os.Stderr, "renv.lock was made with R %s\n", version)
			}
		}
	}
	if version == "" {
		return r.TaskDefinition, nil
	}
	if td, ok := r.Versions[version]; ok {
		return td, nil
	}
	var have []string
	for v := range r.Versions {
		have = append(have, v)
	}
	sort.Strings(have)
	return "", fail("R %s is not available on AWS (available: %s). Make the renv.lock with one of those, or choose one with R_VERSION=...",
		version, strings.Join(have, ", "))
}

func (s *session) runJob(ctx context.Context, args []string) error {
	var envs []types.KeyValuePair
	for len(args) >= 1 && args[0] == "-e" {
		if len(args) < 2 || !strings.Contains(args[1], "=") {
			return fail("-e needs NAME=VALUE")
		}
		name, value, _ := strings.Cut(args[1], "=")
		envs = append(envs, types.KeyValuePair{Name: aws.String(name), Value: aws.String(value)})
		args = args[2:]
	}
	if len(args) < 1 {
		return fail("usage: saturn run [-e NAME=VALUE ...] path/to/script.jl|.R|.py [args...]")
	}
	script := args[0]
	lang, helperFile, err := languageOf(script)
	if err != nil {
		return err
	}
	l, ok := s.dep.Languages[lang]
	if !ok {
		return fail("%s isn't set up on AWS yet (no %s in /saturn-v/deployment)", lang, lang)
	}
	taskDefinition := l.TaskDefinition
	if lang == "r" {
		if taskDefinition, err = s.rTaskDefinition(ctx, script); err != nil {
			return err
		}
	}
	launcher, err := saturnv.Lib.ReadFile("lib/launch-" + lang + ".sh")
	if err != nil {
		return err
	}
	helper, err := saturnv.Lib.ReadFile("lib/" + helperFile)
	if err != nil {
		return err
	}

	envs = append([]types.KeyValuePair{{Name: aws.String("MY_WORKSPACE"), Value: aws.String("/workspace/" + strings.ToLower(s.who))}}, envs...)
	envs = append(envs, types.KeyValuePair{Name: aws.String("SATURN_HELPER"), Value: aws.String(string(helper))})
	overrides := &types.TaskOverride{ContainerOverrides: []types.ContainerOverride{{
		Name:        aws.String(l.Container),
		Command:     append([]string{"bash", "-c", string(launcher), l.Container}, args...),
		Environment: envs,
	}}}
	if v := os.Getenv("CPU"); v != "" {
		overrides.Cpu = aws.String(v)
	}
	if v := os.Getenv("MEMORY"); v != "" {
		overrides.Memory = aws.String(v)
	}

	startedBy := s.who
	if len(startedBy) > 36 {
		startedBy = startedBy[:36]
	}
	in := &ecs.RunTaskInput{
		Cluster:        aws.String(s.dep.Cluster),
		TaskDefinition: aws.String(taskDefinition),
		StartedBy:      aws.String(startedBy),
		Overrides:      overrides,
		// The subnets are public with no NAT, so the job needs a public IP to pull its
		// image and packages. No inbound ports are open.
		NetworkConfiguration: &types.NetworkConfiguration{AwsvpcConfiguration: &types.AwsVpcConfiguration{
			Subnets:        s.dep.Subnets,
			SecurityGroups: []string{s.dep.SecurityGroup},
			AssignPublicIp: types.AssignPublicIpEnabled,
		}},
	}
	if os.Getenv("SPOT") == "1" {
		in.CapacityProviderStrategy = []types.CapacityProviderStrategyItem{{CapacityProvider: aws.String("FARGATE_SPOT"), Weight: 1}}
	} else {
		in.LaunchType = types.LaunchTypeFargate
	}
	out, err := ecs.NewFromConfig(s.cfg).RunTask(ctx, in)
	if err != nil {
		return err
	}
	if len(out.Tasks) == 0 {
		reason := "unknown"
		if len(out.Failures) > 0 {
			reason = aws.ToString(out.Failures[0].Reason)
		}
		return fail("AWS didn't start the job (%s)", reason)
	}
	id := taskID(aws.ToString(out.Tasks[0].TaskArn))
	fmt.Println("Started job", id)

	// Saved so the cost can still be worked out if nobody is following the job when it ends.
	if t, _ := s.describe(ctx, id); t != nil {
		pending, _ := json.Marshal(taskRecord(t))
		s.putObject(ctx, "costs/pending/"+id+".json", pending)
	}

	if os.Getenv("DETACH") == "1" {
		fmt.Println("Follow with: saturn logs", id)
		return nil
	}
	return s.follow(ctx, id)
}

// describe returns the task, or nil once ECS has forgotten it (about an hour after it stops).
func (s *session) describe(ctx context.Context, id string) (*types.Task, error) {
	out, err := ecs.NewFromConfig(s.cfg).DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Cluster: aws.String(s.dep.Cluster), Tasks: []string{id},
	})
	if err != nil {
		return nil, err
	}
	if len(out.Tasks) == 0 {
		return nil, nil
	}
	return &out.Tasks[0], nil
}

func status(t *types.Task) string {
	if t == nil {
		return "None"
	}
	return aws.ToString(t.LastStatus)
}

func (s *session) follow(ctx context.Context, id string) error {
	id = taskID(id)
	t, err := s.describe(ctx, id)
	if err != nil {
		return err
	}
	if t == nil || len(t.Containers) == 0 {
		fmt.Printf("== job %s finished some time ago; AWS no longer has its details (its cost is in saturn costs)\n", id)
		return exitError{code: 1}
	}
	container := aws.ToString(t.Containers[0].Name)
	stoppedFollowing := func() error {
		fmt.Printf("\nStopped following; job %s is still running (saturn logs %s).\n", id, id)
		return exitError{code: 130}
	}

	fmt.Printf("Waiting for job %s to start...\n", id)
	for !strings.Contains(" RUNNING DEACTIVATING STOPPING DEPROVISIONING STOPPED None ", " "+status(t)+" ") {
		if !sleep(ctx, 5*time.Second) {
			return stoppedFollowing()
		}
		if t, err = s.describe(ctx, id); err != nil {
			return err
		}
	}

	logs := &logTail{client: cloudwatchlogs.NewFromConfig(s.cfg), group: s.dep.LogGroup, stream: container + "/" + container + "/" + id}
	lastCheck := time.Now()
	var stoppedAt time.Time
	for {
		logs.print(ctx)
		if !stoppedAt.IsZero() {
			// CloudWatch can take a while to deliver the last lines.
			if time.Since(stoppedAt) > 30*time.Second {
				break
			}
		} else if time.Since(lastCheck) >= 15*time.Second {
			lastCheck = time.Now()
			if t, err = s.describe(ctx, id); err == nil && (t == nil || status(t) == "STOPPED") {
				stoppedAt = time.Now()
			}
		}
		if !sleep(ctx, 2*time.Second) {
			return stoppedFollowing()
		}
	}

	if t == nil || len(t.Containers) == 0 || t.Containers[0].ExitCode == nil {
		fmt.Println("== job finished some time ago; AWS no longer has its details (its cost is in saturn costs)")
		return exitError{code: 1}
	}
	code := *t.Containers[0].ExitCode
	fmt.Printf("== job stopped: exit code %d (%s)\n", code, aws.ToString(t.StoppedReason))
	if _, err := s.recordCost(ctx, id); err != nil {
		fmt.Printf("(couldn't record the cost: %v; retry with: saturn cost %s)\n", err, id)
	}
	if code != 0 {
		return exitError{code: 1}
	}
	return nil
}

// sleep waits, or returns false if the context is cancelled (Ctrl-C).
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// logTail prints a log stream's new lines, like `aws logs tail --format short`.
type logTail struct {
	client        *cloudwatchlogs.Client
	group, stream string
	token         *string
}

func (l *logTail) print(ctx context.Context) {
	for {
		out, err := l.client.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{
			LogGroupName: aws.String(l.group), LogStreamName: aws.String(l.stream),
			StartFromHead: aws.Bool(true), NextToken: l.token,
		})
		if err != nil {
			var nf *logtypes.ResourceNotFoundException
			if !errors.As(err, &nf) && ctx.Err() == nil {
				fmt.Fprintln(os.Stderr, "(reading the job's output:", err, ")")
			}
			return // the stream appears once the job writes its first line
		}
		for _, e := range out.Events {
			ts := time.UnixMilli(aws.ToInt64(e.Timestamp)).UTC().Format("2006-01-02T15:04:05")
			fmt.Println(ts, strings.TrimRight(aws.ToString(e.Message), "\n"))
		}
		same := l.token != nil && aws.ToString(out.NextForwardToken) == aws.ToString(l.token)
		l.token = out.NextForwardToken
		if same || len(out.Events) == 0 {
			return
		}
	}
}

func (s *session) stopJob(ctx context.Context, id string) error {
	out, err := ecs.NewFromConfig(s.cfg).StopTask(ctx, &ecs.StopTaskInput{
		Cluster: aws.String(s.dep.Cluster), Task: aws.String(taskID(id)),
		Reason: aws.String("stopped by " + s.who + " with saturn"),
	})
	if err != nil {
		return err
	}
	fmt.Println(aws.ToString(out.Task.LastStatus))
	return nil
}

func (s *session) listJobs(ctx context.Context) error {
	p := ecs.NewListTasksPaginator(ecs.NewFromConfig(s.cfg), &ecs.ListTasksInput{Cluster: aws.String(s.dep.Cluster)})
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, arn := range out.TaskArns {
			fmt.Println(taskID(arn))
		}
	}
	return nil
}

// taskRecord is the task as the AWS CLI shows it (the fields saturn reads back later),
// for costs/pending/.
func taskRecord(t *types.Task) map[string]any {
	r := map[string]any{
		"taskArn":           aws.ToString(t.TaskArn),
		"taskDefinitionArn": aws.ToString(t.TaskDefinitionArn),
		"lastStatus":        aws.ToString(t.LastStatus),
		"cpu":               aws.ToString(t.Cpu),
		"memory":            aws.ToString(t.Memory),
		"startedBy":         aws.ToString(t.StartedBy),
	}
	for k, v := range map[string]*time.Time{"createdAt": t.CreatedAt, "pullStartedAt": t.PullStartedAt, "startedAt": t.StartedAt, "stoppedAt": t.StoppedAt} {
		if v != nil {
			r[k] = isoTime(*v)
		}
	}
	if t.CapacityProviderName != nil {
		r["capacityProviderName"] = *t.CapacityProviderName
	}
	if t.EphemeralStorage != nil {
		r["ephemeralStorage"] = map[string]any{"sizeInGiB": t.EphemeralStorage.SizeInGiB}
	}
	var containers []map[string]any
	for _, c := range t.Containers {
		cr := map[string]any{"name": aws.ToString(c.Name)}
		if c.ExitCode != nil {
			cr["exitCode"] = *c.ExitCode
		}
		containers = append(containers, cr)
	}
	r["containers"] = containers
	if t.Overrides != nil && len(t.Overrides.ContainerOverrides) > 0 {
		r["overrides"] = map[string]any{"containerOverrides": []map[string]any{{"command": t.Overrides.ContainerOverrides[0].Command}}}
	}
	return r
}

// --- The workspace bucket --------------------------------------------------------------

func (s *session) getObject(ctx context.Context, key string) ([]byte, error) {
	out, err := s3.NewFromConfig(s.cfg).GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.dep.WorkspaceBucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (s *session) putObject(ctx context.Context, key string, body []byte) error {
	_, err := s3.NewFromConfig(s.cfg).PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.dep.WorkspaceBucket), Key: aws.String(key),
		Body: bytes.NewReader(body), ContentType: aws.String("application/json"),
	})
	return err
}
