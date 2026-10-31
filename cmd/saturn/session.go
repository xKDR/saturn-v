package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/processcreds"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"gopkg.in/ini.v1"
)

const (
	ssoProfile = "saturn-v" // written by `saturn login`
	ssoSession = "saturn-v"
	iamProfile = "aws-server" // IAM users, before Identity Center (`aws login`)
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

var (
	region      = env("SATURN_REGION", "ap-south-1")
	ssoStartURL = env("SATURN_SSO_START_URL", "https://d-9067ac7cfe.awsapps.com/start")
	ssoRegion   = env("SATURN_SSO_REGION", "us-east-1")
)

func stateDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "saturn-v")
	os.MkdirAll(dir, 0o755)
	return dir
}

func awsConfigFile() string {
	if f := os.Getenv("AWS_CONFIG_FILE"); f != "" {
		return f
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aws", "config")
}

// profile picks the sign-in: SATURN_PROFILE, else the Identity Center profile written by
// `saturn login`, else the IAM user's aws-server profile.
func profile() string {
	if p := os.Getenv("SATURN_PROFILE"); p != "" {
		return p
	}
	if f, err := ini.Load(awsConfigFile()); err == nil && f.Section("profile "+ssoProfile).HasKey("sso_session") {
		return ssoProfile
	}
	return iamProfile
}

func signInCommand(p string) string {
	if p == ssoProfile {
		return "saturn login"
	}
	return "aws login --profile " + p
}

// deployment is what saturn needs to know about the AWS setup: the SSM parameter
// /saturn-v/deployment that xKDR/AWS-Server publishes.
type deployment struct {
	Cluster         string   `json:"cluster"`
	Subnets         []string `json:"subnets"`
	SecurityGroup   string   `json:"security_group"`
	LogGroup        string   `json:"log_group"`
	WorkspaceBucket string   `json:"workspace_bucket"`
	Languages       map[string]struct {
		TaskDefinition string            `json:"task_definition"`
		Container      string            `json:"container"`
		Versions       map[string]string `json:"versions"`
	} `json:"languages"`
}

type session struct {
	profile string
	cfg     aws.Config
	dep     deployment
	who     string // the signed-in user name, e.g. firstname.lastname
}

func loadConfig(ctx context.Context, p string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{config.WithRegion(region)}
	f, _ := ini.Load(awsConfigFile())
	if f != nil && f.Section("profile "+p).HasKey("sso_session") {
		opts = append(opts, config.WithSharedConfigProfile(p))
	} else {
		// Other sign-ins (e.g. `aws login`) through the AWS CLI, if it's installed.
		awsCLI, err := exec.LookPath("aws")
		if err != nil {
			return aws.Config{}, fail("not signed in. Run: saturn login")
		}
		cmd := fmt.Sprintf("%s configure export-credentials --profile %s --format process", awsCLI, p)
		opts = append(opts, config.WithCredentialsProvider(aws.NewCredentialsCache(processcreds.NewProvider(cmd))))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

func newSession(ctx context.Context) (*session, error) {
	s := &session{profile: profile()}
	cfg, err := loadConfig(ctx, s.profile)
	if err != nil {
		return nil, err
	}
	s.cfg = cfg
	id, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fail("not signed in to AWS (or the sign-in has expired). Run: %s", signInCommand(s.profile))
	}
	s.who = userName(aws.ToString(id.Arn))
	if err := s.loadDeployment(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// userName is the last part of the caller's ARN (an IAM user, or the user name of an
// Identity Center session), without any @domain.
func userName(arn string) string {
	n := arn[strings.LastIndex(arn, "/")+1:]
	if i := strings.Index(n, "@"); i >= 0 {
		n = n[:i]
	}
	return n
}

// Published on every apply; fetched each run, with the last copy as a fallback.
func (s *session) loadDeployment(ctx context.Context) error {
	cache := filepath.Join(stateDir(), "deployment.json")
	var raw []byte
	out, err := ssm.NewFromConfig(s.cfg).GetParameter(ctx, &ssm.GetParameterInput{Name: aws.String("/saturn-v/deployment")})
	if err == nil {
		raw = []byte(aws.ToString(out.Parameter.Value))
		os.WriteFile(cache, raw, 0o644)
	} else if raw, err = os.ReadFile(cache); err != nil {
		return fail("can't read the deployment settings (/saturn-v/deployment). Ask an admin to add you to the team.")
	}
	return json.Unmarshal(raw, &s.dep)
}

// isoTime formats times the way the AWS CLI and Python read them back.
func isoTime(t time.Time) string { return t.Format("2006-01-02T15:04:05.000000-07:00") }
