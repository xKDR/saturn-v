package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// costRecord is one job's cost, as kept in s3://<costs bucket>/tasks/<YYYY-MM>/<id>.json
// (written when a job stops by the job_finished Lambda in xKDR/AWS-Server).
type costRecord struct {
	TaskID         string             `json:"task_id"`
	Who            string             `json:"who"`
	Script         string             `json:"script"`
	Language       string             `json:"language"`
	TaskDefinition string             `json:"task_definition"`
	Started        string             `json:"started"`
	Stopped        string             `json:"stopped"`
	BilledSeconds  int                `json:"billed_seconds"`
	VCPU           float64            `json:"vcpu"`
	MemoryGB       float64            `json:"memory_gb"`
	StorageGB      int                `json:"storage_gb"`
	Spot           bool               `json:"spot"`
	ExitCode       *int               `json:"exit_code"`
	StoppedReason  string             `json:"stopped_reason,omitempty"`
	CostUSD        map[string]float64 `json:"cost_usd"`
	TotalUSD       float64            `json:"total_usd"`
	Note           string             `json:"note"`
}

func money(x float64) string {
	if x < 0.1 {
		return fmt.Sprintf("$%.3f", x)
	}
	return fmt.Sprintf("$%.2f", x)
}

func round4(x float64) float64 { return math.Round(x*1e4) / 1e4 }

// showCost prints a stopped job's cost. AWS records every job's cost for good (the
// job_finished Lambda in xKDR/AWS-Server writes it to the costs bucket, which people can
// only read); this only shows it: worked out from the task while ECS still has it, and
// from AWS's record once ECS has forgotten the task (about an hour after it stops).
func (s *session) showCost(ctx context.Context, id string) (*costRecord, error) {
	id = taskID(id)
	t, err := s.describe(ctx, id)
	if err != nil {
		return nil, err
	}
	var r *costRecord
	if t != nil {
		if status(t) != "STOPPED" {
			return nil, fail("job %s is still running", id)
		}
		if r, err = s.computeCost(ctx, taskRecord(t), false); err != nil {
			return nil, err
		}
	} else if r, err = s.costRecordOf(ctx, id); err != nil {
		return nil, err
	}
	fmt.Printf("== cost: %s for %.1f min at %g vCPU / %g GB (vCPU %s, memory %s, disk %s; %s)\n",
		money(r.TotalUSD), float64(r.BilledSeconds)/60, r.VCPU, r.MemoryGB,
		money(r.CostUSD["vcpu"]), money(r.CostUSD["memory"]), money(r.CostUSD["storage"]), r.Note)
	return r, nil
}

// costRecordOf finds AWS's record of a job: tasks/<YYYY-MM>/<id>.json in the costs bucket.
func (s *session) costRecordOf(ctx context.Context, id string) (*costRecord, error) {
	p := s3.NewListObjectsV2Paginator(s3.NewFromConfig(s.cfg), &s3.ListObjectsV2Input{Bucket: aws.String(s.dep.CostsBucket), Prefix: aws.String("tasks/")})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			if k := aws.ToString(o.Key); strings.HasSuffix(k, "/"+id+".json") {
				raw, err := s.getObjectFrom(ctx, s.dep.CostsBucket, k)
				if err != nil {
					return nil, err
				}
				r := &costRecord{}
				return r, json.Unmarshal(raw, r)
			}
		}
	}
	return nil, fail("no record of job %s", id)
}

