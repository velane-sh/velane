package aws

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

// QueryAPIReader uses AWS Query APIs directly so the host identity seam stays
// small. Credentials come from the control-plane role, never from a host.
type QueryAPIReader struct {
	region, accountID string
	credentials       aws.CredentialsProvider
	client            *http.Client
}

func NewQueryAPIReader(ctx context.Context, region, accountID string) (*QueryAPIReader, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return &QueryAPIReader{region: region, accountID: accountID, credentials: cfg.Credentials, client: http.DefaultClient}, nil
}

func (r *QueryAPIReader) ReadInstance(ctx context.Context, region, instanceID string) (Instance, error) {
	if r == nil || r.credentials == nil || region != r.region {
		return Instance{}, fmt.Errorf("invalid AWS instance reader region")
	}
	ec2, err := r.query(ctx, "ec2", "DescribeInstances", "2016-11-15", url.Values{"InstanceId.1": {instanceID}})
	if err != nil {
		return Instance{}, err
	}
	var ec2Result struct {
		Reservations []struct {
			Instances []struct {
				ID      string `xml:"instanceId"`
				ImageID string `xml:"imageId"`
				Tags    []struct {
					Key   string `xml:"key"`
					Value string `xml:"value"`
				} `xml:"tagSet>item"`
			} `xml:"instancesSet>item"`
		} `xml:"reservationSet>item"`
	}
	if err := xml.Unmarshal(ec2, &ec2Result); err != nil {
		return Instance{}, fmt.Errorf("decode EC2 instance: %w", err)
	}
	if len(ec2Result.Reservations) != 1 || len(ec2Result.Reservations[0].Instances) != 1 {
		return Instance{}, fmt.Errorf("EC2 instance %q was not uniquely found", instanceID)
	}
	instance := ec2Result.Reservations[0].Instances[0]
	if instance.ID != instanceID || instance.ImageID == "" {
		return Instance{}, fmt.Errorf("EC2 instance response is incomplete")
	}
	asg, err := r.query(ctx, "autoscaling", "DescribeAutoScalingInstances", "2011-01-01", url.Values{"InstanceIds.member.1": {instanceID}})
	if err != nil {
		return Instance{}, err
	}
	var asgResult struct {
		Instances []struct {
			Group          string `xml:"autoScalingGroupName"`
			LaunchTemplate struct {
				ID string `xml:"launchTemplateId"`
			} `xml:"launchTemplate"`
		} `xml:"DescribeAutoScalingInstancesResult>AutoScalingInstances>member"`
	}
	if err := xml.Unmarshal(asg, &asgResult); err != nil {
		return Instance{}, fmt.Errorf("decode Auto Scaling instance: %w", err)
	}
	if len(asgResult.Instances) != 1 || asgResult.Instances[0].Group == "" || asgResult.Instances[0].LaunchTemplate.ID == "" {
		return Instance{}, fmt.Errorf("EC2 instance is not in a launch-template Auto Scaling group")
	}
	tags := make(map[string]string, len(instance.Tags))
	for _, tag := range instance.Tags {
		tags[tag.Key] = tag.Value
	}
	return Instance{InstanceID: instanceID, AccountID: r.accountID, Region: region, AMIID: instance.ImageID, AutoScalingGroup: asgResult.Instances[0].Group, LaunchTemplateID: asgResult.Instances[0].LaunchTemplate.ID, Tags: tags}, nil
}

func (r *QueryAPIReader) query(ctx context.Context, service, action, version string, values url.Values) ([]byte, error) {
	endpoint := "https://" + service + "." + r.region + ".amazonaws.com/"
	values.Set("Action", action)
	values.Set("Version", version)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	credentials, err := r.credentials.Retrieve(ctx)
	if err != nil {
		return nil, err
	}
	payload := sha256.Sum256([]byte(values.Encode()))
	if err := v4.NewSigner().SignHTTP(ctx, credentials, request, hex.EncodeToString(payload[:]), service, r.region, time.Now()); err != nil {
		return nil, err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AWS %s returned %s", action, response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 1<<20))
}
