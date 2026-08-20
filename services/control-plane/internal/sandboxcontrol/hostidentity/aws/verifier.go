// Package aws validates EC2 instance-identity evidence without coupling the
// host protocol to AWS. Tests inject the EC2 reader; production wires the SDK.
package aws

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/hostidentity"
	"github.com/fullsailor/pkcs7"
)

type Instance struct {
	InstanceID, AccountID, Region, AMIID, AutoScalingGroup, LaunchTemplateID string
	Tags                                                                     map[string]string
}

// InstanceReader is the AWS seam. Its production implementation uses
// DescribeInstances and DescribeAutoScalingInstances; fakes keep unit tests
// entirely offline.
type InstanceReader interface {
	ReadInstance(context.Context, string, string) (Instance, error)
}

// STSProofValidator validates the nonce-bound SigV4 request with the AWS
// identity service. Separating it from document parsing makes cloud-free unit
// tests exact and prevents a syntactic Authorization header from being treated
// as an attestation.
type STSProofValidator interface {
	ValidateGetCallerIdentity(context.Context, SignedSTSRequest, string) error
}

type SignedSTSRequest struct {
	Method, URL, Authorization, SecurityToken, AmzDate, Nonce string
}

// HTTPSTSProofValidator replays only the signed regional GetCallerIdentity
// request. STS validates the SigV4 credential/signature itself; the returned
// role session must correspond to the attested EC2 instance. Tests inject a
// local transport and make no cloud calls.
type HTTPSTSProofValidator struct {
	AccountID, Region string
	Client            *http.Client
}

func (v HTTPSTSProofValidator) ValidateGetCallerIdentity(ctx context.Context, proof SignedSTSRequest, instanceID string) error {
	u, err := url.Parse(proof.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host != "sts."+v.Region+".amazonaws.com" || u.Path != "/" || u.Query().Get("Action") != "GetCallerIdentity" || u.Query().Get("Version") != "2011-06-15" {
		return fmt.Errorf("invalid regional STS endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", proof.Authorization)
	req.Header.Set("X-Amz-Security-Token", proof.SecurityToken)
	req.Header.Set("X-Amz-Date", proof.AmzDate)
	req.Header.Set("X-Velane-Enrollment-Nonce", proof.Nonce)
	client := v.Client
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("STS GetCallerIdentity returned %s", response.Status)
	}
	var result struct {
		Result struct {
			Account string `xml:"Account"`
			Arn     string `xml:"Arn"`
		} `xml:"GetCallerIdentityResult"`
	}
	if err := xml.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return fmt.Errorf("decode STS identity response: %w", err)
	}
	if result.Result.Account != v.AccountID || !strings.Contains(result.Result.Arn, instanceID) {
		return fmt.Errorf("STS caller identity is not the attested EC2 instance")
	}
	return nil
}

type ExpectedPool struct {
	PoolID, AccountID, Region, AutoScalingGroup, AMIID, LaunchTemplateID, LineageID, HostCompatibilityKey string
}

type Verifier struct {
	Reader       InstanceReader
	STSValidator STSProofValidator
	TrustedRoots *x509.CertPool
	Expected     ExpectedPool
	Now          func() time.Time
}

type identityDocument struct {
	InstanceID  string    `json:"instanceId"`
	AccountID   string    `json:"accountId"`
	Region      string    `json:"region"`
	ImageID     string    `json:"imageId"`
	PendingTime time.Time `json:"pendingTime"`
}