func firstTime(task map[string]any, keys ...string) time.Time {
	for _, k := range keys {
		if v, ok := task[k].(string); ok && v != "" {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// Fargate bills from the start of the image pull until the task stops, per second,
// with a one-minute minimum; ephemeral storage only above the free 20 GB.
func (s *session) computeCost(ctx context.Context, task map[string]any, approx bool) (*costRecord, error) {
	prices, err := s.prices(ctx)
	if err != nil {
		return nil, err
	}
	start := firstTime(task, "pullStartedAt", "startedAt", "createdAt")
	stop := firstTime(task, "stoppedAt")
	seconds := max(60, int(math.Ceil(stop.Sub(start).Seconds())))
	hours := float64(seconds) / 3600
	num := func(k string) float64 { v, _ := strconv.ParseFloat(fmt.Sprint(task[k]), 64); return v }
	vcpu, gb := num("cpu")/1024, num("memory")/1024
	disk := 20
	if e, ok := task["ephemeralStorage"].(map[string]any); ok {
		if v, ok := e["sizeInGiB"].(float64); ok {
			disk = int(v)
		}
	}
	spot := task["capacityProviderName"] == "FARGATE_SPOT"
	cost := map[string]float64{
		"vcpu":    round4(vcpu * hours * prices["Fargate-vCPU-Hours:perCPU"]),
		"memory":  round4(gb * hours * prices["Fargate-GB-Hours"]),
		"storage": round4(math.Max(0, float64(disk-20)) * hours * prices["Fargate-EphemeralStorage-GB-Hours"]),
	}
	note := "on-demand list price"
	if spot {
		note += "; Fargate Spot is billed less"
	}
	if approx {
		note += "; approximate: end time from the last log line"
	}
	r := &costRecord{
		Who: s.who, VCPU: vcpu, MemoryGB: gb, StorageGB: disk, Spot: spot,
		BilledSeconds: seconds, CostUSD: cost, TotalUSD: round4(cost["vcpu"] + cost["memory"] + cost["storage"]), Note: note,
		Started: isoTime(start), Stopped: isoTime(stop),
	}
	arn, _ := task["taskArn"].(string)
	r.TaskID = taskID(arn)
	if td, ok := task["taskDefinitionArn"].(string); ok {
		r.TaskDefinition = td[strings.LastIndex(td, "/")+1:]
	}
	if containers, ok := task["containers"].([]any); ok && len(containers) > 0 {
		c := containers[0].(map[string]any)
		r.Language, _ = c["name"].(string)
		if v, ok := c["exitCode"].(float64); ok {
			code := int(v)
			r.ExitCode = &code
		}
	}
	if o, ok := task["overrides"].(map[string]any); ok {
		if co, ok := o["containerOverrides"].([]any); ok && len(co) > 0 {
			if cmd, ok := co[0].(map[string]any)["command"].([]any); ok && len(cmd) > 4 {
				r.Script, _ = cmd[4].(string)
			}
		}
	}
	return r, nil
}

// prices: Fargate list prices for the region, cached for a day.
func (s *session) prices(ctx context.Context) (map[string]float64, error) {
	cache := filepath.Join(stateDir(), "fargate-prices-"+region+".json")
	var p map[string]float64
	if fi, err := os.Stat(cache); err == nil && time.Since(fi.ModTime()) < 24*time.Hour {
		var raw map[string]string
		if b, err := os.ReadFile(cache); err == nil && json.Unmarshal(b, &raw) == nil {
			p = map[string]float64{}
			for k, v := range raw {
				p[k], _ = strconv.ParseFloat(v, 64)
			}
			return p, nil
		}
	}
	// The billing-code prefix AWS uses for the region (e.g. APS3-Fargate-GB-Hours).
	prefix, ok := map[string]string{"ap-south-1": "APS3", "us-east-1": "USE1", "eu-central-1": "EUC1"}[region]
	if !ok {
		return nil, fail("no billing prefix known for region %s", region)
	}
	client := pricing.NewFromConfig(s.cfg, func(o *pricing.Options) { o.Region = "ap-south-1" })
	raw, p := map[string]string{}, map[string]float64{}
	for _, u := range []string{"Fargate-vCPU-Hours:perCPU", "Fargate-GB-Hours", "Fargate-EphemeralStorage-GB-Hours"} {
		out, err := client.GetProducts(ctx, &pricing.GetProductsInput{
			ServiceCode: aws.String("AmazonECS"),
			Filters:     []pricingtypes.Filter{{Type: pricingtypes.FilterTypeTermMatch, Field: aws.String("usagetype"), Value: aws.String(prefix + "-" + u)}},
		})
		if err != nil {
			return nil, err
		}
		price, err := onDemandPrice(out.PriceList)
		if err != nil {
			return nil, fmt.Errorf("price of %s: %w", u, err)
		}
		raw[u] = price
		p[u], _ = strconv.ParseFloat(price, 64)
	}
	b, _ := json.Marshal(raw)
	os.WriteFile(cache, b, 0o644)
	return p, nil
}

func onDemandPrice(list []string) (string, error) {
	for _, item := range list {
		var prod struct {
			Terms struct {
				OnDemand map[string]struct {
					PriceDimensions map[string]struct {
						PricePerUnit map[string]string `json:"pricePerUnit"`
					} `json:"priceDimensions"`
				} `json:"OnDemand"`
			} `json:"terms"`
		}
		if json.Unmarshal([]byte(item), &prod) != nil {
			continue
		}
		for _, t := range prod.Terms.OnDemand {
			for _, d := range t.PriceDimensions {
				if usd, ok := d.PricePerUnit["USD"]; ok {
					return usd, nil
				}
			}
		}
	}
	return "", fmt.Errorf("not found")
}

// showCosts: spending per month and person, from the cost records (kept locally too, in
// ~/.config/saturn-v/costs, so only new records are downloaded).
func (s *session) showCosts(ctx context.Context) error {
	client := s3.NewFromConfig(s.cfg)
	bucket := aws.String(s.dep.CostsBucket)

	dir := filepath.Join(stateDir(), "costs")
	var keys []string
	p := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: bucket, Prefix: aws.String("tasks/")})
	sizes := map[string]int64{}
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, o := range page.Contents {
			k := aws.ToString(o.Key)
			keys = append(keys, k)
			sizes[k] = aws.ToInt64(o.Size)
		}
	}

	var (
		mu      sync.Mutex
		records []costRecord
		wg      sync.WaitGroup
		limit   = make(chan struct{}, 16)
	)
	for _, k := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			local := filepath.Join(dir, strings.TrimPrefix(k, "tasks/"))
			raw, err := os.ReadFile(local)
			if err != nil || int64(len(raw)) != sizes[k] {
				if raw, err = s.getObjectFrom(ctx, s.dep.CostsBucket, k); err != nil {
					return
				}
				os.MkdirAll(filepath.Dir(local), 0o755)
				os.WriteFile(local, raw, 0o644)
			}
			var r costRecord
			if json.Unmarshal(raw, &r) == nil {
				mu.Lock()
				records = append(records, r)
				mu.Unlock()
			}
		}(k)
	}
	wg.Wait()

	type agg struct {
		jobs        int
		hours, cost float64
	}
	by := map[[2]string]*agg{}
	var total agg
	for _, r := range records {
		month := r.Started
		if len(month) >= 7 {
			month = month[:7]
		}
		k := [2]string{month, r.Who}
		if by[k] == nil {
			by[k] = &agg{}
		}
		h := float64(r.BilledSeconds) / 3600
		by[k].jobs++
		by[k].hours += h
		by[k].cost += r.TotalUSD
		total.jobs++
		total.hours += h
		total.cost += r.TotalUSD
	}
	var ks [][2]string
	for k := range by {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		if ks[i][0] != ks[j][0] {
			return ks[i][0] < ks[j][0]
		}
		return ks[i][1] < ks[j][1]
	})
	fmt.Printf("%-8s %-16s %6s %8s %9s\n", "month", "who", "jobs", "hours", "USD")
	for _, k := range ks {
		a := by[k]
		fmt.Printf("%-8s %-16s %6d %8.2f %9.2f\n", k[0], k[1], a.jobs, a.hours, a.cost)
	}
	fmt.Printf("%-25s %6d %8.2f %9.2f\n", "total", total.jobs, total.hours, total.cost)
	return nil
}
