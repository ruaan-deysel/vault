package notify

import "testing"

func TestSendOnNonLinux(t *testing.T) {
	// On macOS, this should just log and return nil
	err := Send("Vault", "Test", "Test notification", ImportanceNormal)
	if err != nil {
		t.Errorf("Send() error = %v", err)
	}
}

func TestJobSuccess(t *testing.T) {
	err := JobSuccess("test-job", 5, 1073741824)
	if err != nil {
		t.Errorf("JobSuccess() error = %v", err)
	}
}

func TestJobFailed(t *testing.T) {
	err := JobFailed("test-job", "disk full")
	if err != nil {
		t.Errorf("JobFailed() error = %v", err)
	}
}
