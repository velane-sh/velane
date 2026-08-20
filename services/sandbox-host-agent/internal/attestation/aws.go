// Package attestation supplies provider-specific first-boot evidence.
package attestation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

const metadataBaseURL = "http://169.254.169.254/latest"

// AWSEvidence obtains IMDSv2 identity material and creates a nonce-bound,
// SigV4 GetCallerIdentity proof. The instance private keys never leave IMDS.
func AWSEvidence(ctx context.Context, region, nonce string) (string, string, string, error) {
	token, err := metadata(ctx, http.MethodPut, "/api/token", map[string]string{"X-aws-ec2-metadata-token-ttl-seconds": "60"})
	if err != nil {
		return "", "", "", err
	}
	headers := map[string]string{"X-aws-ec2-metadata-token": string(token)}
	document, err := metadata(ctx, http.MethodGet, "/dynamic/instance-identity/document", headers)
	if err != nil {
		return "", "", "", err
	}
	var identity struct {
		Region string `json:"region"`
	}
	if json.Unmarshal(document, &identity) != nil || identity.Region == "" {
		return "", "", "", fmt.Errorf("invalid EC2 identity document")
	}
	if region != "" && region != identity.Region {
		return "", "", "", fmt.Errorf("configured AWS region differs from IMDS")
	}
	region = identity.Region
	signature, err := metadata(ctx, http.MethodGet, "/dynamic/instance-identity/pkcs7", headers)
	if err != nil {
		return "", "", "", err
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return "", "", "", err
	}
	credentials, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return "", "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://sts."+region+".amazonaws.com/?Action=GetCallerIdentity&Version=2011-06-15", nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("X-Velane-Enrollment-Nonce", nonce)
	payload := sha256.Sum256(nil)
	if err := v4.NewSigner().SignHTTP(ctx, credentials, req, fmt.Sprintf("%x", payload), "sts", region, time.Now()); err != nil {
		return "", "", "", err
	}
	proof, err := json.Marshal(map[string]string{"method": req.Method, "url": req.URL.String(), "authorization": req.Header.Get("Authorization"), "security_token": req.Header.Get("X-Amz-Security-Token"), "amz_date": req.Header.Get("X-Amz-Date"), "nonce": nonce})
	if err != nil {
		return "", "", "", err
	}
	return string(document), string(signature), base64.StdEncoding.EncodeToString(proof), nil
}

func metadata(ctx context.Context, method, endpoint string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, metadataBaseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IMDS returned %s", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 128<<10))
}