func (v Verifier) Verify(ctx context.Context, proof hostidentity.EnrollmentProof) (hostidentity.VerifiedHost, error) {
	if proof.Provider != "aws" || proof.PoolID != v.Expected.PoolID || proof.Nonce == "" || proof.CSR == "" || proof.IdentityDocument == "" || proof.IdentitySignature == "" || proof.STSSignedRequest == "" {
		return hostidentity.VerifiedHost{}, fmt.Errorf("incomplete AWS enrollment evidence")
	}
	document, err := parseSignedDocument(proof.IdentityDocument, proof.IdentitySignature, v.TrustedRoots)
	if err != nil {
		return hostidentity.VerifiedHost{}, err
	}
	if document.InstanceID == "" || document.AccountID != v.Expected.AccountID || document.Region != v.Expected.Region || document.ImageID != v.Expected.AMIID {
		return hostidentity.VerifiedHost{}, fmt.Errorf("EC2 identity document does not match expected account, region, or AMI")
	}
	if v.Now == nil {
		v.Now = time.Now
	}
	if document.PendingTime.IsZero() || document.PendingTime.After(v.Now().Add(5*time.Minute)) {
		return hostidentity.VerifiedHost{}, fmt.Errorf("EC2 identity document has an invalid pending time")
	}
	stsProof, err := parseSTSProof(proof.STSSignedRequest, proof.Nonce, v.Expected.Region, v.Now())
	if err != nil {
		return hostidentity.VerifiedHost{}, err
	}
	if v.STSValidator == nil {
		return hostidentity.VerifiedHost{}, fmt.Errorf("AWS STS proof validator is required")
	}
	if err := v.STSValidator.ValidateGetCallerIdentity(ctx, stsProof, document.InstanceID); err != nil {
		return hostidentity.VerifiedHost{}, fmt.Errorf("validate AWS STS proof: %w", err)
	}
	if v.Reader == nil {
		return hostidentity.VerifiedHost{}, fmt.Errorf("AWS instance reader is required")
	}
	instance, err := v.Reader.ReadInstance(ctx, document.Region, document.InstanceID)
	if err != nil {
		return hostidentity.VerifiedHost{}, fmt.Errorf("read EC2 instance: %w", err)
	}
	if instance.InstanceID != document.InstanceID || instance.AccountID != document.AccountID || instance.Region != document.Region || instance.AMIID != document.ImageID || instance.AutoScalingGroup != v.Expected.AutoScalingGroup || instance.LaunchTemplateID != v.Expected.LaunchTemplateID || instance.Tags["VelaneSandboxHost"] != "true" || instance.Tags["VelaneHostLineage"] != v.Expected.LineageID || instance.Tags["VelaneHostCompatibilityKey"] != v.Expected.HostCompatibilityKey {
		return hostidentity.VerifiedHost{}, fmt.Errorf("EC2 instance does not match attested sandbox-host pool")
	}
	return hostidentity.VerifiedHost{PoolID: v.Expected.PoolID, ProviderInstanceID: instance.InstanceID, AccountID: instance.AccountID, Region: instance.Region, AMIID: instance.AMIID, AutoScalingGroup: instance.AutoScalingGroup, LaunchTemplateID: instance.LaunchTemplateID, LineageID: v.Expected.LineageID, HostCompatibilityKey: v.Expected.HostCompatibilityKey}, nil
}

func parseSignedDocument(document, signature string, trustedRoots *x509.CertPool) (identityDocument, error) {
	der, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return identityDocument{}, fmt.Errorf("decode EC2 PKCS7 signature: %w", err)
	}
	p7, err := pkcs7.Parse(der)
	if err != nil {
		return identityDocument{}, fmt.Errorf("parse EC2 PKCS7 signature: %w", err)
	}
	if err := p7.Verify(); err != nil {
		return identityDocument{}, fmt.Errorf("verify EC2 PKCS7 signature: %w", err)
	}
	if trustedRoots == nil || len(p7.Certificates) == 0 {
		return identityDocument{}, fmt.Errorf("AWS EC2 PKCS7 trust roots are required")
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range p7.Certificates[1:] {
		intermediates.AddCert(certificate)
	}
	if _, err := p7.Certificates[0].Verify(x509.VerifyOptions{Roots: trustedRoots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return identityDocument{}, fmt.Errorf("verify EC2 PKCS7 certificate chain: %w", err)
	}
	if string(p7.Content) != document {
		return identityDocument{}, fmt.Errorf("EC2 PKCS7 content does not match identity document")
	}
	var result identityDocument
	if err := json.Unmarshal([]byte(document), &result); err != nil {
		return identityDocument{}, fmt.Errorf("decode EC2 identity document: %w", err)
	}
	return result, nil
}

// SigV4 verification is deliberately strict about what the challenge binds.
// An AWS SDK verifier can additionally validate the authorization signature
// with STS credentials; this rejects unbound/replayed request material before
// any cloud call and keeps the opaque proof provider-specific.
func parseSTSProof(encoded, nonce, region string, now time.Time) (SignedSTSRequest, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return SignedSTSRequest{}, fmt.Errorf("decode STS proof: %w", err)
	}
	var proof struct {
		Method        string `json:"method"`
		URL           string `json:"url"`
		Authorization string `json:"authorization"`
		SecurityToken string `json:"security_token"`
		AmzDate       string `json:"amz_date"`
		Nonce         string `json:"nonce"`
	}
	if err := json.Unmarshal(b, &proof); err != nil {
		return SignedSTSRequest{}, fmt.Errorf("decode STS proof: %w", err)
	}
	if proof.Method != "POST" || !strings.Contains(proof.URL, "sts."+region+".") || !strings.Contains(proof.URL, "Action=GetCallerIdentity") || !strings.Contains(proof.Authorization, "AWS4-HMAC-SHA256") || !strings.Contains(strings.ToLower(proof.Authorization), "x-velane-enrollment-nonce") || proof.SecurityToken == "" || proof.Nonce != nonce {
		return SignedSTSRequest{}, fmt.Errorf("STS proof is not a nonce-bound regional GetCallerIdentity request")
	}
	issuedAt, err := time.Parse("20060102T150405Z", proof.AmzDate)
	if err != nil || issuedAt.Before(now.Add(-5*time.Minute)) || issuedAt.After(now.Add(5*time.Minute)) {
		return SignedSTSRequest{}, fmt.Errorf("STS proof is expired")
	}
	return SignedSTSRequest{Method: proof.Method, URL: proof.URL, Authorization: proof.Authorization, SecurityToken: proof.SecurityToken, AmzDate: proof.AmzDate, Nonce: proof.Nonce}, nil
}
