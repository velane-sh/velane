package resources

import "fmt"

type Limits struct {
	CPUQuotaMicros, CPUPeriodMicros, MemoryMaxBytes, PidsMax, StagingAvailableBytes int64
	SwapMaxBytes                                                                    int64
}
type Request struct{ CPUQuotaMicros, MemoryMaxBytes, PidsMax, StagingBytes int64 }

func ValidateAdmission(l Limits, r Request) error {
	if l.CPUPeriodMicros <= 0 || r.CPUQuotaMicros <= 0 || r.MemoryMaxBytes <= 0 || r.PidsMax <= 0 {
		return fmt.Errorf("invalid cgroup resource request")
	}
	if l.SwapMaxBytes != 0 {
		return fmt.Errorf("sandbox cgroup must disable swap")
	}
	if r.CPUQuotaMicros > l.CPUQuotaMicros || r.MemoryMaxBytes > l.MemoryMaxBytes || r.PidsMax > l.PidsMax || r.StagingBytes > l.StagingAvailableBytes {
		return fmt.Errorf("sandbox resource admission denied")
	}
	return nil
}
