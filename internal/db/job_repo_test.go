package db

import "testing"

func TestCreateAndGetJob(t *testing.T) {
	d := setupTestDB(t)
	// Need a storage destination first
	destID, _ := d.CreateStorageDestination(StorageDestination{Name: "test", Type: "local", Config: "{}"})

	job := Job{
		Name: "daily-backup", Description: "Daily container backup",
		Enabled: true, Schedule: "0 2 * * *", BackupTypeChain: "full",
		RetentionCount: 7, RetentionDays: 30, Compression: "zstd",
		ContainerMode: "one_by_one", NotifyOn: "failure", StorageDestID: destID,
	}
	id, err := d.CreateJob(job)
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	got, err := d.GetJob(id)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got.Name != "daily-backup" {
		t.Errorf("Name = %q, want %q", got.Name, "daily-backup")
	}
	if got.Schedule != "0 2 * * *" {
		t.Errorf("Schedule = %q, want %q", got.Schedule, "0 2 * * *")
	}
}

func TestListJobs(t *testing.T) {
	d := setupTestDB(t)
	destID, _ := d.CreateStorageDestination(StorageDestination{Name: "test", Type: "local", Config: "{}"})
	d.CreateJob(Job{Name: "job-a", StorageDestID: destID})
	d.CreateJob(Job{Name: "job-b", StorageDestID: destID})
	jobs, err := d.ListJobs()
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("got %d jobs, want 2", len(jobs))
	}
}

func TestJobItems(t *testing.T) {
	d := setupTestDB(t)
	destID, _ := d.CreateStorageDestination(StorageDestination{Name: "test", Type: "local", Config: "{}"})
	jobID, _ := d.CreateJob(Job{Name: "test-job", StorageDestID: destID})

	d.AddJobItem(JobItem{JobID: jobID, ItemType: "container", ItemName: "plex", ItemID: "abc123", Settings: "{}"})
	d.AddJobItem(JobItem{JobID: jobID, ItemType: "vm", ItemName: "windows", ItemID: "def456", Settings: `{"backup_mode":"cold"}`})

	items, err := d.GetJobItems(jobID)
	if err != nil {
		t.Fatalf("GetJobItems error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("got %d items, want 2", len(items))
	}
}

func TestJobRunsAndRestorePoints(t *testing.T) {
	d := setupTestDB(t)
	destID, _ := d.CreateStorageDestination(StorageDestination{Name: "test", Type: "local", Config: "{}"})
	jobID, _ := d.CreateJob(Job{Name: "test-job", StorageDestID: destID})

	runID, err := d.CreateJobRun(JobRun{JobID: jobID, Status: "running", BackupType: "full", ItemsTotal: 3})
	if err != nil {
		t.Fatalf("CreateJobRun error = %v", err)
	}

	d.UpdateJobRun(JobRun{ID: runID, Status: "success", ItemsDone: 3, SizeBytes: 1024})

	runs, err := d.GetJobRuns(jobID, 10)
	if err != nil {
		t.Fatalf("GetJobRuns error = %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("got %d runs, want 1", len(runs))
	}
	if runs[0].Status != "success" {
		t.Errorf("status = %q, want %q", runs[0].Status, "success")
	}

	_, err = d.CreateRestorePoint(RestorePoint{
		JobRunID: runID, JobID: jobID, BackupType: "full",
		StoragePath: "/backups/test-job/2026-02-28", Metadata: "{}", SizeBytes: 1024,
	})
	if err != nil {
		t.Fatalf("CreateRestorePoint error = %v", err)
	}

	rps, err := d.ListRestorePoints(jobID)
	if err != nil {
		t.Fatalf("ListRestorePoints error = %v", err)
	}
	if len(rps) != 1 {
		t.Errorf("got %d restore points, want 1", len(rps))
	}
}
