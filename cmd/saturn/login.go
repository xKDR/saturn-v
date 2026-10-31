package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	oidctypes "github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"gopkg.in/ini.v1"
)

// login signs in at the AWS access portal (IAM Identity Center) with a device code: the
// browser opens on the approval page, or, with --use-device-code (a server without a
// browser), the link and code are shown to enter on any phone or laptop. The sign-in is
// cached where the AWS CLI and SDKs look for it, and the saturn-v profile (plus
// saturn-v-admin for admins, which ./tf in xKDR/AWS-Server uses) is written to
// ~/.aws/config, so the saturn script, the AWS CLI and this binary share one sign-in.
func login(ctx context.Context, args []string) error {
	openBrowser := true
	for _, a := range args {
		switch a {
		case "--use-device-code", "--no-browser":
			openBrowser = false
		default:
			return fail("usage: saturn login [--use-device-code]")
		}
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(ssoRegion), config.WithCredentialsProvider(aws.AnonymousCredentials{}))
	if err != nil {
		return err
	}
	oidc := ssooidc.NewFromConfig(cfg)
	reg, err := oidc.RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName: aws.String("saturn"), ClientType: aws.String("public"), Scopes: []string{"sso:account:access"},
	})
	if err != nil {
		return fmt.Errorf("can't reach the AWS access portal: %w", err)
	}
	dev, err := oidc.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId: reg.ClientId, ClientSecret: reg.ClientSecret, StartUrl: aws.String(ssoStartURL),
	})
	if err != nil {
		return err
	}

	fmt.Printf("Sign in at %s\nand check the code there is %s\n", aws.ToString(dev.VerificationUriComplete), aws.ToString(dev.UserCode))
	if openBrowser && !startBrowser(aws.ToString(dev.VerificationUriComplete)) {
		fmt.Println("(open the link yourself; no browser here)")
	}

	interval := time.Duration(max(dev.Interval, 1)) * time.Second
	var tok *ssooidc.CreateTokenOutput
	for tok == nil {
		if !sleep(ctx, interval) {
			return exitError{code: 130}
		}
		tok, err = oidc.CreateToken(ctx, &ssooidc.CreateTokenInput{
			ClientId: reg.ClientId, ClientSecret: reg.ClientSecret, DeviceCode: dev.DeviceCode,
			GrantType: aws.String("urn:ietf:params:oauth:grant-type:device_code"),
		})
		var pending *oidctypes.AuthorizationPendingException
		var slow *oidctypes.SlowDownException
		switch {
		case errors.As(err, &pending):
		case errors.As(err, &slow):
			interval += 5 * time.Second
		case err != nil:
			return fail("sign-in didn't complete: %v", err)
		}
	}

	if err := writeTokenCache(reg, tok); err != nil {
		return err
	}

	client := sso.NewFromConfig(cfg)
	accounts, err := client.ListAccounts(ctx, &sso.ListAccountsInput{AccessToken: tok.AccessToken})
	if err != nil {
		return err
	}
	if len(accounts.AccountList) == 0 {
		return fail("signed in, but you have no access to the AWS account yet. Ask an admin to add you to saturn-v-users.")
	}
	account := aws.ToString(accounts.AccountList[0].AccountId)
	rolesOut, err := client.ListAccountRoles(ctx, &sso.ListAccountRolesInput{AccessToken: tok.AccessToken, AccountId: aws.String(account)})
	if err != nil {
		return err
	}
	var roles []string
	for _, r := range rolesOut.RoleList {
		roles = append(roles, aws.ToString(r.RoleName))
	}
	if len(roles) == 0 {
		return fail("signed in, but you have no role in the AWS account yet. Ask an admin to add you to saturn-v-users.")
	}
	role := roles[0]
	if slices.Contains(roles, "SaturnUser") {
		role = "SaturnUser"
	}
	profiles := map[string]string{ssoProfile: role}
	if slices.Contains(roles, "Admin") {
		profiles[ssoProfile+"-admin"] = "Admin"
	}
	if err := writeProfiles(account, profiles); err != nil {
		return err
	}

	who := "you"
	if c, err := loadConfig(ctx, ssoProfile); err == nil {
		if id, err := sts.NewFromConfig(c).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); err == nil {
			who = userName(aws.ToString(id.Arn))
		}
	}
	fmt.Printf("Signed in as %s (%s). Next: saturn connect\n", who, role)
	return nil
}

func startBrowser(url string) bool {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin":
		cmd = exec.Command("open", url)
	case os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "":
		cmd = exec.Command("xdg-open", url)
	default:
		return false
	}
	return cmd.Start() == nil
}

// writeTokenCache saves the sign-in where the AWS CLI and SDKs read it for an
// sso-session: ~/.aws/sso/cache/<sha1 of the session name>.json.
func writeTokenCache(reg *ssooidc.RegisterClientOutput, tok *ssooidc.CreateTokenOutput) error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".aws", "sso", "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sum := sha1.Sum([]byte(ssoSession))
	utc := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }
	body, _ := json.MarshalIndent(map[string]string{
		"startUrl":              ssoStartURL,
		"region":                ssoRegion,
		"accessToken":           aws.ToString(tok.AccessToken),
		"expiresAt":             utc(time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)),
		"refreshToken":          aws.ToString(tok.RefreshToken),
		"clientId":              aws.ToString(reg.ClientId),
		"clientSecret":          aws.ToString(reg.ClientSecret),
		"registrationExpiresAt": utc(time.Unix(reg.ClientSecretExpiresAt, 0)),
	}, "", "  ")
	return os.WriteFile(filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), body, 0o600)
}

// writeProfiles adds or updates the sso-session and the profiles in ~/.aws/config,
// leaving everything else in it as it was.
func writeProfiles(account string, profiles map[string]string) error {
	path := awsConfigFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := ini.LoadSources(ini.LoadOptions{Loose: true}, path)
	if err != nil {
		return err
	}
	set := func(section string, kv ...string) {
		s := f.Section(section)
		for i := 0; i < len(kv); i += 2 {
			s.Key(kv[i]).SetValue(kv[i+1])
		}
	}
	set("sso-session "+ssoSession, "sso_start_url", ssoStartURL, "sso_region", ssoRegion, "sso_registration_scopes", "sso:account:access")
	for p, role := range profiles {
		set("profile "+p, "sso_session", ssoSession, "sso_account_id", account, "sso_role_name", role, "region", region)
	}
	return f.SaveTo(path)
}
